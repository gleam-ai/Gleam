package agent

import (
	"context"
	"strings"
	"testing"

	"gleam/internal/config"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

type stubLLM struct {
	reply string
	err   error
	seen  *llm.ChatRequest
}

func (s *stubLLM) Chat(_ context.Context, req llm.ChatRequest) (string, error) {
	if s.seen != nil {
		*s.seen = req
	}
	return s.reply, s.err
}
func (s *stubLLM) ChatStream(ctx context.Context, req llm.ChatRequest, _ func(string)) (string, error) {
	return s.Chat(ctx, req)
}
func (s *stubLLM) Name() string { return "stub" }

func planWith(steps ...string) *types.Plan {
	p := &types.Plan{Goal: "做点事"}
	for i, s := range steps {
		p.Steps = append(p.Steps, types.Step{ID: string(rune('a' + i)), Tool: s, Description: s})
	}
	return p
}

func TestDeepReviewPlan(t *testing.T) {
	ctx := context.Background()
	plan := planWith("file.read", "file.write")

	a := &Agent{LLM: &stubLLM{reply: `{"ok":true,"reason":"没问题"}`}}
	if ok, _ := a.deepReviewPlan(ctx, "t1", "整理一下", plan); !ok {
		t.Error("ok=true 应判通过")
	}

	a.LLM = &stubLLM{reply: `{"ok":false,"reason":"计划里在顺手删东西"}`}
	ok, reason := a.deepReviewPlan(ctx, "t1", "整理一下", plan)
	if ok || !strings.Contains(reason, "顺手删东西") {
		t.Errorf("应判否决并带回理由，got ok=%v reason=%q", ok, reason)
	}

	a.LLM = &stubLLM{reply: "我觉得还行"}
	if ok, reason := a.deepReviewPlan(ctx, "t1", "整理一下", plan); ok || reason == "" {
		t.Errorf("解析不了应交人工确认，got ok=%v reason=%q", ok, reason)
	}

	a.LLM = &stubLLM{err: context.DeadlineExceeded}
	if ok, _ := a.deepReviewPlan(ctx, "t1", "整理一下", plan); !ok {
		t.Error("模型不可用时不该阻断")
	}

	a.LLM = nil
	if ok, _ := a.deepReviewPlan(ctx, "t1", "整理一下", plan); !ok {
		t.Error("没有模型时应放行")
	}
	a.LLM = &stubLLM{reply: `{"ok":false,"reason":"x"}`}
	if ok, _ := a.deepReviewPlan(ctx, "t1", "整理一下", &types.Plan{}); !ok {
		t.Error("空计划不该判否决")
	}
}

func TestDeepReviewSendsSteps(t *testing.T) {
	var seen llm.ChatRequest
	a := &Agent{LLM: &stubLLM{reply: `{"ok":true}`, seen: &seen}}
	plan := planWith("file.read", "shell.exec")
	if _, _ = a.deepReviewPlan(context.Background(), "t9", "把日志清一下", plan); true {
	}
	body := seen.Messages[0].Content
	for _, want := range []string{"file.read", "shell.exec", "把日志清一下", "共 2 步"} {
		if !strings.Contains(body, want) {
			t.Errorf("审核请求里没有 %q：\n%s", want, body)
		}
	}
	if seen.TaskID != "t9" {
		t.Errorf("用量应归到任务上，TaskID = %q", seen.TaskID)
	}
}

func TestConfirmDeepReviewNoNotifier(t *testing.T) {
	a := &Agent{Cfg: config.Default()}
	if !a.confirmDeepReview("t1", "理由") {
		t.Error("没有 Notifier 时应放行")
	}
}
