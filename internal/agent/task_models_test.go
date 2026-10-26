package agent

import (
	"context"
	"testing"

	"gleam/internal/config"
	"gleam/internal/llm"
	"gleam/internal/tools/std"
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

func TestDelegateUsesTaskHelperSnapshot(t *testing.T) {
	for _, fast := range []bool{false, true} {
		name := "main_fallback"
		if fast {
			name = "fast_model"
		}
		t.Run(name, func(t *testing.T) {
			old := llm.NewMock()
			old.Enqueue("chat", "pinned result")
			replacement := llm.NewMock()
			replacement.Enqueue("chat", "live result")
			a := &Agent{LLM: old, Notifier: NopNotifier{}}
			if fast {
				a.FastLLM = old
			}
			ctx := withTaskModels(context.Background(), a.snapshotTaskModels("general"))
			a.LLM = replacement
			if fast {
				a.FastLLM = replacement
			}

			out, err := std.NewAgentDelegate(a).Execute(ctx, map[string]any{"goal": "analyze independently", "role": "analyst"})
			if err != nil {
				t.Fatal(err)
			}
			if out.(map[string]any)["result"] != "pinned result" || len(old.Calls) != 1 || len(replacement.Calls) != 0 {
				t.Errorf("delegate used live settings: result=%v pinned_calls=%d live_calls=%d; want pinned result, 1, 0", out, len(old.Calls), len(replacement.Calls))
			}
		})
	}
}

func TestDelegateDirectWithoutNotifier(t *testing.T) {
	for _, fast := range []bool{false, true} {
		name := "main_fallback"
		if fast {
			name = "fast_model"
		}
		t.Run(name, func(t *testing.T) {
			client := llm.NewMock()
			client.Enqueue("chat", "direct result")
			a := &Agent{LLM: client}
			if fast {
				a.LLM = llm.NewMock()
				a.FastLLM = client
			}
			text, err := a.Delegate(context.Background(), "analyze independently", "")
			if err != nil || text != "direct result" {
				t.Fatalf("direct delegate: text=%q err=%v", text, err)
			}
			if len(client.Calls) != 1 || client.Calls[0].TaskID != "" {
				t.Fatalf("direct delegate calls=%+v, want one call without a task ID", client.Calls)
			}
			if fast && len(a.LLM.(*llm.MockClient).Calls) != 0 {
				t.Fatal("direct delegate used main model instead of live helper")
			}
		})
	}
}
