package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/pkg/types"
)

type capturingReviewer struct {
	approved bool
	seen     []map[string]any
}

func (c *capturingReviewer) Review(_, _ string, args map[string]any) (bool, string) {
	c.seen = append(c.seen, args)
	return c.approved, "超出目标范围"
}

func refFixture(work, resolved string, called *bool) (*registry.Registry, *safety.Gate) {
	r := registry.New()
	r.MustRegister(&funcTool{name: "locate", perm: types.PermissionReadOnly,
		fn: func(context.Context, map[string]any) (any, error) {
			return map[string]any{"path": resolved}, nil
		}})
	r.MustRegister(&funcTool{
		name: "writer", perm: types.PermissionUserApproved,
		paths: func(_ context.Context, args map[string]any) []string {
			if p, ok := args["path"].(string); ok && p != "" {
				return []string{p}
			}
			return nil
		},
		fn: func(context.Context, map[string]any) (any, error) {
			*called = true
			return "written", nil
		},
	})
	return r, safety.New("auto", nil, nil, []string{work}, time.Second)
}

func refPlan() types.Plan {
	return types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "locate"},
		{ID: "s2", Tool: "writer", DependsOn: []string{"s1"}, Args: map[string]any{"path": "$ref:s1.path"}},
	}}
}

func auditOf(gate *safety.Gate, tool, action string) *safety.AuditEntry {
	entries := gate.RecentAudit(50)
	for i := range entries {
		if entries[i].Tool == tool && entries[i].Action == action {
			return &entries[i]
		}
	}
	return nil
}

func TestExecute_RefToUntrustedPathAsksHuman(t *testing.T) {
	work := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	var called bool
	reg, gate := refFixture(work, outside, &called)
	n := &recordingNotifier{approveAll: false}
	e := &Executor{Reg: reg, Gate: gate, Notifier: n, StepTimeout: time.Second}
	res := e.Execute(context.Background(), refPlan(), "t", "auto", false)

	if called {
		t.Error("真值落在信任范围外且未获批，工具不该被执行")
	}
	if res.ByID["s2"].Status != types.StepFailed {
		t.Fatalf("被拒应失败，实际 %v（%s）", res.ByID["s2"].Status, res.ByID["s2"].Error)
	}
	if res.ByID["s2"].ErrorKind != types.ErrPermission {
		t.Errorf("拒绝属权限类错误，实际 %v", res.ByID["s2"].ErrorKind)
	}
	if len(n.approvals) != 1 {
		t.Fatalf("应恰好 1 次审批请求，实际 %d（%+v）", len(n.approvals), n.approvals)
	}
	if got := n.approvals[0].Reason; !strings.Contains(got, outside) || strings.Contains(got, "$ref:") {
		t.Errorf("审批卡片上的理由必须是真值，实际 %q", got)
	}
	if a := auditOf(gate, "writer", "denied"); a == nil {
		t.Errorf("被拒必须留痕，实际 %+v", gate.RecentAudit(10))
	} else if !strings.Contains(a.Reason, outside) {
		t.Errorf("留痕的理由必须是真值，实际 %q", a.Reason)
	}
}

func TestExecute_RefEscalationApprovedThenRuns(t *testing.T) {
	work := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x.txt")
	var called bool
	reg, gate := refFixture(work, outside, &called)
	n := &recordingNotifier{approveAll: true}
	e := &Executor{Reg: reg, Gate: gate, Notifier: n, StepTimeout: time.Second}
	res := e.Execute(context.Background(), refPlan(), "t", "auto", false)

	if !called || res.ByID["s2"].Status != types.StepSucceeded {
		t.Fatalf("人批了就该执行，实际 %v（%s）", res.ByID["s2"].Status, res.ByID["s2"].Error)
	}
	if a := auditOf(gate, "writer", "approved"); a == nil || !strings.Contains(a.Reason, outside) {
		t.Errorf("批准留痕应带真值，实际 %+v", gate.RecentAudit(10))
	}
	if a := auditOf(gate, "writer", "auto"); a != nil {
		t.Errorf("已走人工确认的步骤不该再记自动放行：%+v", a)
	}
}

func TestExecute_RefInsideWorkspaceStillAuto(t *testing.T) {
	work := t.TempDir()
	inside := filepath.Join(work, "out.txt")
	var called bool
	reg, gate := refFixture(work, inside, &called)
	n := &recordingNotifier{}
	rev := &capturingReviewer{approved: true}
	e := &Executor{
		Reg: reg, Gate: gate, Notifier: n, StepTimeout: time.Second,
		Reviewer: rev, Goal: "把结果写到工作区",
	}
	res := e.Execute(context.Background(), refPlan(), "t", "auto", false)

	if !called || res.ByID["s2"].Status != types.StepSucceeded {
		t.Fatalf("真值在工作区内应照常执行，实际 %v（%s）", res.ByID["s2"].Status, res.ByID["s2"].Error)
	}
	if len(n.approvals) != 0 {
		t.Errorf("真值可信就不该打扰用户，实际 %+v", n.approvals)
	}
	a := auditOf(gate, "writer", "auto")
	if a == nil {
		t.Fatalf("自动放行的副作用动作必须留痕，实际 %+v", gate.RecentAudit(10))
	}
	if !strings.Contains(a.Reason, "信任路径内") {
		t.Errorf("留痕理由应出自真值那次裁决，实际 %q", a.Reason)
	}
	if len(rev.seen) != 1 {
		t.Fatalf("快筛应恰好 1 次，实际 %d 次（%v）", len(rev.seen), rev.seen)
	}
	if got := rev.seen[0]["path"]; got != inside {
		t.Errorf("快筛看到的应是真值 %q，实际 %v", inside, got)
	}
}

type modeFlipNotifier struct {
	NopNotifier
	gate *safety.Gate
}

func (m *modeFlipNotifier) OnApproval(types.ApprovalRequest) types.ApprovalResponse {
	m.gate.SetMode("auto")
	return types.ApprovalResponse{Approved: true, Note: "顺手把模式改成 auto"}
}

func TestExecute_ModeFlippedMidApprovalNotRecordedAsAuto(t *testing.T) {
	work := t.TempDir()
	inside := filepath.Join(work, "out.txt")
	var called bool
	reg, gate := refFixture(work, inside, &called)
	gate.SetMode("plan_first")
	e := &Executor{
		Reg: reg, Gate: gate, Notifier: &modeFlipNotifier{gate: gate}, StepTimeout: time.Second,
	}
	res := e.Execute(context.Background(), refPlan(), "t", "plan_first", false)

	if !called || res.ByID["s2"].Status != types.StepSucceeded {
		t.Fatalf("人已批准，应执行，实际 %v（%s）", res.ByID["s2"].Status, res.ByID["s2"].Error)
	}
	if a := auditOf(gate, "writer", "approved"); a == nil {
		t.Fatalf("人工批准必须留痕，实际 %+v", gate.RecentAudit(10))
	}
	if a := auditOf(gate, "writer", "auto"); a != nil {
		t.Errorf("同一步不该再记一条自动放行：%+v", a)
	}
}
