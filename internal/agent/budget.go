package agent

import (
	"context"
	"fmt"
	"time"

	"gleam/internal/llm"
	"gleam/pkg/types"
)

// ---------- 消耗统计（消耗看板） ----------

// initUsage 为任务建立用量计数。
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

// budgetExtra 读取用户确认后追加的额度。
func (a *Agent) budgetExtra(taskID string) taskBudget {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	if b := a.budget[taskID]; b != nil {
		return *b
	}
	return taskBudget{}
}

// grantBudget 用户确认继续后追加一倍预算额度。
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

// taskStarted 返回任务开始时间。
func (a *Agent) taskStarted(taskID string) (time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if h := a.tasks[taskID]; h != nil {
		return h.started, true
	}
	return time.Time{}, false
}

// overBudget 判断任务是否超出预算，返回超限原因（空表示未超限）。
// 预算全部为 0 时不做限制。
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

// checkBudget 超预算时征求用户确认：批准则追加一倍额度并继续，拒绝则停止。
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

// confirmDeepReview 深度扫描否决后交用户拍板：批准 = 我知道风险，按原计划继续。
// 没人可问时**放行**：深度扫描是"多一道人工确认"，不是新的硬闸——问不到人就把任务
// 卡死在一个用户看不见的原因上，比多跑一次更糟。
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

// reviewer 构造动作审核器：未开启审核或未配辅助模型时返回 nil（不拦截）。
func (a *Agent) reviewer() Reviewer {
	if a == nil || a.Cfg == nil || !a.Cfg.Safety.AIReview || a.FastLLM == nil {
		return nil
	}
	return &llmReviewer{LLM: a.FastLLM, TaskID: ""}
}

// ---------- 防打转（跨重规划的原地打转检测） ----------

// noteSteps 记录本轮真正执行过的步骤指纹，返回达到阈值的指纹与其出现轮数。
// 单轮内同一指纹只计一次——去重产生的多条结果不会被重复计数。
// 只统计真正跑过的步骤：因依赖失败而跳过的不算一次尝试。
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

// describeFingerprint 把「工具|参数」指纹还原成人能看懂的一句话。
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

// indexOf 是 strings.Index 的简单包装，避免在 budget.go 里多引一个包。
func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// budgetCheckpoint 预算检查点：超限则通过审批通道询问用户是否继续。
// 返回 nil 表示可以继续；非 nil 表示应当中断任务（用户拒绝或无法询问）。
// 检查点放在顺序执行的位置（每轮规划前 / 反思前），避免在并发步骤里争抢审批通道。
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

// installUsageHook 注册一次全局用量钩子：按 taskID 认领本引擎的调用。
func (a *Agent) installUsageHook() {
	a.hookOnce.Do(func() {
		llm.AddUsageHook(func(taskID, kind string, req llm.ChatRequest, text string, u llm.Usage) {
			a.addUsage(taskID, u)
		})
	})
}

// addUsage 累加一次模型调用用量；非本引擎的任务（taskID 未登记）直接忽略。
func (a *Agent) addUsage(taskID string, u llm.Usage) {
	// 先记窗口读数，再看 taskID：它问的是"当前上下文有多大"，跟哪次任务无关。
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

// BumpUsage 累加工具调用、重试与去重次数（由执行器调用）。
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

// usageOf 返回任务用量快照。
func (a *Agent) usageOf(taskID string) types.TaskUsage {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	if st := a.usage[taskID]; st != nil {
		return *st
	}
	return types.TaskUsage{}
}

// dropUsage 释放任务用量计数（任务淘汰时调用，避免长期运行内存增长）。
func (a *Agent) dropUsage(taskID string) {
	a.usageMu.Lock()
	delete(a.usage, taskID)
	delete(a.budget, taskID)
	delete(a.stuck, taskID)
	a.usageMu.Unlock()
}
