package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gleam/internal/config"
	"gleam/internal/harness/registry"
)

// fakeMCPRemote serves a minimal streamable-HTTP MCP endpoint whose tool list
// can change between connections, like a real server upgrade would.
func fakeMCPRemote(t *testing.T) (*httptest.Server, func(string)) {
	t.Helper()
	var mu sync.Mutex
	toolName := "echo"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-retry")
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "fake"}}
		case "tools/list":
			mu.Lock()
			name := toolName
			mu.Unlock()
			result = map[string]any{"tools": []map[string]any{
				{"name": name, "description": "echo", "inputSchema": map[string]any{"type": "object"}},
			}}
		default:
			result = map[string]any{}
		}
		payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	setTool := func(name string) {
		mu.Lock()
		toolName = name
		mu.Unlock()
	}
	return srv, setTool
}

func newMCPTestAgent(t *testing.T, url string) *Agent {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.MCP = []config.MCPServerConfig{{Name: "fake", URL: url, Trust: "readonly", Enabled: true}}
	return &Agent{Cfg: cfg, Reg: registry.New(), MCP: NewMCPManager(), Notifier: NopNotifier{}}
}

func mcpToolNames(a *Agent) []string {
	var out []string
	for _, n := range a.Reg.Names() {
		if strings.HasPrefix(n, "mcp.fake.") {
			out = append(out, n)
		}
	}
	return out
}

// Retry must detach the old tool set: a server upgrade renames tools, and the
// pre-retry entries would otherwise stay registered against a closed client.
func TestMCPRetryUnregistersStaleTools(t *testing.T) {
	srv, setTool := fakeMCPRemote(t)
	a := newMCPTestAgent(t, srv.URL)
	if _, err := a.mcpConnect(a.Cfg.MCP[0]); err != nil {
		t.Fatalf("首次连接失败: %v", err)
	}
	if _, ok := a.Reg.Get("mcp.fake.echo"); !ok {
		t.Fatal("首次连接应注册 mcp.fake.echo")
	}

	setTool("echo2")
	out, err := a.MCPRetry("fake")
	if err != nil {
		t.Fatalf("重试失败: %v", err)
	}
	if out["connected"] != true {
		t.Fatalf("重试应连接成功: %v", out)
	}
	if _, ok := a.Reg.Get("mcp.fake.echo"); ok {
		t.Error("旧工具 mcp.fake.echo 应在重试时注销")
	}
	if _, ok := a.Reg.Get("mcp.fake.echo2"); !ok {
		t.Error("新工具 mcp.fake.echo2 应注册")
	}
}

// A failed retry must not leave dead tools callable: the old client is closed,
// so every entry pointing at it has to go.
func TestMCPRetryFailureClearsTools(t *testing.T) {
	srv, _ := fakeMCPRemote(t)
	a := newMCPTestAgent(t, srv.URL)
	if _, err := a.mcpConnect(a.Cfg.MCP[0]); err != nil {
		t.Fatalf("首次连接失败: %v", err)
	}
	srv.Close()

	out, err := a.MCPRetry("fake")
	if err != nil {
		t.Fatalf("重试应返回结果而不是报错: %v", err)
	}
	if out["connected"] != false {
		t.Fatalf("服务端已关闭，重试应报告未连接: %v", out)
	}
	if names := mcpToolNames(a); len(names) != 0 {
		t.Errorf("连接失败后不应残留工具: %v", names)
	}
}
