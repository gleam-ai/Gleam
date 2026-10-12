package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

type NotifyTool struct{}

func NewNotify() *NotifyTool { return &NotifyTool{} }

func (t *NotifyTool) Name() string { return "desktop.notify" }
func (t *NotifyTool) Description() string {
	return "发送桌面通知（系统弹窗/Toast）。用于任务完成、重要事件提醒"
}
func (t *NotifyTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *NotifyTool) Schema() map[string]any {
	return toolutil.Schema("发送桌面通知", []string{"title"}, map[string]any{
		"title":   toolutil.SchemaProp("通知标题", "string"),
		"message": toolutil.SchemaProp("通知内容", "string"),
	})
}
func (t *NotifyTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	title, err := toolutil.RequireStr(args, "title")
	if err != nil {
		return nil, err
	}
	message := toolutil.Str(args, "message")

	if err := Notify(title, message); err != nil {
		return nil, fmt.Errorf("发送通知失败: %w", err)
	}
	return map[string]any{"ok": true, "title": title, "message": message}, nil
}

func Notify(title, message string) error {
	switch runtime.GOOS {
	case "windows":
		ps := fmt.Sprintf(
			`[System.Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms') | Out-Null; $n=New-Object System.Windows.Forms.NotifyIcon; $n.Icon=[System.Drawing.SystemIcons]::Information; $n.Visible=$true; $n.ShowBalloonTip(5000,'%s','%s',[System.Windows.Forms.ToolTipIcon]::Info); Start-Sleep -Seconds 6; $n.Dispose()`,
			escapePS(title), escapePS(message))
		return exec.Command("powershell", "-NoProfile", "-Command", ps).Run()
	case "darwin":
		script := fmt.Sprintf(`display notification %q with title %q`, message, title)
		return exec.Command("osascript", "-e", script).Run()
	default:
		if _, err := exec.LookPath("notify-send"); err == nil {
			return exec.Command("notify-send", title, message).Run()
		}
		return fmt.Errorf("未找到通知工具")
	}
}

func escapePS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
