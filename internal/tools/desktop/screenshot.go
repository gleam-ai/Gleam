package desktop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

type ScreenshotTool struct {
	Roots []string
}

func NewScreenshot(roots ...string) *ScreenshotTool { return &ScreenshotTool{Roots: roots} }

func (t *ScreenshotTool) Name() string { return "desktop.screenshot" }
func (t *ScreenshotTool) Description() string {
	return "截取当前屏幕画面并保存为 PNG 文件。返回文件路径（会写入磁盘，需要用户批准）"
}
func (t *ScreenshotTool) Permission() types.Permission {
	return types.PermissionUserApproved
}

func (t *ScreenshotTool) Paths(ctx context.Context, args map[string]any) []string {
	p := toolutil.Str(args, "path")
	if p == "" {
		return nil
	}
	if abs, err := toolutil.ResolveInRoots(p, toolutil.RootsFrom(ctx, t.Roots)); err == nil {
		return []string{abs}
	}
	return []string{p}
}

func (t *ScreenshotTool) Schema() map[string]any {
	return toolutil.Schema("截取屏幕", nil, map[string]any{
		"path": toolutil.SchemaProp("保存路径（默认工作目录下 screenshot-时间戳.png）", "string"),
	})
}
func (t *ScreenshotTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	path := toolutil.Str(args, "path")
	if path == "" {
		path = fmt.Sprintf("screenshot-%d.png", time.Now().Unix())
	}
	// Resolve through the workspace boundary: the path is model-supplied, and
	// `../..` must not turn "take a screenshot" into an arbitrary file write.
	abs, err := toolutil.ResolveInRoots(path, toolutil.RootsFrom(ctx, t.Roots))
	if err != nil {
		return nil, err
	}
	path = abs

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
	return map[string]any{"path": path, "absolute": path}, nil
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
