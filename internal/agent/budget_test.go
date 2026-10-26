package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"gleam/internal/llm"
	"gleam/internal/tools/std"
	"gleam/pkg/types"
)

func TestBudget_ZeroMeansUnlimited(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"ok"}}]}`),
		reflectScript(92, "done", "完成"),
	})
	f.a.Cfg.Agent.MaxLLMCallsPerTask = 0
	f.a.Cfg.Agent.MaxTokensPerTask = 0
	f.a.Cfg.Agent.MaxTaskDurationSecs = 0
	f.a.initUsage("t-unlimited")
	f.a.addUsage("t-unlimited", "plan", llm.Usage{PromptTokens: 100, CompletionTokens: 100})
	if got := f.a.overBudget("t-unlimited"); got != "" {
		t.Fatalf("预算全为 0 时不应限流，实际：%s", got)
	}
}

func TestBudget_StopsOnCallLimit(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"ok"}}]}`),
		reflectScript(92, "done", "完成"),
	})
	f.a.Cfg.Agent.MaxLLMCallsPerTask = 1
	f.notify.approveAll = false

	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "统计一下目录", Mode: "auto"})
	if res.Status != types.GoalFailed {
		t.Fatalf("超预算且用户拒绝时应失败停止，实际 %s（%s）", res.Status, res.Error)
	}
	if !strings.Contains(res.Error, "超出任务预算") {
		t.Fatalf("错误信息应说明原因，实际：%s", res.Error)
	}
	found := false
	for _, req := range f.notify.approvals {
		if strings.Contains(req.Reason, "预算") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应通过审批通道询问用户是否继续，实际审批记录：%+v", f.notify.approvals)
	}
}

func TestBudget_GrantExtendsLimit(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.Agent.MaxLLMCallsPerTask = 2
	f.a.initUsage("t-grant")
	f.a.addUsage("t-grant", "plan", llm.Usage{PromptTokens: 10, CompletionTokens: 10})
	f.a.addUsage("t-grant", "plan", llm.Usage{PromptTokens: 10, CompletionTokens: 10})
	if got := f.a.overBudget("t-grant"); got == "" {
		t.Fatal("达到调用上限时应报告超限")
	}
	f.a.grantBudget("t-grant")
	if got := f.a.overBudget("t-grant"); got != "" {
		t.Fatalf("批准后应获得追加额度，仍超限：%s", got)
	}
	f.a.addUsage("t-grant", "plan", llm.Usage{PromptTokens: 10, CompletionTokens: 10})
	f.a.addUsage("t-grant", "plan", llm.Usage{PromptTokens: 10, CompletionTokens: 10})
	if got := f.a.overBudget("t-grant"); got == "" {
		t.Fatal("追加额度用尽后应再次报告超限")
	}
}

func TestBudget_TokenLimit(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.Agent.MaxTokensPerTask = 100
	f.a.initUsage("t-token")
	f.a.addUsage("t-token", "plan", llm.Usage{PromptTokens: 60, CompletionTokens: 30})
	if got := f.a.overBudget("t-token"); got != "" {
		t.Fatalf("90 < 100 不应超限，实际：%s", got)
	}
	f.a.addUsage("t-token", "plan", llm.Usage{PromptTokens: 20, CompletionTokens: 0})
	if got := f.a.overBudget("t-token"); got == "" {
		t.Fatal("累计 110 超过 100 应报告超限")
	}
}

func TestBudget_DurationLimit(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.Agent.MaxTaskDurationSecs = 1
	f.a.initUsage("t-dur")
	f.a.mu.Lock()
	f.a.tasks["t-dur"] = &taskHandle{id: "t-dur", started: time.Now().Add(-3 * time.Second)}
	f.a.mu.Unlock()
	if got := f.a.overBudget("t-dur"); got == "" {
		t.Fatal("运行时长超预算应报告超限")
	}
}

func TestBudget_ClearedWithTask(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.Agent.MaxLLMCallsPerTask = 1
	f.a.initUsage("t-drop")
	f.a.grantBudget("t-drop")
	f.a.dropUsage("t-drop")
	f.a.usageMu.Lock()
	_, left := f.a.budget["t-drop"]
	f.a.usageMu.Unlock()
	if left {
		t.Fatal("任务淘汰后预算记录应被清除")
	}
}

func TestDelegateUsageReachesParentBudget(t *testing.T) {
	for _, limit := range []string{"calls", "tokens"} {
		t.Run(limit, func(t *testing.T) {
			f := newFixture(t, []llm.Scripted{
				planScript(`{"steps":[{"id":"delegate","description":"independent analysis","tool":"agent.delegate","args":{"goal":"analyze independently"}}]}`),
				reflectScript(95, "done", "finished"),
			})
			f.a.Cfg.Agent.ContextCompress = false
			f.a.Cfg.Agent.GEOEnabled = false
			f.a.Cfg.Agent.MaxReplans = 0
			f.a.Cfg.Agent.MaxToolSchemas = 0
			f.a.Cfg.Agent.MaxLLMCallsPerTask = 0
			f.a.Cfg.Agent.MaxTokensPerTask = 0
			f.a.Cfg.Agent.MaxTaskDurationSecs = 0
			f.a.Cfg.Safety.AIReview = false
			f.a.Cfg.Safety.DeepReview = false
			if limit == "calls" {
				f.a.Cfg.Agent.MaxLLMCallsPerTask = 2
			} else {
				f.a.Cfg.Agent.MaxTokensPerTask = 100000
			}
			var delegated []llm.ChatRequest
			f.a.FastLLM = &lifecycleClient{chat: func(_ context.Context, req llm.ChatRequest) (string, error) {
				delegated = append(delegated, req)
				llm.ReportUsage(req, "independent result", llm.Usage{PromptTokens: 100000, CompletionTokens: 3})
				return "independent result", nil
			}}
			f.a.Reg.MustRegister(std.NewAgentDelegate(f.a))
			if !f.a.Gate.TrustForSession("agent.delegate") {
				t.Fatal("delegate test setup could not preapprove the tool")
			}
			taskID := types.NewID()
			res := f.a.RunGoal(context.Background(), types.GoalRequest{
				TaskID: taskID, Goal: "delegate an independent analysis", Mode: "auto", TaskMode: types.TaskWork,
			})
			if len(delegated) != 1 {
				t.Fatalf("delegate calls=%d, want 1; result=%+v", len(delegated), res)
			}
			if llm.KindOf(delegated[0].System) != "chat" || len(delegated[0].Messages) != 1 || delegated[0].Messages[0].Content != "analyze independently" {
				t.Fatalf("helper call was not the isolated delegate request: %+v", delegated[0])
			}
			if delegated[0].TaskID != taskID {
				t.Errorf("delegate TaskID=%q, want parent %q", delegated[0].TaskID, taskID)
			}
			u := f.a.usageOf(taskID)
			if want := len(f.llm.Calls) + len(delegated); u.LLMCalls != want {
				t.Errorf("parent LLMCalls=%d, want %d including delegate", u.LLMCalls, want)
			}
			if u.PromptTokens < 100000 || u.CompletionTokens < 3 {
				t.Errorf("parent usage lost delegate tokens: prompt=%d completion=%d, want at least 100000/3", u.PromptTokens, u.CompletionTokens)
			}
			reflections := 0
			for _, req := range f.llm.Calls {
				if llm.KindOf(req.System) == "reflect" {
					reflections++
				}
			}
			budgetApprovals := 0
			for _, req := range f.notify.approvals {
				if req.TaskID == taskID && strings.Contains(req.Reason, "预算") {
					budgetApprovals++
				}
			}
			if res.Status != types.GoalFailed || !strings.Contains(res.Error, "超出任务预算") || reflections != 0 || budgetApprovals != 1 {
				t.Errorf("delegate bypassed post-execute budget: status=%s reflections=%d budget_approvals=%d error=%q; want failed, 0, 1, budget error", res.Status, reflections, budgetApprovals, res.Error)
			}
		})
	}
}
