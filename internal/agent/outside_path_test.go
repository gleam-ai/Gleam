package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/tools/file"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

func outsidePathTool(name string, perm types.Permission, write bool) *funcTool {
	return &funcTool{
		name: name, perm: perm,
		schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []any{"path"},
		},
		paths: func(_ context.Context, args map[string]any) []string {
			return []string{toolutil.Str(args, "path")}
		},
		fn: func(ctx context.Context, args map[string]any) (any, error) {
			abs, err := toolutil.ResolveInRoots(toolutil.Str(args, "path"), toolutil.RootsFrom(ctx, nil))
			if err != nil {
				return nil, err
			}
			if write {
				if err := os.WriteFile(abs, []byte("ok"), 0o644); err != nil {
					return nil, err
				}
				return "written", nil
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return nil, err
			}
			return string(b), nil
		},
	}
}

// One approval must be enough for the rest of the task: listing a desktop
// folder, then reading files from it, should not ask again for every step.
func TestExecute_OutsideReadApprovalExtendsTaskRoots(t *testing.T) {
	ws, outside := t.TempDir(), t.TempDir()
	one := filepath.Join(outside, "one.txt")
	two := filepath.Join(outside, "two.txt")
	for _, p := range []string{one, two} {
		if err := os.WriteFile(p, []byte("desk"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	written := filepath.Join(outside, "new.txt")

	r := registry.New()
	r.MustRegister(outsidePathTool("probe.read", types.PermissionReadOnly, false))
	r.MustRegister(outsidePathTool("probe.write", types.PermissionUserApproved, true))
	file.New(ws).RegisterAll(r)
	gate := safety.New("auto", nil, nil, []string{ws}, time.Second)
	notify := &recordingNotifier{approveAll: true}
	e := &Executor{Reg: r, Gate: gate, Notifier: notify, StepTimeout: 5 * time.Second, Grants: &pathGrants{}}

	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "file.list", Args: map[string]any{"path": outside}},
		{ID: "s2", Tool: "probe.read", Args: map[string]any{"path": two}, DependsOn: []string{"s1"}},
		{ID: "s3", Tool: "probe.write", Args: map[string]any{"path": written}, DependsOn: []string{"s2"}},
	}}
	ctx := toolutil.WithRoots(context.Background(), []string{ws})
	res := e.Execute(ctx, plan, "task-1", "auto", false)
	if res.Succeeded != 3 || res.Failed != 0 {
		t.Fatalf("工作区外路径批准后应能执行: %+v", res.Steps)
	}

	// Two prompts total: the read grant covers the second read, but writing
	// must ask again -- approving "look" is not approving "change".
	if len(notify.approvals) != 2 {
		t.Fatalf("授权次数 = %d, want 2: %+v", len(notify.approvals), notify.approvals)
	}
	if !strings.Contains(notify.approvals[0].Reason, outside) {
		t.Errorf("授权理由应说明工作区外路径: %q", notify.approvals[0].Reason)
	}
	if _, err := os.Stat(written); err != nil {
		t.Errorf("批准后写入应真正落盘: %v", err)
	}
}

func TestExecute_OutsidePathDeniedStaysBlocked(t *testing.T) {
	ws, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := registry.New()
	r.MustRegister(outsidePathTool("probe.read", types.PermissionReadOnly, false))
	gate := safety.New("auto", nil, nil, []string{ws}, time.Second)
	// No approveAll: every request is answered with a rejection.
	notify := &recordingNotifier{}
	e := &Executor{Reg: r, Gate: gate, Notifier: notify, StepTimeout: 5 * time.Second}

	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "probe.read", Args: map[string]any{"path": target}},
	}}
	ctx := toolutil.WithRoots(context.Background(), []string{ws})
	res := e.Execute(ctx, plan, "task-2", "auto", false)
	if res.Failed != 1 || res.Succeeded != 0 {
		t.Fatalf("拒绝后这一步应失败: %+v", res.Steps)
	}
	if got := res.Steps[0].ErrorKind; got != types.ErrPermission {
		t.Errorf("ErrorKind = %q, want %q", got, types.ErrPermission)
	}
	if len(notify.approvals) != 1 {
		t.Errorf("授权次数 = %d, want 1", len(notify.approvals))
	}
}

func TestExecute_OutsideConcurrentReadsAskOnce(t *testing.T) {
	ws, outside := t.TempDir(), t.TempDir()
	r := registry.New()
	file.New(ws).RegisterAll(r)
	n := &blockingApprovalNotifier{approveReq: make(chan struct{}, 20), release: make(chan struct{})}
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, []string{ws}, time.Second), Notifier: n}
	var plan types.Plan
	for i := 0; i < 20; i++ {
		plan.Steps = append(plan.Steps, types.Step{ID: fmt.Sprint(i), Tool: "file.list", Args: map[string]any{"path": outside}})
	}
	done := make(chan *Result, 1)
	go func() { done <- e.Execute(context.Background(), plan, "parallel", "auto", false) }()
	// The executor might need a moment to start scheduling steps. Retry up to 3 times
	// with decreasing timeout if first select doesn't get approval immediately.
	var approved bool
	for attempt := 0; attempt < 3 && !approved; attempt++ {
		select {
		case <-n.approveReq:
			approved = true
		case <-time.After(3 * time.Second):
			if attempt == 2 {
				close(n.release)
				t.Fatal("no approval after 3 attempts")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	duplicate := false
	select {
	case <-n.approveReq:
		duplicate = true
	case <-time.After(100 * time.Millisecond):
	}
	close(n.release)
	res := <-done
	if duplicate || len(n.approveReq) > 0 {
		t.Error("same directory prompted more than once")
	}
	if res.Succeeded != 20 {
		t.Fatalf("approved directory did not execute: %+v", res.Steps)
	}
}

func TestExecute_ConcurrentSessionToolsAskOnce(t *testing.T) {
	r := registry.New()
	r.MustRegister(&funcTool{
		name: "agent.delegate", perm: types.PermissionUserApproved,
		fn: func(context.Context, map[string]any) (any, error) { return "done", nil },
	})
	n := &blockingApprovalNotifier{approveReq: make(chan struct{}, 20), release: make(chan struct{})}
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second), Notifier: n}
	var plan types.Plan
	for i := 0; i < 20; i++ {
		plan.Steps = append(plan.Steps, types.Step{ID: fmt.Sprint(i), Tool: "agent.delegate"})
	}
	done := make(chan *Result, 1)
	go func() { done <- e.Execute(context.Background(), plan, "parallel", "auto", false) }()
	// The executor might need a moment to start scheduling steps. Retry up to 3 times
	// with decreasing timeout if first select doesn't get approval immediately.
	var approved bool
	for attempt := 0; attempt < 3 && !approved; attempt++ {
		select {
		case <-n.approveReq:
			approved = true
		case <-time.After(3 * time.Second):
			if attempt == 2 {
				close(n.release)
				t.Fatal("no approval after 3 attempts")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	duplicate := false
	select {
	case <-n.approveReq:
		duplicate = true
	case <-time.After(100 * time.Millisecond):
	}
	close(n.release)
	res := <-done
	if duplicate || len(n.approveReq) > 0 {
		t.Error("session-trustable tool prompted more than once")
	}
	if res.Succeeded != 20 {
		t.Fatalf("approved tools did not execute: %+v", res.Steps)
	}
}

func TestExecute_RejectedSessionToolsDoNotPromptAgain(t *testing.T) {
	r := registry.New()
	r.MustRegister(&funcTool{
		name: "agent.delegate", perm: types.PermissionUserApproved,
		fn: func(context.Context, map[string]any) (any, error) {
			t.Error("rejected tool executed")
			return nil, nil
		},
	})
	n := &recordingNotifier{}
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second), Notifier: n, Grants: &pathGrants{}}
	plan := types.Plan{Steps: []types.Step{{ID: "one", Tool: "agent.delegate"}, {ID: "two", Tool: "agent.delegate"}}}
	for i := 0; i < 2; i++ {
		res := e.Execute(context.Background(), plan, "denied", "auto", false)
		if res.Failed != 2 || len(n.approvals) != 1 {
			t.Fatalf("rejected delegate prompted again: failed=%d approvals=%d", res.Failed, len(n.approvals))
		}
	}
}

func TestExecute_OutsideFileGrantDoesNotCoverSibling(t *testing.T) {
	ws, outside := t.TempDir(), t.TempDir()
	one, two := filepath.Join(outside, "one.txt"), filepath.Join(outside, "two.txt")
	for _, p := range []string{one, two} {
		if err := os.WriteFile(p, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := registry.New()
	file.New(ws).RegisterAll(r)
	n := &recordingNotifier{approveAll: true}
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, []string{ws}, time.Second), Notifier: n, Grants: &pathGrants{}}
	plan := types.Plan{Steps: []types.Step{{ID: "read", Tool: "file.read", Args: map[string]any{"path": one}}}}
	if res := e.Execute(context.Background(), plan, "t", "auto", false); res.Succeeded != 1 {
		t.Fatalf("approved file failed: %+v", res.Steps)
	}
	n.approveAll = false
	plan.Steps[0].Args["path"] = two
	res := e.Execute(context.Background(), plan, "t", "auto", false)
	if res.Succeeded != 0 || len(n.approvals) != 2 {
		t.Fatalf("file approval leaked to sibling: result=%+v approvals=%d", res.Steps, len(n.approvals))
	}
	res = e.Execute(context.Background(), plan, "t", "auto", false)
	if res.Succeeded != 0 || len(n.approvals) != 2 {
		t.Fatalf("replan asked again after denial: %+v approvals=%d", res.Steps, len(n.approvals))
	}
}

func TestExecute_OutsideWriteApprovalIsPerStep(t *testing.T) {
	ws, outside := t.TempDir(), t.TempDir()
	r := registry.New()
	file.New(ws).RegisterAll(r)
	n := &recordingNotifier{approveAll: true}
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, []string{ws}, time.Second), Notifier: n, Grants: &pathGrants{}}
	p := filepath.Join(outside, "new.txt")
	plan := types.Plan{Steps: []types.Step{{ID: "write", Tool: "file.write", Args: map[string]any{"path": p, "content": "approved"}}}}
	if res := e.Execute(context.Background(), plan, "t", "auto", false); res.Succeeded != 1 {
		t.Fatalf("approved write failed: %+v", res.Steps)
	}
	n.approveAll = false
	plan.Steps[0].Args["content"] = "not approved"
	if res := e.Execute(context.Background(), plan, "t", "auto", false); res.Failed != 1 {
		t.Fatalf("write approval persisted: %+v", res.Steps)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "approved" || len(n.approvals) != 2 {
		t.Fatalf("rejected write changed the file: %q, %v", b, err)
	}
}
