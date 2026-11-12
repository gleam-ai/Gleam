// Package cua provides the desktop automation tool (computer use): screenshot,
// click, text input, and app launching.
//
// Three real execution backends plus one explicit dry-run backend:
//   - powershell: native Windows (user32 screenshot/click + clipboard-paste
//     input), zero external dependencies, the default backend
//   - direct: an external cua binary (if the user supplies one)
//   - mcp: a local CUA MCP service (GLEAM_CUA_MCP_URL, default 127.0.0.1:8796)
//   - mock: enabled explicitly via GLEAM_CUA_MODE=mock, exercises the pipeline
//     only and never falsely reports "executed"
//
// Mode selection order: GLEAM_CUA_MODE > cua binary (direct) > Windows
// (powershell) > mcp. Older versions silently returned {"success":true} when
// no backend existed; a machine action that lies about having executed is more
// dangerous than refusing, so that path was removed. Now every action either
// really happens or fails with an explicit error.
package cua

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gleam/internal/tools/desktop"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

// ComputerUse implements types.Tool for desktop automation.
type ComputerUse struct {
	mode    string
	mcpURL  string
	timeout time.Duration
	// run is the execution seam tests can swap out; nil uses the real process.
	run func(ctx context.Context, name string, args []string) (stdout []byte, err error)
	// post is the test seam for mcp mode; nil uses real HTTP.
	post func(ctx context.Context, url string, body []byte) (int, []byte, error)
}

// NewComputerUse creates the tool and picks an execution backend automatically.
func NewComputerUse() *ComputerUse {
	return &ComputerUse{
		mode:    detectMode(),
		mcpURL:  mcpURLOrDefault(),
		timeout: 30 * time.Second,
	}
}

func mcpURLOrDefault() string {
	if u := strings.TrimSpace(os.Getenv("GLEAM_CUA_MCP_URL")); u != "" {
		return u
	}
	return "http://127.0.0.1:8796/rpc"
}

// detectMode: env var > cua binary > native Windows > mcp.
func detectMode() string {
	switch m := strings.ToLower(strings.TrimSpace(os.Getenv("GLEAM_CUA_MODE"))); m {
	case "direct", "powershell", "mcp", "mock":
		return m
	}
	if _, err := exec.LookPath("cua"); err == nil {
		return "direct"
	}
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "mcp"
}

func (cu *ComputerUse) Name() string { return "computer_use" }

func (cu *ComputerUse) Description() string {
	return "控制桌面应用：截屏、点击屏幕坐标、输入文本、启动应用或打开网址。" +
		"当前执行方式：" + cu.mode + "。会真实操作这台机器，调用前确认目标必要。" +
		"典型顺序：先 screenshot 观察画面，再 click/type_text，完成后再 screenshot 验证结果。" +
		"这不是 HTTP 抓取工具：网页被反爬验证或登录墙挡住时，用 open_app 传入该 URL 打开浏览器，" +
		"请用户在窗口里完成验证或登录，再用 screenshot 读取页面内容。"
}

func (cu *ComputerUse) Permission() types.Permission { return types.PermissionUserApproved }

func (cu *ComputerUse) Schema() map[string]any {
	return toolutil.Schema("桌面自动化动作", []string{"action"}, map[string]any{
		"action": map[string]any{
			"type":        "string",
			"description": "动作：screenshot 截屏（返回 PNG 路径）；click 点击坐标；type_text 输入文本（粘贴到当前焦点）；open_app 启动应用",
			"enum":        []string{"screenshot", "click", "type_text", "open_app"},
		},
		"x":        toolutil.SchemaProp("click：屏幕 X 坐标（像素，来自 screenshot 画面）", "integer"),
		"y":        toolutil.SchemaProp("click：屏幕 Y 坐标（像素）", "integer"),
		"text":     toolutil.SchemaProp("type_text：要输入的文本（先 Set-Clipboard 再 Ctrl+V，中文可靠）", "string"),
		"app_name": toolutil.SchemaProp("open_app：应用名、可执行文件路径，或要交给浏览器打开的 http(s) 网址", "string"),
	})
}

// Execute runs a single action. The executed field in its result is always
// true; faked actions never escape from here.
func (cu *ComputerUse) Execute(ctx context.Context, args map[string]any) (any, error) {
	action := strings.ToLower(toolutil.Str(args, "action"))
	ctx, cancel := context.WithTimeout(ctx, cu.timeout)
	defer cancel()

	switch action {
	case "screenshot":
		return cu.screenshot(ctx)
	case "click":
		x, y := toolutil.Int(args, "x", -1), toolutil.Int(args, "y", -1)
		if x < 0 || y < 0 {
			return nil, fmt.Errorf("click 需要非负的 x/y 坐标")
		}
		return cu.click(ctx, x, y)
	case "type_text":
		text := toolutil.Str(args, "text")
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("type_text 需要非空 text")
		}
		return cu.typeText(ctx, text)
	case "open_app":
		app := toolutil.Str(args, "app_name")
		if err := validateAppTarget(app, "app_name"); err != nil {
			return nil, err
		}
		return cu.openApp(ctx, app)
	default:
		return nil, fmt.Errorf("未知动作 %q（可选 screenshot|click|type_text|open_app）", action)
	}
}

// validateAppTarget accepts either a bare executable token or an http(s) URL.
// A URL goes through its own check because normal query strings contain "&"
// and "?", which validateSingleToken rejects as command separators.
func validateAppTarget(s, field string) error {
	trimmed := strings.TrimSpace(s)
	if strings.ContainsAny(trimmed, "\n\r") {
		return fmt.Errorf("%s 不能包含换行", field)
	}
	if u, err := url.Parse(trimmed); err == nil && u.Scheme != "" {
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("%s 只允许 http/https 链接", field)
		}
		if strings.TrimSpace(u.Host) == "" {
			return fmt.Errorf("%s 的链接缺少主机名", field)
		}
		return nil
	}
	return validateSingleToken(trimmed, field)
}

// validateSingleToken rejects characters that could escalate an argument into
// a command: these actions splice their arguments into a command line, so
// quotes and separators must be kept out.
func validateSingleToken(s, field string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s 不能为空", field)
	}
	if strings.ContainsAny(s, "\"'`|;&$<>(){}\n\r") {
		return fmt.Errorf("%s 含有不允许的字符（引号/管道/分号等）", field)
	}
	return nil
}

func (cu *ComputerUse) screenshot(ctx context.Context) (any, error) {
	switch cu.mode {
	case "mock":
		return nil, fmt.Errorf("mock 模式不产生真实截图（设置 GLEAM_CUA_MODE=powershell|direct|mcp 以真实执行）")
	case "direct":
		out, err := cu.runCmd(ctx, "cua", "screenshot", "--format", "base64")
		if err != nil {
			return nil, fmt.Errorf("cua 截屏失败: %w", err)
		}
		return map[string]any{"action": "screenshot", "mode": "direct", "format": "base64", "data": strings.TrimSpace(string(out))}, nil
	case "mcp":
		return cu.mcpCall(ctx, "cua:screenshot", map[string]any{"format": "base64"})
	default: // powershell
		dir := filepath.Join(os.TempDir(), "gleam-screenshots")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		path := filepath.Join(dir, fmt.Sprintf("cua-%d.png", time.Now().UnixNano()))
		// desktop.Capture and the desktop.screenshot tool share one platform implementation.
		if err := runSeparate(ctx, func(ctx context.Context) error { return desktop.Capture(path) }); err != nil {
			return nil, fmt.Errorf("截屏失败: %w", err)
		}
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			return nil, fmt.Errorf("截屏文件未生成或为空: %s", path)
		}
		return map[string]any{"action": "screenshot", "mode": cu.mode, "path": path}, nil
	}
}

func (cu *ComputerUse) click(ctx context.Context, x, y int) (any, error) {
	switch cu.mode {
	case "mock":
		return mockDeferred("click", fmt.Sprintf("(%d,%d)", x, y)), nil
	case "direct":
		out, err := cu.runCmd(ctx, "cua", "click", "--x", strconv.Itoa(x), "--y", strconv.Itoa(y))
		if err != nil {
			return nil, fmt.Errorf("cua 点击失败: %w", err)
		}
		return map[string]any{"action": "click", "mode": "direct", "executed": true, "output": strings.TrimSpace(string(out))}, nil
	case "mcp":
		return cu.mcpCall(ctx, "cua:click", map[string]any{"x": x, "y": y})
	default: // powershell
		// user32: SetCursorPos to move + mouse_event(2=LEFTDOWN, 4=LEFTUP).
		script := psWrap(fmt.Sprintf(`
Add-Type -TypeDefinition 'using System;using System.Runtime.InteropServices;public class GleamMouse{[DllImport("user32.dll")]public static extern bool SetCursorPos(int x,int y);[DllImport("user32.dll")]public static extern void mouse_event(uint f,uint dx,uint dy,uint d,int e);}';
[GleamMouse]::SetCursorPos(%d,%d) | Out-Null; Start-Sleep -Milliseconds 80;
[GleamMouse]::mouse_event(2,0,0,0,0); Start-Sleep -Milliseconds 40; [GleamMouse]::mouse_event(4,0,0,0,0);`, x, y))
		if _, err := cu.runPowerShell(ctx, script); err != nil {
			return nil, fmt.Errorf("点击失败: %w", err)
		}
		return map[string]any{"action": "click", "mode": "powershell", "executed": true, "x": x, "y": y}, nil
	}
}

func (cu *ComputerUse) typeText(ctx context.Context, text string) (any, error) {
	switch cu.mode {
	case "mock":
		return mockDeferred("type_text", strconv.Itoa(len([]rune(text)))+" chars"), nil
	case "direct":
		if _, err := cu.runCmd(ctx, "cua", "type", "--text", text); err != nil {
			return nil, fmt.Errorf("cua 输入失败: %w", err)
		}
		return map[string]any{"action": "type_text", "mode": "direct", "executed": true}, nil
	case "mcp":
		return cu.mcpCall(ctx, "cua:type_text", map[string]any{"text": text})
	default: // powershell
		// CJK text goes through clipboard paste (SendKeys is unreliable for CJK);
		// the text is escaped for a PS single-quoted string.
		script := psWrap(fmt.Sprintf(`
Set-Clipboard -Value '%s';
Add-Type -AssemblyName System.Windows.Forms;
[System.Windows.Forms.SendKeys]::SendWait('^v');`, psEscape(text)))
		if _, err := cu.runPowerShell(ctx, script); err != nil {
			return nil, fmt.Errorf("输入失败: %w", err)
		}
		return map[string]any{"action": "type_text", "mode": "powershell", "executed": true, "chars": len([]rune(text)), "via": "clipboard-paste"}, nil
	}
}

func (cu *ComputerUse) openApp(ctx context.Context, app string) (any, error) {
	switch cu.mode {
	case "mock":
		return mockDeferred("open_app", app), nil
	case "direct":
		out, err := cu.runCmd(ctx, "cua", "launch", app)
		if err != nil {
			return nil, fmt.Errorf("cua 启动失败: %w", err)
		}
		return map[string]any{"action": "open_app", "mode": "direct", "executed": true, "output": strings.TrimSpace(string(out))}, nil
	case "mcp":
		return cu.mcpCall(ctx, "cua:open_app", map[string]any{"app_name": app})
	default: // powershell
		// Start-Process goes straight to the process table with no shell parsing;
		// the argument has already passed validateSingleToken.
		script := psWrap(fmt.Sprintf("Start-Process -FilePath '%s';", psEscape(app)))
		if _, err := cu.runPowerShell(ctx, script); err != nil {
			return nil, fmt.Errorf("启动应用失败: %w", err)
		}
		return map[string]any{"action": "open_app", "mode": "powershell", "executed": true, "app": app}, nil
	}
}

// mockDeferred is the only legitimate return shape for mock mode: it says
// explicitly that nothing was executed.
func mockDeferred(action, detail string) map[string]any {
	return map[string]any{
		"action": action, "mode": "mock", "executed": false,
		"note": "mock 模式仅演练链路，未真实执行（GLEAM_CUA_MODE=mock）", "detail": detail,
	}
}

// psEscape places text safely inside a PS single-quoted string: doubling
// single quotes is enough; every other character passes through unchanged.
func psEscape(s string) string { return strings.ReplaceAll(s, "'", "''") }

// psWrap normalizes the shape of a PS invocation: collapse indented newlines,
// run non-interactively, pass the exit code through.
func psWrap(script string) string {
	return strings.Join(strings.Fields(script), " ")
}

func (cu *ComputerUse) runPowerShell(ctx context.Context, script string) ([]byte, error) {
	return cu.runCmd(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
}

func (cu *ComputerUse) runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	if cu.run != nil {
		return cu.run(ctx, name, args)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return out, fmt.Errorf("%v: %s", err, msg)
		}
		return out, err
	}
	return out, nil
}

// mcpCall talks to the local CUA MCP service. Older versions ignored HTTP
// errors and reported success; fixed: non-2xx statuses and parse failures
// now come back as honest errors.
func (cu *ComputerUse) mcpCall(ctx context.Context, method string, params map[string]any) (any, error) {
	payload, err := json.Marshal(map[string]any{"method": method, "params": params})
	if err != nil {
		return nil, err
	}
	var status int
	var body []byte
	if cu.post != nil {
		status, body, err = cu.post(ctx, cu.mcpURL, payload)
	} else {
		req, err2 := http.NewRequestWithContext(ctx, http.MethodPost, cu.mcpURL, bytes.NewReader(payload))
		if err2 != nil {
			return nil, err2
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err2 := http.DefaultClient.Do(req)
		if err2 != nil {
			return nil, fmt.Errorf("CUA MCP 服务不可达（%s）: %w", cu.mcpURL, err2)
		}
		defer resp.Body.Close()
		status = resp.StatusCode
		body, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	}
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("CUA MCP 返回 %d: %s", status, types.Shorten(string(body), 200))
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("CUA MCP 响应不是 JSON: %w", err)
	}
	out := map[string]any{"action": method, "mode": "mcp", "executed": true, "result": result["result"]}
	return out, nil
}

// runSeparate makes the blocking syscall respect ctx (desktop.Capture takes
// no ctx parameter internally).
func runSeparate(ctx context.Context, fn func(ctx context.Context) error) error {
	done := make(chan error, 1)
	go func() { done <- fn(ctx) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("动作超时（%v）", cuTimeoutHint())
	}
}

func cuTimeoutHint() time.Duration { return 30 * time.Second }
