package agent

import (
	"context"
	"fmt"
	"time"

	"gleam/internal/llm"
	"gleam/pkg/types"
)

// Usage accounting (usage dashboard)

// initUsage establishes usage accounting for a task.
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

// budgetExtra reads the quota granted after user confirmation.
func (a *Agent) budgetExtra(taskID string) taskBudget {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	if b := a.budget[taskID]; b != nil {
		return *b
	}
	return taskBudget{}
}

// grantBudget doubles the budget quota after the user confirms continuation.
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

// taskStarted returns the task's start time.
func (a *Agent) taskStarted(taskID string) (time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if h := a.tasks[taskID]; h != nil {
		return h.started, true
	}
	return time.Time{}, false
}

// overBudget reports whether the task is over budget, returning the reason
// (empty means within budget). No limit applies when all budgets are 0.
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

// checkBudget asks the user for confirmation when over budget: approved, add
// one more round of quota and continue; rejected, stop.
func (a *Agent) checkBudget(ctx context.Context, taskID, reason string) bool {
	if a == nil || a.Notifier == nil {
		return false
	}
	resp := askApproval(ctx, a.Notifier, types.ApprovalRequest{
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

// confirmDeepReview puts the decision to the user after a deep-review veto:
// approved = I accept the risk, continue with the original plan. When nobody
// can be asked it lets the task through: deep review is "one extra human
// confirmation", not a new hard gate -- stalling the task on a reason the user
// cannot see is worse than one extra run.
func (a *Agent) confirmDeepReview(ctx context.Context, taskID, reason string) bool {
	if a == nil || a.Notifier == nil {
		return true
	}
	return askApproval(ctx, a.Notifier, types.ApprovalRequest{
		TaskID: taskID,
		Plan:   []string{"按原计划继续执行"},
		Risk:   "medium",
		Reason: "深度扫描觉得这段计划有问题：" + reason + "。仍要继续吗？",
	}).Approved
}

// reviewer builds the action reviewer: nil when review is off or no auxiliary
// model is configured (no interception).
func (a *Agent) reviewer() Reviewer {
	if a == nil || a.Cfg == nil || !a.Cfg.Safety.AIReview || a.FastLLM == nil {
		return nil
	}
	return &llmReviewer{LLM: a.FastLLM, TaskID: ""}
}

// Anti-spin (in-place spinning detection across replans)

// noteSteps records this round's actually executed step fingerprints,
// returning the fingerprint that reached the threshold and how many rounds it
// appeared in. A fingerprint counts once per round -- multiple results from
// deduplication are not counted twice. Only steps that actually ran count:
// steps skipped due to a failed dependency are not an attempt.
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

// describeFingerprint restores a "tool|args" fingerprint into a
// human-readable sentence.
func describeFingerprint(fp string) string {
	i := indexOf(fp, "|")
	if i < 0 {
		return fp
	}
	tool, args := fp[:i], fp[i+1:]
	if args == "" || args == "{}" || args == "null" {
		return tool
	}
	return tool + " " + types.Shorten(args, 120)
}

// indexOf is a thin wrapper around strings.Index, avoiding one more import in
// budget.go.
func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// budgetCheckpoint is the budget checkpoint: when over budget, ask the user
// through the approval channel whether to continue. nil means continue; non-nil
// means the task should be interrupted (user rejected or nobody to ask).
// Checkpoints sit at sequential points (before each planning round / before
// reflection) to avoid contending for the approval channel inside concurrent
// steps.
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
	if a.checkBudget(ctx, taskID, reason) {
		return nil
	}
	// Cancelling while the budget prompt is up is a stop, not a budget
	// rejection: the result the user sees must not blame the budget for it.
	if ctx.Err() != nil {
		return &types.GoalResult{
			Goal:   goal,
			Status: types.GoalCancelled,
			Error:  "任务已取消",
			Steps:  steps,
			Score:  0,
		}
	}
	return &types.GoalResult{
		Goal:   goal,
		Status: types.GoalFailed,
		Error:  "超出任务预算，已停止：" + reason,
		Steps:  steps,
		Score:  0,
	}
}

// installUsageHook registers the one-time global usage hook: claims this
// engine's calls by taskID.
func (a *Agent) installUsageHook() {
	a.hookOnce.Do(func() {
		llm.AddUsageHook(func(taskID, kind string, req llm.ChatRequest, text string, u llm.Usage) {
			a.addUsage(taskID, kind, u)
		})
	})
}

// addUsage accumulates one model call's usage; tasks not belonging to this
// engine (unregistered taskID) are ignored.
func (a *Agent) addUsage(taskID, kind string, u llm.Usage) {
	// Only planning and chat carry the conversation's context. Auxiliary calls
	// (verification, compression, connection tests) report a much smaller
	// prompt; letting them update the watermark made a 97% conversation read as
	// 10% and skipped the compaction it needed.
	switch kind {
	case "plan", "chat_mode":
		a.notePromptSize(u)
	}
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

// BumpUsage accumulates tool calls, retries and dedupe counts (called by the
// executor).
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

// usageOf returns a snapshot of the task's usage.
func (a *Agent) usageOf(taskID string) types.TaskUsage {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	if st := a.usage[taskID]; st != nil {
		return *st
	}
	return types.TaskUsage{}
}

// dropUsage releases the task's usage counters (called on eviction to avoid
// unbounded memory growth in long runs).
func (a *Agent) dropUsage(taskID string) {
	a.usageMu.Lock()
	delete(a.usage, taskID)
	delete(a.budget, taskID)
	delete(a.stuck, taskID)
	a.usageMu.Unlock()
}
