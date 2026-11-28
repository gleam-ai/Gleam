package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/tools/file"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

func TestDirectToolCall_FreezesRootsAndShowsArgs(t *testing.T) {
	workA, workB := t.TempDir(), t.TempDir()
	reg := registry.New()
	fileTools := file.New(workA)
	fileTools.RegisterAll(reg)
	gate := safety.New("plan_first", nil, nil, []string{workA}, time.Second)
	a := &Agent{Reg: reg, Gate: gate, FileTools: fileTools}

	var approval types.ApprovalRequest
	_, err := a.DirectToolCall(context.Background(), "file.write", map[string]any{
		"path": "out.txt", "content": "payload",
	}, func(req types.ApprovalRequest) types.ApprovalResponse {
		approval = req
		// Simulate a workspace hot-switch while the user is deciding.
		a.FileTools.SetRoots([]string{workB})
		a.Gate.SetWorkspaceRoots([]string{workB})
		return types.ApprovalResponse{Approved: true}
	})
	if err != nil {
		t.Fatalf("direct call: %v", err)
	}

	if b, err := os.ReadFile(filepath.Join(workA, "out.txt")); err != nil || string(b) != "payload" {
		t.Fatalf("写入应落在调用开始时的根目录，实际 %q err=%v", b, err)
	}
	if _, err := os.Stat(filepath.Join(workB, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("切换后的工作区不该出现文件: %v", err)
	}
	plan := strings.Join(approval.Plan, "\n")
	if !strings.Contains(plan, "file.write") || !strings.Contains(plan, "payload") {
		t.Fatalf("审批应展示真实参数，实际 %q", plan)
	}
}

func TestDirectToolCall_AuditsDecisionWithoutContent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     string
		response types.ApprovalResponse
		action   string
		wantErr  bool
	}{
		{name: "automatic", mode: "auto", action: "auto"},
		{name: "approved", mode: "interactive", response: types.ApprovalResponse{Approved: true}, action: "approved"},
		{name: "denied", mode: "interactive", action: "denied", wantErr: true},
		{name: "unattended", mode: "interactive", response: types.ApprovalResponse{Unattended: true}, action: "unattended", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := t.TempDir()
			reg := registry.New()
			ft := file.New(work)
			ft.RegisterAll(reg)
			gate := safety.New(tc.mode, nil, nil, []string{work}, time.Second)
			logPath := filepath.Join(t.TempDir(), "audit.jsonl")
			gate.SetAuditPath(logPath)
			a := &Agent{Reg: reg, Gate: gate, FileTools: ft}
			const content = "private-document-body-not-for-audit"
			approvals := 0
			_, err := a.DirectToolCall(context.Background(), "file.write", map[string]any{
				"path": "out.txt", "content": content,
			}, func(types.ApprovalRequest) types.ApprovalResponse {
				approvals++
				return tc.response
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tc.wantErr)
			}
			if (approvals == 1) != (tc.mode == "interactive") {
				t.Fatalf("approval count = %d for mode %s", approvals, tc.mode)
			}
			data, readErr := os.ReadFile(filepath.Join(work, "out.txt"))
			if tc.wantErr {
				if !os.IsNotExist(readErr) {
					t.Fatalf("denied call created a file: %q, %v", data, readErr)
				}
			} else if readErr != nil || string(data) != content {
				t.Fatalf("allowed call did not write: %q, %v", data, readErr)
			}
			persisted, err := safety.LoadAuditLog(logPath, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, entries := range [][]safety.AuditEntry{gate.RecentAudit(10), persisted} {
				if len(entries) != 1 {
					t.Fatalf("audit entries = %v, want one decision", entries)
				}
				e := entries[0]
				if e.Tool != "file.write" || e.Risk != "medium" || e.Action != tc.action || e.Reason == "" || e.Time.IsZero() {
					t.Errorf("unexpected audit decision: %+v", e)
				}
				encoded, err := json.Marshal(e)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(encoded), content) {
					t.Error("audit leaked file content")
				}
			}
		})
	}
}

func TestDirectToolCall_OutsideApprovalIsPerCall(t *testing.T) {
	for _, name := range []string{"file.read", "file.write"} {
		t.Run(name, func(t *testing.T) {
			work, outside := t.TempDir(), t.TempDir()
			path := filepath.Join(outside, "target.txt")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			reg := registry.New()
			ft := file.New(work)
			ft.RegisterAll(reg)
			gate := safety.New("auto", nil, nil, []string{work}, time.Second)
			a := &Agent{Reg: reg, Gate: gate, FileTools: ft}
			args := map[string]any{"path": path}
			if name == "file.write" {
				args["content"] = "approved content"
			}
			approvals := 0
			_, err := a.DirectToolCall(context.Background(), name, args, func(types.ApprovalRequest) types.ApprovalResponse {
				approvals++
				return types.ApprovalResponse{Approved: true}
			})
			if err != nil || approvals != 1 {
				t.Fatalf("approved outside call: approvals=%d, error=%v", approvals, err)
			}
			for _, next := range []string{path, filepath.Join(outside, "sibling.txt")} {
				args["path"] = next
				_, err = a.DirectToolCall(context.Background(), name, args, func(types.ApprovalRequest) types.ApprovalResponse {
					approvals++
					return types.ApprovalResponse{}
				})
				if err == nil || !strings.Contains(err.Error(), "操作被拒绝") {
					t.Fatalf("later outside call should be denied: %v", err)
				}
			}
			if approvals != 3 {
				t.Fatalf("path approval leaked to later calls: %d approvals", approvals)
			}
			if roots := gate.RootSnapshot(); len(roots) != 1 || roots[0] != work {
				t.Fatalf("approval changed global roots: %v", roots)
			}
		})
	}
}

func TestDirectToolCall_ExactGrantAuditedBeforeExecution(t *testing.T) {
	work, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "target.txt")
	reg := registry.New()
	gate := safety.New("auto", nil, nil, []string{work}, time.Second)
	logPath := filepath.Join(t.TempDir(), "audit.jsonl")
	gate.SetAuditPath(logPath)
	executionErr := errors.New("tool execution failed")
	reg.MustRegister(&funcTool{
		name: "probe.read", perm: types.PermissionReadOnly,
		paths: func(context.Context, map[string]any) []string { return []string{target} },
		fn: func(ctx context.Context, _ map[string]any) (any, error) {
			entries, err := safety.LoadAuditLog(logPath, 0)
			if err != nil || len(entries) != 1 || entries[0].Action != "approved" {
				t.Fatalf("decision must be persisted before execution: %+v, %v", entries, err)
			}
			roots := toolutil.RootsFrom(ctx, nil)
			if _, err := toolutil.ResolveInRoots(target, roots); err != nil {
				t.Errorf("approved target blocked: %v", err)
			}
			if _, err := toolutil.ResolveInRoots(filepath.Join(outside, "sibling.txt"), roots); err == nil {
				t.Error("file approval granted its parent directory")
			}
			return nil, executionErr
		},
	})
	a := &Agent{Reg: reg, Gate: gate, FileTools: file.New(work)}
	_, err := a.DirectToolCall(context.Background(), "probe.read", nil, func(types.ApprovalRequest) types.ApprovalResponse {
		return types.ApprovalResponse{Approved: true}
	})
	if !errors.Is(err, executionErr) {
		t.Fatalf("tool error lost: %v", err)
	}
	if entries := gate.RecentAudit(10); len(entries) != 1 || entries[0].Action != "approved" {
		t.Fatalf("tool failure lost or duplicated the approval decision: %+v", entries)
	}
}

func TestDirectToolCall_ReadOnlyIsAudited(t *testing.T) {
	work := t.TempDir()
	reg := registry.New()
	ft := file.New(work)
	ft.RegisterAll(reg)
	gate := safety.New("auto", nil, nil, []string{work}, time.Second)
	a := &Agent{Reg: reg, Gate: gate, FileTools: ft}
	_, err := a.DirectToolCall(context.Background(), "file.list", map[string]any{"path": work}, func(types.ApprovalRequest) types.ApprovalResponse {
		t.Fatal("workspace read should not request approval")
		return types.ApprovalResponse{}
	})
	if err != nil {
		t.Fatal(err)
	}
	if entries := gate.RecentAudit(10); len(entries) != 1 || entries[0].Action != "auto" || entries[0].Risk != "low" {
		t.Fatalf("read-only call missing from audit: %+v", entries)
	}
}

func TestDirectToolCall_CancelledCallDoesNotWrite(t *testing.T) {
	for _, duringApproval := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_call", true: "during_approval"}[duringApproval], func(t *testing.T) {
			work := t.TempDir()
			reg := registry.New()
			ft := file.New(work)
			ft.RegisterAll(reg)
			mode := "auto"
			if duringApproval {
				mode = "interactive"
			}
			gate := safety.New(mode, nil, nil, []string{work}, time.Second)
			a := &Agent{Reg: reg, Gate: gate, FileTools: ft}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if !duringApproval {
				cancel()
			}
			_, err := a.DirectToolCall(ctx, "file.write", map[string]any{"path": "out.txt", "content": "must not write"},
				func(types.ApprovalRequest) types.ApprovalResponse {
					cancel()
					return types.ApprovalResponse{Approved: true}
				})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("cancelled call error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(work, "out.txt")); !os.IsNotExist(err) {
				t.Errorf("cancelled call wrote a file: %v", err)
			}
			entries := gate.RecentAudit(10)
			if len(entries) != 1 || entries[0].Action != "cancelled" {
				t.Errorf("cancellation not audited: %+v", entries)
			}
		})
	}
}

type cancelAfterCheckContext struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int32
}

func (c *cancelAfterCheckContext) Err() error {
	err := c.Context.Err()
	// Race cancellation against the pre-audit snapshot without relying on disk timing.
	if c.checks.Add(1) == 2 {
		c.cancel()
	}
	return err
}

func TestDirectToolCall_CancelledAfterDecisionCheckDoesNotWrite(t *testing.T) {
	for _, mode := range []string{"auto", "interactive"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			reg := registry.New()
			ft := file.New(work)
			ft.RegisterAll(reg)
			gate := safety.New(mode, nil, nil, []string{work}, time.Second)
			logPath := filepath.Join(t.TempDir(), "audit.jsonl")
			gate.SetAuditPath(logPath)
			a := &Agent{Reg: reg, Gate: gate, FileTools: ft}
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &cancelAfterCheckContext{Context: base, cancel: cancel}
			target := filepath.Join(work, "out.txt")
			if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := a.DirectToolCall(ctx, "file.write", map[string]any{"path": target, "content": "must not write"},
				func(types.ApprovalRequest) types.ApprovalResponse {
					return types.ApprovalResponse{Approved: true}
				})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("cancelled call error = %v", err)
			}
			if data, err := os.ReadFile(target); err != nil || string(data) != "original" {
				t.Errorf("cancelled call changed the file: %q, %v", data, err)
			}
			action := "auto"
			if mode == "interactive" {
				action = "approved"
			}
			entries, err := safety.LoadAuditLog(logPath, 0)
			if err != nil || len(entries) != 1 || entries[0].Action != action {
				t.Errorf("decision should persist even when execution is cancelled: %+v, %v", entries, err)
			}
		})
	}
}

func TestDirectToolCall_RejectsMissingRequiredArg(t *testing.T) {
	work := t.TempDir()
	reg := registry.New()
	fileTools := file.New(work)
	fileTools.RegisterAll(reg)
	a := &Agent{Reg: reg, Gate: safety.New("auto", nil, nil, []string{work}, time.Second), FileTools: fileTools}

	if _, err := a.DirectToolCall(context.Background(), "file.write", map[string]any{"path": "x.txt"},
		func(types.ApprovalRequest) types.ApprovalResponse { return types.ApprovalResponse{Approved: true} }); err == nil {
		t.Fatal("缺少 content 应被拒")
	}
	if _, err := os.Stat(filepath.Join(work, "x.txt")); !os.IsNotExist(err) {
		t.Fatalf("被拒调用不该创建文件: %v", err)
	}
}
