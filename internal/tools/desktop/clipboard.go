package desktop

import (
	"context"
	"fmt"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

type ClipboardReadTool struct{}

func NewClipboardRead() *ClipboardReadTool { return &ClipboardReadTool{} }

func (t *ClipboardReadTool) Name() string { return "desktop.clipboard.read" }
func (t *ClipboardReadTool) Description() string {
	return "读取系统剪贴板中的文本（可能含密码或令牌，首次使用需批准）"
}
func (t *ClipboardReadTool) Permission() types.Permission {
	return types.PermissionUserApproved
}
func (t *ClipboardReadTool) Schema() map[string]any {
	return toolutil.Schema("读取系统剪贴板文本", nil, map[string]any{})
}
func (t *ClipboardReadTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	text, err := clipboardRead()
	if err != nil {
		return nil, fmt.Errorf("读取剪贴板失败: %w", err)
	}
	return map[string]any{"text": text}, nil
}

type ClipboardWriteTool struct{}

func NewClipboardWrite() *ClipboardWriteTool { return &ClipboardWriteTool{} }

func (t *ClipboardWriteTool) Name() string { return "desktop.clipboard.write" }
func (t *ClipboardWriteTool) Description() string {
	return "把文本写入系统剪贴板（会覆盖用户当前剪贴板内容，需要用户批准）"
}
func (t *ClipboardWriteTool) Permission() types.Permission {
	return types.PermissionUserApproved
}
func (t *ClipboardWriteTool) Schema() map[string]any {
	return toolutil.Schema("写入文本到系统剪贴板", []string{"text"}, map[string]any{
		"text": toolutil.SchemaProp("要写入的文本", "string"),
	})
}
func (t *ClipboardWriteTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	text, err := toolutil.RequireStr(args, "text")
	if err != nil {
		return nil, err
	}
	if err := clipboardWrite(text); err != nil {
		return nil, fmt.Errorf("写入剪贴板失败: %w", err)
	}
	return map[string]any{"ok": true, "bytes": len(text)}, nil
}
