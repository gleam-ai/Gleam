package safety

import (
	"context"
	"testing"

	"gleam/pkg/types"
)

type overrideTool struct {
	name string
	perm types.Permission
}

func (f *overrideTool) Name() string                 { return f.name }
func (f *overrideTool) Description() string          { return "fake" }
func (f *overrideTool) Schema() map[string]any       { return map[string]any{"type": "object"} }
func (f *overrideTool) Permission() types.Permission { return f.perm }
func (f *overrideTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return "ok", nil
}

func TestGate_ToolPermissionOverride(t *testing.T) {
	g := New("auto", nil, nil, nil, 0)
	ro := &overrideTool{name: "file.read", perm: types.PermissionReadOnly}
	fa := &overrideTool{name: "shell.exec", perm: types.PermissionFullAccess}

	if d := g.Evaluate(ro, nil); d.NeedApproval {
		t.Error("内置只读不应审批")
	}
	if d := g.Evaluate(fa, nil); !d.NeedApproval || d.Risk != "high" {
		t.Errorf("内置完全访问应高风险审批: %+v", d)
	}

	g.SetToolPermission("file.read", types.PermissionUserApproved)
	if d := g.Evaluate(ro, nil); !d.NeedApproval || d.Risk != "medium" {
		t.Errorf("覆盖为需批准后应中风险审批: %+v", d)
	}

	g.SetToolPermission("shell.exec", types.PermissionReadOnly)
	if d := g.Evaluate(fa, nil); d.NeedApproval || d.Risk != "low" {
		t.Errorf("覆盖为只读后应放行: %+v", d)
	}

	g.ClearToolPermission("shell.exec")
	if d := g.Evaluate(fa, nil); !d.NeedApproval {
		t.Error("清除覆盖后应恢复内置审批")
	}
	if _, ok := g.OverrideOf("shell.exec"); ok {
		t.Error("清除后不应有覆盖")
	}
	if _, ok := g.OverrideOf("file.read"); !ok {
		t.Error("file.read 覆盖应保留")
	}

	g.SetToolPermissions(map[string]types.Permission{"file.read": types.PermissionReadOnly})
	if d := g.Evaluate(ro, nil); d.NeedApproval {
		t.Error("批量装载后应恢复只读放行")
	}
}
