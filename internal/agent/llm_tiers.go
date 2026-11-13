package agent

import (
	"strings"

	"gleam/internal/config"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

func (a *Agent) HelperLLM() llm.Client {
	if fast := a.FastClient(); fast != nil {
		return fast
	}
	return a.PrimaryLLM()
}

// RebuildFastClient recomputes the auxiliary client from the published config.
func (a *Agent) RebuildFastClient() {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	a.rebuildFastClientLocked(a.Cfg)
}

// rebuildFastClientLocked assigns a.FastLLM and therefore requires the cfgMu
// write lock; cfg is the caller's updateConfig clone (or the live config).
func (a *Agent) rebuildFastClientLocked(cfg *config.Config) {
	if a == nil || cfg == nil || cfg.LLM.Provider == "mock" {
		a.FastLLM = nil
		return
	}
	// An entry checked as the auxiliary model carries full connection details
	// (possibly another vendor), so prefer it. The fast_model field in advanced
	// settings is the legacy form on the same connection.
	if entry := fastModelEntry(cfg); entry != nil {
		base, model, protocol := llm.ResolveTarget(entry.ProviderID, entry.Plan, entry.BaseURL, entry.Model, entry.Protocol)
		if strings.TrimSpace(model) == "" {
			a.FastLLM = nil
			return
		}
		key, _ := LLMKeyFor(cfg, a.Creds, base, entry.APIKey)
		a.FastLLM = llm.New(protocol, base, key, model,
			cfg.LLM.Temperature, cfg.LLM.MaxTokens, cfg.LLM.TimeoutSecs,
			llm.WithEnableThinking(cfg.LLM.EnableThinking))
		return
	}
	if strings.TrimSpace(cfg.LLM.FastModel) == "" {
		a.FastLLM = nil
		return
	}
	protocol := cfg.LLM.Protocol
	if protocol == "" {
		protocol = llm.ProtocolOpenAIChat
	}
	a.FastLLM = llm.New(protocol, cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.FastModel,
		cfg.LLM.Temperature, cfg.LLM.MaxTokens, cfg.LLM.TimeoutSecs,
		llm.WithEnableThinking(cfg.LLM.EnableThinking))
}

func (a *Agent) RebuildTierClients() {
	if a == nil {
		return
	}
	a.tierMu.Lock()
	a.tierClients = nil
	a.tierMu.Unlock()
}

func (a *Agent) TierLLM(role string) llm.Client {
	if a == nil || a.Config() == nil {
		return a.PrimaryLLM()
	}
	return a.tierLLM(FindRole(role).ModelTier)
}

func (a *Agent) tierLLM(tier string) llm.Client {
	if a == nil {
		return nil
	}
	cfg := a.Config()
	if cfg == nil {
		return a.PrimaryLLM()
	}
	if tier == "" || len(cfg.LLM.Tiers) == 0 {
		return a.PrimaryLLM()
	}
	model := strings.TrimSpace(cfg.LLM.Tiers[tier])
	if model == "" || model == cfg.LLM.Model {
		return a.PrimaryLLM()
	}
	if cfg.LLM.Provider == "mock" {
		return a.PrimaryLLM()
	}
	a.tierMu.Lock()
	defer a.tierMu.Unlock()
	if c, ok := a.tierClients[tier]; ok {
		return c
	}
	protocol := cfg.LLM.Protocol
	if protocol == "" {
		protocol = llm.ProtocolOpenAIChat
	}
	c := llm.New(protocol, cfg.LLM.BaseURL, cfg.LLM.APIKey, model,
		cfg.LLM.Temperature, cfg.LLM.MaxTokens, cfg.LLM.TimeoutSecs,
		llm.WithEnableThinking(cfg.LLM.EnableThinking))
	if a.tierClients == nil {
		a.tierClients = map[string]llm.Client{}
	}
	a.tierClients[tier] = c
	return c
}

const DefaultVerifyTier = "reasoning"

func (a *Agent) VerifyLLM(role string) llm.Client {
	if a == nil || a.Config() == nil {
		return a.PrimaryLLM()
	}
	if !a.verifyIndependent(role) {
		return a.TierLLM(role)
	}
	return a.tierLLM(DefaultVerifyTier)
}

func (a *Agent) VerifyModel(role string) string {
	if a == nil {
		return ""
	}
	cfg := a.Config()
	if cfg == nil {
		return ""
	}
	if a.verifyIndependent(role) {
		return strings.TrimSpace(cfg.LLM.Tiers[DefaultVerifyTier])
	}
	return a.execModel(role)
}

func (a *Agent) verifyIndependent(role string) bool {
	if a == nil {
		return false
	}
	cfg := a.Config()
	if cfg == nil || cfg.LLM.Provider == "mock" {
		return false
	}
	m := strings.TrimSpace(cfg.LLM.Tiers[DefaultVerifyTier])
	return m != "" && m != a.execModel(role)
}

func (a *Agent) execModel(role string) string {
	if a == nil {
		return ""
	}
	cfg := a.Config()
	if cfg == nil {
		return ""
	}
	if t := a.EffectiveTier(role); t != "" {
		return strings.TrimSpace(cfg.LLM.Tiers[t])
	}
	return cfg.LLM.Model
}

func (a *Agent) EffectiveTier(role string) string {
	if a == nil {
		return ""
	}
	cfg := a.Config()
	if cfg == nil {
		return ""
	}
	tier := FindRole(role).ModelTier
	if tier == "" || len(cfg.LLM.Tiers) == 0 {
		return ""
	}
	model := strings.TrimSpace(cfg.LLM.Tiers[tier])
	if model == "" || model == cfg.LLM.Model {
		return ""
	}
	if cfg.LLM.Provider == "mock" {
		return ""
	}
	return tier
}

func normalizeTaskMode(m types.TaskMode) types.TaskMode {
	switch types.TaskMode(strings.TrimSpace(string(m))) {
	case types.TaskChat:
		return types.TaskChat
	case types.TaskCode:
		return types.TaskCode
	default:
		return types.TaskWork
	}
}
