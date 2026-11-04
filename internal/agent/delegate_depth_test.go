package agent

import (
	"context"
	"strings"
	"testing"

	"gleam/internal/llm"
	"gleam/internal/tools/std"
)

// Nested agent.delegate must be capped by MaxDelegateDepth: a runaway model
// that could keep delegating subtasks inside subtasks would burn tokens
// without limit until the budget runs out.
func TestDelegateDepthCap(t *testing.T) {
	client := llm.NewMock()
	client.Enqueue("chat", "top-level result")
	a := &Agent{LLM: client}

	// Depth 0 (top-level goal): allowed.
	if _, err := a.Delegate(context.Background(), "sub goal", ""); err != nil {
		t.Fatalf("depth 0 should be allowed, got %v", err)
	}

	// Depth 1 (first-level sub agent): one more delegation is still allowed.
	client.Enqueue("chat", "nested result")
	ctx1 := withDelegateDepth(context.Background(), 1)
	if _, err := a.Delegate(ctx1, "nested sub goal", ""); err != nil {
		t.Fatalf("depth 1 should be allowed, got %v", err)
	}

	// Depth 2: at the cap, must reject without calling the LLM again.
	callsBefore := len(client.Calls)
	ctx2 := withDelegateDepth(context.Background(), 2)
	if _, err := a.Delegate(ctx2, "too deep", ""); err == nil {
		t.Fatal("depth 2 should be rejected")
	} else if !strings.Contains(err.Error(), "委派深度已达上限") {
		t.Errorf("error should mention depth cap, got %v", err)
	}
	if len(client.Calls) != callsBefore {
		t.Errorf("rejected delegate must not call LLM, calls went %d -> %d", callsBefore, len(client.Calls))
	}

	// Depth 3+ (defensive): rejected as well.
	ctx3 := withDelegateDepth(context.Background(), 5)
	if _, err := a.Delegate(ctx3, "way too deep", ""); err == nil {
		t.Fatal("depth 5 should be rejected")
	}
}

// The tool entry point (agent.delegate) must go through the same gate;
// locking a.Delegate alone is not enough.
func TestAgentDelegateToolRespectsDepthCap(t *testing.T) {
	client := llm.NewMock()
	a := &Agent{LLM: client}
	tool := std.NewAgentDelegate(a)

	ctx := withDelegateDepth(context.Background(), MaxDelegateDepth)
	if _, err := tool.Execute(ctx, map[string]any{"goal": "too deep"}); err == nil {
		t.Fatal("tool.Execute at MaxDelegateDepth should be rejected")
	}
	if len(client.Calls) != 0 {
		t.Errorf("rejected delegate must not call LLM, got %d calls", len(client.Calls))
	}
}

// Delegate depth flows one way through ctx: the child ctx gets depth +1 and
// the parent ctx stays untouched.
func TestDelegateDepthCtxSemantics(t *testing.T) {
	base := context.Background()
	if got := delegateDepthFrom(base); got != 0 {
		t.Errorf("empty ctx depth = %d, want 0", got)
	}
	next := withDelegateDepth(base, 1)
	if got := delegateDepthFrom(next); got != 1 {
		t.Errorf("withDelegateDepth(1) -> %d, want 1", got)
	}
	deeper := withDelegateDepth(next, 2)
	if got := delegateDepthFrom(deeper); got != 2 {
		t.Errorf("withDelegateDepth(2) -> %d, want 2", got)
	}
	if got := delegateDepthFrom(next); got != 1 {
		t.Errorf("parent ctx mutated: got %d, want 1", got)
	}
}
