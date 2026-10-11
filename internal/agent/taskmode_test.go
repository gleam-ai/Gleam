package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gleam/internal/llm"
	"gleam/pkg/types"
)

func TestRunGoal_ChatModeDirectAnswer(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		{Kind: "chat_mode", Texts: []string{"你好呀！我是 Gleam，你的桌面智能体。"}},
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{
		Goal:     "你好，介绍下你自己",
		TaskMode: types.TaskChat,
	})
	if res.Status != types.GoalSuccess {
		t.Fatalf("chat 模式应直接成功: %s %s", res.Status, res.Error)
	}
	if !strings.Contains(res.Summary, "Gleam") {
		t.Errorf("summary = %q", res.Summary)
	}
	if res.Score != 100 {
		t.Errorf("score = %d", res.Score)
	}
	for _, call := range f.llm.Calls {
		if strings.Contains(call.System, llm.MarkerPlan) {
			t.Fatal("chat 模式不应调用规划器")
		}
	}
	found := false
	for _, ev := range f.notify.progress {
		if ev.Phase == "chat" {
			found = true
		}
	}
	if !found {
		t.Error("应推送 chat 阶段进度")
	}
}

func TestRunGoal_WorkModeChatFallbackOnPlanFailures(t *testing.T) {
	f := newFixture(t, nil)
	for i := 0; i < 3; i++ {
		f.llm.Enqueue("plan", "抱歉，这不是 JSON")
	}
	f.llm.Enqueue("chat_mode", "（回落）你好呀，我是 Gleam。")

	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "你好，介绍下你自己"})
	if res.Status != types.GoalSuccess {
		t.Fatalf("规划屡败应回落直聊成功: %s %s", res.Status, res.Error)
	}
	if !strings.Contains(res.Summary, "Gleam") {
		t.Errorf("summary = %q", res.Summary)
	}
	if res.Score != 70 {
		t.Errorf("回落完成度应为 70, got %d", res.Score)
	}
	if !strings.Contains(res.Suggestion, "对话") {
		t.Errorf("应提示对话模式: %q", res.Suggestion)
	}
}

func TestRunGoal_PlannedFailureDoesNotFallback(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"失败","tool":"fail"}]}`),
		reflectScript(10, "failed", "工具必然失败"),
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "不可能任务"})
	if res.Status != types.GoalFailed {
		t.Fatalf("计划已产出的真实失败不应回落: %s", res.Status)
	}
}

func TestPlanner_CodeModeGuidance(t *testing.T) {
	m := llm.NewMock()
	m.Enqueue("plan", `{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"ok"}}]}`)
	p := &Planner{LLM: m, Reg: newTestRegistry(), TaskMode: "code"}
	if _, err := p.Plan(testCtx(), "g", "", nil, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	if len(m.Calls) == 0 || !strings.Contains(m.Calls[0].System, "编程模式准则") {
		t.Error("code 模式提示词应包含编程准则")
	}
	m2 := llm.NewMock()
	m2.Enqueue("plan", `{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"ok"}}]}`)
	p2 := &Planner{LLM: m2, Reg: newTestRegistry()}
	if _, err := p2.Plan(testCtx(), "g", "", nil, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m2.Calls[0].System, "编程模式准则") {
		t.Error("work 模式不应注入编程准则")
	}
}

func TestNormalizeTaskMode(t *testing.T) {
	if normalizeTaskMode("") != types.TaskWork || normalizeTaskMode("xxx") != types.TaskWork {
		t.Error("默认应为 work")
	}
	if normalizeTaskMode(types.TaskChat) != types.TaskChat || normalizeTaskMode(types.TaskCode) != types.TaskCode {
		t.Error("chat/code 应保留")
	}
}

func TestRunGoal_LLMCallFailureFallsBackWithRealError(t *testing.T) {
	f := newFixture(t, nil)
	f.llm.FailNextN(3, errors.New("llm: HTTP 401: {\"error\":{\"code\":\"1001\"}}"))
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "你好"})
	if res.Status != types.GoalFailed {
		t.Fatalf("401 下应失败: %s %s", res.Status, res.Error)
	}
	if !strings.Contains(res.Error, "401") {
		t.Errorf("错误应保留真实原因: %q", res.Error)
	}
	planCalls := 0
	for _, c := range f.llm.Calls {
		if strings.Contains(c.System, llm.MarkerPlan) {
			planCalls++
		}
	}
	if planCalls != 1 {
		t.Errorf("鉴权失败不应反复重试规划, plan calls = %d", planCalls)
	}
}

func TestRunGoal_LLMRecoverFallsBackSuccess(t *testing.T) {
	f := newFixture(t, nil)
	f.llm.FailNext(errors.New("llm: 网络抖动"))
	f.llm.Enqueue("chat_mode", "（回落）直接回答")
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "g"})
	if res.Status != types.GoalSuccess || !strings.Contains(res.Summary, "直接回答") {
		t.Fatalf("回落应成功: %s %q", res.Status, res.Summary)
	}
}
