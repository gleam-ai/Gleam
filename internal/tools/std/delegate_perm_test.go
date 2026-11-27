package std

import (
	"context"
	"testing"

	"gleam/pkg/types"
)

type nopDelegator struct{}

func (nopDelegator) Delegate(context.Context, string, string) (string, error) { return "", nil }

// The answer mode is one model completion with no tools behind it; gating it
// at approval level made every "extract / compare / conclude" step a dialog,
// and a hard failure on a host with nobody to ask.
func TestAgentDelegate_ArgPermissionByMode(t *testing.T) {
	tool := NewAgentDelegate(nopDelegator{})
	ap, ok := tool.(types.ArgPermission)
	if !ok {
		t.Fatal("agent.delegate must advertise a per-argument permission")
	}
	if tool.Permission() != types.PermissionUserApproved {
		t.Fatalf("base class = %v, want user_approved", tool.Permission())
	}
	cases := []struct {
		name string
		args map[string]any
		want types.Permission
	}{
		{"default mode", map[string]any{"goal": "x"}, types.PermissionReadOnly},
		{"nil args", nil, types.PermissionReadOnly},
		{"answer", map[string]any{"goal": "x", "mode": "answer"}, types.PermissionReadOnly},
		{"task", map[string]any{"goal": "x", "mode": "task"}, types.PermissionUserApproved},
		{"unknown mode is not lowered", map[string]any{"goal": "x", "mode": "Answer"}, types.PermissionUserApproved},
		{"non-string mode is not lowered", map[string]any{"goal": "x", "mode": true}, types.PermissionUserApproved},
	}
	for _, c := range cases {
		if got := ap.PermissionForArgs(c.args); got != c.want {
			t.Errorf("%s: class = %v, want %v", c.name, got, c.want)
		}
	}
}
