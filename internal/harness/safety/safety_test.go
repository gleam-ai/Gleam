package safety

import (
	"context"
	"testing"
	"time"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

type fakeTool struct {
	name    string
	perm    types.Permission
	pathKey bool
}

func (f *fakeTool) Name() string                                         { return f.name }
func (f *fakeTool) Description() string                                  { return "fake" }
func (f *fakeTool) Schema() map[string]any                               { return map[string]any{"type": "object"} }
func (f *fakeTool) Permission() types.Permission                         { return f.perm }
func (f *fakeTool) Execute(context.Context, map[string]any) (any, error) { return "ok", nil }
func (f *fakeTool) Paths(_ context.Context, args map[string]any) []string {
	if v, ok := args["path"].(string); ok && v != "" {
		return []string{v}
	}
	return nil
}

func TestGate_ReadOnlyAlwaysAllowed(t *testing.T) {
	g := New("auto", nil, nil, []string{`D:\ws`}, time.Minute)
	d := g.Evaluate(&fakeTool{name: "file.read", perm: types.PermissionReadOnly}, nil)
	if d.NeedApproval {
		t.Errorf("只读应放行: %+v", d)
	}
	if d.Risk != "low" {
		t.Errorf("risk = %s", d.Risk)
	}
}

func TestGate_ReadOnlyOutsideWorkspaceNeedsApproval(t *testing.T) {
	g := New("auto", nil, nil, []string{`D:\ws`}, time.Minute)
	tool := &fakeTool{name: "file.list", perm: types.PermissionReadOnly}

	inside := g.Evaluate(tool, map[string]any{"path": `D:\ws\sub`})
	if inside.NeedApproval {
		t.Errorf("工作区内只读应放行: %+v", inside)
	}
	out := g.Evaluate(tool, map[string]any{"path": `C:\Users\Public\Desktop`})
	if !out.NeedApproval {
		t.Fatalf("工作区外只读应审批: %+v", out)
	}
	if len(out.Outside) != 1 || out.Outside[0] != `C:\Users\Public\Desktop` {
		t.Errorf("Outside = %v", out.Outside)
	}
	pre := g.EvaluateStep(tool, map[string]any{"path": `C:\Users\Public\Desktop`}, true)
	if pre.NeedApproval {
		t.Errorf("整份计划已批准时不该再问同一条路径: %+v", pre)
	}
	if len(pre.Outside) != 1 {
		t.Errorf("已批准的外部路径仍要交给执行器扩根: %+v", pre)
	}
}

func TestGate_SessionTrustOnlyForSensitiveNoPathTools(t *testing.T) {
	g := New("auto", nil, nil, []string{`D:\ws`}, time.Minute)

	web := &fakeTool{name: "web.fetch", perm: types.PermissionUserApproved}
	if d := g.Evaluate(web, nil); !d.NeedApproval {
		t.Errorf("web.fetch 首次应审批: %+v", d)
	}
	if !g.TrustForSession("web.fetch") {
		t.Fatal("web.fetch 应可记入会话信任")
	}
	if d := g.Evaluate(web, nil); d.NeedApproval {
		t.Errorf("会话内已批准过，不该重复弹窗: %+v", d)
	}

	if g.TrustForSession("file.write") {
		t.Error("写文件工具不能一次批准后永久放行")
	}
	if d := g.Evaluate(&fakeTool{name: "file.write", perm: types.PermissionUserApproved}, map[string]any{"path": `D:\other\a.txt`}); !d.NeedApproval {
		t.Errorf("工作区外的写操作仍应逐次审批: %+v", d)
	}
}

func TestGate_HighRiskAlwaysNeedsApproval(t *testing.T) {
	g := New("auto", []string{"shell.exec"}, nil, []string{`D:\ws`}, time.Minute)
	d := g.Evaluate(&fakeTool{name: "shell.exec", perm: types.PermissionFullAccess}, nil)
	if !d.NeedApproval || d.Risk != "high" {
		t.Errorf("高风险必须审批: %+v", d)
	}
}

func TestGate_TrustedToolBypass(t *testing.T) {
	g := New("auto", []string{"file.write"}, nil, []string{`D:\ws`}, time.Minute)
	d := g.Evaluate(&fakeTool{name: "file.write", perm: types.PermissionUserApproved}, nil)
	if d.NeedApproval {
		t.Errorf("信任工具应放行: %+v", d)
	}
}

func TestGate_MediumPathCheck(t *testing.T) {
	g := New("auto", nil, nil, []string{`D:\ws`}, time.Minute)
	tool := &fakeTool{name: "file.write", perm: types.PermissionUserApproved}

	if d := g.Evaluate(tool, map[string]any{"path": `D:\ws\sub\a.txt`}); d.NeedApproval {
		t.Errorf("工作区内应放行: %+v", d)
	}
	if d := g.Evaluate(tool, map[string]any{"path": `D:\other\a.txt`}); !d.NeedApproval {
		t.Errorf("工作区外应审批: %+v", d)
	}
	if d := g.Evaluate(tool, map[string]any{"path": "a.txt"}); d.NeedApproval {
		t.Errorf("相对路径应放行: %+v", d)
	}
}

func TestGate_TaskRootsAreTrustedPerTask(t *testing.T) {
	g := New("auto", nil, nil, []string{`D:\ws`}, time.Minute)
	tool := &fakeTool{name: "file.write", perm: types.PermissionUserApproved}
	wt := `D:\gleam\worktrees\t1`
	args := map[string]any{"path": wt + `\a.txt`}

	if d := g.EvaluateStepIn(context.Background(), tool, args, false); !d.NeedApproval {
		t.Errorf("没有 per-task 边界时它就是个外部路径，应当审批: %+v", d)
	}
	ctx := toolutil.WithRoots(context.Background(), []string{wt})
	if d := g.EvaluateStepIn(ctx, tool, args, false); d.NeedApproval {
		t.Errorf("本次任务自己的边界内应当放行: %+v", d)
	}
	other := toolutil.WithRoots(context.Background(), []string{`D:\gleam\worktrees\t2`})
	if d := g.EvaluateStepIn(other, tool, args, false); !d.NeedApproval {
		t.Errorf("别的任务的边界不能顺带把这条路径变可信: %+v", d)
	}
}

func TestGate_Modes(t *testing.T) {
	tool := &fakeTool{name: "file.write", perm: types.PermissionUserApproved}
	for _, mode := range []string{"plan_first", "interactive"} {
		g := New(mode, nil, nil, []string{`D:\ws`}, time.Minute)
		if d := g.Evaluate(tool, map[string]any{"path": `D:\ws\a.txt`}); !d.NeedApproval {
			t.Errorf("%s 模式下写操作应审批: %+v", mode, d)
		}
	}
}

func TestGate_EvaluateStep_PreApproved(t *testing.T) {
	g := New("plan_first", nil, nil, []string{`D:\ws`}, time.Minute)
	medium := &fakeTool{name: "file.write", perm: types.PermissionUserApproved}
	high := &fakeTool{name: "shell.exec", perm: types.PermissionFullAccess}

	if d := g.EvaluateStep(medium, map[string]any{"path": `D:\ws\a.txt`}, true); d.NeedApproval {
		t.Errorf("计划已批准后中风险免审: %+v", d)
	}
	if d := g.EvaluateStep(high, nil, true); !d.NeedApproval {
		t.Errorf("计划批准不能豁免高风险: %+v", d)
	}
}

func TestGate_NoPathAware(t *testing.T) {
	g := New("auto", nil, nil, []string{`D:\ws`}, time.Minute)
	tool := &fakeTool{name: "mystery.write", perm: types.PermissionUserApproved, pathKey: true}
	if d := g.Evaluate(tool, nil); !d.NeedApproval {
		t.Errorf("未知范围应审批: %+v", d)
	}
}

func TestGate_SetMode(t *testing.T) {
	g := New("auto", nil, nil, []string{`D:\ws`}, time.Minute)
	g.SetMode("interactive")
	tool := &fakeTool{name: "file.write", perm: types.PermissionUserApproved}
	if d := g.Evaluate(tool, map[string]any{"path": `D:\ws\a.txt`}); !d.NeedApproval {
		t.Error("切换模式后应生效")
	}
}
