package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gleam/internal/atomicfile"
	"gleam/pkg/types"
)

type Config struct {
	LLM              LLMConfig
	Agent            AgentConfig
	Safety           SafetyConfig
	Memory           MemoryConfig
	Scheduler        SchedulerConfig
	Persona          PersonaConfig
	Network          NetworkConfig
	Git              GitConfig
	Worktrees        WorktreeConfig
	MCP              []MCPServerConfig
	DataDir          string
	Workspace        string
	WorkspaceRecents []string
}

type GitConfig struct {
	BranchPrefix       string
	ForcePush          bool
	CommitInstructions string
}

type WorktreeConfig struct {
	Enabled           bool
	FetchBeforeCreate bool
	AutoDelete        bool
	MaxCount          int
}

type NetworkConfig struct {
	ProxyMode string
	ProxyURL  string
}

func (n NetworkConfig) ProxyModeOrDefault() string {
	if n.ProxyMode == "" {
		return "system"
	}
	return n.ProxyMode
}

type ModelEntry struct {
	ID         string `yaml:"id" json:"id"`
	Name       string `yaml:"name" json:"name"`
	ProviderID string `yaml:"provider_id" json:"provider_id"`
	Protocol   string `yaml:"protocol" json:"protocol"`
	BaseURL    string `yaml:"base_url" json:"base_url"`
	Model      string `yaml:"model" json:"model"`
	Plan       string `yaml:"plan" json:"plan"`
	APIKey     string `yaml:"api_key" json:"api_key"`
	IsDefault  bool   `yaml:"is_default" json:"is_default"`
	IsFast     bool   `yaml:"is_fast" json:"is_fast"`
}

type LLMConfig struct {
	Provider      string
	Protocol      string
	ProviderID    string
	Plan          string
	BaseURL       string
	Model         string
	FastModel     string
	Tiers         map[string]string
	Models        []ModelEntry
	APIKey        string
	APIKeyScope   string
	Temperature   float64
	MaxTokens     int
	TimeoutSecs   int
	ContextWindow int
}

const DefaultMaxSteps = 32

type AgentConfig struct {
	MaxReplans          int
	MaxSteps            int
	StepTimeoutSecs     int
	StepRetries         int
	DoneThreshold       int
	MaxConcurrency      int
	ReflectEachStep     bool
	ContextCompress     bool
	SkillAutoOptimize   bool
	GEOEnabled          bool
	DedupeCalls         bool
	MaxLLMCallsPerTask  int
	MaxTokensPerTask    int
	MaxTaskDurationSecs int
	StuckThreshold      int
	MaxOutputRunes      int
	MaxToolSchemas      int
	ChatAcceptance      bool
}

type SafetyConfig struct {
	Mode                string
	ApprovalTimeoutSecs int
	TrustedPaths        []string
	TrustedTools        []string
	ToolPermissions     map[string]string
	AIReview            bool
	DeepReview          bool
	AllowPrivateWeb     bool
}

type MemoryConfig struct {
	ShortTermCap int
	VectorDim    int
	MaxItems     int
	ProjectScope bool
}

type SchedulerConfig struct {
	Enabled bool
}

type PersonaConfig struct {
	Name  string
	Style string
}

type MCPServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Headers map[string]string
	Trust   string
	Enabled bool
}

func Default() *Config {
	return &Config{
		LLM: LLMConfig{
			Provider:    "glm",
			BaseURL:     "https://open.bigmodel.cn/api/paas/v4",
			Model:       "glm-5.3-flash",
			Temperature: 0.3,
			MaxTokens:   4096,
			TimeoutSecs: 60,
		},
		Agent: AgentConfig{
			MaxReplans:          2,
			MaxSteps:            DefaultMaxSteps,
			StepTimeoutSecs:     30,
			StepRetries:         1,
			DoneThreshold:       80,
			MaxConcurrency:      8,
			ReflectEachStep:     false,
			ContextCompress:     true,
			SkillAutoOptimize:   true,
			GEOEnabled:          true,
			DedupeCalls:         true,
			MaxLLMCallsPerTask:  40,
			MaxTokensPerTask:    300000,
			MaxTaskDurationSecs: 900,
			StuckThreshold:      3,
			MaxOutputRunes:      6000,
			MaxToolSchemas:      12,
			ChatAcceptance:      true,
		},
		Safety: SafetyConfig{
			Mode:                "auto",
			ApprovalTimeoutSecs: 300,
			AIReview:            true,
		},
		Memory: MemoryConfig{
			ShortTermCap: 20,
			VectorDim:    256,
			MaxItems:     5000,
		},
		Scheduler: SchedulerConfig{Enabled: true},
		Persona:   PersonaConfig{Name: "Gleam", Style: "efficient"},
		Git:       GitConfig{BranchPrefix: "gleam/"},
		Worktrees: WorktreeConfig{},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取配置文件失败: %w", err)
		}
		m, err := ParseYAML(string(data))
		if err != nil {
			return nil, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
		}
		cfg.apply(m)
	}
	cfg.applyEnv()
	if cfg.DataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		cfg.DataDir = filepath.Join(home, ".gleam")
	}
	return cfg, nil
}

func (c *Config) Apply(m map[string]any) { c.apply(m) }

func (c *Config) Snapshot() types.ConfigSnapshot {
	if c == nil {
		return types.ConfigSnapshot{}
	}
	tiers := make(map[string]string, len(c.LLM.Tiers))
	for k, v := range c.LLM.Tiers {
		tiers[k] = v
	}
	return types.ConfigSnapshot{
		Provider:        c.LLM.Provider,
		Model:           c.LLM.Model,
		FastModel:       c.LLM.FastModel,
		Tiers:           tiers,
		SafetyMode:      c.Safety.Mode,
		DoneThreshold:   c.Agent.DoneThreshold,
		MaxReplans:      c.Agent.MaxReplans,
		MaxSteps:        c.Agent.MaxSteps,
		StepTimeoutSecs: c.Agent.StepTimeoutSecs,
		StepRetries:     c.Agent.StepRetries,
		MaxConcurrency:  c.Agent.MaxConcurrency,
		DedupeCalls:     c.Agent.DedupeCalls,
		MaxOutputRunes:  c.Agent.MaxOutputRunes,
		MaxToolSchemas:  c.Agent.MaxToolSchemas,
		ReflectEachStep: c.Agent.ReflectEachStep,
	}
}

const OverlayFile = "settings.yaml"

func (c *Config) SaveOverlay(path string) error {
	root := NewYMap()
	llm := NewYMap()
	llm.Set("protocol", c.LLM.Protocol)
	llm.Set("provider_id", c.LLM.ProviderID)
	llm.Set("plan", c.LLM.Plan)
	llm.Set("base_url", c.LLM.BaseURL)
	llm.Set("model", c.LLM.Model)
	llm.Set("fast_model", c.LLM.FastModel)
	if len(c.LLM.Tiers) > 0 {
		tiers := NewYMap()
		for name, model := range c.LLM.Tiers {
			tiers.Set(name, model)
		}
		llm.Set("tiers", tiers)
	}
	if len(c.LLM.Models) > 0 {
		modelsArr := make([]any, 0, len(c.LLM.Models))
		for _, m := range c.LLM.Models {
			mm := NewYMap()
			mm.Set("id", m.ID)
			mm.Set("name", m.Name)
			mm.Set("provider_id", m.ProviderID)
			mm.Set("protocol", m.Protocol)
			mm.Set("base_url", m.BaseURL)
			mm.Set("model", m.Model)
			mm.Set("plan", m.Plan)
			// Keys go into the encrypted credential file (per endpoint host);
			// the config keeps only public metadata.
			mm.Set("is_default", m.IsDefault)
			mm.Set("is_fast", m.IsFast)
			modelsArr = append(modelsArr, mm)
		}
		llm.Set("models", modelsArr)
	}
	llm.Set("temperature", c.LLM.Temperature)
	llm.Set("max_tokens", c.LLM.MaxTokens)
	llm.Set("timeout_seconds", c.LLM.TimeoutSecs)
	llm.Set("context_window", c.LLM.ContextWindow)
	root.Set("llm", llm)

	agent := NewYMap()
	agent.Set("max_replans", c.Agent.MaxReplans)
	agent.Set("max_steps", c.Agent.MaxSteps)
	agent.Set("step_timeout_seconds", c.Agent.StepTimeoutSecs)
	agent.Set("step_retries", c.Agent.StepRetries)
	agent.Set("done_threshold", c.Agent.DoneThreshold)
	agent.Set("max_concurrency", c.Agent.MaxConcurrency)
	agent.Set("reflect_each_step", c.Agent.ReflectEachStep)
	agent.Set("context_compress", c.Agent.ContextCompress)
	agent.Set("skill_auto_optimize", c.Agent.SkillAutoOptimize)
	agent.Set("geo_enabled", c.Agent.GEOEnabled)
	agent.Set("dedupe_calls", c.Agent.DedupeCalls)
	agent.Set("max_llm_calls_per_task", c.Agent.MaxLLMCallsPerTask)
	agent.Set("max_tokens_per_task", c.Agent.MaxTokensPerTask)
	agent.Set("max_task_duration_seconds", c.Agent.MaxTaskDurationSecs)
	agent.Set("stuck_threshold", c.Agent.StuckThreshold)
	agent.Set("max_output_runes", c.Agent.MaxOutputRunes)
	agent.Set("max_tool_schemas", c.Agent.MaxToolSchemas)
	agent.Set("chat_acceptance", c.Agent.ChatAcceptance)
	root.Set("agent", agent)

	safety := NewYMap()
	safety.Set("mode", c.Safety.Mode)
	safety.Set("approval_timeout_seconds", c.Safety.ApprovalTimeoutSecs)
	safety.Set("ai_review", c.Safety.AIReview)
	safety.Set("deep_review", c.Safety.DeepReview)
	safety.Set("allow_private_web", c.Safety.AllowPrivateWeb)
	tpMap := NewYMap()
	for name, perm := range c.Safety.ToolPermissions {
		tpMap.Set(name, perm)
	}
	safety.Set("tool_permissions", tpMap)
	root.Set("safety", safety)

	mem := NewYMap()
	mem.Set("short_term_capacity", c.Memory.ShortTermCap)
	mem.Set("vector_dim", c.Memory.VectorDim)
	mem.Set("max_items", c.Memory.MaxItems)
	mem.Set("project_scope", c.Memory.ProjectScope)
	root.Set("memory", mem)

	gitc := NewYMap()
	gitc.Set("branch_prefix", c.Git.BranchPrefix)
	gitc.Set("force_push", c.Git.ForcePush)
	gitc.Set("commit_instructions", c.Git.CommitInstructions)
	root.Set("git", gitc)

	wt := NewYMap()
	wt.Set("enabled", c.Worktrees.Enabled)
	wt.Set("fetch_before_create", c.Worktrees.FetchBeforeCreate)
	wt.Set("auto_delete", c.Worktrees.AutoDelete)
	wt.Set("max_count", c.Worktrees.MaxCount)
	root.Set("worktrees", wt)

	persona := NewYMap()
	persona.Set("name", c.Persona.Name)
	persona.Set("style", c.Persona.Style)
	root.Set("persona", persona)

	sched := NewYMap()
	sched.Set("enabled", c.Scheduler.Enabled)
	root.Set("scheduler", sched)

	netw := NewYMap()
	netw.Set("proxy_mode", c.Network.ProxyModeOrDefault())
	netw.Set("proxy_url", c.Network.ProxyURL)
	root.Set("network", netw)

	root.Set("workspace", c.Workspace)
	wsArr := make([]any, 0, len(c.WorkspaceRecents))
	for _, w := range c.WorkspaceRecents {
		wsArr = append(wsArr, w)
	}
	root.Set("workspace_recents", wsArr)

	mcpArr := make([]any, 0, len(c.MCP))
	for _, s := range c.MCP {
		sm := NewYMap()
		sm.Set("name", s.Name)
		sm.Set("command", s.Command)
		args := make([]any, 0, len(s.Args))
		for _, a := range s.Args {
			args = append(args, a)
		}
		sm.Set("args", args)
		if len(s.Env) > 0 {
			env := NewYMap()
			for k, v := range s.Env {
				env.Set(k, v)
			}
			sm.Set("env", env)
		}
		sm.Set("url", s.URL)
		if len(s.Headers) > 0 {
			headers := NewYMap()
			for k, v := range s.Headers {
				headers.Set(k, v)
			}
			sm.Set("headers", headers)
		}
		sm.Set("trust", s.Trust)
		sm.Set("enabled", s.Enabled)
		mcpArr = append(mcpArr, sm)
	}
	root.Set("mcp", mcpArr)

	data, err := MarshalYAML(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o644)
}

func LoadOverlay(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	m, err := ParseYAML(string(data))
	if err != nil {
		_ = os.Rename(path, path+".corrupt")
		return nil
	}
	cfg.apply(m)
	return nil
}

func (c *Config) apply(m map[string]any) {
	if v, ok := sub(m, "llm"); ok {
		getStr(v, "provider", &c.LLM.Provider)
		getStr(v, "protocol", &c.LLM.Protocol)
		getStr(v, "provider_id", &c.LLM.ProviderID)
		getStr(v, "plan", &c.LLM.Plan)
		getStrAssign(v, "base_url", &c.LLM.BaseURL)
		getStr(v, "model", &c.LLM.Model)
		getStrAssign(v, "fast_model", &c.LLM.FastModel)
		if tiers, ok := v["tiers"].(map[string]any); ok {
			c.LLM.Tiers = map[string]string{}
			for name, model := range tiers {
				name = strings.TrimSpace(name)
				s, _ := model.(string)
				s = strings.TrimSpace(s)
				if name != "" && s != "" {
					c.LLM.Tiers[name] = s
				}
			}
		}
		if raw, ok := v["models"]; ok {
			if arr, ok := raw.([]any); ok {
				c.LLM.Models = c.LLM.Models[:0]
				for _, item := range arr {
					sm, ok := item.(map[string]any)
					if !ok {
						continue
					}
					m := ModelEntry{}
					getStr(sm, "id", &m.ID)
					getStr(sm, "name", &m.Name)
					getStr(sm, "provider_id", &m.ProviderID)
					getStr(sm, "protocol", &m.Protocol)
					getStrAssign(sm, "base_url", &m.BaseURL)
					getStr(sm, "model", &m.Model)
					getStr(sm, "plan", &m.Plan)
					getStrAssign(sm, "api_key", &m.APIKey)
					getBool(sm, "is_default", &m.IsDefault)
					getBool(sm, "is_fast", &m.IsFast)
					if m.Name != "" && m.Model != "" {
						c.LLM.Models = append(c.LLM.Models, m)
					}
				}
			}
		}
		getStrAssign(v, "api_key", &c.LLM.APIKey)
		getFloat(v, "temperature", &c.LLM.Temperature)
		getInt(v, "max_tokens", &c.LLM.MaxTokens)
		getInt(v, "timeout_seconds", &c.LLM.TimeoutSecs)
		getInt(v, "context_window", &c.LLM.ContextWindow)
	}
	if v, ok := sub(m, "agent"); ok {
		getInt(v, "max_replans", &c.Agent.MaxReplans)
		getInt(v, "max_steps", &c.Agent.MaxSteps)
		getInt(v, "step_timeout_seconds", &c.Agent.StepTimeoutSecs)
		getInt(v, "step_retries", &c.Agent.StepRetries)
		getInt(v, "done_threshold", &c.Agent.DoneThreshold)
		getInt(v, "max_concurrency", &c.Agent.MaxConcurrency)
		getBool(v, "reflect_each_step", &c.Agent.ReflectEachStep)
		getBool(v, "context_compress", &c.Agent.ContextCompress)
		getBool(v, "skill_auto_optimize", &c.Agent.SkillAutoOptimize)
		getBool(v, "geo_enabled", &c.Agent.GEOEnabled)
		getBool(v, "dedupe_calls", &c.Agent.DedupeCalls)
		getInt(v, "max_llm_calls_per_task", &c.Agent.MaxLLMCallsPerTask)
		getInt(v, "max_tokens_per_task", &c.Agent.MaxTokensPerTask)
		getInt(v, "max_task_duration_seconds", &c.Agent.MaxTaskDurationSecs)
		getInt(v, "stuck_threshold", &c.Agent.StuckThreshold)
		getInt(v, "max_output_runes", &c.Agent.MaxOutputRunes)
		getInt(v, "max_tool_schemas", &c.Agent.MaxToolSchemas)
		getBool(v, "chat_acceptance", &c.Agent.ChatAcceptance)
	}
	if v, ok := sub(m, "safety"); ok {
		getStr(v, "mode", &c.Safety.Mode)
		getInt(v, "approval_timeout_seconds", &c.Safety.ApprovalTimeoutSecs)
		getBool(v, "ai_review", &c.Safety.AIReview)
		getBool(v, "deep_review", &c.Safety.DeepReview)
		getBool(v, "allow_private_web", &c.Safety.AllowPrivateWeb)
		getStrs(v, "trusted_paths", &c.Safety.TrustedPaths)
		getStrs(v, "trusted_tools", &c.Safety.TrustedTools)
		if tp, ok := v["tool_permissions"].(map[string]any); ok {
			c.Safety.ToolPermissions = map[string]string{}
			for name, perm := range tp {
				if ps, ok := perm.(string); ok {
					c.Safety.ToolPermissions[name] = ps
				}
			}
		}
	}
	if v, ok := sub(m, "memory"); ok {
		getInt(v, "short_term_capacity", &c.Memory.ShortTermCap)
		getInt(v, "vector_dim", &c.Memory.VectorDim)
		getInt(v, "max_items", &c.Memory.MaxItems)
		getBool(v, "project_scope", &c.Memory.ProjectScope)
	}
	if v, ok := sub(m, "scheduler"); ok {
		getBool(v, "enabled", &c.Scheduler.Enabled)
	}
	if v, ok := sub(m, "persona"); ok {
		getStr(v, "name", &c.Persona.Name)
		getStr(v, "style", &c.Persona.Style)
	}
	if v, ok := sub(m, "git"); ok {
		getStr(v, "branch_prefix", &c.Git.BranchPrefix)
		getBool(v, "force_push", &c.Git.ForcePush)
		getStr(v, "commit_instructions", &c.Git.CommitInstructions)
	}
	if v, ok := sub(m, "worktrees"); ok {
		getBool(v, "enabled", &c.Worktrees.Enabled)
		getBool(v, "fetch_before_create", &c.Worktrees.FetchBeforeCreate)
		getBool(v, "auto_delete", &c.Worktrees.AutoDelete)
		getInt(v, "max_count", &c.Worktrees.MaxCount)
	}
	if v, ok := sub(m, "network"); ok {
		getStr(v, "proxy_mode", &c.Network.ProxyMode)
		getStr(v, "proxy_url", &c.Network.ProxyURL)
	}
	if raw, ok := m["mcp"]; ok {
		if arr, ok := raw.([]any); ok {
			c.MCP = c.MCP[:0]
			for _, item := range arr {
				sm, ok := item.(map[string]any)
				if !ok {
					continue
				}
				srv := MCPServerConfig{Trust: "user_approved", Enabled: true}
				getStr(sm, "name", &srv.Name)
				getStr(sm, "command", &srv.Command)
				getStrs(sm, "args", &srv.Args)
				getStr(sm, "url", &srv.URL)
				getStrMap(sm, "env", &srv.Env)
				getStrMap(sm, "headers", &srv.Headers)
				getStr(sm, "trust", &srv.Trust)
				if b, ok := sm["enabled"].(bool); ok {
					srv.Enabled = b
				}
				// Remote MCP entries have only a URL and no command; do not
				// drop them by the stdio condition.
				if srv.Name != "" && (srv.Command != "" || srv.URL != "") {
					c.MCP = append(c.MCP, srv)
				}
			}
		}
	}
	getStr(m, "data_dir", &c.DataDir)
	getStr(m, "workspace", &c.Workspace)
	getStrs(m, "workspace_recents", &c.WorkspaceRecents)
}

func (c *Config) applyEnv() {
	if v := os.Getenv("GLEAM_API_KEY"); v != "" {
		c.LLM.APIKey = v
	}
	if v := os.Getenv("GLEAM_LLM_BASE_URL"); v != "" {
		c.LLM.BaseURL = v
	}
	if v := os.Getenv("GLEAM_LLM_MODEL"); v != "" {
		c.LLM.Model = v
	}
	if os.Getenv("GLEAM_MOCK_LLM") == "1" {
		c.LLM.Provider = "mock"
	}
	if v := os.Getenv("GLEAM_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("GLEAM_WORKSPACE"); v != "" {
		c.Workspace = v
	}
	if v := os.Getenv("GLEAM_SAFETY_MODE"); v != "" {
		c.Safety.Mode = v
	}
}

func sub(m map[string]any, key string) (map[string]any, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	sm, ok := v.(map[string]any)
	return sm, ok
}

func getStr(m map[string]any, key string, dst *string) {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			*dst = s
		}
	}
}

func getStrAssign(m map[string]any, key string, dst *string) {
	if v, ok := m[key].(string); ok {
		*dst = v
	}
}

func getInt(m map[string]any, key string, dst *int) {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int64:
			*dst = int(n)
		case int:
			*dst = n
		case float64:
			*dst = int(n)
		case string:
			if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
				*dst = i
			}
		}
	}
}

func getFloat(m map[string]any, key string, dst *float64) {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			*dst = n
		case int64:
			*dst = float64(n)
		case int:
			*dst = float64(n)
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				*dst = f
			}
		}
	}
}

func getBool(m map[string]any, key string, dst *bool) {
	if v, ok := m[key].(bool); ok {
		*dst = v
	}
}

func getStrMap(m map[string]any, key string, dst *map[string]string) {
	v, ok := m[key]
	if !ok {
		return
	}
	raw, ok := v.(map[string]any)
	if !ok || len(raw) == 0 {
		return
	}
	out := make(map[string]string, len(raw))
	for k, val := range raw {
		k = strings.TrimSpace(k)
		s, ok := val.(string)
		if k == "" || !ok || strings.TrimSpace(s) == "" {
			continue
		}
		out[k] = s
	}
	if len(out) > 0 {
		*dst = out
	}
}

func getStrs(m map[string]any, key string, dst *[]string) {
	v, ok := m[key]
	if !ok {
		return
	}
	arr, ok := v.([]any)
	if !ok {
		if s, ok := v.(string); ok && s != "" {
			*dst = []string{s}
		}
		return
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	if len(out) > 0 {
		*dst = out
	}
}
