package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"gleam/internal/config"
	"gleam/internal/harness/safety"
	"gleam/pkg/types"
)

func hookTestRunner(t *testing.T, hooks []config.HookConfig, exec func(ctx context.Context, command string, stdin []byte) (int, string, error)) *HookRunner {
	t.Helper()
	h := NewHookRunner(hooks)
	if h == nil {
		t.Fatal("有钩子时 NewHookRunner 不应返回 nil")
	}
	h.Exec = exec
	return h
}

func TestHookRunner_PreBlocksOnExit2(t *testing.T) {
	var gotPayload string
	h := hookTestRunner(t, []config.HookConfig{{Event: "pre_tool", Command: "guard"}}, func(_ context.Context, cmd string, stdin []byte) (int, string, error) {
		gotPayload = string(stdin)
		return 2, "禁止删除文件", nil
	})
	reason := h.PreTool(context.Background(), "file.delete", map[string]any{"path": "x"})
	if reason == "" {
		t.Fatal("exit 2 应阻断")
	}
	if !strings.Contains(reason, "禁止删除文件") {
		t.Errorf("阻断原因应来自钩子输出，got %q", reason)
	}
	if !strings.Contains(gotPayload, `"event":"pre_tool"`) || !strings.Contains(gotPayload, `"tool":"file.delete"`) {
		t.Errorf("载荷应含 event/tool，got %s", gotPayload)
	}
}

func TestHookRunner_PreAllowsOnExit0AndOthers(t *testing.T) {
	h := hookTestRunner(t, []config.HookConfig{{Event: "pre_tool", Command: "ok"}}, func(_ context.Context, _ string, _ []byte) (int, string, error) {
		return 0, "", nil
	})
	if reason := h.PreTool(context.Background(), "file.read", nil); reason != "" {
		t.Errorf("exit 0 应放行，got %q", reason)
	}

	// Exit 1 = the hook itself is broken: non-blocking, warning only.
	h2 := hookTestRunner(t, []config.HookConfig{{Event: "pre_tool", Command: "bug"}}, func(_ context.Context, _ string, _ []byte) (int, string, error) {
		return 1, "hook exploded", nil
	})
	if reason := h2.PreTool(context.Background(), "file.read", nil); reason != "" {
		t.Errorf("非 0/2 退出码不应阻断，got %q", reason)
	}

	// Execution failure (spawn error or timeout): non-blocking.
	h3 := hookTestRunner(t, []config.HookConfig{{Event: "pre_tool", Command: "ghost"}}, func(_ context.Context, _ string, _ []byte) (int, string, error) {
		return -1, "", errors.New("executable not found")
	})
	if reason := h3.PreTool(context.Background(), "file.read", nil); reason != "" {
		t.Errorf("执行异常不应阻断，got %q", reason)
	}
}

func TestHookRunner_ToolFilterAndEventFilter(t *testing.T) {
	called := 0
	h := hookTestRunner(t, []config.HookConfig{
		{Event: "pre_tool", Tools: []string{"shell.exec"}, Command: "only-shell"},
	}, func(_ context.Context, _ string, _ []byte) (int, string, error) {
		called++
		return 2, "blocked", nil
	})
	if reason := h.PreTool(context.Background(), "file.read", nil); reason != "" {
		t.Errorf("不匹配的工具不应触发钩子，got %q", reason)
	}
	if reason := h.PreTool(context.Background(), "shell.exec", nil); reason == "" {
		t.Error("匹配的工具应触发钩子")
	}
	if called != 1 {
		t.Errorf("钩子调用次数 = %d, want 1", called)
	}
}

func TestHookRunner_PostNeverAffectsResult(t *testing.T) {
	h := hookTestRunner(t, []config.HookConfig{{Event: "post_tool", Command: "notify"}}, func(_ context.Context, _ string, _ []byte) (int, string, error) {
		return 2, "post hook exit 2", nil
	})
	// post_tool must never panic or carry blocking semantics, whatever the exit code.
	h.PostTool(context.Background(), "shell.exec", nil, false, "boom")
}

func TestHookRunner_TimeoutDoesNotBlock(t *testing.T) {
	h := hookTestRunner(t, []config.HookConfig{{Event: "pre_tool", Command: "hang"}}, func(ctx context.Context, _ string, _ []byte) (int, string, error) {
		<-ctx.Done() // simulate a hang: wait for the ctx deadline
		return -1, "", ctx.Err()
	})
	h.Timeout = 50 * time.Millisecond
	if reason := h.PreTool(context.Background(), "file.read", nil); reason != "" {
		t.Errorf("超时不应阻断，got %q", reason)
	}
}

func TestNewHookRunner_EmptyIsNil(t *testing.T) {
	if NewHookRunner(nil) != nil {
		t.Error("无钩子应返回 nil")
	}
	if NewHookRunner([]config.HookConfig{}) != nil {
		t.Error("空钩子应返回 nil")
	}
}

// Real shell integration: `exit 2` means the same under Windows cmd /c and
// POSIX sh -c, proving the default execution seam works on this machine
// (bypassing the test-only Exec seam).
func TestHookRunner_RealShellExit2(t *testing.T) {
	h := NewHookRunner([]config.HookConfig{{Event: "pre_tool", Command: "exit 2"}})
	if reason := h.PreTool(context.Background(), "file.write", nil); reason == "" {
		t.Fatal("真 shell 下 exit 2 应阻断")
	}
}

// Executor integration: a blocking pre_tool fails the step; post_tool fires
// only for steps that really executed.
func TestExecutorHooks_PreBlocksPostFires(t *testing.T) {
	r := newTestRegistry()
	var postCalls []string
	hooks := &HookRunner{
		Hooks: []config.HookConfig{
			{Event: "pre_tool", Tools: []string{"counter"}, Command: "block-counter"},
			{Event: "post_tool", Command: "notify-all"},
		},
		Timeout: time.Second,
		Exec: func(_ context.Context, cmd string, _ []byte) (int, string, error) {
			switch cmd {
			case "block-counter":
				return 2, "counter 被禁", nil
			default:
				postCalls = append(postCalls, cmd)
				return 0, "", nil
			}
		},
	}
	e := &Executor{
		Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, Hooks: hooks,
	}
	plan := types.Plan{Goal: "g", Steps: []types.Step{
		{ID: "s1", Tool: "counter"},                                // blocked by pre_tool: must not trigger post
		{ID: "s2", Tool: "echo", Args: map[string]any{"in": "hi"}}, // succeeded: triggers post
		{ID: "s3", Tool: "fail"},                                   // failed but really executed: also triggers post
	}}
	res := e.Execute(context.Background(), plan, "task-hooks", "auto", false)

	if res.Steps[0].Status != types.StepFailed {
		t.Errorf("s1 应被 pre_tool 钩子阻断，got %s (%s)", res.Steps[0].Status, res.Steps[0].Error)
	}
	if !strings.Contains(res.Steps[0].Error, "counter 被禁") {
		t.Errorf("阻断原因应进入步骤错误，got %q", res.Steps[0].Error)
	}
	if res.Steps[1].Status != types.StepSucceeded {
		t.Errorf("s2 不受影响，got %s (%s)", res.Steps[1].Status, res.Steps[1].Error)
	}
	if len(postCalls) != 2 {
		t.Errorf("真实执行过的步骤（成功或失败）各触发一次 post_tool，got %d", len(postCalls))
	}
}

func TestHookMatches(t *testing.T) {
	cases := []struct {
		patterns []string
		tool     string
		want     bool
	}{
		{nil, "any", true},
		{[]string{"shell.exec"}, "shell.exec", true},
		{[]string{"shell.exec"}, "file.read", false},
	}
	for _, c := range cases {
		if got := hookMatches(c.patterns, c.tool); got != c.want {
			t.Errorf("hookMatches(%v, %q) = %v, want %v", c.patterns, c.tool, got, c.want)
		}
	}
}

// Config loading: safety.hooks list parsing plus dropping invalid entries.
func TestConfigHooksParsing(t *testing.T) {
	text := `
safety:
  mode: auto
  hooks:
    - event: pre_tool
      command: echo guard
      tools: [shell.exec, file.write]
    - event: post_tool
      command: echo done
    - event: bad_event
      command: echo nope
    - event: pre_tool
      command: "   "
`
	cfg := config.Default()
	m, err := config.ParseYAML(text)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Apply(m)
	got := cfg.Safety.Hooks
	if len(got) != 2 {
		t.Fatalf("hooks = %+v，应只保留 2 条合法钩子", got)
	}
	if got[0].Event != "pre_tool" || got[0].Command != "echo guard" || !reflect.DeepEqual(got[0].Tools, []string{"shell.exec", "file.write"}) {
		t.Errorf("hook[0] = %+v", got[0])
	}
	if got[1].Event != "post_tool" {
		t.Errorf("hook[1] = %+v", got[1])
	}
}
