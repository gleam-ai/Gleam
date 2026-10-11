package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"gleam/internal/config"
	"gleam/internal/harness/skill"
	"gleam/pkg/types"
)

func TestWebUI_Providers(t *testing.T) {
	f := newFixture(t, nil)
	out := f.call("GET", "/api/providers", nil)
	list := out["providers"].([]any)
	if len(list) < 5 {
		t.Errorf("厂商预设过少: %d", len(list))
	}
	first := list[0].(map[string]any)
	if first["id"] == "" || first["plans"] == nil {
		t.Errorf("预设字段缺失: %v", first)
	}
	for _, raw := range list {
		p := raw.(map[string]any)
		models, ok := p["models"].([]any)
		if !ok || len(models) == 0 {
			t.Errorf("厂商 %v 没带 models（界面离线选不了模型）", p["id"])
			continue
		}
		for _, m := range models {
			if s, _ := m.(string); strings.TrimSpace(s) == "" {
				t.Errorf("厂商 %v 的 models 里有空条目", p["id"])
			}
		}
	}
}

func TestWebUI_MarketSkillInstallAndList(t *testing.T) {
	f := newFixture(t, nil)
	out := f.call("GET", "/api/market/skills?q=速记", nil)
	presets := out["presets"].([]any)
	quick := map[string]any(nil)
	for _, it := range presets {
		if m, ok := it.(map[string]any); ok && m["name"] == "quick-note" {
			quick = m
		}
	}
	if quick == nil {
		t.Fatal("速记应命中 quick-note")
	}
	if _, ok := quick["installed"].(bool); !ok {
		t.Fatalf("installed 不是布尔: %#v", quick["installed"])
	}
	if quick["installed"] != false {
		t.Fatal("未安装时 installed 应为 false")
	}
	inst := f.call("POST", "/api/market/skills/install", map[string]any{"name": "quick-note"})
	if inst["installed"] != true || inst["version"] != float64(1) {
		t.Errorf("安装结果 = %v", inst)
	}
	after := f.call("GET", "/api/market/skills?q=速记", nil)["presets"].([]any)
	gotInstalled := any(nil)
	for _, it := range after {
		if m, ok := it.(map[string]any); ok && m["name"] == "quick-note" {
			gotInstalled = m["installed"]
		}
	}
	if gotInstalled != true {
		t.Errorf("安装后 installed = %v", gotInstalled)
	}
	skills := f.call("GET", "/api/skills", nil)
	names := skills["skills"].([]any)
	ok := false
	for _, s := range names {
		if s.(map[string]any)["name"] == "quick-note" {
			ok = true
		}
	}
	if !ok {
		t.Error("安装后技能列表应包含 quick-note")
	}
	if code, _ := f.status("POST", "/api/market/skills/install", `{"name":"quick-note"}`); code != 409 {
		t.Errorf("技能重装未确认应 409, got %d", code)
	}
	again := f.call("POST", "/api/market/skills/install", map[string]any{"name": "quick-note", "force": true})
	if again["version"] != float64(2) {
		t.Errorf("重装应升版本: %v", again)
	}
}

func TestWebUI_SkillToggleEndToEnd(t *testing.T) {
	f := newFixture(t, nil)
	f.call("POST", "/api/market/skills/install", map[string]any{"name": "quick-note"})

	off := f.call("POST", "/api/skills/quick-note/enabled", map[string]any{"enabled": false})
	if off["enabled"] != false {
		t.Fatalf("停用返回 = %v", off)
	}
	list := f.call("GET", "/api/skills", nil)["skills"].([]any)
	if len(list) != 1 {
		t.Fatalf("停用的技能应仍留在库里（卸载才是删）: %v", list)
	}
	if list[0].(map[string]any)["disabled"] != true {
		t.Errorf("列表未体现停用状态: %v", list[0])
	}
	if code, msg := f.status("POST", "/api/skills/quick-note/run", `{}`); code != 400 || !strings.Contains(msg, "已停用") {
		t.Errorf("停用后运行应被拒并说明原因: %d %q", code, msg)
	}
	if n := len(f.agent.Skills.ListSummaries()); n != 0 {
		t.Errorf("技能清单仍含停用项: %d 条", n)
	}
	reopened, err := skill.Open(filepath.Join(f.dataDir, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if sk, err := reopened.Get("quick-note"); err != nil || !sk.Disabled {
		t.Errorf("停用未落盘: %+v %v", sk, err)
	}
	on := f.call("POST", "/api/skills/quick-note/enabled", map[string]any{"enabled": true})
	if on["enabled"] != true {
		t.Fatalf("启用返回 = %v", on)
	}
	if n := len(f.agent.Skills.ListSummaries()); n != 1 {
		t.Errorf("启用后应回到清单: %d 条", n)
	}
	if code, msg := f.status("POST", "/api/skills/no-such/enabled", `{"enabled":false}`); code != 404 {
		t.Errorf("未安装技能启停应 404, got %d (%s)", code, msg)
	}
}

func TestWebUI_MCPInstallCustomRemove(t *testing.T) {
	f := newFixture(t, nil)
	res := f.call("POST", "/api/mcp", map[string]any{
		"name": "my-mcp", "command": "gleam-nonexistent-cmd", "args": []string{"a", "b"}, "trust": "readonly",
	})
	if res["installed"] != true || res["connected"] != false {
		t.Fatalf("安装结果 = %v", res)
	}
	if res["warning"] == "" {
		t.Error("连接失败应有告警信息")
	}
	list := f.call("GET", "/api/mcp", nil)
	if list["count"] != float64(1) {
		t.Errorf("列表 = %v", list)
	}
	fresh := config.Default()
	if err := config.LoadOverlay(fresh, f.dataDir+"/settings.yaml"); err != nil {
		t.Fatal(err)
	}
	if len(fresh.MCP) != 1 || fresh.MCP[0].Name != "my-mcp" {
		t.Errorf("覆盖层 = %+v", fresh.MCP)
	}
	resp, err := http.Post(f.ts.URL+"/api/mcp", "application/json", strings.NewReader(`{"name":"my-mcp","command":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Errorf("重名应 409, got %d", resp.StatusCode)
	}
	rm := f.call("DELETE", "/api/mcp/my-mcp", nil)
	if rm["removed_tools"] != float64(0) {
		t.Errorf("卸载结果 = %v", rm)
	}
	if cnt := f.call("GET", "/api/mcp", nil)["count"]; cnt != float64(0) {
		t.Errorf("卸载后应为空")
	}
}

func TestWebUI_MarketMCPInstallMissingParam(t *testing.T) {
	f := newFixture(t, nil)
	resp, err := http.Post(f.ts.URL+"/api/market/mcp/install", "application/json", strings.NewReader(`{"id":"filesystem"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("缺必填参数应 400, got %d", resp.StatusCode)
	}
}

type fakeMCPTool struct{ name string }

func (t fakeMCPTool) Name() string                 { return t.name }
func (t fakeMCPTool) Description() string          { return "测试用假工具" }
func (t fakeMCPTool) Schema() map[string]any       { return map[string]any{"type": "object"} }
func (t fakeMCPTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t fakeMCPTool) Execute(context.Context, map[string]any) (any, error) {
	return "ok", nil
}

func (f *fixture) status(method, path string, body string) (int, string) {
	f.t.Helper()
	req, err := http.NewRequest(method, f.ts.URL+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Error
}

func TestWebUI_MCPLifecycleReinstallAndToggle(t *testing.T) {
	f := newFixture(t, nil)
	const toolName = "mcp.my-mcp.echo"

	first := f.call("POST", "/api/mcp", map[string]any{
		"name": "my-mcp", "command": "gleam-nonexistent-cmd", "args": []string{"a"}, "trust": "readonly",
	})
	if first["installed"] != true || first["replaced"] != false {
		t.Fatalf("首装结果 = %v", first)
	}
	f.agent.Reg.MustRegister(fakeMCPTool{name: toolName})

	code, msg := f.status("POST", "/api/mcp", `{"name":"my-mcp","command":"other-cmd"}`)
	if code != 409 {
		t.Fatalf("重名未确认应 409, got %d (%s)", code, msg)
	}
	if !strings.Contains(msg, "已经安装") {
		t.Errorf("409 文案应说明已安装: %q", msg)
	}
	if c := f.call("GET", "/api/mcp", nil)["count"]; c != float64(1) {
		t.Errorf("被拒的安装不该留下条目，count = %v", c)
	}
	if _, ok := f.agent.Reg.Get(toolName); !ok {
		t.Error("被拒的安装不该把已注册的工具摘掉")
	}

	re := f.call("POST", "/api/mcp", map[string]any{
		"name": "my-mcp", "command": "gleam-nonexistent-cmd", "args": []string{"--v2"}, "trust": "readonly", "force": true,
	})
	if re["replaced"] != true {
		t.Errorf("force 安装应标记 replaced: %v", re)
	}
	if c := f.call("GET", "/api/mcp", nil)["count"]; c != float64(1) {
		t.Errorf("重装应覆盖而非追加，count = %v", c)
	}
	row := f.call("GET", "/api/mcp", nil)["mcp"].([]any)[0].(map[string]any)
	if got := fmt.Sprint(row["args"]); got != "[--v2]" {
		t.Errorf("参数应被替换为 [--v2]，实际 %v", got)
	}
	if _, ok := f.agent.Reg.Get(toolName); ok {
		t.Error("重装前应摘掉旧服务器的工具")
	}

	f.agent.Reg.MustRegister(fakeMCPTool{name: toolName})
	off := f.call("POST", "/api/mcp/my-mcp/enabled", map[string]any{"enabled": false})
	if off["enabled"] != false {
		t.Fatalf("停用返回 = %v", off)
	}
	if _, ok := f.agent.Reg.Get(toolName); ok {
		t.Error("停用后工具仍在注册表：它还会被规划器看见")
	}
	fresh := config.Default()
	if err := config.LoadOverlay(fresh, f.dataDir+"/settings.yaml"); err != nil {
		t.Fatal(err)
	}
	if len(fresh.MCP) != 1 || fresh.MCP[0].Enabled {
		t.Errorf("停用未落盘: %+v", fresh.MCP)
	}
	if code, msg := f.status("POST", "/api/mcp/my-mcp/reconnect", ""); code != 404 || !strings.Contains(msg, "已停用") {
		t.Errorf("停用后重连应被拒: %d %q", code, msg)
	}

	on := f.call("POST", "/api/mcp/my-mcp/enabled", map[string]any{"enabled": true})
	if on["enabled"] != true || on["connected"] != false || on["warning"] == "" {
		t.Errorf("启用返回 = %v", on)
	}
	fresh2 := config.Default()
	if err := config.LoadOverlay(fresh2, f.dataDir+"/settings.yaml"); err != nil {
		t.Fatal(err)
	}
	if !fresh2.MCP[0].Enabled {
		t.Error("启用未落盘")
	}

	if code, _ := f.status("POST", "/api/mcp/no-such/enabled", `{"enabled":false}`); code != 404 {
		t.Errorf("未安装应 404, got %d", code)
	}
	if c := f.call("GET", "/api/mcp", nil)["count"]; c != float64(1) {
		t.Errorf("失败操作不该改列表，count = %v", c)
	}
}

func TestWebUI_MarketMCPReinstallNeedsForce(t *testing.T) {
	f := newFixture(t, nil)
	f.agent.Cfg.MCP = append(f.agent.Cfg.MCP, config.MCPServerConfig{
		Name: "filesystem", Command: "npx", Trust: "user_approved", Enabled: true,
	})
	code, msg := f.status("POST", "/api/market/mcp/install", `{"id":"filesystem","params":{"path":"D:/x"}}`)
	if code != 409 {
		t.Errorf("市场预设重名应 409, got %d (%s)", code, msg)
	}
	if !strings.Contains(msg, "已经安装") {
		t.Errorf("409 文案应引导重装确认: %q", msg)
	}
	f.agent.Cfg.MCP = nil
	if code, _ := f.status("POST", "/api/market/mcp/install", `{"id":"no-such","params":{}}`); code != 400 {
		t.Errorf("未知预设应 400, got %d", code)
	}
}
