package cua

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

func newTestCUA(mode string, run func(ctx context.Context, name string, args []string) ([]byte, error)) *ComputerUse {
	cu := NewComputerUse()
	cu.mode = mode
	cu.run = run
	return cu
}

// Tool contract: the CUA must be a valid types.Tool, visible once registered
// in the menu and judgeable by the permission gate.
func TestComputerUse_ToolContract(t *testing.T) {
	cu := NewComputerUse()
	if cu.Name() != "computer_use" {
		t.Errorf("name = %q", cu.Name())
	}
	if cu.Permission() != types.PermissionUserApproved {
		t.Errorf("permission = %v, want user_approved", cu.Permission())
	}
	if strings.TrimSpace(cu.Description()) == "" {
		t.Error("description 不能为空")
	}
	if err := toolutil.ValidateArgs(cu, map[string]any{"action": "screenshot"}); err != nil {
		t.Errorf("screenshot 合法参数不应报错: %v", err)
	}
	if err := toolutil.ValidateArgs(cu, map[string]any{}); err == nil {
		t.Error("缺 action 应报错")
	}
}

func TestComputerUse_MockIsHonest(t *testing.T) {
	cu := newTestCUA("mock", nil)
	out, err := cu.Execute(context.Background(), map[string]any{"action": "click", "x": 3.0, "y": 4.0})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["executed"] != false {
		t.Errorf("mock 模式绝不能谎报 executed=true: %v", m)
	}
	if _, err := cu.Execute(context.Background(), map[string]any{"action": "screenshot"}); err == nil {
		t.Error("mock 模式截图应明确报错，而不是编造图片")
	}
}

func TestComputerUse_PowerShellMode(t *testing.T) {
	var gotName string
	var gotArgs []string
	cu := newTestCUA("powershell", func(_ context.Context, name string, args []string) ([]byte, error) {
		gotName, gotArgs = name, args
		return []byte("ok"), nil
	})

	out, err := cu.Execute(context.Background(), map[string]any{"action": "click", "x": 12.0, "y": 34.0})
	if err != nil {
		t.Fatal(err)
	}
	if m := out.(map[string]any); m["executed"] != true {
		t.Errorf("真实后端应报告 executed=true: %v", m)
	}
	script := strings.Join(gotArgs, " ")
	if gotName != "powershell" || !strings.Contains(script, "SetCursorPos(12,34)") || !strings.Contains(script, "mouse_event") {
		t.Errorf("点击脚本缺少坐标注入: %s", script)
	}

	// type_text: CJK goes through clipboard paste; single quotes must be
	// escaped to prevent script injection.
	if _, err := cu.Execute(context.Background(), map[string]any{"action": "type_text", "text": "it's 测试"}); err != nil {
		t.Fatal(err)
	}
	script = strings.Join(gotArgs, " ")
	if !strings.Contains(script, "Set-Clipboard") || !strings.Contains(script, "SendWait") {
		t.Errorf("输入脚本缺少剪贴板粘贴: %s", script)
	}
	if !strings.Contains(script, "it''s") {
		t.Errorf("单引号未转义（脚本注入风险）: %s", script)
	}

	// open_app: goes through Start-Process; the argument must be a clean token.
	if _, err := cu.Execute(context.Background(), map[string]any{"action": "open_app", "app_name": "notepad"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(gotArgs, " "), "Start-Process") {
		t.Errorf("启动脚本缺少 Start-Process: %s", strings.Join(gotArgs, " "))
	}
}

func TestComputerUse_OpenAppAcceptsURL(t *testing.T) {
	cu := newTestCUA("powershell", func(ctx context.Context, name string, args []string) ([]byte, error) {
		return []byte("ok"), nil
	})
	out, err := cu.Execute(context.Background(), map[string]any{
		"action":   "open_app",
		"app_name": "https://mp.weixin.qq.com/s/abc?from=timeline&isappinstalled=0",
	})
	if err != nil {
		t.Fatalf("带查询串的 http 链接应被接受: %v", err)
	}
	if got, _ := out.(map[string]any)["executed"].(bool); !got {
		t.Errorf("应真实执行: %+v", out)
	}
	if _, err := cu.Execute(context.Background(), map[string]any{
		"action":   "open_app",
		"app_name": "file:///C:/Windows/System32/cmd.exe",
	}); err == nil {
		t.Error("非 http(s) 协议应被拒绝")
	}
}

func TestComputerUse_ArgValidation(t *testing.T) {
	cu := newTestCUA("powershell", nil)
	if _, err := cu.Execute(context.Background(), map[string]any{"action": "click"}); err == nil {
		t.Error("click 缺坐标应报错")
	}
	if _, err := cu.Execute(context.Background(), map[string]any{"action": "type_text", "text": " "}); err == nil {
		t.Error("type_text 空文本应报错")
	}
	if _, err := cu.Execute(context.Background(), map[string]any{"action": "open_app", "app_name": "notepad; rm -rf"}); err == nil {
		t.Error("app_name 含命令分隔符应报错")
	}
	if _, err := cu.Execute(context.Background(), map[string]any{"action": "fly"}); err == nil {
		t.Error("未知动作应报错")
	}
}

func TestComputerUse_MCPErrorPropagation(t *testing.T) {
	// Non-2xx must come back as an error; older versions reported
	// {"success":true} here.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	cu := NewComputerUse()
	cu.mode = "mcp"
	cu.mcpURL = srv.URL
	cu.post = func(_ context.Context, url string, body []byte) (int, []byte, error) {
		resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		buf := make([]byte, 4096)
		n, _ := resp.Body.Read(buf)
		return resp.StatusCode, buf[:n], nil
	}
	if _, err := cu.Execute(context.Background(), map[string]any{"action": "click", "x": 1.0, "y": 1.0}); err == nil {
		t.Fatal("MCP 500 应报错")
	}

	// An unreachable server must also fail explicitly, not succeed.
	cu2 := NewComputerUse()
	cu2.mode = "mcp"
	cu2.mcpURL = "http://127.0.0.1:1/rpc"
	if _, err := cu2.Execute(context.Background(), map[string]any{"action": "screenshot"}); err == nil {
		t.Fatal("MCP 不可达应报错")
	}
}

func TestDetectMode_EnvOverrides(t *testing.T) {
	t.Setenv("GLEAM_CUA_MODE", "mock")
	if m := detectMode(); m != "mock" {
		t.Errorf("GLEAM_CUA_MODE 应最高优先: got %s", m)
	}
	t.Setenv("GLEAM_CUA_MODE", "")
	m := detectMode()
	switch m {
	case "direct", "powershell", "mcp":
	default:
		t.Errorf("fallback 模式非法: %s", m)
	}
}

// validateSingleToken is the injection defense: action arguments get spliced
// into a command line, so separators must be blocked.
func TestValidateSingleToken(t *testing.T) {
	bad := []string{"notepad; calc", `a"b`, "x&y", "a|b", "a'b", "$(calc)", "a\nb"}
	for _, s := range bad {
		if err := validateSingleToken(s, "app_name"); err == nil {
			t.Errorf("应拒绝 %q", s)
		}
	}
	if err := validateSingleToken("notepad", "app_name"); err != nil {
		t.Errorf("干净 token 不应拒绝: %v", err)
	}
}

func TestComputerUse_ScreenshotWindowGrounding(t *testing.T) {
	cu := NewComputerUse()
	cu.mode = "direct"
	cu.run = func(_ context.Context, _ string, _ []string) ([]byte, error) {
		return []byte("ZmFrZQ=="), nil
	}
	if _, err := cu.Execute(context.Background(), map[string]any{"action": "screenshot", "window": "记事本"}); err == nil {
		t.Error("direct 后端不支持 window 参数，应诚实报错而不是假装聚焦")
	}
	cu.mode = "mcp"
	if _, err := cu.Execute(context.Background(), map[string]any{"action": "screenshot", "window": "记事本"}); err == nil {
		t.Error("mcp 后端不支持 window 参数，应诚实报错")
	}
	schema := cu.Schema()
	props := schema["properties"].(map[string]any)
	if _, ok := props["window"]; !ok {
		t.Error("schema 应声明 window 参数")
	}
}
