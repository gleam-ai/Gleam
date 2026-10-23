package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gleam/internal/agent/geo"
	"gleam/internal/harness/conversation"
	"gleam/internal/harness/growth"
	"gleam/internal/harness/memory"
	"gleam/internal/harness/skill"
	"gleam/internal/llm"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

var slugStrip = regexp.MustCompile(`[^a-zA-Z0-9_\-\p{Han}]+`)

// RunGoal runs one goal synchronously; callers may wrap it in a goroutine.
// A non-empty req.TaskID is reused.
func (a *Agent) RunGoal(ctx context.Context, req types.GoalRequest) *types.GoalResult {
	taskID := req.TaskID
	if taskID == "" {
		taskID = types.NewID()
	}
	req.TaskID = taskID
	goal := strings.TrimSpace(req.Goal)
	mode := normalizeMode(req.Mode)

	// Retry detection (P3-4 / site H15): a similar goal resubmitted in the same
	// conversation within a short window means the user is retrying.
	// Must run before this user message is appended to the conversation,
	// otherwise it detects the message it just wrote.
	retried := req.ConversationID != "" && a.Convos != nil && a.Convos.DetectRetry(req.ConversationID, goal)

	handle := &taskHandle{id: taskID, goal: goal, mode: mode, status: types.GoalRunning, started: time.Now()}
	a.mu.Lock()
	// A duplicated submission (same reused TaskID) must not append the user
	// message twice; check before recording, not after.
	if old, ok := a.tasks[taskID]; ok && old.status == types.GoalRunning {
		a.mu.Unlock()
		return a.resultOf(old) // same ID already running: reuse its result
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	handle.cancel = cancel
	a.tasks[taskID] = handle
	a.mu.Unlock()

	// Record in the persisted conversation (left sidebar list); the raw user
	// goal, without the system-injected reference prefix.
	convoErr := a.appendConvo(req.ConversationID, conversation.Message{
		Role: "user", Content: goal, TaskID: taskID,
		Mode: string(normalizeTaskMode(req.TaskMode)), CreatedAt: time.Now(),
	})
	// Start usage accounting (async calls after task completion, e.g. GEO, also count).
	a.initUsage(taskID)

	// The config snapshot captures startup values, not teardown values: if the
	// user changes the model or tier mid-task, the second half runs on the new
	// config, while "what this task ran with" must be judged by startup --
	// taking it at teardown would record a config that never fully applied.
	snap := a.Cfg.Snapshot()
	// prog collects delivery-side facts from inside the loop (which attempt
	// succeeded, how many reworks) and writes them back into the result --
	// kept next to the growth record below so "what we count" and "what we
	// use" stay in one place.
	prog := &goalProgress{}
	result := a.runGoalLoop(ctx, req, goal, mode, normalizeTaskMode(req.TaskMode), taskID, prog)
	// Settle the worktree copy once done: dirty ones always stay (see
	// settleWorktree), clean ones are removed when config allows.
	a.settleWorktree(taskID)
	prog.stamp(result)
	result.ConfigSnapshot = &snap
	result.TaskID = taskID
	result.ConversationID = req.ConversationID
	result.Mode = mode
	result.TaskMode = normalizeTaskMode(req.TaskMode)
	result.StartedAt = handle.started
	// trace_id derives from goal + task mode + role + model, deliberately
	// without a timestamp: the same input with the same model yields the same
	// ID, so failed runs cluster by ID -- "the same problem recurring" becomes
	// visible without an extra index.
	result.TraceID = deriveTraceID(goal, string(normalizeTaskMode(req.TaskMode)), req.Role, a.LLMName())
	// Which rule set produced this run: version fingerprint plus the rule IDs
	// actually in effect. After a rule change the first question is "which
	// version produced that earlier batch of results"; unanswerable means
	// guessing. Chat mode skips the planner and has no rules, so both fields
	// stay honestly empty.
	if tm := normalizeTaskMode(req.TaskMode); tm != types.TaskChat {
		result.RuleSet = RuleSetVersion()
		result.Rules = ActiveRuleIDs(string(tm), a.EffectiveTier(req.Role), req.Role)
	}
	result.FinishedAt = time.Now()
	result.Usage = a.usageOf(taskID)
	result.Usage.DurationMs = elapsedMs(result.FinishedAt.Sub(handle.started))

	// Record the assistant reply in the same conversation; failures and
	// cancellations leave a trace too, so history never misses a turn.
	if err := a.appendConvo(req.ConversationID, conversation.Message{
		Role: "assistant", Content: convoReply(result), TaskID: taskID,
		Mode: string(normalizeTaskMode(req.TaskMode)), Status: string(result.Status), CreatedAt: time.Now(),
	}); err != nil {
		convoErr = errors.Join(convoErr, err)
	}
	// Losing the record must not be silent: the answer is still delivered, but
	// "the history is missing this turn" belongs on the result card, otherwise
	// the next session looks like the task never happened.
	if convoErr != nil && result != nil {
		note := "⚠️ 对话记录未能写入本机：" + convoErr.Error()
		if strings.TrimSpace(result.Suggestion) == "" {
			result.Suggestion = note
		} else {
			result.Suggestion = strings.TrimSpace(result.Suggestion) + "\n" + note
		}
	}

	a.mu.Lock()
	handle.status = result.Status
	handle.result = result
	a.pruneTasksLocked()
	a.mu.Unlock()
	// Record the growth log.
	// Cancellations are recorded separately as task_aborted: mixing abort rate
	// with pass rate in one denominator makes "the user gave up" and "we
	// failed" indistinguishable -- they need completely different fixes.
	if a.Growth != nil {
		entry := growth.Entry{
			Goal: goal, Score: result.Score,
			Steps: len(result.Steps), Duration: result.FinishedAt.Sub(result.StartedAt).Seconds(),
			Tokens: result.Usage.TotalTokens(), LLMCalls: result.Usage.LLMCalls, ToolCalls: result.Usage.ToolCalls,
			RuleSet: result.RuleSet,
			// Delivery-side metric (first pass / rework rounds): whether the
			// artifact qualified on the first try. Work mode only -- chat mode
			// has no artifact to check; and even when work mode falls back to
			// chat after repeated planning failures, that is still a failed
			// delivery and must stay in the denominator (dropping the worst
			// few turns the ratio into decoration).
			Delivery:  normalizeTaskMode(req.TaskMode) != types.TaskChat,
			FirstPass: result.FirstPass,
			Reworks:   result.Reworks,
		}
		if result.Status == types.GoalCancelled {
			entry.Type = "task_aborted"
		} else {
			entry.Type = "task_completed"
			entry.Retried = retried
		}
		a.Growth.Record(entry)
	}
	return result
}

// goalProgress holds one-shot facts from inside the main loop, for the
// delivery-side metric (first pass / rework rounds).
//
// Why callers cannot infer it from result: they cannot. "Which attempt
// succeeded" and "how many reworks" are local facts of the loop -- succeeding
// on the third retry and getting it right the first time end with identical
// status/score; only the loop knows it spun twice extra. So the loop writes it
// and RunGoal reads it, instead of guessing after the fact.
type goalProgress struct {
	attempts   int  // which attempt was actually reached; 0 in chat mode (never entered the loop)
	reworks    int  // rounds actually re-executed after reflection judged the result below threshold
	firstRound bool // whether the completion check was reached on the first attempt
}

// stamp writes the loop's delivery-side facts back into the result.
//
// Only when the work loop actually ran: chat mode has no concept of "rounds",
// and its two fields are set by the chat pipeline's own verdict (see
// runChatPath). Writing unconditionally would overwrite that verdict with zero
// values, so a chat that passed verification would be counted as "did not
// pass first acceptance".
func (p *goalProgress) stamp(res *types.GoalResult) {
	if p == nil || res == nil || p.attempts == 0 {
		return
	}
	res.Reworks = p.reworks
	// First pass = completed the flow on the first round AND ended up
	// genuinely successful. Both conditions: after the completion check,
	// applyAcceptanceVerdict can still correct the status to "partially done"
	// (artifact verification failed, reflector did not verify), which is not
	// "qualified on the first try".
	res.FirstPass = p.firstRound && res.Status == types.GoalSuccess
}

// runGoalLoop is the Plan-Execute-Reflect main loop (with automatic
// replanning). taskMode=chat takes the direct-chat pipeline.
// prog is passed in by the caller and read back, recording "which attempt
// succeeded, how many reworks" (see goalProgress).
func (a *Agent) runGoalLoop(ctx context.Context, req types.GoalRequest, goal, mode string, taskMode types.TaskMode, taskID string, prog *goalProgress) *types.GoalResult {
	// Structured references are injected by the Web UI; converted into an
	// explicit context section before the planning prompt, while the original
	// goal text is preserved so references do not stay at the UI chip layer.
	refs := req.References
	if len(refs) == 0 {
		if legacy, ok := req.Context["references"].([]map[string]string); ok {
			for _, ref := range legacy {
				refs = append(refs, types.Reference{Kind: ref["kind"], Label: ref["label"], Value: ref["value"]})
			}
		}
	}
	if len(refs) > 0 {
		var b strings.Builder
		b.WriteString("目标上下文引用（仅作为用户提供的工作上下文，不要把标记本身当作任务）：\n")
		for _, ref := range refs {
			fmt.Fprintf(&b, "- [%s] %s: %s\n", ref.Kind, ref.Label, ref.Value)
		}
		goal = b.String() + "\n用户目标：" + goal
	}
	// Chat mode: straight to the LLM, no plan/execute/reflect
	if taskMode == types.TaskChat {
		return a.runChatPath(ctx, goal, taskID, req.Role)
	}
	if a.Mem != nil {
		a.Mem.AddTurn("user", goal)
	}
	notify := func(phase, msg string, pct int, kind string) {
		a.Notifier.OnProgress(types.ProgressEvent{TaskID: taskID, Phase: phase, Message: msg, Progress: pct, Kind: kind})
	}
	// A stop must win over a verdict computed after it: reflection may still
	// return "done" for the steps that finished before the user pressed stop,
	// and reporting that as success hides the fact that the run was cut short.
	markCancelled := func(res *types.GoalResult) {
		if res != nil && ctx.Err() != nil {
			res.Status = types.GoalCancelled
			res.Error = "任务已取消"
		}
	}

	cwd := a.workspaceOf(req)
	// Isolation: run this task in its own worktree
	// Timed before planning: the cwd the planner sees and the file boundary at
	// execution are two sides of one thing; changing only one side yields
	// "planned against the main workspace, files written into the worktree".
	if iso := a.isolateForTask(ctx, req, taskID, notify); len(iso.roots) > 0 {
		ctx = toolutil.WithRoots(ctx, iso.roots)
		cwd = iso.cwd
	}

	// Model tier for scenario templates: switch models when the role declares
	// a tier and config has a matching model, otherwise the main model.
	// Planning, execution and chat all go through TierLLM (what BuildPlanner
	// resolves internally) -- switching models within one scenario is
	// pointless and only makes the style inconsistent.
	//
	// Reflection is the exception: it is verification, so it uses VerifyLLM
	// with a different-tier model. This previously reused the same client, so
	// "independent verification" only meant "independent call + independent
	// context" while the doer and the scorer were still the same model -- same
	// blind spots, with nothing to offset self-assessment bias.
	planner := a.BuildPlanner(req)
	reflector := &Reflector{
		LLM:           a.VerifyLLM(req.Role),
		DoneThreshold: a.Cfg.Agent.DoneThreshold,
		TaskID:        taskID,
	}
	tell := Narrator{Style: a.Cfg.Persona.Style}

	var feedback string
	var last *types.GoalResult
	var usedTools []string  // tools actually used across rounds (schemas force-attached on replan in menu mode)
	var acceptance []string // acceptance criteria: once set they follow this goal, restated or not
	planned := false
	attempts := a.Cfg.Agent.MaxReplans + 1
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		if prog != nil {
			prog.attempts = attempt
		}
		if ctx.Err() != nil {
			return a.cancelledResult(goal, last)
		}
		// Budget circuit-breaker checkpoint
		if stop := a.budgetCheckpoint(ctx, taskID, goal, nil); stop != nil {
			notify("budget", stop.Error, 88, "error")
			a.finalize(taskID, stop, nil, req.Role)
			return stop
		}
		// Plan
		notify("plan", tell.PlanStart(), 3, "info")
		// Automatic context compression: old conversation turns evicted from
		// the short window are summarized into a rolling summary (section 9
		// risk response).
		if a.Cfg.Agent.ContextCompress && a.Mem != nil {
			if a.Mem.Compress(a.CompressSummarizer(ctx, taskID)) {
				notify("plan", tell.Compressed(), 4, "info")
			}
		}
		var recent []memory.Turn
		if a.Mem != nil && a.Mem.Short != nil {
			// How many turns to carry is memory's call (tightened when the
			// water level crosses the line, see CarryTurns).
			recent = a.Mem.Short.Recent(a.Mem.CarryTurns())
		}
		var relevant []memory.Hit
		if a.Mem != nil && a.Mem.Long != nil {
			relevant = a.Mem.Relevant(goal, 3)
		}
		var summary string
		if a.Mem != nil {
			// Summary annotated with how much was omitted: "auto-compressed"
			// must say how much was dropped, or readers cannot tell the whole
			// from the remainder.
			summary = a.Mem.SummaryAnnotated()
		}
		// The plan stream reports progress only, never content: the planning
		// response is itself JSON, and forwarding it token by token to
		// CLI/WebUI would splinter raw JSON fragments into the progress line
		// (2026-09-23 QA report M5).
		var planChars int
		onDelta, flush := DeltaThrottle(50, func(text string) {
			planChars += len([]rune(text))
			notify("plan", fmt.Sprintf("规划中…（已生成 %d 字）", planChars), 5, "llm")
		})
		planner.OnLLMDelta = onDelta
		// In menu mode, pin the tools used in earlier rounds so replanning
		// does not want a schema that is missing.
		planner.ForceTools = usedTools
		plan, err := planner.Plan(ctx, goal, cwd, recent, relevant, summary, feedback)
		flush()
		if err != nil {
			if ctx.Err() != nil {
				return a.cancelledResult(goal, last)
			}
			// Model call failure (auth/network/rate limit): retrying is
			// pointless, fall back to direct chat immediately with the real
			// error; only plan validation failures are worth replanning.
			if attempt == attempts || strings.Contains(err.Error(), "llm:") {
				notify("plan", "规划变更前，尝试直接回答…", 20, "warn")
				last = &types.GoalResult{Goal: goal, Status: types.GoalFailed, Error: err.Error(), Score: 0}
				break
			}
			feedback = "上一次计划无效，请修正：" + err.Error()
			notify("plan", "计划校验未通过，正在重新规划…", 5, "warn")
			continue
		}
		notify("plan", tell.PlanDone(len(plan.Steps)), 8, "info")
		planned = true
		// Only the first round's acceptance criteria count: they are a promise
		// to the user, not to be rewritten on retry by the side that failed to
		// meet them. This also fixes "replan did not restate them -> criteria
		// silently vanish -> a single done slips through again".
		if len(acceptance) == 0 {
			acceptance = plan.Acceptance
		}
		plan.Acceptance = acceptance
		// Record the tools used this round: their schemas are guaranteed
		// present in the next round (a backstop for menu mode's filter).
		for _, st := range plan.Steps {
			usedTools = appendUnique(usedTools, st.Tool)
		}

		// plan_first: show the full plan first, one approval.
		preApproved := false
		if mode == string(types.ModePlanFirst) {
			resp := askApproval(ctx, a.Notifier, types.ApprovalRequest{
				TaskID: taskID, Plan: planDescriptions(plan), WholePlan: true,
				Risk: planMaxRisk(a.Reg, plan), Reason: "plan_first 模式：执行前需要确认完整计划",
			})
			if !resp.Approved {
				// This line is the reason the user reads on the card; it must
				// not always say "user rejected the plan": cancellation (the
				// host tearing down the gate to wake him) and timeout
				// auto-rejection are not the user's rejection.
				cause := "用户拒绝了执行计划"
				switch {
				case ctx.Err() != nil:
					cause = "任务已取消"
				case resp.Note != "":
					cause = resp.Note
				}
				res := &types.GoalResult{Goal: goal, Status: types.GoalCancelled, Error: cause, Score: 0}
				a.finalize(taskID, res, nil, req.Role)
				return res
			}
			preApproved = true
		}

		// Execute
		executor := &Executor{
			Reg: a.Reg, Gate: a.Gate, Notifier: a.Notifier,
			MaxConcurrency: a.Cfg.Agent.MaxConcurrency,
			StepTimeout:    time.Duration(a.Cfg.Agent.StepTimeoutSecs) * time.Second,
			StepRetries:    a.Cfg.Agent.StepRetries,
			Dedupe:         a.Cfg.Agent.DedupeCalls,
			MaxOutputRunes: a.Cfg.Agent.MaxOutputRunes,
			DataDir:        a.Cfg.DataDir,
			OnUsage:        a.BumpUsage,
			// Run-time persistence: if the process exits mid-run, tasks/
			// holds nothing, and runs/<taskID>.jsonl is the only evidence
			// answering "how far did it get". DiscardRunLog removes it on
			// normal completion (the terminal snapshot is more complete;
			// keeping it is noise).
			Sink: NewRunLog(a.Cfg.DataDir),
			Goal: goal,
			// The reviewer model is enabled only when an auxiliary model is
			// really configured: screening with the main model just costs an
			// extra copy.
			Reviewer: a.reviewer(),
		}
		// Deep review: hand the whole plan to the main model before execution.
		// A veto goes to the user -- approved, continue; rejected, stop here.
		// Stopping before Execute is deliberate: what it reviews is exactly
		// "what happens next", and once running, things are already moving.
		if a.Cfg.Safety.DeepReview {
			if ok, reason := a.deepReviewPlan(ctx, taskID, goal, &plan); !ok && !a.confirmDeepReview(ctx, taskID, reason) {
				res := &types.GoalResult{
					Goal: goal, Status: types.GoalFailed, Score: 0,
					Error: "深度扫描未通过：" + reason,
				}
				markCancelled(res)
				a.finalize(taskID, res, &plan, req.Role)
				return res
			}
		}
		exec := executor.Execute(ctx, plan, taskID, mode, preApproved)
		if ctx.Err() != nil && exec.Succeeded == 0 {
			res := &types.GoalResult{Goal: goal, Status: types.GoalCancelled, Score: 0, Error: "任务已取消", Steps: exec.Steps, ExecutedPlan: &exec.ExecutedPlan}
			a.finalize(taskID, res, nil, req.Role)
			return res
		}

		// Budget circuit-breaker checkpoint (after execute, before reflect)
		if stop := a.budgetCheckpoint(ctx, taskID, goal, exec.Steps); stop != nil {
			notify("budget", stop.Error, 92, "error")
			a.finalize(taskID, stop, &plan, req.Role)
			return stop
		}

		// Context window checkpoint (same spot: after execute, before reflect)
		// Placed here because another request goes out immediately
		// (reflection), and this round's execution has already pushed the
		// context to its maximum. The task is not over (reflection below,
		// possibly replanning), so it only acts past 96% -- compression does
		// not end the task; the round continues.
		if compressed, tightened := a.compressMidTask(ctx, taskID); compressed || tightened {
			notify("context", a.contextActionText(compressed, tightened, true), 92, "info")
		}

		// Reflect
		notify("reflect", tell.ReflectStart(), 92, "info")
		// Acceptance criteria were set at planning time; the reflector judges
		// them one by one (instead of scoring by feel).
		reflector.Acceptance = plan.Acceptance
		reflection := reflector.Evaluate(ctx, goal, exec, attempt)
		result := a.buildResult(goal, exec, reflection, plan.Acceptance, planner.LastBreakdown)
		last = result

		// Completion check: score at threshold or the model says done, AND all
		// acceptance criteria pass AND artifact verification passes. Extracted
		// into completionReached so each of the three guards is individually
		// testable -- it used to be one long condition where a missing clause
		// was invisible (see TestCompletionReached_*).
		if completionReached(reflection, plan.Acceptance, exec, a.Cfg.Agent.DoneThreshold) {
			if prog != nil && attempt == 1 {
				prog.firstRound = true
			}
			result.Status = statusOfExec(exec)
			if reflection.Suggestion != "" {
				result.Suggestion = reflection.Suggestion
			}
			markCancelled(result)
			a.finalize(taskID, result, &plan, req.Role)
			return result
		}
		// ---------- Anti-spin: the same action repeating means running in
		// place; stop spending replan attempts ----------
		// Placed before the "last replan" check, otherwise it never fires
		// under default parameters (the loop is ending anyway).
		if fp, rounds := a.noteSteps(taskID, exec.Steps); fp != "" {
			stuckMsg := fmt.Sprintf("检测到原地打转：%s 已重复执行 %d 轮", describeFingerprint(fp), rounds)
			notify("reflect", "⚠ "+stuckMsg+"，停止自动重规划", 90, "warn")
			result.Status = statusOfExec(exec)
			if result.Status == types.GoalRunning || result.Status == types.GoalSuccess {
				result.Status = types.GoalFailed
			}
			result.Error = stuckMsg + "。继续重规划大概率还是同样结果。"
			result.Suggestion = "建议把目标说得更具体（例如指定要处理的文件或期望的输出格式），或补充缺少的信息后重试；也可以切换到「对话」模式先对齐需求。"
			markCancelled(result)
			a.finalize(taskID, result, &plan, req.Role)
			return result
		}
		if reflection.Verdict == "failed" || attempt == attempts {
			result.Status = statusOfExec(exec)
			if result.Status == types.GoalRunning || result.Status == types.GoalSuccess {
				if reflection.Verdict == "failed" {
					result.Status = types.GoalFailed
				}
			}
			if reflection.Suggestion != "" {
				result.Suggestion = reflection.Suggestion
			}
			markCancelled(result)
			a.finalize(taskID, result, &plan, req.Role)
			return result
		}

		// Continue replanning. Carry the result of every step from last time --
		// previously only a score and one sentence, so the second round of
		// planning was nearly as blind as the first (the text said "for
		// reference" with nothing to reference), which was one of the reasons
		// "running in place" fired so often.
		//
		// This is the only place that counts as "rework": it executed, was
		// judged below threshold, and is redone. The "plan validation failed ->
		// replan" path above does not count -- nothing executed there, so it is
		// not a redo, and counting it would make this metric drift with "how
		// well the plan was written" instead of with delivery quality.
		if prog != nil {
			prog.reworks++
		}
		feedback = fmt.Sprintf("完成度 %d/100。问题：%s\n\n上次执行的情况（针对它们改，不要原样重来）：\n%s",
			reflection.Score, reflection.Reason, buildStepDigest(exec))
		notify("reflect", tell.Replan(reflection.Score), 90, "warn")
	}
	// Planning never produced a valid plan (typically a pure chat goal straying
	// into work mode) -> fall back to direct chat instead of hard-failing.
	if !planned && (last == nil || last.Status == types.GoalFailed) {
		notify("plan", "多次规划未成功，切换直接回答…", 20, "warn")
		res := a.runChatPath(ctx, goal, taskID, req.Role)
		if res.Status == types.GoalSuccess {
			res.Score = 70
			res.Suggestion = "这个问题更适合「对话」模式，切换后响应更快"
		}
		// The deliverable came from direct chat, not the work loop -> prog
		// stays "never reached the completion check", so this delivery lands
		// as FirstPass=false. It must NOT be excluded from the metric:
		// repeated planning failure is the worst delivery, and removing the
		// worst from the denominator dresses the ratio up.
		return res
	}
	return a.cancelledResult(goal, last)
}

// runChatPath is the chat-mode pipeline: memory + rolling summary injection,
// straight to a streaming LLM answer. No plan/execute/reflect; work mode also
// falls back here after repeated planning failures.
func (a *Agent) runChatPath(ctx context.Context, goal, taskID, role string) *types.GoalResult {
	notify := func(phase, msg string, pct int, kind string) {
		a.Notifier.OnProgress(types.ProgressEvent{TaskID: taskID, Phase: phase, Message: msg, Progress: pct, Kind: kind})
	}
	if a.Mem != nil {
		a.Mem.AddTurn("user", goal)
		if a.Cfg.Agent.ContextCompress && a.Mem.Compress(a.CompressSummarizer(ctx)) {
			notify("chat", "已自动压缩早期上下文", 15, "info")
		}
	}
	name := a.Cfg.Persona.Name
	if strings.TrimSpace(name) == "" {
		name = "Gleam"
	}
	// Stable segment first, volatile last (same as the planner): across turns,
	// system + earlier messages are a natural long common prefix, and putting
	// per-turn variable content like the workspace or summary up front would
	// waste the whole prefix.
	sys := fmt.Sprintf("你是 %s（微光），运行在用户本机的桌面智能体，当前对话模式。"+
		"用与用户一致的语言自然对话：回答简洁、具体、可执行；涉及本机文件或命令的操作，"+
		"提示切换到「工作」或「编程」模式提交。%s", name, styleInstruction(a.Cfg.Persona.Style))
	var volatile strings.Builder
	fmt.Fprintf(&volatile, "\n\n## 当前环境\n工作目录：%s", a.Cfg.Workspace)
	if a.Mem != nil {
		// Same tail and annotation as the planner: a rolling summary is most
		// useful at its newest end, and the "omitted N turns" note lives there.
		if s := a.Mem.SummaryAnnotated(); strings.TrimSpace(s) != "" {
			volatile.WriteString("\n\n## 早期上下文摘要（已自动压缩）\n" + types.ShortenTail(s, 400))
		}
	}
	sys += volatile.String()
	var msgs []llm.Message
	if a.Mem != nil && a.Mem.Short != nil {
		for _, t := range a.Mem.Short.Recent(a.Mem.CarryTurns()) {
			role := llm.RoleUser
			if t.Role == "assistant" {
				role = llm.RoleAssistant
			}
			msgs = append(msgs, llm.Message{Role: role, Content: t.Content})
		}
	}
	// The latest turn is this user message (AddTurn already put it in
	// short-term memory); avoid injecting it twice.
	if n := len(msgs); n > 0 && strings.TrimSpace(msgs[n-1].Content) == strings.TrimSpace(goal) {
		msgs = msgs[:n-1]
	}
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: goal})

	notify("chat", "思考中…", 30, "info")
	onDelta, flush := DeltaThrottle(40, func(text string) {
		notify("chat", strings.TrimSpace(text), 55, "llm")
	})
	text, err := a.TierLLM(role).ChatStream(ctx, llm.ChatRequest{
		System:      "你是 " + name + " 的对话管线。" + llm.MarkerChat + "\n\n" + sys,
		Messages:    msgs,
		Temperature: 0.6,
		TaskID:      taskID,
	}, onDelta)
	flush()
	if err != nil {
		if ctx.Err() != nil {
			return &types.GoalResult{Goal: goal, Status: types.GoalCancelled, Error: "任务已取消"}
		}
		notify("chat", "模型调用失败: "+err.Error(), 100, "error")
		res := &types.GoalResult{Goal: goal, Status: types.GoalFailed, Score: 0, Error: err.Error()}
		a.finalize(taskID, res, nil, role)
		return res
	}
	if a.Mem != nil {
		a.Mem.AddTurn("assistant", text)
	}
	// The answer is produced; hand it to the auxiliary model for one
	// independent verification -- chat mode has no planning stage, so this is
	// the only place that can stop "the doer grading itself full marks". A
	// failed verification never blocks the chat.
	if a.shouldChatCheck(goal, text) {
		notify("chat", "核对回答是否达成目标…", 95, "info")
	}
	res := &types.GoalResult{Goal: goal, Status: types.GoalSuccess, Score: 100, Summary: text}
	criteria, checks, outcome := a.chatSelfCheck(ctx, goal, text, taskID)
	if outcome == chatCheckDone {
		applyChatCheck(res, criteria, checks)
		// No need to say more when it passes -- the checklist shows on the
		// task card; only partial failure is worth a note.
		if res.Status != types.GoalSuccess {
			notify("chat", "自检未全过："+res.Error, 100, "warn")
		}
	} else if outcome == chatCheckUnusable {
		// Verification did run, but the call failed or the output was
		// unreadable, so no verdict was reached. Status and score stay as
		// they were -- the answer was already delivered; zeroing the score
		// would render a red "completeness 0/100" in the UI, which lies in
		// the opposite direction (the answer may be fine, just unverified).
		// But the user must not be left thinking verification happened:
		// silently keeping 100 is exactly what this feature exists to
		// eliminate.
		notify("chat", "对话自检未能完成，本次回答未经核对", 100, "warn")
	}
	notify("chat", "已回复", 100, "info")
	a.finalize(taskID, res, nil, role)
	return res
}

// finalize wraps up: write memory, emit proactive suggestions and
// skill-crystallization proposals; then compress context asynchronously
// (without blocking the completion event; a no-op when there is no overflow).
func (a *Agent) finalize(taskID string, result *types.GoalResult, plan *types.Plan, role string) {
	// Correct the status by the acceptance verdict first, then archive and
	// propose -- otherwise "partially done" is recorded as success in growth stats.
	var acceptance []string
	if plan != nil {
		acceptance = plan.Acceptance
	}
	applyAcceptanceVerdict(result, acceptance)
	if result.Summary != "" && a.Mem != nil {
		a.Mem.AddTurn("assistant", result.Summary)
	}
	if a.compressionOn() {
		// Done on the wrap-up path but never blocking delivery: the summary
		// costs a model call, and the task's conclusion should not wait for
		// it. Uses an independent ctx and timeout -- the task's own ctx may
		// already be cancelled (the user pressed stop, or the task ended in
		// failure), while "summarize the early context" should happen
		// regardless of task failure.
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		go func() {
			defer cancelC()
			if compressed, tightened := a.compressAfterTask(ctxC, taskID); compressed || tightened {
				a.notifyContextAction(compressed, tightened, false)
			}
		}()
	}
	// Persist the task record into long-term memory (best effort).
	goal := result.Goal
	if goal != "" && a.Mem != nil && a.Mem.Long != nil {
		_, err := a.Mem.RememberScoped(fmt.Sprintf("任务记录[%s] 目标：%s 结果：%s（完成度 %d）",
			result.Status, goal, types.Shorten(result.Summary, 200), result.Score), "task", a.MemoryScope(), []string{"task"})
		if err == nil {
			_ = a.Mem.Long.Flush()
		}
	}
	if result.Suggestion != "" {
		a.Notifier.OnSuggestion(taskID, result.Suggestion)
	}
	// GEO suggestions: analyze and archive after a successful creative
	// deliverable (writer role or a task recognized as creative intent).
	if a.shouldAnalyzeGEO(role, result) {
		a.analyzeGEOAsync(taskID, result.Goal, result.Summary)
	}
	// Skill crystallization proposal: success with at least 2 non-reply steps.
	if plan != nil && result.Status == types.GoalSuccess {
		nonReply := 0
		for _, st := range plan.Steps {
			if st.Tool != "reply" {
				nonReply++
			}
		}
		if nonReply >= 2 {
			a.Notifier.OnSuggestSkill(taskID, types.SkillDraft{
				Name:        skillSlug(goal, a.Skills),
				Description: goal,
				Steps:       plan.Steps,
			})
		}
	}
}

// minGEOContentRunes is the shortest deliverable that triggers GEO
// auto-analysis: very short replies ("ok", "done") have no structure to
// optimize; analyzing them only spams.
const minGEOContentRunes = 80

// shouldAnalyzeGEO decides whether this deliverable is worth a GEO analysis.
// Conditions: switch on + task succeeded + deliverable long enough + creative
// (writer role or recognized creative intent).
func (a *Agent) shouldAnalyzeGEO(role string, result *types.GoalResult) bool {
	if a == nil || a.LLM == nil || result == nil || a.Cfg == nil {
		return false
	}
	if !a.Cfg.Agent.GEOEnabled {
		return false
	}
	if result.Status != types.GoalSuccess {
		return false
	}
	if len([]rune(strings.TrimSpace(result.Summary))) < minGEOContentRunes {
		return false
	}
	return geo.IsCreative(result.Goal, role)
}

// analyzeGEOAsync analyzes a creative deliverable asynchronously: archives it
// to GEO history and pushes suggestions to the frontend. Analysis failures are
// silently skipped -- it enhances successful tasks and must not affect the task
// result.
func (a *Agent) analyzeGEOAsync(taskID, goal, content string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	go func() {
		defer cancel()
		suggestion, err := (&geo.Analyzer{LLM: a.HelperLLM(), TaskID: taskID}).Analyze(ctx, content)
		if err != nil || suggestion == nil {
			return
		}
		if a.GEO != nil {
			a.GEO.Add(geo.Record{
				TaskID:      taskID,
				Goal:        types.Shorten(goal, 140),
				Score:       suggestion.Score,
				Summary:     suggestion.Summary,
				Strengths:   suggestion.Strengths,
				Weaknesses:  suggestion.Weaknesses,
				Actionables: suggestion.Actionables,
				Source:      "auto",
			})
		}
		a.Notifier.OnSuggestion(taskID, geo.Format(suggestion))
	}()
}

// appendUnique appends an element if not already present.
func appendUnique(list []string, v string) []string {
	for _, s := range list {
		if s == v {
			return list
		}
	}
	return append(list, v)
}

// skillSlug derives a skill name from the goal (avoiding collisions with
// existing ones).
func skillSlug(goal string, store *skill.Store) string {
	base := slugStrip.ReplaceAllString(strings.TrimSpace(goal), "-")
	base = strings.Trim(base, "-")
	r := []rune(base)
	if len(r) > 24 {
		base = string(r[:24])
	}
	if base == "" {
		base = "skill"
	}
	if store == nil {
		return base
	}
	name := base
	// Bounded: an unexpected store error must not turn naming into a spin.
	for i := 2; i <= 100; i++ {
		if _, err := store.Get(name); err != nil {
			break // does not exist, usable
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}
