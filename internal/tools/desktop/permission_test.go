package desktop

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gleam/internal/harness/safety"
	"gleam/pkg/types"
)

func TestClipboardSplitBySideEffect(t *testing.T) {
	read := NewClipboardRead()
	if read.Permission() != types.PermissionReadOnly {
		t.Errorf("剪贴板读取应为只读，实际 %v", read.Permission())
	}
	write := NewClipboardWrite()
	if write.Permission() != types.PermissionUserApproved {
		t.Errorf("剪贴板写入应为需批准，实际 %v", write.Permission())
	}
	if read.Name() == write.Name() {
		t.Error("读写必须是两个不同的工具名")
	}
}

func TestScreenshotDeclaresWritePermission(t *testing.T) {
	tool := NewScreenshot()
	if tool.Permission() == types.PermissionReadOnly {
		t.Fatal("截屏会落盘，不能声明只读（只读会走自动放行，绕过审批闸门）")
	}
	if tool.Permission() != types.PermissionUserApproved {
		t.Errorf("应为中风险，实际 %v", tool.Permission())
	}
	if got := tool.Paths(context.Background(), map[string]any{"path": "/tmp/a.png"}); len(got) != 1 || got[0] != "/tmp/a.png" {
		t.Errorf("Paths 应透传目标路径，实际 %v", got)
	}
	if got := tool.Paths(context.Background(), map[string]any{}); got != nil {
		t.Errorf("未指定路径应返回 nil（= 无法确认操作路径），实际 %v", got)
	}
}

func TestGateCatchesClipboardWrite(t *testing.T) {
	dir := t.TempDir()
	g := safety.New("auto", nil, []string{dir}, []string{dir}, time.Second)

	if d := g.EvaluateStep(NewClipboardRead(), map[string]any{}, false); d.NeedApproval {
		t.Error("剪贴板读取是只读，不该需要审批")
	}
	if d := g.EvaluateStep(NewClipboardWrite(), map[string]any{"text": "x"}, false); !d.NeedApproval {
		t.Error("剪贴板写入在 auto 模式下必须需要审批")
	}

	inTrust := map[string]any{"path": filepath.Join(dir, "shot.png")}
	if d := g.EvaluateStep(NewScreenshot(), inTrust, false); d.NeedApproval {
		t.Errorf("信任区内的截屏应自动放行，实际：%s", d.Reason)
	}
	if d := g.EvaluateStep(NewScreenshot(), map[string]any{}, false); !d.NeedApproval {
		t.Error("未指定路径的截屏应要求人工确认")
	}
}
