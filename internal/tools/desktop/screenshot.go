package desktop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

type ScreenshotTool struct{}

func NewScreenshot() *ScreenshotTool { return &ScreenshotTool{} }

func (t *ScreenshotTool) Name() string { return "desktop.screenshot" }
func (t *ScreenshotTool) Description() string {
	return "截取当前屏幕画面并保存为 PNG 文件。返回文件路径（会写入磁盘，需要用户批准）"
}
func (t *ScreenshotTool) Permission() types.Permission {
	return types.PermissionUserApproved
}

func (t *ScreenshotTool) Paths(ctx context.Context, args map[string]any) []string {
	if p := toolutil.Str(args, "path"); p != "" {
		return []string{p}
	}
	return nil
}

func (t *ScreenshotTool) Schema() map[string]any {
	return toolutil.Schema("截取屏幕", nil, map[string]any{
		"path": toolutil.SchemaProp("保存路径（默认工作目录下 screenshot-时间戳.png）", "string"),
	})
}
func (t *ScreenshotTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	path := toolutil.Str(args, "path")
	if path == "" {
		path = filepath.Join(".", fmt.Sprintf("screenshot-%d.png", time.Now().Unix()))
	}

	var err error
	switch runtime.GOOS {
	case "windows":
		err = takeScreenshotWindows(path)
	case "darwin":
		err = takeScreenshotMac(path)
	default:
		err = takeScreenshotLinux(path)
	}
	if err != nil {
		return nil, fmt.Errorf("截屏失败: %w", err)
	}
	abs, _ := filepath.Abs(path)
	return map[string]any{"path": path, "absolute": abs}, nil
}

func takeScreenshotWindows(path string) error {
	script := `Add-Type -AssemblyName System.Windows.Forms,System.Drawing; $path=$args[0]; $b=[System.Drawing.Rectangle]::new(0,0,[System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Width,[System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Height); $bmp=[System.Drawing.Bitmap]::new($b.Width,$b.Height); $g=[System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen(0,0,0,0,$b.Size); $bmp.Save($path,[System.Drawing.Imaging.ImageFormat]::Png); $g.Dispose(); $bmp.Dispose()`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script, path)
	return cmd.Run()
}

func takeScreenshotMac(path string) error {
	return exec.Command("screencapture", "-x", path).Run()
}

func takeScreenshotLinux(path string) error {
	if _, err := exec.LookPath("scrot"); err == nil {
		return exec.Command("scrot", path).Run()
	}
	if _, err := exec.LookPath("gnome-screenshot"); err == nil {
		return exec.Command("gnome-screenshot", "-f", path).Run()
	}
	if _, err := exec.LookPath("import"); err == nil {
		return exec.Command("import", "-window", "root", path).Run()
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("未找到截屏工具（scrot/gnome-screenshot/import）")
	}
	return nil
}
