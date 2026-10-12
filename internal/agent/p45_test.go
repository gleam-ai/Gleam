package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/pkg/types"
)

type probingNotifier struct {
	NopNotifier
	gate      *safety.Gate
	pendingAt string
	sawItems  int
	sawOnDisk bool
	approved  bool
}

func (p *probingNotifier) OnApproval(req types.ApprovalRequest) types.ApprovalResponse {
	p.sawItems = len(p.gate.PendingApprovals())
	if items, err := safety.LoadPendingApprovals(p.pendingAt); err == nil {
		p.sawOnDisk = len(items) > 0
	}
	return types.ApprovalResponse{Approved: p.approved, Note: "测试"}
}

func TestExecute_PendingRecordedWhileWaiting(t *testing.T) {
	dir := t.TempDir()
	pendingPath := filepath.Join(dir, "pending.json")
	gate := safety.New("auto", nil, nil, nil, time.Second)
	gate.SetPendingPath(pendingPath)

	r := registry.New()
	r.MustRegister(&funcTool{name: "needs-approval", perm: types.PermissionUserApproved, fn: func(ctx context.Context, args map[string]any) (any, error) {
		return "done", nil
	}})
	n := &probingNotifier{gate: gate, pendingAt: pendingPath, approved: false}
	e := &Executor{Reg: r, Gate: gate, Notifier: n, StepTimeout: time.Second}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "needs-approval"}}}, "task-x", "auto", false)

	if n.sawItems != 1 {
		t.Errorf("审批进行中应能看到 1 条等待记录，实际 %d", n.sawItems)
	}
	if !n.sawOnDisk {
		t.Error("等待审批状态应在审批期间就落盘")
	}
	if res.Steps[0].Status != types.StepFailed {
		t.Fatalf("审批被拒应失败，实际 %s", res.Steps[0].Status)
	}
	if got := gate.PendingApprovals(); len(got) != 0 {
		t.Errorf("审批结束后应清空等待记录，实际 %+v", got)
	}
	items, _ := safety.LoadPendingApprovals(pendingPath)
	if len(items) != 0 {
		t.Errorf("落盘文件也应清空，实际 %+v", items)
	}
}

func TestExecute_AutoApprovedSideEffectAudited(t *testing.T) {
	gate := safety.New("auto", []string{"trusted-writer"}, nil, nil, time.Second)
	r := registry.New()
	r.MustRegister(&funcTool{name: "trusted-writer", perm: types.PermissionUserApproved, fn: func(ctx context.Context, args map[string]any) (any, error) {
		return "written", nil
	}})
	e := &Executor{Reg: r, Gate: gate, Notifier: NopNotifier{}, StepTimeout: time.Second}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "trusted-writer"}}}, "t", "auto", false)

	if res.Steps[0].Status != types.StepSucceeded {
		t.Fatalf("白名单工具应自动放行并成功，实际 %s（%s）", res.Steps[0].Status, res.Steps[0].Error)
	}
	var found bool
	for _, a := range gate.RecentAudit(10) {
		if a.Tool == "trusted-writer" && a.Action == "auto" {
			found = true
		}
	}
	if !found {
		t.Errorf("自动放行的副作用动作应留痕: %+v", gate.RecentAudit(10))
	}
}

func TestExecute_ReadOnlyNotAudited(t *testing.T) {
	gate := safety.New("auto", nil, nil, nil, time.Second)
	r := registry.New()
	r.MustRegister(&funcTool{name: "reader", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		return "data", nil
	}})
	e := &Executor{Reg: r, Gate: gate, Notifier: NopNotifier{}, StepTimeout: time.Second}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "reader"}}}, "t", "auto", false)

	if res.Steps[0].Status != types.StepSucceeded {
		t.Fatalf("只读工具应成功，实际 %s", res.Steps[0].Status)
	}
	if got := gate.RecentAudit(10); len(got) != 0 {
		t.Errorf("只读自动放行不该留痕，实际 %+v", got)
	}
}
