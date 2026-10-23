package agent

import (
	"strings"

	"gleam/internal/llm"
	"gleam/pkg/types"
)

func (a *Agent) HelperLLM() llm.Client {
	if a != nil && a.FastLLM != nil {
		return a.FastLLM
	}
	return a.LLM
}

func (a *Agent) RebuildFastClient() {
	if a == nil || a.Cfg == nil || a.Cfg.LLM.Provider == "mock" {
		a.FastLLM = nil
		return
	}
	// An entry checked as the auxiliary model carries full connection details
	// (possibly another vendor), so prefer it. The fast_model field in advanced
	// settings is the legacy form on the same connection.
	if entry := a.fastModelEntry(); entry != nil {
		base, model, protocol := llm.ResolveTarget(entry.ProviderID, entry.Plan, entry.BaseURL, entry.Model, entry.Protocol)
		if strings.TrimSpace(model) == "" {
			a.FastLLM = nil
			return
		}
		key, _ := a.llmKeyFor(base, entry.APIKey)
		a.FastLLM = llm.New(protocol, base, key, model,
			a.Cfg.LLM.Temperature, a.Cfg.LLM.MaxTokens, a.Cfg.LLM.TimeoutSecs)
		return
	}
	if strings.TrimSpace(a.Cfg.LLM.FastModel) == "" {
		a.FastLLM = nil
		return
	}
	protocol := a.Cfg.LLM.Protocol
	if protocol == "" {
		protocol = llm.ProtocolOpenAIChat
	}
	a.FastLLM = llm.New(protocol, a.Cfg.LLM.BaseURL, a.Cfg.LLM.APIKey, a.Cfg.LLM.FastModel,
		a.Cfg.LLM.Temperature, a.Cfg.LLM.MaxTokens, a.Cfg.LLM.TimeoutSecs)
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
	if a == nil || a.Cfg == nil {
		return a.LLM
	}
	return a.tierLLM(FindRole(role).ModelTier)
}

func (a *Agent) tierLLM(tier string) llm.Client {
	if a == nil || a.Cfg == nil {
		return a.LLM
	}
	if tier == "" || len(a.Cfg.LLM.Tiers) == 0 {
		return a.LLM
	}
	model := strings.TrimSpace(a.Cfg.LLM.Tiers[tier])
	if model == "" || model == a.Cfg.LLM.Model {
		return a.LLM
	}
	if a.Cfg.LLM.Provider == "mock" {
		return a.LLM
	}
	a.tierMu.Lock()
	defer a.tierMu.Unlock()
	if c, ok := a.tierClients[tier]; ok {
		return c
	}
	protocol := a.Cfg.LLM.Protocol
	if protocol == "" {
		protocol = llm.ProtocolOpenAIChat
	}
	c := llm.New(protocol, a.Cfg.LLM.BaseURL, a.Cfg.LLM.APIKey, model,
		a.Cfg.LLM.Temperature, a.Cfg.LLM.MaxTokens, a.Cfg.LLM.TimeoutSecs)
	if a.tierClients == nil {
		a.tierClients = map[string]llm.Client{}
	}
	a.tierClients[tier] = c
	return c
}

const DefaultVerifyTier = "reasoning"

func (a *Agent) VerifyLLM(role string) llm.Client {
	if a == nil || a.Cfg == nil {
		return a.LLM
	}
	if !a.verifyIndependent(role) {
		return a.TierLLM(role)
	}
	return a.tierLLM(DefaultVerifyTier)
}

func (a *Agent) VerifyModel(role string) string {
	if a == nil || a.Cfg == nil {
		return ""
	}
	if a.verifyIndependent(role) {
		return strings.TrimSpace(a.Cfg.LLM.Tiers[DefaultVerifyTier])
	}
	return a.execModel(role)
}

func (a *Agent) verifyIndependent(role string) bool {
	if a == nil || a.Cfg == nil || a.Cfg.LLM.Provider == "mock" {
		return false
	}
	m := strings.TrimSpace(a.Cfg.LLM.Tiers[DefaultVerifyTier])
	return m != "" && m != a.execModel(role)
}

func (a *Agent) execModel(role string) string {
	if a == nil || a.Cfg == nil {
		return ""
	}
	if t := a.EffectiveTier(role); t != "" {
		return strings.TrimSpace(a.Cfg.LLM.Tiers[t])
	}
	return a.Cfg.LLM.Model
}

func (a *Agent) EffectiveTier(role string) string {
	if a == nil || a.Cfg == nil {
		return ""
	}
	tier := FindRole(role).ModelTier
	if tier == "" || len(a.Cfg.LLM.Tiers) == 0 {
		return ""
	}
	model := strings.TrimSpace(a.Cfg.LLM.Tiers[tier])
	if model == "" || model == a.Cfg.LLM.Model {
		return ""
	}
	if a.Cfg.LLM.Provider == "mock" {
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
