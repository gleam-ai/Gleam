package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"gleam/internal/config"
	"gleam/internal/harness/safety"
	"gleam/internal/llm"
	"gleam/internal/tools/std"
	"gleam/pkg/types"
)

func newSubtaskTestAgent(t *testing.T, client llm.Client) *Agent {
	t.Helper()
	r := newTestRegistry()
	return &Agent{
		LLM: client, Reg: r,
		Gate:     safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{},
		Cfg: &config.Config{
			DataDir: t.TempDir(),
			Agent:   config.AgentConfig{MaxSteps: 6, StepTimeoutSecs: 5, MaxConcurrency: 2},
		},
	}
}

const subtaskPlanJSON = `{"steps":[{"id":"s1","description":"调 counter","tool":"counter","args":{},"depends_on":[]}]}`

func TestDelegateSubtask_ExecutesToolsAndSummarizes(t *testing.T) {
	client := llm.NewMock()
	client.Enqueue("plan", subtaskPlanJSON)
	a := newSubtaskTestAgent(t, client)

	out, err := a.DelegateSubtask(context.Background(), "数一下", "")
	if err != nil {
		t.Fatal(err)
	}
	// Under the mock model the deterministic fallback runs: the answer must carry
	// the real tool output from counter.
	if !strings.Contains(out, "hello") || !strings.Contains(out, "42") {
		t.Errorf("子任务答案应包含工具真实产出，got %q", out)
	}
	if !strings.Contains(out, "1/1") {
		t.Errorf("应报告完成步数，got %q", out)
	}
	// The parent ctx depth stays clean: another answer-mode delegate is allowed.
	client.Enqueue("chat", "answer text")
	if _, err := a.Delegate(context.Background(), "x", ""); err != nil {
		t.Fatalf("父 ctx 深度被污染: %v", err)
	}
}

func TestDelegateSubtask_DepthCap(t *testing.T) {
	client := llm.NewMock()
	a := newSubtaskTestAgent(t, client)
	callsBefore := len(client.Calls)

	ctx := withDelegateDepth(context.Background(), MaxDelegateDepth)
	if _, err := a.DelegateSubtask(ctx, "too deep", ""); err == nil {
		t.Fatal("到达 MaxDelegateDepth 应拒绝")
	} else if !strings.Contains(err.Error(), "委派深度已达上限") {
		t.Errorf("错误应说明深度上限，got %v", err)
	}
	if len(client.Calls) != callsBefore {
		t.Errorf("拒绝后不应调用 LLM，calls %d -> %d", callsBefore, len(client.Calls))
	}
}

func TestDelegateSubtask_AllStepsFail(t *testing.T) {
	client := llm.NewMock()
	client.Enqueue("plan", `{"steps":[{"id":"s1","description":"炸","tool":"fail","args":{}}]}`)
	a := newSubtaskTestAgent(t, client)

	_, err := a.DelegateSubtask(context.Background(), "会失败", "")
	if err == nil {
		t.Fatal("全部步骤失败应返回错误")
	}
	if !strings.Contains(err.Error(), "fail") {
		t.Errorf("错误应包含失败步骤信息，got %v", err)
	}
}

func TestDelegateSubtask_NestedSubtaskDepthGrows(t *testing.T) {
	client := llm.NewMock()
	a := newSubtaskTestAgent(t, client)

	// Tools inside the subtask must see depth+1: verify with a probe tool.
	var seenDepth = -1
	a.Reg.MustRegister(&funcTool{name: "depth-probe", perm: types.PermissionReadOnly,
		fn: func(ctx context.Context, args map[string]any) (any, error) {
			seenDepth = delegateDepthFrom(ctx)
			return "ok", nil
		}})
	client.Enqueue("plan", `{"steps":[{"id":"s1","description":"探深度","tool":"depth-probe","args":{}}]}`)
	if _, err := a.DelegateSubtask(context.Background(), "探", ""); err != nil {
		t.Fatal(err)
	}
	if seenDepth != 1 {
		t.Errorf("子任务内深度 = %d, want 1", seenDepth)
	}
}

// Tool-level mode routing: mode=task must really reach DelegateSubtask.
func TestAgentDelegateTool_TaskModeRouting(t *testing.T) {
	client := llm.NewMock()
	client.Enqueue("plan", subtaskPlanJSON)
	a := newSubtaskTestAgent(t, client)
	tool := std.NewAgentDelegate(a)

	out, err := tool.Execute(context.Background(), map[string]any{"goal": "数一下", "mode": "task"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["mode"] != "task" {
		t.Errorf("mode = %v, want task", m["mode"])
	}
	if !strings.Contains(m["result"].(string), "42") {
		t.Errorf("task 模式应返回执行结果，got %v", m["result"])
	}

	// The answer mode (default) stays a single call and never touches the executor.
	client.Enqueue("chat", "plain answer")
	out, err = tool.Execute(context.Background(), map[string]any{"goal": "直接答"})
	if err != nil {
		t.Fatal(err)
	}
	if m := out.(map[string]any); m["mode"] != "answer" {
		t.Errorf("mode = %v, want answer", m["mode"])
	}

	// An unknown mode fails loudly instead of silently degrading.
	if _, err := tool.Execute(context.Background(), map[string]any{"goal": "x", "mode": "yolo"}); err == nil {
		t.Fatal("未知 mode 应报错")
	}
}

func TestSubtaskStepCap(t *testing.T) {
	if subtaskMaxSteps > config.DefaultMaxSteps {
		t.Errorf("subtaskMaxSteps=%d 不应超过全局上限 %d", subtaskMaxSteps, config.DefaultMaxSteps)
	}
}
