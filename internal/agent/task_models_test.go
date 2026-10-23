package agent

import (
	"context"
	"testing"

	"gleam/internal/config"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

func TestRunChatPathUsesTaskModelSnapshot(t *testing.T) {
	old := llm.NewMock()
	replacement := llm.NewMock()
	a := &Agent{LLM: old, Cfg: config.Default(), Notifier: NopNotifier{}}
	ctx := withTaskModels(context.Background(), taskModels{tier: old, helper: old})

	// A settings save swaps the live client while the goal is running.
	a.LLM = replacement
	res := a.runChatPath(ctx, "你好", "task-models", "")
	if res.Status != types.GoalSuccess {
		t.Fatalf("chat failed: %+v", res)
	}
	if len(old.Calls) == 0 {
		t.Fatal("任务应继续使用启动时的客户端")
	}
	if len(replacement.Calls) != 0 {
		t.Fatalf("任务中途换用了新客户端: %d 次调用", len(replacement.Calls))
	}
}

func TestTaskLLMStaysOnGoalSnapshot(t *testing.T) {
	pinned := llm.NewMock()
	a := &Agent{LLM: pinned}
	if got := a.TaskLLM(context.Background()); got != pinned {
		t.Fatal("任务外应使用实时客户端")
	}
	ctx := withTaskModels(context.Background(), taskModels{tier: pinned})
	a.LLM = llm.NewMock()
	if got := a.TaskLLM(ctx); got != pinned {
		t.Fatal("任务内应使用启动时固定的客户端")
	}
}

func TestCompressSummarizerUsesTaskHelperSnapshot(t *testing.T) {
	old := llm.NewMock()
	replacement := llm.NewMock()
	a := &Agent{LLM: old, Cfg: config.Default()}
	ctx := withTaskModels(context.Background(), taskModels{tier: old, helper: old})
	a.LLM = replacement

	summarize := a.CompressSummarizer(ctx)
	if _, err := summarize("很长的早期对话"); err != nil {
		t.Fatal(err)
	}
	if len(old.Calls) != 1 || len(replacement.Calls) != 0 {
		t.Fatalf("压缩应使用任务快照: old=%d replacement=%d", len(old.Calls), len(replacement.Calls))
	}
}
