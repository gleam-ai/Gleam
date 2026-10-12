package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gleam/internal/agent/geo"
	"gleam/internal/config"
	"gleam/internal/harness/conversation"
	"gleam/internal/harness/credentials"
	"gleam/internal/harness/growth"
	"gleam/internal/harness/memory"
	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/harness/scheduler"
	"gleam/internal/harness/skill"
	"gleam/internal/harness/space"
	"gleam/internal/llm"
	"gleam/internal/tools/file"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

type Agent struct {
	LLM       llm.Client
	Reg       *registry.Registry
	Mem       *memory.Manager
	Gate      *safety.Gate
	Skills    *skill.Store
	Sched     *scheduler.Scheduler
	Cfg       *config.Config
	Notifier  Notifier
	MCP       *MCPManager
	FileTools *file.Tools
	Growth    *growth.Log
	Convos    *conversation.Store
	Spaces    *space.Store
	Creds     *credentials.Store
	Auth      AuthProvider
	GEO       *geo.Store
	FastLLM   llm.Client

	mu    sync.Mutex
	tasks map[string]*taskHandle

	usageMu  sync.Mutex
	usage    map[string]*types.TaskUsage
	budget   map[string]*taskBudget
	stuck    map[string]map[string]int
	hookOnce sync.Once

	tierMu      sync.Mutex
	tierClients map[string]llm.Client

	ctxMu               sync.Mutex
	lastPromptTokens    int
	lastPromptEstimated bool
}

type taskBudget struct {
	extraCalls  int
	extraTokens int
}

type AuthProvider interface {
	Configured() bool
	Session() map[string]any
	Configure(supabaseURL, anonKey string) error
	SignUp(email, password string) error
	SignIn(email, password string) error
	SignOut() error
	StartOAuth(provider string) (string, error)
}

type taskHandle struct {
	id      string
	goal    string
	mode    string
	cancel  context.CancelFunc
	status  types.GoalStatus
	result  *types.GoalResult
	started time.Time
}

func New(cfg *config.Config, client llm.Client, reg *registry.Registry, mem *memory.Manager,
	gate *safety.Gate, skills *skill.Store, sched *scheduler.Scheduler, notifier Notifier) *Agent {
	if notifier == nil {
		notifier = NopNotifier{}
	}
	a := &Agent{
		LLM: client, Reg: reg, Mem: mem, Gate: gate,
		Skills: skills, Sched: sched, Cfg: cfg, Notifier: notifier,
		tasks: map[string]*taskHandle{},
	}
	a.syncModelRuntime()
	return a
}

func (a *Agent) RunGoal(ctx context.Context, req types.GoalRequest) *types.GoalResult {
	taskID := req.TaskID
	if taskID == "" {
		taskID = types.NewID()
	}
	req.TaskID = taskID
	goal := strings.TrimSpace(req.Goal)
	mode := normalizeMode(req.Mode)

	retried := req.ConversationID != "" && a.Convos != nil && a.Convos.DetectRetry(req.ConversationID, goal)

	a.appendConvo(req.ConversationID, conversation.Message{
		Role: "user", Content: goal, TaskID: taskID,
		Mode: string(normalizeTaskMode(req.TaskMode)), CreatedAt: time.Now(),
	})

	handle := &taskHandle{id: taskID, goal: goal, mode: mode, status: types.GoalRunning, started: time.Now()}
	a.mu.Lock()
	if old, ok := a.tasks[taskID]; ok && old.status == types.GoalRunning {
		a.mu.Unlock()
		return a.resultOf(old)
	}
	a.tasks[taskID] = handle
	a.mu.Unlock()
	a.initUsage(taskID)

	ctx, cancel := context.WithCancel(ctx)
	handle.cancel = cancel
	defer cancel()

	snap := a.Cfg.Snapshot()
	prog := &goalProgress{}
	result := a.runGoalLoop(ctx, req, goal, mode, normalizeTaskMode(req.TaskMode), taskID, prog)
	a.settleWorktree(taskID)
	prog.stamp(result)
	result.ConfigSnapshot = &snap
	result.TaskID = taskID
	result.Mode = mode
	result.TaskMode = normalizeTaskMode(req.TaskMode)
	result.StartedAt = handle.started
	result.TraceID = deriveTraceID(goal, string(normalizeTaskMode(req.TaskMode)), req.Role, a.LLMName())
	if tm := normalizeTaskMode(req.TaskMode); tm != types.TaskChat {
		result.RuleSet = RuleSetVersion()
		result.Rules = ActiveRuleIDs(string(tm), a.EffectiveTier(req.Role), req.Role)
	}
	result.FinishedAt = time.Now()
	result.Usage = a.usageOf(taskID)
	result.Usage.DurationMs = elapsedMs(result.FinishedAt.Sub(handle.started))

	a.appendConvo(req.ConversationID, conversation.Message{
		Role: "assistant", Content: convoReply(result), TaskID: taskID,
		Mode: string(normalizeTaskMode(req.TaskMode)), Status: string(result.Status), CreatedAt: time.Now(),
	})

	a.mu.Lock()
	handle.status = result.Status
	handle.result = result
	a.pruneTasksLocked()
	a.mu.Unlock()
	if a.Growth != nil {
		entry := growth.Entry{
			Goal: goal, Score: result.Score,
			Steps: len(result.Steps), Duration: result.FinishedAt.Sub(result.StartedAt).Seconds(),
			Tokens: result.Usage.TotalTokens(), LLMCalls: result.Usage.LLMCalls, ToolCalls: result.Usage.ToolCalls,
			RuleSet:   result.RuleSet,
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

func ValidateGoalRequest(req types.GoalRequest) error {
	if err := types.ValidateReferences(req.References); err != nil {
		return err
	}
	if req.Mode != "" && req.Mode != string(types.ModeAuto) && req.Mode != string(types.ModePlanFirst) && req.Mode != string(types.ModeInteractive) {
		return fmt.Errorf("不支持的安全模式 %q", req.Mode)
	}
	if req.TaskMode != "" && req.TaskMode != types.TaskChat && req.TaskMode != types.TaskWork && req.TaskMode != types.TaskCode {
		return fmt.Errorf("不支持的任务模式 %q", req.TaskMode)
	}
	if req.Role != "" && !HasRole(req.Role) {
		return fmt.Errorf("不支持的专家角色 %q", req.Role)
	}
	if req.TaskID != "" && SafeTaskName(req.TaskID) == "" {
		return fmt.Errorf("task_id %q 不能作文件名：只允许字母、数字与 - _，长度不超过 128", req.TaskID)
	}
	return nil
}

const maxRetainedTasks = 200

func (a *Agent) pruneTasksLocked() {
	if len(a.tasks) <= maxRetainedTasks {
		return
	}
	type rec struct {
		id      string
		started time.Time
	}
	var done []rec
	for id, h := range a.tasks {
		if h.status != types.GoalRunning {
			done = append(done, rec{id, h.started})
		}
	}
	sort.Slice(done, func(i, j int) bool { return done[i].started.Before(done[j].started) })
	for i := 0; i < len(done) && len(a.tasks) > maxRetainedTasks; i++ {
		delete(a.tasks, done[i].id)
		a.dropUsage(done[i].id)
	}
}

func (a *Agent) HelperLLM() llm.Client {
	if a != nil && a.FastLLM != nil {
		return a.FastLLM
	}
	return a.LLM
}

func (a *Agent) RebuildFastClient() {
	if a == nil {
		return
	}
	if a.Cfg == nil || a.Cfg.LLM.FastModel == "" || a.Cfg.LLM.Provider == "mock" {
		a.FastLLM = nil
		return
	}
	protocol := a.Cfg.LLM.Protocol
	if protocol == "" {
		protocol = llm.ProtocolOpenAIChat
	}
	a.FastLLM = llm.New(protocol, a.Cfg.LLM.BaseURL, a.Cfg.LLM.APIKey, a.Cfg.LLM.FastModel,
		a.Cfg.LLM.Temperature, a.Cfg.LLM.MaxTokens, a.Cfg.LLM.TimeoutSecs)
}

func (a *Agent) RebuildTierClients() {
	if a == nil {
		return
	}
	a.tierMu.Lock()
	a.tierClients = nil
	a.tierMu.Unlock()
}

func (a *Agent) TierLLM(role string) llm.Client {
	if a == nil || a.Cfg == nil {
		return a.LLM
	}
	return a.tierLLM(FindRole(role).ModelTier)
}

func (a *Agent) tierLLM(tier string) llm.Client {
	if a == nil || a.Cfg == nil {
		return a.LLM
	}
	if tier == "" || len(a.Cfg.LLM.Tiers) == 0 {
		return a.LLM
	}
	model := strings.TrimSpace(a.Cfg.LLM.Tiers[tier])
	if model == "" || model == a.Cfg.LLM.Model {
		return a.LLM
	}
	if a.Cfg.LLM.Provider == "mock" {
		return a.LLM
	}
	a.tierMu.Lock()
	defer a.tierMu.Unlock()
	if c, ok := a.tierClients[tier]; ok {
		return c
	}
	protocol := a.Cfg.LLM.Protocol
	if protocol == "" {
		protocol = llm.ProtocolOpenAIChat
	}
	c := llm.New(protocol, a.Cfg.LLM.BaseURL, a.Cfg.LLM.APIKey, model,
		a.Cfg.LLM.Temperature, a.Cfg.LLM.MaxTokens, a.Cfg.LLM.TimeoutSecs)
	if a.tierClients == nil {
		a.tierClients = map[string]llm.Client{}
	}
	a.tierClients[tier] = c
	return c
}

const DefaultVerifyTier = "reasoning"

func (a *Agent) VerifyLLM(role string) llm.Client {
	if a == nil || a.Cfg == nil {
		return a.LLM
	}
	if !a.verifyIndependent(role) {
		return a.TierLLM(role)
	}
	return a.tierLLM(DefaultVerifyTier)
}

func (a *Agent) VerifyModel(role string) string {
	if a == nil || a.Cfg == nil {
		return ""
	}
	if a.verifyIndependent(role) {
		return strings.TrimSpace(a.Cfg.LLM.Tiers[DefaultVerifyTier])
	}
	return a.execModel(role)
}

func (a *Agent) verifyIndependent(role string) bool {
	if a == nil || a.Cfg == nil || a.Cfg.LLM.Provider == "mock" {
		return false
	}
	m := strings.TrimSpace(a.Cfg.LLM.Tiers[DefaultVerifyTier])
	return m != "" && m != a.execModel(role)
}

func (a *Agent) execModel(role string) string {
	if a == nil || a.Cfg == nil {
		return ""
	}
	if t := a.EffectiveTier(role); t != "" {
		return strings.TrimSpace(a.Cfg.LLM.Tiers[t])
	}
	return a.Cfg.LLM.Model
}

func (a *Agent) EffectiveTier(role string) string {
	if a == nil || a.Cfg == nil {
		return ""
	}
	tier := FindRole(role).ModelTier
	if tier == "" || len(a.Cfg.LLM.Tiers) == 0 {
		return ""
	}
	model := strings.TrimSpace(a.Cfg.LLM.Tiers[tier])
	if model == "" || model == a.Cfg.LLM.Model {
		return ""
	}
	if a.Cfg.LLM.Provider == "mock" {
		return ""
	}
	return tier
}

func (a *Agent) BuildPlanner(req types.GoalRequest) *Planner {
	taskID := req.TaskID
	if taskID == "" {
		taskID = types.NewID()
	}
	pl := &Planner{
		LLM: a.TierLLM(req.Role), Reg: a.Reg,
		MaxSteps:       a.Cfg.Agent.MaxSteps,
		Style:          a.Cfg.Persona.Style,
		TaskMode:       string(normalizeTaskMode(req.TaskMode)),
		CommitHint:     a.Cfg.Git.CommitInstructions,
		Role:           req.Role,
		TaskID:         taskID,
		Tier:           a.EffectiveTier(req.Role),
		MaxToolSchemas: a.Cfg.Agent.MaxToolSchemas,
		Helper:         a.FastLLM,
		RoleTools:      FindRole(req.Role).Tools,
	}
	if a.Mem != nil {
		pl.Pinned = a.Mem.Pinned()
	}
	return pl
}

func (a *Agent) initUsage(taskID string) {
	a.installUsageHook()
	a.usageMu.Lock()
	if a.usage == nil {
		a.usage = map[string]*types.TaskUsage{}
	}
	if a.budget == nil {
		a.budget = map[string]*taskBudget{}
	}
	if a.stuck == nil {
		a.stuck = map[string]map[string]int{}
	}
	a.usage[taskID] = &types.TaskUsage{}
	a.budget[taskID] = &taskBudget{}
	a.stuck[taskID] = map[string]int{}
	a.usageMu.Unlock()
}

func (a *Agent) budgetExtra(taskID string) taskBudget {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	if b := a.budget[taskID]; b != nil {
		return *b
	}
	return taskBudget{}
}

func (a *Agent) grantBudget(taskID string) {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	if a.budget == nil {
		a.budget = map[string]*taskBudget{}
	}
	b := a.budget[taskID]
	if b == nil {
		b = &taskBudget{}
		a.budget[taskID] = b
	}
	b.extraCalls += a.Cfg.Agent.MaxLLMCallsPerTask
	b.extraTokens += a.Cfg.Agent.MaxTokensPerTask
}

func (a *Agent) taskStarted(taskID string) (time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if h := a.tasks[taskID]; h != nil {
		return h.started, true
	}
	return time.Time{}, false
}

func (a *Agent) overBudget(taskID string) string {
	if a == nil || a.Cfg == nil {
		return ""
	}
	cfg := a.Cfg.Agent
	if cfg.MaxLLMCallsPerTask <= 0 && cfg.MaxTokensPerTask <= 0 && cfg.MaxTaskDurationSecs <= 0 {
		return ""
	}
	u := a.usageOf(taskID)
	extra := a.budgetExtra(taskID)
	if cfg.MaxLLMCallsPerTask > 0 {
		limit := cfg.MaxLLMCallsPerTask + extra.extraCalls
		if u.LLMCalls >= limit {
			return fmt.Sprintf("模型调用已达预算上限（%d 次）", limit)
		}
	}
	if cfg.MaxTokensPerTask > 0 {
		limit := cfg.MaxTokensPerTask + extra.extraTokens
		if u.TotalTokens() >= limit {
			return fmt.Sprintf("token 消耗已达预算上限（约 %d）", limit)
		}
	}
	if cfg.MaxTaskDurationSecs > 0 {
		if started, ok := a.taskStarted(taskID); ok {
			if elapsed := time.Since(started).Seconds(); elapsed >= float64(cfg.MaxTaskDurationSecs) {
				return fmt.Sprintf("任务已运行 %.0f 秒，超过时长预算（%d 秒）", elapsed, cfg.MaxTaskDurationSecs)
			}
		}
	}
	return ""
}

func (a *Agent) checkBudget(taskID, reason string) bool {
	if a == nil || a.Notifier == nil {
		return false
	}
	resp := a.Notifier.OnApproval(types.ApprovalRequest{
		TaskID: taskID,
		Plan:   []string{"继续执行（再追加一倍预算）"},
		Risk:   "medium",
		Reason: reason + "。继续会继续产生调用开销，是否继续？",
	})
	if !resp.Approved {
		return false
	}
	a.grantBudget(taskID)
	return true
}

func (a *Agent) confirmDeepReview(taskID, reason string) bool {
	if a == nil || a.Notifier == nil {
		return true
	}
	return a.Notifier.OnApproval(types.ApprovalRequest{
		TaskID: taskID,
		Plan:   []string{"按原计划继续执行"},
		Risk:   "medium",
		Reason: "深度扫描觉得这段计划有问题：" + reason + "。仍要继续吗？",
	}).Approved
}

func (a *Agent) reviewer() Reviewer {
	if a == nil || a.Cfg == nil || !a.Cfg.Safety.AIReview || a.FastLLM == nil {
		return nil
	}
	return &llmReviewer{LLM: a.FastLLM, TaskID: ""}
}

func appendUnique(list []string, v string) []string {
	for _, s := range list {
		if s == v {
			return list
		}
	}
	return append(list, v)
}

func (a *Agent) noteSteps(taskID string, steps []types.StepResult) (fp string, rounds int) {
	if a == nil || a.Cfg == nil || a.Cfg.Agent.StuckThreshold <= 0 {
		return "", 0
	}
	th := a.Cfg.Agent.StuckThreshold
	seen := make(map[string]struct{}, len(steps))
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	if a.stuck == nil {
		a.stuck = map[string]map[string]int{}
	}
	counts := a.stuck[taskID]
	if counts == nil {
		counts = map[string]int{}
		a.stuck[taskID] = counts
	}
	for _, st := range steps {
		if st.Status != types.StepSucceeded && st.Status != types.StepFailed {
			continue
		}
		f := stepFingerprint(st.Tool, st.FinalArgs)
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		counts[f]++
		if counts[f] >= th {
			return f, counts[f]
		}
	}
	return "", 0
}

func describeFingerprint(fp string) string {
	i := strings.Index(fp, "|")
	if i < 0 {
		return fp
	}
	tool, args := fp[:i], fp[i+1:]
	if args == "" || args == "{}" || args == "null" {
		return tool
	}
	return tool + " " + types.Shorten(args, 120)
}

func (a *Agent) budgetCheckpoint(ctx context.Context, taskID, goal string, steps []types.StepResult) *types.GoalResult {
	if a == nil || ctx == nil || ctx.Err() != nil {
		return nil
	}
	reason := a.overBudget(taskID)
	if reason == "" {
		return nil
	}
	if a.Notifier != nil {
		a.Notifier.OnProgress(types.ProgressEvent{
			TaskID: taskID, Phase: "budget", Message: "⚠ " + reason, Progress: 88, Kind: "warn",
		})
	}
	if a.checkBudget(taskID, reason) {
		return nil
	}
	return &types.GoalResult{
		Goal:   goal,
		Status: types.GoalFailed,
		Error:  "超出任务预算，已停止：" + reason,
		Steps:  steps,
		Score:  0,
	}
}

func (a *Agent) installUsageHook() {
	a.hookOnce.Do(func() {
		llm.AddUsageHook(func(taskID, kind string, req llm.ChatRequest, text string, u llm.Usage) {
			a.addUsage(taskID, u)
		})
	})
}

func (a *Agent) addUsage(taskID string, u llm.Usage) {
	a.notePromptSize(u)
	if taskID == "" {
		return
	}
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	st := a.usage[taskID]
	if st == nil {
		return
	}
	st.LLMCalls++
	st.PromptTokens += u.PromptTokens
	st.CompletionTokens += u.CompletionTokens
	if u.CachedTokens > 0 {
		st.CachedTokens += u.CachedTokens
		st.CachedCalls++
	}
	if u.Estimated {
		st.EstimatedCalls++
	}
}

func (a *Agent) BumpUsage(taskID string, toolCalls, retries, deduped int) {
	if taskID == "" || (toolCalls == 0 && retries == 0 && deduped == 0) {
		return
	}
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	if st := a.usage[taskID]; st != nil {
		st.ToolCalls += toolCalls
		st.Retries += retries
		st.Deduped += deduped
	}
}

func (a *Agent) usageOf(taskID string) types.TaskUsage {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	if st := a.usage[taskID]; st != nil {
		return *st
	}
	return types.TaskUsage{}
}

func (a *Agent) dropUsage(taskID string) {
	a.usageMu.Lock()
	delete(a.usage, taskID)
	delete(a.budget, taskID)
	delete(a.stuck, taskID)
	a.usageMu.Unlock()
}

func normalizeTaskMode(m types.TaskMode) types.TaskMode {
	switch types.TaskMode(strings.TrimSpace(string(m))) {
	case types.TaskChat:
		return types.TaskChat
	case types.TaskCode:
		return types.TaskCode
	default:
		return types.TaskWork
	}
}

type goalProgress struct {
	attempts   int
	reworks    int
	firstRound bool
}

func (p *goalProgress) stamp(res *types.GoalResult) {
	if p == nil || res == nil || p.attempts == 0 {
		return
	}
	res.Reworks = p.reworks
	res.FirstPass = p.firstRound && res.Status == types.GoalSuccess
}

func (a *Agent) runGoalLoop(ctx context.Context, req types.GoalRequest, goal, mode string, taskMode types.TaskMode, taskID string, prog *goalProgress) *types.GoalResult {
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
	if taskMode == types.TaskChat {
		return a.runChatPath(ctx, goal, taskID, req.Role)
	}
	if a.Mem != nil {
		a.Mem.AddTurn("user", goal)
	}
	notify := func(phase, msg string, pct int, kind string) {
		a.Notifier.OnProgress(types.ProgressEvent{TaskID: taskID, Phase: phase, Message: msg, Progress: pct, Kind: kind})
	}

	cwd := a.workspaceOf(req)
	if iso := a.isolateForTask(ctx, req, taskID, notify); len(iso.roots) > 0 {
		ctx = toolutil.WithRoots(ctx, iso.roots)
		cwd = iso.cwd
	}

	planner := a.BuildPlanner(req)
	reflector := &Reflector{
		LLM:           a.VerifyLLM(req.Role),
		DoneThreshold: a.Cfg.Agent.DoneThreshold,
		TaskID:        taskID,
	}
	tell := Narrator{Style: a.Cfg.Persona.Style}

	var feedback string
	var last *types.GoalResult
	var usedTools []string
	var acceptance []string
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
		if stop := a.budgetCheckpoint(ctx, taskID, goal, nil); stop != nil {
			notify("budget", stop.Error, 88, "error")
			a.finalize(taskID, stop, nil, req.Role)
			return stop
		}
		notify("plan", tell.PlanStart(), 3, "info")
		if a.Cfg.Agent.ContextCompress && a.Mem != nil {
			if a.Mem.Compress(a.CompressSummarizer(ctx, taskID)) {
				notify("plan", tell.Compressed(), 4, "info")
			}
		}
		var recent []memory.Turn
		if a.Mem != nil && a.Mem.Short != nil {
			recent = a.Mem.Short.Recent(a.Mem.CarryTurns())
		}
		var relevant []memory.Hit
		if a.Mem != nil && a.Mem.Long != nil {
			relevant = a.Mem.Relevant(goal, 3)
		}
		var summary string
		if a.Mem != nil {
			summary = a.Mem.SummaryAnnotated()
		}
		var planChars int
		onDelta, flush := DeltaThrottle(50, func(text string) {
			planChars += len([]rune(text))
			notify("plan", fmt.Sprintf("规划中…（已生成 %d 字）", planChars), 5, "llm")
		})
		planner.OnLLMDelta = onDelta
		planner.ForceTools = usedTools
		plan, err := planner.Plan(ctx, goal, cwd, recent, relevant, summary, feedback)
		flush()
		if err != nil {
			if ctx.Err() != nil {
				return a.cancelledResult(goal, last)
			}
			if attempt == attempts || strings.Contains(err.Error(), "llm:") {
				notify("plan", "规划未成功，尝试直接回答…", 20, "warn")
				last = &types.GoalResult{Goal: goal, Status: types.GoalFailed, Error: err.Error(), Score: 0}
				break
			}
			feedback = "上一次计划无效，请修正：" + err.Error()
			notify("plan", "计划校验未通过，正在重新规划…", 5, "warn")
			continue
		}
		notify("plan", tell.PlanDone(len(plan.Steps)), 8, "info")
		planned = true
		if len(acceptance) == 0 {
			acceptance = plan.Acceptance
		}
		plan.Acceptance = acceptance
		for _, st := range plan.Steps {
			usedTools = appendUnique(usedTools, st.Tool)
		}

		preApproved := false
		if mode == string(types.ModePlanFirst) {
			resp := a.Notifier.OnApproval(types.ApprovalRequest{
				TaskID: taskID, Plan: planDescriptions(plan), WholePlan: true,
				Risk: planMaxRisk(a.Reg, plan), Reason: "plan_first 模式：执行前需要确认完整计划",
			})
			if !resp.Approved {
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

		executor := &Executor{
			Reg: a.Reg, Gate: a.Gate, Notifier: a.Notifier,
			MaxConcurrency: a.Cfg.Agent.MaxConcurrency,
			StepTimeout:    time.Duration(a.Cfg.Agent.StepTimeoutSecs) * time.Second,
			StepRetries:    a.Cfg.Agent.StepRetries,
			Dedupe:         a.Cfg.Agent.DedupeCalls,
			MaxOutputRunes: a.Cfg.Agent.MaxOutputRunes,
			DataDir:        a.Cfg.DataDir,
			OnUsage:        a.BumpUsage,
			Sink:           NewRunLog(a.Cfg.DataDir),
			Goal:           goal,
			Reviewer:       a.reviewer(),
		}
		if a.Cfg.Safety.DeepReview {
			if ok, reason := a.deepReviewPlan(ctx, taskID, goal, &plan); !ok && !a.confirmDeepReview(taskID, reason) {
				res := &types.GoalResult{
					Goal: goal, Status: types.GoalFailed, Score: 0,
					Error: "深度扫描未通过：" + reason,
				}
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

		if stop := a.budgetCheckpoint(ctx, taskID, goal, exec.Steps); stop != nil {
			notify("budget", stop.Error, 92, "error")
			a.finalize(taskID, stop, &plan, req.Role)
			return stop
		}

		if compressed, tightened := a.compressMidTask(ctx, taskID); compressed || tightened {
			notify("context", a.contextActionText(compressed, tightened, true), 92, "info")
		}

		notify("reflect", tell.ReflectStart(), 92, "info")
		reflector.Acceptance = plan.Acceptance
		reflection := reflector.Evaluate(ctx, goal, exec, attempt)
		result := a.buildResult(goal, exec, reflection, plan.Acceptance, planner.LastBreakdown, taskID)
		last = result

		if completionReached(reflection, plan.Acceptance, exec, a.Cfg.Agent.DoneThreshold) {
			if prog != nil && attempt == 1 {
				prog.firstRound = true
			}
			result.Status = statusOfExec(exec)
			if reflection.Suggestion != "" {
				result.Suggestion = reflection.Suggestion
			}
			a.finalize(taskID, result, &plan, req.Role)
			return result
		}
		if fp, rounds := a.noteSteps(taskID, exec.Steps); fp != "" {
			stuckMsg := fmt.Sprintf("检测到原地打转：%s 已重复执行 %d 轮", describeFingerprint(fp), rounds)
			notify("reflect", "⚠ "+stuckMsg+"，停止自动重规划", 90, "warn")
			result.Status = statusOfExec(exec)
			if result.Status == types.GoalRunning || result.Status == types.GoalSuccess {
				result.Status = types.GoalFailed
			}
			result.Error = stuckMsg + "。继续重规划大概率还是同样结果。"
			result.Suggestion = "建议把目标说得更具体（例如指定要处理的文件或期望的输出格式），或补充缺少的信息后重试；也可以切换到「对话」模式先对齐需求。"
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
			a.finalize(taskID, result, &plan, req.Role)
			return result
		}

		if prog != nil {
			prog.reworks++
		}
		feedback = fmt.Sprintf("完成度 %d/100。问题：%s\n\n上次执行的情况（针对它们改，不要原样重来）：\n%s",
			reflection.Score, reflection.Reason, buildStepDigest(exec))
		notify("reflect", tell.Replan(reflection.Score), 90, "warn")
	}
	if !planned && (last == nil || last.Status == types.GoalFailed) {
		notify("plan", "多次规划未成功，切换直接回答…", 20, "warn")
		res := a.runChatPath(ctx, goal, taskID, req.Role)
		if res.Status == types.GoalSuccess {
			res.Score = 70
			res.Suggestion = "这个问题更适合「对话」模式，切换后响应更快"
		}
		return res
	}
	return a.cancelledResult(goal, last)
}

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
	sys := fmt.Sprintf("你是 %s（微光），运行在用户本机的桌面智能体，当前对话模式。"+
		"用与用户一致的语言自然对话：回答简洁、具体、可执行；涉及本机文件或命令的操作，"+
		"提示切换到「工作」或「编程」模式提交。%s", name, styleInstruction(a.Cfg.Persona.Style))
	var volatile strings.Builder
	fmt.Fprintf(&volatile, "\n\n## 当前环境\n工作目录：%s", a.Cfg.Workspace)
	if a.Mem != nil {
		if s := a.Mem.Summary(); strings.TrimSpace(s) != "" {
			volatile.WriteString("\n\n## 早期上下文摘要\n" + types.Shorten(s, 400))
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
	if a.shouldChatCheck(goal, text) {
		notify("chat", "核对回答是否达成目标…", 95, "info")
	}
	res := &types.GoalResult{Goal: goal, Status: types.GoalSuccess, Score: 100, Summary: text}
	criteria, checks, outcome := a.chatSelfCheck(ctx, goal, text, taskID)
	if outcome == chatCheckDone {
		applyChatCheck(res, criteria, checks)
		if res.Status != types.GoalSuccess {
			notify("chat", "自检未全过："+res.Error, 100, "warn")
		}
	} else if outcome == chatCheckUnusable {
		notify("chat", "对话自检未能完成，本次回答未经核对", 100, "warn")
	}
	notify("chat", "已回复", 100, "info")
	a.finalize(taskID, res, nil, role)
	return res
}

func (a *Agent) finalize(taskID string, result *types.GoalResult, plan *types.Plan, role string) {
	var acceptance []string
	if plan != nil {
		acceptance = plan.Acceptance
	}
	applyAcceptanceVerdict(result, acceptance)
	if result.Summary != "" && a.Mem != nil {
		a.Mem.AddTurn("assistant", result.Summary)
	}
	if a.compressionOn() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		go func() {
			defer cancelC()
			if compressed, tightened := a.compressAfterTask(ctxC, taskID); compressed || tightened {
				a.notifyContextAction(compressed, tightened, false)
			}
		}()
	}
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
	if a.shouldAnalyzeGEO(role, result) {
		a.analyzeGEOAsync(taskID, result.Goal, result.Summary)
	}
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

const minGEOContentRunes = 80

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

func (a *Agent) CompressSummarizer(ctx context.Context, taskID ...string) func(string) (string, error) {
	var owner string
	if len(taskID) > 0 {
		owner = taskID[0]
	}
	return func(overflow string) (string, error) {
		sys := "你是 Gleam 的上下文压缩器。" + llm.MarkerCompress + "\n\n" +
			"接下来是一条包含早期对话记录的消息。请用不超过 200 字的中文摘要，保留对后续任务仍有价值的信息：用户偏好、项目背景、已做的决定、遗留问题。只输出摘要正文。"
		req := llm.ChatRequest{System: sys, Messages: []llm.Message{{Role: llm.RoleUser, Content: overflow}}, Temperature: 0.2, TaskID: owner}
		return a.HelperLLM().Chat(ctx, req)
	}
}

func (a *Agent) autoOptimizeSkill(ctx context.Context, taskID, name string, sk *skill.Skill, exec *Result) {
	if a.LLM == nil || llm.IsMockClient(a.LLM) || !a.Cfg.Agent.SkillAutoOptimize {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## 技能 %s（当前 v%d）运行失败\n目标：%s\n\n## 现有步骤\n", name, sk.Version, sk.Description)
	for _, st := range sk.Steps {
		args, _ := json.Marshal(st.Args)
		fmt.Fprintf(&b, "- {\"id\":%q,\"description\":%q,\"tool\":%q,\"args\":%s,\"depends_on\":%s}\n",
			st.ID, st.Description, st.Tool, args, stepDepsJSON(st.DependsOn))
	}
	fmt.Fprintf(&b, "\n## 失败详情\n")
	for _, r := range exec.Steps {
		if r.Status == types.StepFailed {
			fmt.Fprintf(&b, "- [%s] %s: %s\n", r.StepID, r.Tool, types.Shorten(r.Error, 160))
		}
	}
	sys := strings.Join([]string{
		"你是 Gleam 的技能优化器。" + llm.MarkerSkillFix,
		"",
		b.String(),
		`
## 输出（只输出严格 JSON，不要 markdown 代码块）
{"steps":[{"id":"s1","description":"一句话说明","tool":"工具名","args":{},"depends_on":[]}]}

针对失败原因修订：调整参数取值、修正步骤引用（"$ref:s2"、"{ref:s2}"）、或删除多余步骤。
只能使用原技能中出现过的工具，保持技能语义不变。`,
	}, "\n")
	req := llm.ChatRequest{System: sys, Messages: []llm.Message{{Role: llm.RoleUser, Content: "请输出修订后的步骤。"}}, Temperature: 0.2, TaskID: taskID}
	text, err := a.HelperLLM().Chat(ctx, req)
	if err != nil {
		return
	}
	raw, err := extractJSON(text)
	if err != nil {
		return
	}
	validator := &Planner{LLM: a.LLM, Reg: a.Reg, MaxSteps: a.Cfg.Agent.MaxSteps}
	plan, err := validator.validate(raw, "技能 "+name)
	if err != nil || len(plan.Steps) == 0 {
		return
	}
	optimized := *sk
	optimized.Steps = plan.Steps
	if v, err := a.SkillSave(optimized); err == nil {
		a.Notifier.OnProgress(types.ProgressEvent{
			TaskID: taskID, Phase: "reflect",
			Message:  fmt.Sprintf("技能 %s 运行失败，已自动优化参数并保存为 v%d，可重试", name, v),
			Progress: 90, Kind: "warn",
		})
	}
}

func stepDepsJSON(deps []string) string {
	if len(deps) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(deps)
	return string(b)
}

func (a *Agent) buildResult(goal string, exec *Result, refl types.Reflection, acceptance []string, bd types.ContextBreakdown, taskID string) *types.GoalResult {
	var dataDir string
	if a != nil && a.Cfg != nil {
		dataDir = a.Cfg.DataDir
	}
	res := &types.GoalResult{
		Goal:             goal,
		Score:            refl.Score,
		Steps:            exec.Steps,
		Acceptance:       acceptance,
		Checks:           refl.Checks,
		ExecutedPlan:     &exec.ExecutedPlan,
		PromptBreakdown:  &bd,
		FailureBreakdown: failureBreakdown(exec.Steps),
		Changes:          buildChanges(exec.Steps, exec.PreImages, changeCtx{DataDir: dataDir, TaskID: taskID}),
	}
	res.Summary = exec.ReplyText
	if res.Summary == "" {
		if exec.Succeeded == exec.Total && exec.Total > 0 {
			res.Summary = fmt.Sprintf("已完成全部 %d 个步骤。", exec.Total)
		} else {
			res.Summary = fmt.Sprintf("执行了 %d 个步骤：成功 %d，失败 %d，跳过 %d。%s",
				exec.Total, exec.Succeeded, exec.Failed, exec.Skipped, refl.Reason)
		}
	}
	if refl.Verdict == "failed" {
		res.Status = types.GoalFailed
	}
	return res
}

func statusOfExec(exec *Result) types.GoalStatus {
	switch {
	case exec.Cancelled && exec.Succeeded == 0:
		return types.GoalCancelled
	case exec.Succeeded == exec.Total && exec.Total > 0:
		return types.GoalSuccess
	case exec.Succeeded == 0:
		return types.GoalFailed
	default:
		return types.GoalPartial
	}
}

func failureBreakdown(steps []types.StepResult) map[types.ErrorKind]int {
	var out map[types.ErrorKind]int
	for _, st := range steps {
		if st.Status != types.StepFailed && st.Outcome != types.OutcomeFailed {
			continue
		}
		kind := st.ErrorKind
		if kind == "" {
			kind = types.ErrUnknown
		}
		if out == nil {
			out = map[types.ErrorKind]int{}
		}
		out[kind]++
	}
	return out
}

func deriveTraceID(parts ...string) string {
	h := fnv.New64a()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0x1f})
	}
	return fmt.Sprintf("%010x", h.Sum64()&0xFFFFFFFFFF)
}

func completionReached(refl types.Reflection, acceptance []string, exec *Result, doneThreshold int) bool {
	if refl.Verdict != "done" && refl.Score < doneThreshold {
		return false
	}
	if !acceptanceSatisfied(acceptance, refl.Checks) {
		return false
	}
	var steps []types.StepResult
	if exec != nil {
		steps = exec.Steps
	}
	return artifactsSatisfied(verifyArtifacts(steps))
}

func acceptanceSatisfied(acceptance []string, checks []types.CheckResult) bool {
	if len(acceptance) == 0 {
		return true
	}
	if len(checks) == 0 {
		return false
	}
	for _, c := range checks {
		if !c.Passed {
			return false
		}
	}
	return true
}

func applyAcceptanceVerdict(res *types.GoalResult, acceptance []string) {
	if res == nil || res.Status != types.GoalSuccess {
		return
	}
	if bad := failedArtifacts(verifyArtifacts(res.Steps)); len(bad) > 0 {
		res.Status = types.GoalPartial
		if res.Error == "" {
			res.Error = artifactFailureNote(bad) + "。步骤自述成功，但代码实测对不上。"
		}
		return
	}
	if len(acceptance) == 0 {
		return
	}
	if len(res.Checks) == 0 {
		res.Status = types.GoalPartial
		if res.Error == "" {
			res.Error = fmt.Sprintf("验收标准（%d 条）未能核对：反思器不可用或输出无法解析，无法确认是否达标",
				len(acceptance))
		}
		return
	}
	missed := missedCriteria(res.Checks)
	if len(missed) == 0 {
		return
	}
	if countPassed(res.Checks) == 0 {
		res.Status = types.GoalFailed
	} else {
		res.Status = types.GoalPartial
	}
	if res.Error == "" {
		res.Error = fmt.Sprintf("验收未全部通过（%d/%d），未达标：%s",
			countPassed(res.Checks), len(res.Checks), strings.Join(missed, "；"))
	}
}

func (a *Agent) cancelledResult(goal string, last *types.GoalResult) *types.GoalResult {
	if last != nil {
		last.Status = types.GoalCancelled
		last.Error = "任务已取消"
		return last
	}
	return &types.GoalResult{Goal: goal, Status: types.GoalCancelled, Error: "任务已取消"}
}

func (a *Agent) resultOf(h *taskHandle) *types.GoalResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	if h.result != nil {
		cp := *h.result
		return &cp
	}
	return &types.GoalResult{TaskID: h.id, Goal: h.goal, Status: h.status, StartedAt: h.started}
}

func (a *Agent) workspaceOf(req types.GoalRequest) string {
	if v, ok := req.Context["cwd"].(string); ok && v != "" {
		return v
	}
	if a.Cfg != nil && a.Cfg.Workspace != "" {
		return a.Cfg.Workspace
	}
	cwd, _ := os.Getwd()
	return cwd
}

func normalizeMode(m string) string {
	switch m {
	case "plan_first":
		return string(types.ModePlanFirst)
	case "interactive":
		return string(types.ModeInteractive)
	default:
		return string(types.ModeAuto)
	}
}

func planDescriptions(plan types.Plan) []string {
	out := make([]string, 0, len(plan.Steps))
	for _, st := range plan.Steps {
		out = append(out, fmt.Sprintf("%s. %s（%s）", st.ID, st.Describe(), st.Tool))
	}
	return out
}

func planMaxRisk(reg *registry.Registry, plan types.Plan) string {
	risk := "low"
	for _, st := range plan.Steps {
		t, ok := reg.Get(st.Tool)
		if !ok {
			continue
		}
		switch t.Permission() {
		case types.PermissionFullAccess:
			return "high"
		case types.PermissionUserApproved:
			risk = "medium"
		}
	}
	return risk
}

func (a *Agent) RunSkill(ctx context.Context, name string, params map[string]string) (map[string]any, error) {
	if a.Skills == nil {
		return nil, fmt.Errorf("技能系统未启用")
	}
	sk, err := a.Skills.Get(name)
	if err != nil {
		return nil, err
	}
	if sk.Disabled {
		return nil, fmt.Errorf("技能 %q 已停用，请先在技能页启用它", name)
	}
	plan := types.Plan{Goal: "运行技能 " + name}
	for i, st := range sk.Steps {
		args, err := skill.SubstituteParams(st.Args, params)
		if err != nil {
			return nil, fmt.Errorf("技能参数替换失败（步骤 %s）: %w", st.ID, err)
		}
		plan.Steps = append(plan.Steps, types.Step{ID: st.ID, Description: st.Description, Tool: st.Tool, Args: args, DependsOn: st.DependsOn})
		_ = i
	}
	taskID := types.NewID()
	a.Notifier.OnProgress(types.ProgressEvent{TaskID: taskID, Phase: "execute", Message: fmt.Sprintf("运行技能 %s v%d", name, sk.Version), Progress: 10, Kind: "info"})
	executor := &Executor{
		Reg: a.Reg, Gate: a.Gate, Notifier: a.Notifier,
		MaxConcurrency: a.Cfg.Agent.MaxConcurrency,
		StepTimeout:    time.Duration(a.Cfg.Agent.StepTimeoutSecs) * time.Second,
		StepRetries:    a.Cfg.Agent.StepRetries,
		Dedupe:         a.Cfg.Agent.DedupeCalls,
		MaxOutputRunes: a.Cfg.Agent.MaxOutputRunes,
		DataDir:        a.Cfg.DataDir,
		OnUsage:        a.BumpUsage,
	}
	exec := executor.Execute(ctx, plan, taskID, string(types.ModeAuto), false)
	ok := exec.Failed == 0 && exec.Succeeded > 0
	if err := a.Skills.RecordRun(name, ok); err != nil {
		a.Notifier.OnProgress(types.ProgressEvent{
			TaskID: taskID, Phase: "reflect",
			Message:  fmt.Sprintf("技能 %s 的运行统计未写入（次数与成功率会偏少）: %v", name, err),
			Progress: 95, Kind: "warn",
		})
	}
	if ok && a.Growth != nil {
		a.Growth.Record(growth.Entry{
			Type: "skill_used", SkillName: name, Goal: "复用技能 " + name,
			Steps: exec.Succeeded,
		})
	}
	if !ok && !exec.Cancelled {
		a.autoOptimizeSkill(ctx, taskID, name, sk, exec)
	}
	result := a.buildResult("技能 "+name, exec, heuristicReflection(exec, "", len(plan.Acceptance) > 0), plan.Acceptance, types.ContextBreakdown{}, taskID)
	result.TaskID = taskID
	result.StartedAt = time.Now()
	result.FinishedAt = time.Now()
	result.Status = statusOfExec(exec)
	if result.Summary == "" {
		result.Summary = fmt.Sprintf("技能 %s 执行完成（成功 %d/%d）", name, exec.Succeeded, exec.Total)
	}
	return map[string]any{
		"task_id": taskID,
		"status":  string(result.Status),
		"score":   result.Score,
		"summary": result.Summary,
		"steps":   result.Steps,
	}, nil
}

const delegateTimeout = 60 * time.Second

func (a *Agent) Delegate(ctx context.Context, goal string, role string) (string, error) {
	dctx, cancel := context.WithTimeout(ctx, delegateTimeout)
	defer cancel()

	sys := "你是一个专注的子任务执行者。根据用户目标，直接给出完整、可独立使用的结果。\n" +
		"要求：\n" +
		"- 结果自包含，不依赖主对话上下文\n" +
		"- 如果目标涉及代码，给出可直接运行的代码\n" +
		"- 如果目标涉及分析，给出结论和关键数据\n" +
		"- 不要解释你在做什么，直接给结果"

	if role != "" && role != "general" {
		if r := FindRole(role); r.ID != "general" {
			sys = r.SystemPrompt + "\n\n" + sys
		}
	}

	req := llm.ChatRequest{
		System:      sys,
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: goal}},
		Temperature: 0.3,
		MaxTokens:   2000,
	}
	client := a.LLM
	if a.FastLLM != nil {
		client = a.FastLLM
	}
	text, err := client.Chat(dctx, req)
	if err != nil {
		return "", fmt.Errorf("委派调用失败: %w", err)
	}
	return text, nil
}

func (a *Agent) SetNotifier(n Notifier) {
	a.Notifier = n
}

func (a *Agent) ApprovalTimeout() time.Duration {
	if a.Gate == nil {
		return 5 * time.Minute
	}
	return a.Gate.ApprovalTimeoutDuration()
}

func (a *Agent) CancelTask(taskID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	h, ok := a.tasks[taskID]
	if !ok || h.cancel == nil {
		return false
	}
	h.cancel()
	return true
}

func (a *Agent) TaskStatus(taskID string) (types.GoalStatus, *types.GoalResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h, ok := a.tasks[taskID]
	if !ok {
		return "", nil
	}
	return h.status, h.result
}

func (a *Agent) RunningTasks() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, h := range a.tasks {
		if h.status == types.GoalRunning {
			n++
		}
	}
	return n
}

var slugStrip = regexp.MustCompile(`[\s\p{P}]+`)

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
	name := base
	for i := 2; ; i++ {
		if store != nil {
			if _, err := store.Get(name); err != nil {
				break
			}
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

func DataDir(cfg *config.Config) string {
	_ = os.MkdirAll(filepath.Join(cfg.DataDir, "memory"), 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.DataDir, "skills"), 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.DataDir, "tasks"), 0o755)
	return cfg.DataDir
}

func elapsedMs(d time.Duration) int64 {
	if ms := d.Milliseconds(); ms >= 1 {
		return ms
	}
	return 1
}
