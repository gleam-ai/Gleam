package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/tools/file"
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
		a.FileTools.Roots = []string{workB}
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
