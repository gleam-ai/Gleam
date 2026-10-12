package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/pkg/types"
)

type Executor struct {
	Reg            *registry.Registry
	Gate           *safety.Gate
	Notifier       Notifier
	MaxConcurrency int
	StepTimeout    time.Duration
	StepRetries    int
	Dedupe         bool
	MaxOutputRunes int
	DataDir        string
	Reviewer       Reviewer
	Goal           string
	OnUsage        func(taskID string, toolCalls, retries, deduped int)
	Carried        map[string]types.StepResult
	Sink           StepSink
}

type StepSink interface {
	RecordStep(taskID string, r types.StepResult)
}

type Reviewer interface {
	Review(userGoal, tool string, args map[string]any) (approved bool, reason string)
}

func (e *Executor) reportUsage(taskID string, toolCalls, retries, deduped int) {
	if e == nil || e.OnUsage == nil {
		return
	}
	e.OnUsage(taskID, toolCalls, retries, deduped)
}

func (e *Executor) readOnly(tool types.Tool) bool {
	if tool == nil {
		return false
	}
	if e.Gate == nil {
		return tool.Permission() == types.PermissionReadOnly
	}
	return e.Gate.EffectivePermission(tool) == types.PermissionReadOnly
}

func (e *Executor) capOutput(v any, taskID, spillID string) any {
	if e.MaxOutputRunes <= 0 {
		return v
	}
	if s, ok := v.(string); ok {
		return e.fitRunes(s, taskID, spillID)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	if len([]rune(string(b))) <= e.MaxOutputRunes {
		return v
	}
	return e.fitRunes(string(b), taskID, spillID)
}

const spillMinBudget = 160

const truncNoteMinBody = 80

func (e *Executor) fitRunes(s, taskID, spillID string) string {
	r := []rune(s)
	budget := e.MaxOutputRunes
	if len(r) <= budget {
		return s
	}
	if budget < spillMinBudget {
		return string(r[:budget])
	}
	note := e.truncNote(len(r), taskID, spillID, s)
	body := budget - len([]rune(note))
	if body < truncNoteMinBody {
		return string(r[:budget])
	}
	head := body * 3 / 5
	tail := body - head
	return string(r[:head]) + note + string(r[len(r)-tail:])
}

func (e *Executor) truncNote(total int, taskID, spillID, content string) string {
	if p, err := SpillOutput(e.DataDir, taskID, spillID, content); err == nil {
		return fmt.Sprintf("\n…[已截断：原始 %d 字。完整内容见 %s（临时文件，任务结束后可能被清理）]…\n", total, p)
	} else {
		return fmt.Sprintf("\n…[已截断：原始 %d 字。完整内容未能落盘（%v），请缩小读取范围或用 file.read 按路径精确取用]…\n", total, err)
	}
}

func stepFingerprint(tool string, args map[string]any) string {
	b, err := json.Marshal(args)
	if err != nil {
		return tool + "|<?>"
	}
	return tool + "|" + string(b)
}

type Result struct {
	Steps        []types.StepResult
	ByID         map[string]*types.StepResult
	ExecutedPlan types.Plan
	ReplyText    string
	Succeeded    int
	Failed       int
	Skipped      int
	Empty        int
	Total        int
	Carried      int
	Cancelled    bool
	PreImages    []preImage
}

type cachedCall struct {
	out     any
	outcome types.StepOutcome
	note    string
}

type execState struct {
	plan     types.Plan
	results  []types.StepResult
	finished []chan struct{}
	idxOf    map[string]int

	mu       sync.Mutex
	done     map[string]cachedCall
	failed   map[string]string
	inflight map[string]chan struct{}

	snap *snapshotter
}

func (s *execState) tryAcquire(fp string) (chan struct{}, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.inflight[fp]; ok {
		return ch, false
	}
	ch := make(chan struct{})
	s.inflight[fp] = ch
	return ch, true
}

func (s *execState) release(fp string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.inflight[fp]; ok {
		close(ch)
		delete(s.inflight, fp)
	}
}

func (s *execState) cacheLoad(fp string) (cachedCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.done[fp]
	return v, ok
}

func (s *execState) cacheStore(fp string, out any, outcome types.StepOutcome, note string) {
	s.mu.Lock()
	s.done[fp] = cachedCall{out: out, outcome: outcome, note: note}
	s.mu.Unlock()
}

func (s *execState) failedLoad(fp string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.failed[fp]
	return v, ok
}

func (s *execState) failedStore(fp, errMsg string) {
	s.mu.Lock()
	s.failed[fp] = errMsg
	s.mu.Unlock()
}

func (s *execState) resultOf(id string) (*types.StepResult, bool) {
	i, ok := s.idxOf[id]
	if !ok {
		return nil, false
	}
	<-s.finished[i]
	return &s.results[i], true
}

func (e *Executor) record(taskID string, r types.StepResult) {
	if e.Sink == nil {
		return
	}
	e.Sink.RecordStep(taskID, r)
}

func (e *Executor) Execute(ctx context.Context, plan types.Plan, taskID, mode string, preApproved bool) *Result {
	res := &Result{Total: len(plan.Steps), ByID: map[string]*types.StepResult{}}
	if res.Total == 0 {
		return res
	}
	res.ExecutedPlan = types.Plan{Goal: plan.Goal, EstimatedTime: plan.EstimatedTime}

	state := &execState{
		plan:     plan,
		results:  make([]types.StepResult, res.Total),
		finished: make([]chan struct{}, res.Total),
		idxOf:    map[string]int{},
		done:     map[string]cachedCall{},
		failed:   map[string]string{},
		inflight: map[string]chan struct{}{},
		snap:     newSnapshotter(e.DataDir, taskID),
	}
	for i := range state.finished {
		state.finished[i] = make(chan struct{})
	}
	for i, st := range plan.Steps {
		state.idxOf[st.ID] = i
	}

	carried := 0
	for i, st := range plan.Steps {
		prev, ok := e.Carried[st.ID]
		if !ok {
			continue
		}
		prev.StepID = st.ID
		prev.Tool = st.Tool
		prev.Carried = true
		state.results[i] = prev
		close(state.finished[i])
		e.record(taskID, state.results[i])
		carried++
	}

	conc := e.MaxConcurrency
	if conc <= 0 {
		conc = 8
	}
	sem := make(chan struct{}, conc)
	timeout := e.StepTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	var doneCount atomic.Int64
	doneCount.Store(int64(carried))
	var wg sync.WaitGroup
	total := int64(res.Total)
	notify := func(ev types.ProgressEvent) {
		if e.Notifier != nil {
			e.Notifier.OnProgress(ev)
		}
	}
	if carried > 0 {
		notify(progress(taskID, "execute",
			fmt.Sprintf("开始执行 %d 个步骤（前 %d 步沿用上次结果，不会重新执行）", total, carried), 10, "info"))
	} else {
		notify(progress(taskID, "execute", fmt.Sprintf("开始执行 %d 个步骤", total), 10, "info"))
	}

	for i := range plan.Steps {
		if _, ok := e.Carried[plan.Steps[i].ID]; ok {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			step := plan.Steps[i]

			var depFailed []string
			for _, dep := range step.DependsOn {
				if _, exists := state.idxOf[dep]; !exists {
					continue
				}
				dr, _ := state.resultOf(dep)
				if dr == nil || dr.Status != types.StepSucceeded {
					depFailed = append(depFailed, dep)
				}
			}
			if len(depFailed) > 0 {
				state.results[i] = types.StepResult{
					StepID: step.ID, Tool: step.Tool, Status: types.StepSkipped,
					Error:     fmt.Sprintf("依赖步骤未成功: %s", strings.Join(depFailed, ", ")),
					StartedAt: time.Now(), FinishedAt: time.Now(),
				}
				close(state.finished[i])
				e.record(taskID, state.results[i])
				n := doneCount.Add(1)
				notify(progress(taskID, "execute", fmt.Sprintf("⏭ 跳过 %s（依赖未满足）", step.ID), progressPct(n, total, 10, 80), "warn"))
				e.emitTool(types.ToolEvent{
					TaskID: taskID, StepID: step.ID, Tool: step.Tool,
					Phase: types.ToolEnd, Title: step.Describe(),
					Status: types.ToolSkipped,
					Error:  fmt.Sprintf("依赖步骤未成功：%s", strings.Join(depFailed, ", ")),
				})
				return
			}
			if ctx.Err() != nil {
				state.results[i] = types.StepResult{
					StepID: step.ID, Tool: step.Tool, Status: types.StepSkipped,
					Error: "任务已取消", StartedAt: time.Now(), FinishedAt: time.Now(),
				}
				close(state.finished[i])
				e.record(taskID, state.results[i])
				n := doneCount.Add(1)
				notify(progress(taskID, "execute", fmt.Sprintf("⏭ 跳过 %s（任务已取消）", step.ID), progressPct(n, total, 10, 80), "warn"))
				e.emitTool(types.ToolEvent{
					TaskID: taskID, StepID: step.ID, Tool: step.Tool,
					Phase: types.ToolEnd, Title: step.Describe(),
					Status: types.ToolSkipped, Error: "任务已取消",
				})
				return
			}

			state.results[i] = e.runStepGuarded(ctx, state, step, taskID, mode, preApproved, timeout, sem)
			close(state.finished[i])
			e.record(taskID, state.results[i])

			n := doneCount.Add(1)
			r := state.results[i]
			icon, kind := "✅", "info"
			if r.Status == types.StepFailed {
				icon, kind = "❌", "error"
			}
			notify(progress(taskID, "execute",
				fmt.Sprintf("%s %s %s（%d/%d）", icon, step.ID, step.Describe(), n, total),
				progressPct(n, total, 10, 80), kind))
		}(i)
	}
	wg.Wait()

	for i := range state.results {
		r := state.results[i]
		res.Steps = append(res.Steps, r)
		res.ByID[r.StepID] = &res.Steps[i]
		if r.Carried {
			res.Carried++
		}
		switch {
		case r.Status == types.StepSucceeded && r.Outcome == types.OutcomeFailed:
			res.Failed++
		case r.Status == types.StepSucceeded:
			res.Succeeded++
			if r.Outcome == types.OutcomeEmpty {
				res.Empty++
			}
			if r.Tool == "reply" && res.ReplyText == "" {
				if m, ok := r.Output.(map[string]any); ok {
					if text, ok := m["text"].(string); ok {
						res.ReplyText = text
					}
				}
			}
		case r.Status == types.StepFailed:
			res.Failed++
		default:
			res.Skipped++
		}
		exec := types.Step{ID: r.StepID, Tool: r.Tool, DependsOn: plan.Steps[i].DependsOn, Description: plan.Steps[i].Description}
		if r.FinalArgs != nil {
			exec.Args = r.FinalArgs
		} else {
			exec.Args = plan.Steps[i].Args
		}
		res.ExecutedPlan.Steps = append(res.ExecutedPlan.Steps, exec)
	}
	if ctx.Err() != nil {
		res.Cancelled = true
	} else {
		for _, r := range res.Steps {
			if r.Status == types.StepSkipped && strings.Contains(r.Error, "取消") {
				res.Cancelled = true
				break
			}
		}
	}
	PruneSpill(e.DataDir, spillMaxTasks)
	PruneSnapshots(e.DataDir, snapshotMaxTasks)
	res.PreImages = state.snap.records()
	return res
}

func (e *Executor) runStepGuarded(ctx context.Context, state *execState, step types.Step, taskID, mode string, preApproved bool, timeout time.Duration, sem chan struct{}) (r types.StepResult) {
	started := time.Now()
	defer func() {
		p := recover()
		if p == nil {
			return
		}
		r = types.StepResult{
			StepID:     step.ID,
			Tool:       step.Tool,
			Status:     types.StepFailed,
			ErrorKind:  types.ErrInternal,
			Error:      panicBrief(p),
			StartedAt:  started,
			FinishedAt: time.Now(),
		}
		r.DurationMS = r.FinishedAt.Sub(started).Milliseconds()
		e.emitTool(toolEndEvent(taskID, step, r))
	}()
	return e.runStep(ctx, state, step, taskID, mode, preApproved, timeout, sem)
}

func panicBrief(p any) string {
	lines := strings.Split(string(debug.Stack()), "\n")
	if len(lines) > 7 {
		lines = lines[:7]
	}
	return fmt.Sprintf("工具 panic: %v\n%s", p, strings.TrimRight(strings.Join(lines, "\n"), "\n"))
}

func (e *Executor) adjudicate(ctx context.Context, tool types.Tool, args map[string]any, preApproved, review bool) safety.Decision {
	dec := e.Gate.EvaluateStepIn(ctx, tool, args, preApproved)
	if !review || dec.NeedApproval || dec.Risk == "low" || e.Reviewer == nil {
		return dec
	}
	if ok, why := e.Reviewer.Review(e.Goal, tool.Name(), args); !ok {
		if e.Gate != nil {
			e.Gate.Record(safety.AuditEntry{
				Tool: tool.Name(), Risk: dec.Risk, Action: "reviewed_block",
				Reason: "审核模型标记，已升级为人工确认", Detail: why,
			})
		}
		dec.NeedApproval = true
		dec.Reason = "审核模型认为这个动作超出了目标范围：" + why
	}
	return dec
}

func (e *Executor) awaitApproval(ctx context.Context, r *types.StepResult, taskID string, step types.Step, dec safety.Decision) (approved, cancelled bool) {
	req := types.ApprovalRequest{
		TaskID: taskID,
		Plan:   []string{step.Describe()},
		Risk:   dec.Risk,
		Reason: dec.Reason,
		StepID: step.ID,
	}
	if e.Gate != nil {
		e.Gate.MarkPending(safety.PendingApproval{
			TaskID: taskID, StepID: step.ID, Tool: step.Tool,
			Risk: dec.Risk, Reason: dec.Reason,
		})
	}
	clearPending := func() {
		if e.Gate != nil {
			e.Gate.ClearPending(taskID, step.ID)
		}
	}
	respCh := make(chan types.ApprovalResponse, 1)
	if e.Notifier != nil {
		go func() { respCh <- e.Notifier.OnApproval(req) }()
	} else {
		respCh <- types.ApprovalResponse{Approved: false, Note: "无审批通道"}
	}
	actx, acancel := context.WithTimeout(ctx, e.Gate.ApprovalTimeoutDuration())
	var note string
	select {
	case resp := <-respCh:
		approved, note = resp.Approved, resp.Note
	case <-actx.Done():
		if ctx.Err() != nil {
			r.Status = types.StepSkipped
			r.Error = "任务已取消"
			acancel()
			clearPending()
			return false, true
		}
		note = "审批超时，自动拒绝"
	}
	acancel()
	clearPending()
	if e.Gate != nil {
		action := "approved"
		if !approved {
			action = "denied"
		}
		e.Gate.Record(safety.AuditEntry{
			Tool: step.Tool, Risk: dec.Risk, Action: action,
			Reason: dec.Reason, Detail: note,
		})
	}
	if !approved {
		r.Status = types.StepFailed
		r.Error = types.ErrStepRejected.Error()
		if note != "" {
			r.Error += "（" + note + "）"
		}
		r.ErrorKind = types.ErrPermission
	}
	return approved, false
}

func (e *Executor) runStep(ctx context.Context, state *execState, step types.Step, taskID, mode string, preApproved bool, timeout time.Duration, sem chan struct{}) types.StepResult {
	start := time.Now()
	r := types.StepResult{StepID: step.ID, Tool: step.Tool, Status: types.StepRunning, StartedAt: start}
	dispatched := false
	finalize := func() types.StepResult {
		r.FinishedAt = time.Now()
		r.DurationMS = time.Since(start).Milliseconds()
		if dispatched {
			e.emitTool(toolEndEvent(taskID, step, r))
		}
		return r
	}

	tool, ok := e.Reg.Get(step.Tool)
	if !ok {
		r.Status = types.StepFailed
		r.Error = fmt.Sprintf("工具 %q 不存在", step.Tool)
		r.ErrorKind = types.ErrNotFound
		return finalize()
	}

	dec := e.adjudicate(ctx, tool, step.Args, preApproved, !hasRefs(step.Args))
	if dec.NeedApproval {
		if approved, cancelled := e.awaitApproval(ctx, &r, taskID, step, dec); cancelled || !approved {
			return finalize()
		}
	}

	args, err := substituteArgs(step.Args, func(ref string) (any, bool, error) {
		id, path := splitRef(ref)
		dep, ok := state.resultOf(id)
		if !ok {
			return nil, false, fmt.Errorf("引用的步骤 %q 不存在", id)
		}
		if dep.Status != types.StepSucceeded {
			return nil, false, fmt.Errorf("引用的步骤 %q 未成功执行", id)
		}
		v, ok := navigate(dep.Output, path)
		if !ok {
			return nil, false, fmt.Errorf("步骤 %q 的结果中不存在字段 %q", id, path)
		}
		return e.capOutput(v, taskID, id), true, nil
	})
	if err != nil {
		r.Status = types.StepFailed
		r.Error = err.Error()
		r.ErrorKind = types.ErrParam
		return finalize()
	}
	r.FinalArgs = args

	final := dec
	if hasRefs(step.Args) {
		final = e.adjudicate(ctx, tool, args, preApproved, true)
		if final.NeedApproval && !dec.NeedApproval {
			if approved, cancelled := e.awaitApproval(ctx, &r, taskID, step, final); cancelled || !approved {
				return finalize()
			}
		}
	}
	if !dec.NeedApproval && !final.NeedApproval && final.Risk != "low" && e.Gate != nil {
		e.Gate.Record(safety.AuditEntry{
			Tool: step.Tool, Risk: final.Risk, Action: "auto", Reason: final.Reason,
		})
	}

	dedupable := e.Dedupe && step.Tool != "reply"
	var fp string
	if dedupable {
		fp = stepFingerprint(step.Tool, args)
		if hit, ok := state.cacheLoad(fp); ok {
			r.Status = types.StepSucceeded
			r.Output = hit.out
			r.Outcome = hit.outcome
			r.Error = hit.note
			r.Deduped = true
			e.reportUsage(taskID, 0, 0, 1)
			if e.Notifier != nil {
				e.Notifier.OnProgress(progress(taskID, "execute",
					fmt.Sprintf("♻️ %s 复用前序相同调用，跳过重复执行", step.ID), 0, "info"))
			}
			return finalize()
		}
		if errMsg, ok := state.failedLoad(fp); ok {
			r.Status = types.StepFailed
			r.Error = fmt.Sprintf("与已失败的调用完全相同，已跳过（原因：%s）", types.Shorten(errMsg, 120))
			r.ErrorKind = types.ClassifyError(errMsg)
			r.Deduped = true
			e.reportUsage(taskID, 0, 0, 1)
			if e.Notifier != nil {
				e.Notifier.OnProgress(progress(taskID, "execute",
					fmt.Sprintf("⏭ %s 跳过重复的失败调用（%s）", step.ID, step.Describe()), 0, "warn"))
			}
			return finalize()
		}
		if ch, mine := state.tryAcquire(fp); !mine {
			select {
			case <-ch:
			case <-ctx.Done():
			}
			if hit, ok := state.cacheLoad(fp); ok {
				r.Status = types.StepSucceeded
				r.Output = hit.out
				r.Outcome = hit.outcome
				r.Error = hit.note
				r.Deduped = true
				e.reportUsage(taskID, 0, 0, 1)
				return finalize()
			}
			if errMsg, ok := state.failedLoad(fp); ok {
				r.Status = types.StepFailed
				r.Error = fmt.Sprintf("与已失败的调用完全相同，已跳过（原因：%s）", types.Shorten(errMsg, 120))
				r.ErrorKind = types.ClassifyError(errMsg)
				r.Deduped = true
				e.reportUsage(taskID, 0, 0, 1)
				return finalize()
			}
		} else {
			_ = ch
			defer state.release(fp)
		}
	}

	if tool.Permission() != types.PermissionReadOnly {
		state.snap.capture(ctx, step.ID, step.Tool, tool, args)
	}

	dispatched = true
	e.emitTool(types.ToolEvent{
		TaskID:    taskID,
		StepID:    step.ID,
		Tool:      step.Tool,
		Phase:     types.ToolStart,
		Title:     step.Describe(),
		Detail:    toolDetail(args),
		Status:    "running",
		StartedAt: start,
	})

	sem <- struct{}{}
	defer func() { <-sem }()
	attempts := 1 + e.StepRetries
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; ; attempt++ {
		e.reportUsage(taskID, 1, 0, 0)
		tctx, cancel := context.WithTimeout(ctx, timeout)
		out, err := tool.Execute(tctx, args)
		timedOut := tctx.Err() != nil && ctx.Err() == nil
		cancel()
		if ctx.Err() != nil {
			r.Status = types.StepSkipped
			r.Error = "任务已取消"
			return finalize()
		}
		if err == nil && !timedOut {
			r.Status = types.StepSucceeded
			r.Output = out
			r.Outcome = types.OutcomeOK
			if rep, ok := tool.(types.OutcomeReporter); ok {
				if oc, note := rep.Outcome(args, out); oc != "" {
					r.Outcome = oc
					r.Error = note
					if oc == types.OutcomeFailed {
						r.ErrorKind = types.ClassifyError(note)
					}
				}
			}
			if dedupable && e.readOnly(tool) {
				state.cacheStore(fp, out, r.Outcome, r.Error)
			}
			r.Attempt = attempt
			r.Retried = attempt > 1
			return finalize()
		}
		r.Status = types.StepFailed
		r.Outcome = types.OutcomeFailed
		if err != nil {
			if timedOut {
				r.Error = fmt.Sprintf("步骤超时（%v）: %v", timeout, err)
				r.ErrorKind = types.ErrTimeout
			} else {
				r.Error = err.Error()
				r.ErrorKind = types.ClassifyError(r.Error)
				if dedupable {
					state.failedStore(fp, r.Error)
				}
				r.Attempt = attempt
				r.Retried = attempt > 1
				return finalize()
			}
		} else {
			r.Error = fmt.Sprintf("步骤超时（%v）", timeout)
			r.ErrorKind = types.ErrTimeout
		}
		if attempt >= attempts {
			if dedupable {
				state.failedStore(fp, r.Error)
			}
			r.Attempt = attempt
			r.Retried = attempt > 1
			return finalize()
		}
		e.reportUsage(taskID, 0, 1, 0)
		if e.Notifier != nil {
			e.Notifier.OnProgress(progress(taskID, "execute",
				fmt.Sprintf("⏳ %s 超时，自动重试（第 %d/%d 次）", step.ID, attempt, attempts-1), 0, "warn"))
		}
	}
}

func substituteArgs(args map[string]any, resolve func(ref string) (any, bool, error)) (map[string]any, error) {
	if len(args) == 0 {
		return args, nil
	}
	return substMap(args, resolve)
}

func substMap(m map[string]any, resolve func(ref string) (any, bool, error)) (map[string]any, error) {
	out := make(map[string]any, len(m))
	for k, v := range m {
		sv, err := substValue(v, resolve)
		if err != nil {
			return nil, err
		}
		out[k] = sv
	}
	return out, nil
}

func substValue(v any, resolve func(ref string) (any, bool, error)) (any, error) {
	switch t := v.(type) {
	case string:
		if ref, ok := exactRef(t); ok {
			v, _, err := resolve(ref)
			return v, err
		}
		if strings.Contains(t, "{ref:") {
			return interpRef(t, resolve)
		}
		return t, nil
	case map[string]any:
		return substMap(t, resolve)
	case []any:
		arr := make([]any, 0, len(t))
		for _, item := range t {
			sv, err := substValue(item, resolve)
			if err != nil {
				return nil, err
			}
			arr = append(arr, sv)
		}
		return arr, nil
	default:
		return t, nil
	}
}

func exactRef(s string) (string, bool) {
	if strings.HasPrefix(s, "$ref:") && len(s) > len("$ref:") {
		return s[len("$ref:"):], true
	}
	return "", false
}

func hasRefs(v any) bool {
	switch t := v.(type) {
	case string:
		if _, ok := exactRef(t); ok {
			return true
		}
		return strings.Contains(t, "{ref:")
	case map[string]any:
		for _, item := range t {
			if hasRefs(item) {
				return true
			}
		}
	case []any:
		for _, item := range t {
			if hasRefs(item) {
				return true
			}
		}
	}
	return false
}

func splitRef(ref string) (id, path string) {
	if i := strings.Index(ref, "."); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}

func navigate(v any, path string) (any, bool) {
	if path == "" {
		return v, true
	}
	parts := strings.Split(path, ".")
	for _, p := range parts {
		switch t := v.(type) {
		case map[string]any:
			if nv, ok := t[p]; ok {
				v = nv
				continue
			}
			return nil, false
		case []any:
			idx := 0
			if _, err := fmt.Sscanf(p, "%d", &idx); err != nil || idx < 0 || idx >= len(t) {
				return nil, false
			}
			v = t[idx]
			continue
		default:
			return nil, false
		}
	}
	return v, true
}

func interpRef(s string, resolve func(ref string) (any, bool, error)) (any, error) {
	var b strings.Builder
	for {
		start := strings.Index(s, "{ref:")
		if start < 0 {
			b.WriteString(s)
			break
		}
		end := strings.Index(s[start:], "}")
		if end < 0 {
			b.WriteString(s)
			break
		}
		end += start
		b.WriteString(s[:start])
		ref := s[start+len("{ref:") : end]
		v, _, err := resolve(ref)
		if err != nil {
			return nil, err
		}
		b.WriteString(stringify(v))
		s = s[end+1:]
	}
	return b.String(), nil
}

func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func progress(taskID, phase, msg string, pct int, kind string) types.ProgressEvent {
	return types.ProgressEvent{TaskID: taskID, Phase: phase, Message: msg, Progress: pct, Kind: kind}
}

func progressPct(done, total int64, base, span int) int {
	if total == 0 {
		return base
	}
	return base + int(int64(span)*done/total)
}

func (e *Executor) emitTool(ev types.ToolEvent) {
	if e.Notifier == nil {
		return
	}
	if tn, ok := toolSink(e.Notifier); ok {
		tn.OnTool(ev)
	}
}

func toolEndEvent(taskID string, step types.Step, r types.StepResult) types.ToolEvent {
	ev := types.ToolEvent{
		TaskID:     taskID,
		StepID:     step.ID,
		Tool:       step.Tool,
		Phase:      types.ToolEnd,
		Title:      step.Describe(),
		Status:     types.ToolDone,
		StartedAt:  r.StartedAt,
		DurationMS: r.DurationMS,
	}
	switch {
	case r.Status == types.StepSkipped:
		ev.Status = types.ToolSkipped
	case r.Status == types.StepFailed:
		ev.Status = types.ToolFailed
	}
	ev.Error = types.Shorten(oneLine(r.Error), 160)
	return ev
}

var toolDetailKeys = []string{"path", "file", "file_path", "dir", "command", "cmd", "query", "pattern", "url", "name"}

func toolDetail(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	for _, k := range toolDetailKeys {
		v, ok := args[k]
		if !ok {
			continue
		}
		if s := oneLine(scalarText(v)); s != "" {
			return types.Shorten(s, 120)
		}
	}
	return ""
}

func scalarText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool, int, int64, float64:
		return fmt.Sprint(x)
	default:
		return ""
	}
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
