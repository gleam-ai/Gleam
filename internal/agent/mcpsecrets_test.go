package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gleam/internal/config"
	"gleam/internal/harness/credentials"
	"gleam/internal/harness/registry"
)

func TestMigrateMCPSecretsMovesValuesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	creds, err := credentials.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DataDir: dir, MCP: []config.MCPServerConfig{
		{Name: "github", Command: "npx", Env: map[string]string{"GITHUB_TOKEN": "ghp-migrate-secret"}, Enabled: true},
		{Name: "remote", URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "Bearer migrate-header"}, Enabled: true},
	}}
	if err := MigrateMCPSecrets(cfg, creds); err != nil {
		t.Fatal(err)
	}
	if v, ok := cfg.MCP[0].Env["GITHUB_TOKEN"]; !ok || v != "" {
		t.Errorf("迁移后 overlay 应保留键且值为空: %q %v", v, ok)
	}
	if v, ok := cfg.MCP[1].Headers["Authorization"]; !ok || v != "" {
		t.Errorf("迁移后 header 键应保留且值为空: %q %v", v, ok)
	}
	data, err := os.ReadFile(filepath.Join(dir, config.OverlayFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("ghp-migrate-secret")) || bytes.Contains(data, []byte("migrate-header")) {
		t.Error("overlay 不应再含明文密钥")
	}
	if sec := creds.GetMCPSecrets("github"); sec.Env["GITHUB_TOKEN"] != "ghp-migrate-secret" {
		t.Errorf("密钥未入库: %+v", sec)
	}

	fresh := config.Default()
	if err := config.LoadOverlay(fresh, filepath.Join(dir, config.OverlayFile)); err != nil {
		t.Fatal(err)
	}
	if len(fresh.MCP) != 2 {
		t.Fatalf("重载后服务器数 = %d", len(fresh.MCP))
	}
	resolved := ResolveMCPSecrets(creds, fresh.MCP[0])
	if resolved.Env["GITHUB_TOKEN"] != "ghp-migrate-secret" {
		t.Errorf("连接时应从库中补回密钥: %+v", resolved.Env)
	}
	resolved = ResolveMCPSecrets(creds, fresh.MCP[1])
	if resolved.Headers["Authorization"] != "Bearer migrate-header" {
		t.Errorf("连接时应从库中补回请求头: %+v", resolved.Headers)
	}

	if err := MigrateMCPSecrets(cfg, creds); err != nil {
		t.Fatalf("二次迁移应无操作: %v", err)
	}
	if sec := creds.GetMCPSecrets("github"); sec.Env["GITHUB_TOKEN"] != "ghp-migrate-secret" {
		t.Errorf("二次迁移不应改动库: %+v", sec)
	}
}

func TestResolveMCPSecretsExplicitValueWins(t *testing.T) {
	dir := t.TempDir()
	creds, err := credentials.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.SetMCPSecrets("srv", credentials.MCPSecrets{Env: map[string]string{"TOKEN": "stored"}}); err != nil {
		t.Fatal(err)
	}
	srv := config.MCPServerConfig{Name: "srv", Env: map[string]string{"TOKEN": "explicit"}}
	if got := ResolveMCPSecrets(creds, srv).Env["TOKEN"]; got != "explicit" {
		t.Errorf("显式值应优先: %q", got)
	}
	if srv.Env["TOKEN"] != "explicit" {
		t.Error("解析不应改动入参 map")
	}
	if got := ResolveMCPSecrets(nil, srv).Env["TOKEN"]; got != "explicit" {
		t.Errorf("无凭证库时应原样返回: %q", got)
	}
}
func TestMCPInstallStoresSecretsOutsideOverlay(t *testing.T) {
	dir := t.TempDir()
	creds, err := credentials.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = dir
	a := &Agent{Cfg: cfg, Creds: creds, Reg: registry.New(), MCP: NewMCPManager()}
	res, err := a.MCPInstallCustom("secret-srv", "gleam-nonexistent-cmd", nil,
		map[string]string{"TOKEN": "ghp-install-secret"}, "readonly", false)
	if err != nil {
		t.Fatal(err)
	}
	if res["installed"] != true {
		t.Fatalf("安装结果 = %v", res)
	}
	data, err := os.ReadFile(filepath.Join(dir, config.OverlayFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("ghp-install-secret")) {
		t.Error("overlay 不应含明文密钥")
	}
	if sec := creds.GetMCPSecrets("secret-srv"); sec.Env["TOKEN"] != "ghp-install-secret" {
		t.Errorf("密钥未入库: %+v", sec)
	}
	if _, err := a.MCPInstallCustom("secret-srv", "gleam-nonexistent-cmd", nil,
		map[string]string{"TOKEN": ""}, "readonly", true); err != nil {
		t.Fatal(err)
	}
	if sec := creds.GetMCPSecrets("secret-srv"); sec.Env["TOKEN"] != "ghp-install-secret" {
		t.Errorf("空值重装不应清空已存密钥: %+v", sec)
	}
	if _, err := a.MCPRemove("secret-srv"); err != nil {
		t.Fatal(err)
	}
	if sec := creds.GetMCPSecrets("secret-srv"); len(sec.Env) != 0 || len(sec.Headers) != 0 {
		t.Errorf("卸载后密钥应清除: %+v", sec)
	}
}
