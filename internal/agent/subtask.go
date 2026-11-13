package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gleam/internal/llm"
	"gleam/pkg/types"
)

// subtaskMaxSteps caps a subtask's plan size: it exists to do one concrete
// piece of work for the main task, not to be a task of its own. More than 6
// steps means the goal belongs in the main task, or should be split across
// several mode=answer delegate calls.
const subtaskMaxSteps = 6

// subtaskTimeout is the hard ceiling for a subtask: the longest wait across
// plan + execute + summarize. The answer mode gets 60s (a single call);
// task mode really runs tools, so it gets 5 minutes.
const subtaskTimeout = 5 * time.Minute

// DelegateSubtask is the task mode of agent.delegate: the subtask runs on
// its own -- plan the steps first, actually call tools through the Executor
// (safety gate applies per tool), then fold everything into one text the
// main task can consume.
//
// Deliberately lighter than RunGoal:
//   - no task slot (prepareGoal's idle mutex does not apply: the subtask
//     happens inside the running main task);
//   - no conversation writes, no worktree, no deep review -- side effects
//     belong to the main task;
//   - budget is bounded by step count / timeout only; nesting depth is
//     still capped by MaxDelegateDepth.
func (a *Agent) DelegateSubtask(ctx context.Context, goal, role string) (string, error) {
	depth := delegateDepthFrom(ctx)
	if depth >= MaxDelegateDepth {
		return "", fmt.Errorf("委派深度已达上限 %d，请直接在主任务中完成子目标", MaxDelegateDepth)
	}
	taskID := taskIDFrom(ctx)
	dctx, cancel := context.WithTimeout(withDelegateDepth(ctx, depth+1), subtaskTimeout)
	defer cancel()

	roleLabel := ""
	if role != "" && role != "general" {
		if r := FindRole(role); r.ID != "general" {
			roleLabel = r.ID
		}
	}
	a.notifySubtask(taskID, "子任务开始"+roleLabel+"："+types.Shorten(goal, 60))

	// Plan: give the subtask a bounded plan (step count capped).
	cfg := a.Config()
	planner := &Planner{
		LLM:      a.taskTierLLM(dctx, role),
		Reg:      a.Reg,
		MaxSteps: min(cfg.Agent.MaxSteps, subtaskMaxSteps),
		Role:     role,
		TaskID:   taskID,
	}
	plan, err := planner.Plan(dctx, goal, cfg.Workspace, nil, nil, "", "")
	if err != nil {
		return "", fmt.Errorf("子任务规划失败: %w", err)
	}
	a.notifySubtask(taskID, fmt.Sprintf("子任务计划 %d 步，开始执行", len(plan.Steps)))

	// Execute: the same Executor chain as RunGoal -- gate, audit and spill
	// all stay in effect.
	executor := &Executor{
		Reg: a.Reg, Gate: a.Gate, Notifier: a.Notifier,
		MaxConcurrency: cfg.Agent.MaxConcurrency,
		StepTimeout:    time.Duration(cfg.Agent.StepTimeoutSecs) * time.Second,
		StepRetries:    cfg.Agent.StepRetries,
		Dedupe:         cfg.Agent.DedupeCalls,
		MaxOutputRunes: cfg.Agent.MaxOutputRunes,
		DataDir:        cfg.DataDir,
		OnUsage:        a.BumpUsage,
		Goal:           "子任务：" + types.Shorten(goal, 40),
		Hooks:          a.Hooks,
	}
	exec := executor.Execute(dctx, plan, taskID, string(types.ModeAuto), false)
	if exec.Cancelled {
		return "", fmt.Errorf("子任务已取消")
	}
	if exec.Succeeded == 0 && exec.Failed > 0 {
		return "", fmt.Errorf("子任务全部步骤失败：%s", subtaskFailureSummary(exec))
	}

	// Summarize: compress the step outputs into one self-contained answer.
	return a.summarizeSubtask(dctx, taskID, goal, role, exec), nil
}

func (a *Agent) notifySubtask(taskID, message string) {
	if a.Notifier == nil {
		return
	}
	a.Notifier.OnProgress(types.ProgressEvent{
		TaskID: taskID, Phase: "delegate", Message: message, Progress: 55, Kind: "info",
	})
}

func subtaskFailureSummary(exec *Result) string {
	var b strings.Builder
	for i, r := range exec.Steps {
		if i >= 3 {
			fmt.Fprintf(&b, "…（其余 %d 步略）", len(exec.Steps)-i)
			break
		}
		fmt.Fprintf(&b, "[%s] %s: %s；", r.StepID, r.Tool, types.Shorten(r.Error, 120))
	}
	return b.String()
}

// summarizeSubtask prefers a model to weave the step outputs into an answer;
// under the mock model it degrades to deterministic output joining -- offline
// self-tests must be reproducible and cannot depend on model phrasing.
func (a *Agent) summarizeSubtask(ctx context.Context, taskID, goal, role string, exec *Result) string {
	budget := a.Config().Agent.MaxOutputRunes
	var b strings.Builder
	fmt.Fprintf(&b, "子任务目标：%s\n\n各步骤产出：\n", goal)
	for _, r := range exec.Steps {
		switch r.Status {
		case types.StepSucceeded:
			fmt.Fprintf(&b, "- %s：成功\n%s\n", r.Tool, fitSubtaskOutput(r.Output, budget))
		case types.StepFailed:
			fmt.Fprintf(&b, "- %s：失败 %s\n", r.Tool, types.Shorten(oneLineOf(r.Error), 200))
		default:
			fmt.Fprintf(&b, "- %s：%s（未产出）\n", r.Tool, r.Status)
		}
	}
	facts := b.String()

	tier := a.taskTierLLM(ctx, role)
	if tier == nil || llm.IsMockClient(tier) {
		return deterministicSubtaskAnswer(goal, exec, facts, budget)
	}
	sys := "你是子任务汇总器。根据下面的步骤产出，回答子任务目标。要求：自包含、直接给结论、" +
		"不要复述步骤流水账。若产出不足以回答目标，明确说缺什么。"
	req := llm.ChatRequest{
		System:      sys,
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: facts}},
		Temperature: 0.2,
		MaxTokens:   1500,
		TaskID:      taskID,
	}
	text, err := tier.Chat(ctx, req)
	if err != nil || strings.TrimSpace(text) == "" {
		return deterministicSubtaskAnswer(goal, exec, facts, budget)
	}
	return strings.TrimSpace(text)
}

// deterministicSubtaskAnswer is the no-model fallback: the raw output of the
// successful steps, capped at the Executor's own output budget. The caller
// gets information equivalent to the model-written summary, just without the
// polish.
func deterministicSubtaskAnswer(goal string, exec *Result, facts string, budget int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "子任务「%s」完成（成功 %d/%d 步）。\n\n", types.Shorten(goal, 60), exec.Succeeded, exec.Total)
	for _, r := range exec.Steps {
		if r.Status != types.StepSucceeded {
			continue
		}
		if out := strings.TrimSpace(fitSubtaskOutput(r.Output, budget)); out != "" {
			fmt.Fprintf(&b, "%s\n", out)
		}
	}
	if b.Len() == 0 {
		return facts
	}
	return strings.TrimSpace(b.String())
}

// fitSubtaskOutput renders a successful step's output for the summary, capped
// at the Executor's own MaxOutputRunes budget. The Executor already truncated
// (and spilled) each step to that budget, so re-cutting to a small fixed size
// here would drop the middle layers of a multi-file trace and their line
// numbers. max <= 0 means the Executor kept the full output, so the summary
// keeps it too. Newlines are preserved so file:line structure survives.
func fitSubtaskOutput(v any, max int) string {
	s := textOf(v)
	if max > 0 {
		return types.Shorten(s, max)
	}
	return s
}

// textOf renders a step output or error as text, preserving newlines.
func textOf(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case error:
		return t.Error()
	default:
		return fmt.Sprintf("%v", t)
	}
}

// oneLineOf flattens a step output into single-line text for compact error
// summaries.
func oneLineOf(v any) string {
	return strings.ReplaceAll(textOf(v), "\n", " ")
}
