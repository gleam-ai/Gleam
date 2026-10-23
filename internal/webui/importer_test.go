package webui

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseMCPJSON(t *testing.T) {
	raw := []byte(`{"mcpServers":{
		"stdio-one":{"command":"npx","args":["-y","@a/b"],"env":{"TOKEN":"secret","MODE":"x"}},
		"remote-one":{"url":"https://example.com/sse"},
		"empty":{}
	}}`)
	got := parseMCPJSON(raw)
	if len(got) != 3 {
		t.Fatalf("应解析出 3 台，实际 %d", len(got))
	}
	one := got["stdio-one"]
	if one.Command != "npx" || len(one.Args) != 2 {
		t.Errorf("stdio-one = %+v", one)
	}
	if len(one.EnvKeys) != 2 || one.EnvKeys[0] != "MODE" || one.EnvKeys[1] != "TOKEN" {
		t.Errorf("env keys = %v", one.EnvKeys)
	}
	if got["remote-one"].Command != "" {
		t.Errorf("URL 型不应有 command")
	}
}

func TestParseCodexTOML(t *testing.T) {
	text := `
[model_providers.custom]
name = "x"

[mcp_servers.node_repl]
args = []
command = 'C:\tools\node_repl.exe'

[mcp_servers.fs]
command = "mcp-fs"
args = ["--root", "/tmp"]

[projects.'C:\Users\me']
trust_level = "trusted"
`
	got := parseCodexTOML(text)
	if len(got) != 2 {
		t.Fatalf("应解析出 2 台，实际 %d: %+v", len(got), got)
	}
	if got["node_repl"].Command != `C:\tools\node_repl.exe` {
		t.Errorf("node_repl command = %q", got["node_repl"].Command)
	}
	fs := got["fs"]
	if fs.Command != "mcp-fs" || len(fs.Args) != 2 || fs.Args[1] != "/tmp" {
		t.Errorf("fs = %+v", fs)
	}
}

func TestCountEntries(t *testing.T) {
	if n := countEntries("a\n\nb\n\n\nc"); n != 3 {
		t.Errorf("三段应为 3，实际 %d", n)
	}
	if n := countEntries("只有一段\n连着两行"); n != 1 {
		t.Errorf("无空行应算 1 段，实际 %d", n)
	}
	if n := countEntries("   "); n != 1 {
		t.Errorf("空文件兜底为 1，实际 %d", n)
	}
}

func TestImportApplyRejectsUnknownID(t *testing.T) {
	f := newFixture(t, nil)
	out := f.call("POST", "/api/import/apply", map[string]any{
		"memory": []string{"deadbeefdeadbeef"},
		"mcp":    []string{"deadbeefdeadbeef"},
	})
	mem, _ := out["memory"].(map[string]any)
	if mem == nil || mem["imported"].(float64) != 0 || mem["skipped"].(float64) != 1 {
		t.Errorf("未知 id 应被跳过: %v", out)
	}
	mcpRes, _ := out["mcp"].(map[string]any)
	if mcpRes == nil || mcpRes["imported"].(float64) != 0 {
		t.Errorf("未知 id 不应装上任何 MCP: %v", out)
	}
}

func TestImportScanShape(t *testing.T) {
	f := newFixture(t, nil)
	snap := localImportScan(f.agent)
	for _, k := range []string{"memory", "mcp", "skills"} {
		if _, ok := snap[k]; !ok {
			t.Errorf("扫描结果缺少 %s", k)
		}
	}
	allowed := map[string]bool{
		"id": true, "app": true, "name": true, "command": true,
		"args": true, "env_keys": true, "path": true, "installed": true,
	}
	raw, _ := json.Marshal(snap["mcp"])
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("mcp 候选不是数组: %v", err)
	}
	for _, m := range arr {
		for k := range m {
			if !allowed[k] {
				t.Errorf("MCP 候选多了字段 %q：它可能把环境变量的值带出去了", k)
			}
		}
	}
}

func TestReadCappedRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.md")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), importReadMaxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCapped(path); err == nil {
		t.Fatal("超过上限的文件应被拒绝，不能整个读进内存")
	}
	small := filepath.Join(t.TempDir(), "ok.md")
	if err := os.WriteFile(small, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if raw, err := readCapped(small); err != nil || string(raw) != "ok" {
		t.Fatalf("raw=%q err=%v", raw, err)
	}
}
