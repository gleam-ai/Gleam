package agent

import (
	"fmt"
	"strings"

	"gleam/internal/config"
	"gleam/internal/harness/credentials"
	"gleam/internal/llm"
)

func LLMKeyFor(cfg *config.Config, creds *credentials.Store, baseURL, explicit string) (key, storedHost string) {
	explicit = strings.TrimSpace(explicit)
	scope := llm.KeyScope(baseURL)
	switch {
	case explicit != "":
		return explicit, scope
	case cfg != nil && cfg.LLM.APIKey != "" && cfg.LLM.APIKeyScope == scope:
		return cfg.LLM.APIKey, scope
	}
	if creds == nil {
		return "", ""
	}
	return creds.ResolveLLMAPIKey(scope)
}

func BindLLMKey(cfg *config.Config, creds *credentials.Store, baseURL, explicit string, clear bool) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("配置为空，无法绑定密钥")
	}
	scope := llm.KeyScope(baseURL)
	explicit = strings.TrimSpace(explicit)
	if explicit != "" && scope == "" {
		return "", fmt.Errorf("请先填写 base_url 再保存密钥：密钥按接入主机绑定")
	}
	if clear {
		if creds != nil {
			if err := creds.SetLLMAPIKey("", ""); err != nil {
				return "", fmt.Errorf("密钥已清除但本地凭证文件更新失败: %w", err)
			}
		}
		cfg.LLM.APIKey, cfg.LLM.APIKeyScope = "", ""
		return "", nil
	}
	if explicit != "" {
		if creds != nil {
			if err := creds.SetLLMAPIKey(explicit, scope); err != nil {
				return "", fmt.Errorf("密钥已生效但保存到本地失败: %w", err)
			}
		}
		cfg.LLM.APIKey, cfg.LLM.APIKeyScope = explicit, scope
		return explicit, nil
	}
	key, _ := LLMKeyFor(cfg, creds, baseURL, "")
	cfg.LLM.APIKey, cfg.LLM.APIKeyScope = key, scope
	if key == "" {
		cfg.LLM.APIKeyScope = ""
	}
	return key, nil
}

func MarkInjectedLLMKey(cfg *config.Config, baseURL string) {
	if cfg == nil || cfg.LLM.APIKey == "" || cfg.LLM.APIKeyScope != "" {
		return
	}
	cfg.LLM.APIKeyScope = llm.KeyScope(baseURL)
}

func (a *Agent) llmKeyFor(baseURL, explicit string) (key, storedHost string) {
	if a == nil {
		return "", ""
	}
	return LLMKeyFor(a.Cfg, a.Creds, baseURL, explicit)
}
