package safety

import (
	"testing"
	"time"

	"gleam/pkg/types"
)

// argTool lowers itself to read-only for mode=answer only; every other mode
// keeps the approval-level class.
type argTool struct{ plainTool }

func (a *argTool) PermissionForArgs(args map[string]any) types.Permission {
	if m, _ := args["mode"].(string); m == "answer" {
		return types.PermissionReadOnly
	}
	return types.PermissionUserApproved
}

// raiseTool claims a higher class than its own; the gate must ignore that.
type raiseTool struct{ plainTool }

func (r *raiseTool) PermissionForArgs(map[string]any) types.Permission {
	return types.PermissionFullAccess
}

func TestGate_ArgPermissionLowersClassForSafeArgs(t *testing.T) {
	g := New("plan_first", nil, nil, []string{t.TempDir()}, time.Minute)
	tool := &argTool{plainTool{name: "split.by.args", perm: types.PermissionUserApproved}}

	d := g.Evaluate(tool, map[string]any{"mode": "answer"})
	if d.NeedApproval {
		t.Fatalf("answer mode has no gated side effect, should not ask: %+v", d)
	}
	if d.Risk != "low" {
		t.Errorf("risk = %s, want low", d.Risk)
	}
}

func TestGate_ArgPermissionKeepsApprovalForOtherArgs(t *testing.T) {
	// plan_first confirms every write-level step, so a task-mode call must
	// still reach the user.
	g := New("plan_first", nil, nil, []string{t.TempDir()}, time.Minute)
	tool := &argTool{plainTool{name: "split.by.args", perm: types.PermissionUserApproved}}

	for _, args := range []map[string]any{{"mode": "task"}, {"mode": "answer "}, {"mode": 7}} {
		if d := g.Evaluate(tool, args); !d.NeedApproval {
			t.Errorf("args %v should still need approval: %+v", args, d)
		}
	}
}

func TestGate_ArgPermissionNeverRaisesClass(t *testing.T) {
	g := New("auto", nil, nil, []string{t.TempDir()}, time.Minute)
	tool := &raiseTool{plainTool{name: "raises.itself", perm: types.PermissionReadOnly}}

	if d := g.Evaluate(tool, map[string]any{"x": 1}); d.NeedApproval {
		t.Errorf("a tool cannot raise its own class: %+v", d)
	}
}

func TestGate_UserOverrideWinsOverArgPermission(t *testing.T) {
	g := New("auto", nil, nil, []string{t.TempDir()}, time.Minute)
	tool := &argTool{plainTool{name: "split.by.args", perm: types.PermissionUserApproved}}

	// The user pinned this tool to approval-level in the permission settings:
	// the tool's own "these args are safe" claim no longer applies.
	g.SetToolPermission("split.by.args", types.PermissionUserApproved)
	if d := g.Evaluate(tool, map[string]any{"mode": "answer"}); !d.NeedApproval {
		t.Fatalf("an explicit user override must win: %+v", d)
	}

	// And the other way round: pinned to read-only, every mode runs unasked.
	g.SetToolPermission("split.by.args", types.PermissionReadOnly)
	if d := g.Evaluate(tool, map[string]any{"mode": "task"}); d.NeedApproval {
		t.Errorf("pinned read-only should run unasked: %+v", d)
	}
}

func TestGate_ArgPermissionWithoutArgsMap(t *testing.T) {
	g := New("auto", nil, nil, []string{t.TempDir()}, time.Minute)
	tool := &argTool{plainTool{name: "split.by.args", perm: types.PermissionUserApproved}}

	// No args means nothing lowered the class; the lookup must not panic.
	if d := g.Evaluate(tool, nil); !d.NeedApproval {
		t.Errorf("nil args keep the tool's own class: %+v", d)
	}
}
