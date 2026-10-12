package agent

import (
	"path/filepath"
	"testing"

	"gleam/internal/config"
	"gleam/internal/harness/credentials"
	"gleam/internal/llm"
)

const (
	glmURL = "https://open.bigmodel.cn/api/paas/v4"
	dsURL  = "https://api.deepseek.com/v1"
)

func newKeyEnv(t *testing.T) (*config.Config, *credentials.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.Workspace = dir
	creds, err := credentials.Open(filepath.Join(dir, "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg, creds
}

func TestLLMKey_InjectedKeyBoundAtStartup(t *testing.T) {
	cfg, creds := newKeyEnv(t)
	cfg.LLM.APIKey = "sk-env"
	MarkInjectedLLMKey(cfg, glmURL)

	if k, _ := LLMKeyFor(cfg, creds, glmURL, ""); k != "sk-env" {
		t.Errorf("当次主机应可用，得到 %q", k)
	}
	if k, _ := LLMKeyFor(cfg, creds, dsURL, ""); k != "" {
		t.Errorf("换厂商后不该把 env 密钥发出去，得到 %q", k)
	}
	MarkInjectedLLMKey(cfg, dsURL)
	if cfg.LLM.APIKeyScope != llm.KeyScope(glmURL) {
		t.Errorf("scope 被覆盖成 %q", cfg.LLM.APIKeyScope)
	}
}

func TestBindLLMKey_FormAndSwitch(t *testing.T) {
	cfg, creds := newKeyEnv(t)

	if k, err := BindLLMKey(cfg, creds, glmURL, "  sk-glm  ", false); err != nil || k != "sk-glm" {
		t.Fatalf("绑定 = %q err=%v", k, err)
	}
	if cfg.LLM.APIKey != "sk-glm" || cfg.LLM.APIKeyScope != llm.KeyScope(glmURL) {
		t.Fatalf("生效值 = %q / %q", cfg.LLM.APIKey, cfg.LLM.APIKeyScope)
	}

	if k, err := BindLLMKey(cfg, creds, dsURL, "", false); err != nil || k != "" {
		t.Fatalf("换厂商应拿不到密钥 = %q err=%v", k, err)
	}
	if cfg.LLM.APIKey != "" {
		t.Errorf("内存里不该还留着上一家的 key: %q", cfg.LLM.APIKey)
	}
	if k, _ := creds.ResolveLLMAPIKey(llm.KeyScope(glmURL)); k != "sk-glm" {
		t.Errorf("凭证不该被动过，得到 %q", k)
	}
	if k, _ := BindLLMKey(cfg, creds, glmURL, "", false); k != "sk-glm" {
		t.Errorf("换回应免重填，得到 %q", k)
	}

	cfg.LLM.APIKey, cfg.LLM.APIKeyScope = "", ""
	if k, _ := BindLLMKey(cfg, creds, glmURL, "", false); k != "sk-glm" {
		t.Errorf("同主机重存应找回原 key，得到 %q", k)
	}

	if _, err := BindLLMKey(cfg, creds, glmURL, "", true); err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.APIKey != "" || cfg.LLM.APIKeyScope != "" {
		t.Errorf("清除后内存残留 %q / %q", cfg.LLM.APIKey, cfg.LLM.APIKeyScope)
	}
	if k, h := creds.ResolveLLMAPIKey(llm.KeyScope(glmURL)); k != "" || h != "" {
		t.Errorf("清除后磁盘残留 %q / %q", k, h)
	}
}

func TestLLMKeyFor_ExplicitWins(t *testing.T) {
	cfg, creds := newKeyEnv(t)
	if _, err := BindLLMKey(cfg, creds, glmURL, "sk-glm", false); err != nil {
		t.Fatal(err)
	}
	if k, host := LLMKeyFor(cfg, creds, dsURL, "sk-trial"); k != "sk-trial" || host != llm.KeyScope(dsURL) {
		t.Errorf("显式 key = %q / %q", k, host)
	}
}
