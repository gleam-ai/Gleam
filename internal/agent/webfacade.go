package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"gleam/internal/config"
	"gleam/internal/harness/growth"
	"gleam/internal/harness/safety"
	"gleam/internal/harness/scheduler"
	"gleam/internal/harness/skill"
	"gleam/internal/llm"
	"gleam/internal/market"
	"gleam/internal/nlcron"
	"gleam/internal/tools/mcp"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

var mcpNameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,32}$`)

func (a *Agent) LLMName() string {
	if a.LLM == nil {
		return "unavailable"
	}
	return a.LLM.Name()
}

func (a *Agent) ToolNames() []string { return a.Reg.Names() }

func (a *Agent) ToolList() []map[string]any {
	list := make([]map[string]any, 0)
	for _, t := range a.Reg.List() {
		effective := a.Gate.EffectivePermission(t)
		_, overridden := a.Gate.OverrideOf(t.Name())
		list = append(list, map[string]any{
			"name":        t.Name(),
			"description": t.Description(),
			"permission":  effective.String(),
			"overridden":  overridden,
			"schema":      t.Schema(),
		})
	}
	return list
}

func (a *Agent) ToolPermissionSet(name, perm string) (map[string]any, error) {
	if _, ok := a.Reg.Get(name); !ok {
		return nil, fmt.Errorf("工具 %q 不存在", name)
	}
	switch perm {
	case "default":
		a.Gate.ClearToolPermission(name)
	case "readonly", "user_approved", "full_access":
		a.Gate.SetToolPermission(name, permFromStr(perm))
	default:
		return nil, fmt.Errorf("非法权限 %q（readonly | user_approved | full_access | default）", perm)
	}
	a.Cfg.Safety.ToolPermissions = map[string]string{}
	for _, t := range a.Reg.List() {
		if p, ok := a.Gate.OverrideOf(t.Name()); ok {
			a.Cfg.Safety.ToolPermissions[t.Name()] = p.String()
		}
	}
	if err := a.Cfg.SaveOverlay(filepath.Join(a.Cfg.DataDir, config.OverlayFile)); err != nil {
		return map[string]any{
			"name":       name,
			"permission": a.PermissionOf(name).String(),
			"overridden": perm != "default",
			"tools":      a.ToolList(),
			"warning":    "权限已生效但未保存到磁盘，重启后可能丢失：" + err.Error(),
		}, nil
	}
	return map[string]any{
		"name":       name,
		"permission": a.PermissionOf(name).String(),
		"overridden": perm != "default",
		"tools":      a.ToolList(),
	}, nil
}

func (a *Agent) DirectToolCall(ctx context.Context, name string, args map[string]any,
	approve func(types.ApprovalRequest) types.ApprovalResponse) (map[string]any, error) {
	tool, ok := a.Reg.Get(name)
	if !ok {
		return nil, fmt.Errorf("工具 %q 不存在", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	if err := toolutil.ValidateArgs(tool, args); err != nil {
		return nil, fmt.Errorf("工具 %s 参数不合法: %w", tool.Name(), err)
	}
	// Pin the roots for this call so a concurrent workspace switch cannot move
	// the boundary between approval and execution.
	ctx = toolutil.WithRoots(ctx, a.workspaceRoots())
	dec := a.Gate.EvaluateStepIn(ctx, tool, args, false)
	if dec.NeedApproval {
		detail, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("参数无法序列化: %w", err)
		}
		resp := approve(types.ApprovalRequest{
			Plan:   []string{fmt.Sprintf("%s: %s", tool.Name(), detail)},
			Risk:   dec.Risk,
			Reason: dec.Reason,
		})
		if !resp.Approved {
			return nil, fmt.Errorf("操作被拒绝: %s", resp.Note)
		}
	}
	out, err := tool.Execute(ctx, args)
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": tool.Name(), "output": out}, nil
}

func (a *Agent) MemorySearch(query string, k int) []types.MemoryHitView {
	if a.Mem == nil {
		return nil
	}
	hits := a.Mem.RelevantScoped(query, k, a.MemoryScope())
	out := make([]types.MemoryHitView, 0, len(hits))
	for _, h := range hits {
		out = append(out, types.MemoryHitView{ID: h.ID, Content: h.Content, Tags: h.Tags, Score: h.Score})
	}
	return out
}

func (a *Agent) MemoryCount() int {
	if a.Mem == nil || a.Mem.Long == nil {
		return 0
	}
	return a.Mem.Long.Count()
}

func (a *Agent) MemoryDelete(id string) bool {
	if a.Mem == nil || a.Mem.Long == nil {
		return false
	}
	return a.Mem.Long.SoftDelete(id)
}

func (a *Agent) MemorySave(content string, tags []string) (string, error) {
	if a.Mem == nil {
		return "", errors.New("记忆系统未启用")
	}
	return a.Mem.RememberScoped(content, "", a.MemoryScope(), tags)
}

func (a *Agent) MemoryScope() string {
	if a.Cfg == nil {
		return ""
	}
	return a.memoryScopeFor(a.Cfg.Workspace)
}

func (a *Agent) memoryScopeFor(workspace string) string {
	if a.Cfg == nil || !a.Cfg.Memory.ProjectScope {
		return ""
	}
	return strings.TrimSpace(workspace)
}

func (a *Agent) SkillList() []skill.Skill { return a.Skills.List() }

func (a *Agent) SkillGet(name string) (*skill.Skill, error) { return a.Skills.Get(name) }

func (a *Agent) SkillSave(sk skill.Skill) (int, error) {
	v, err := a.Skills.Save(sk)
	if err == nil && v == 1 {
		a.recordSkillCreated(sk.Name, sk.Description)
	}
	return v, err
}

func (a *Agent) recordSkillCreated(name, description string) {
	if a.Growth == nil {
		return
	}
	a.Growth.Record(growth.Entry{Type: "skill_created", SkillName: name, Goal: description})
}

func (a *Agent) SkillRun(ctx context.Context, name string, params map[string]string) (map[string]any, error) {
	return a.RunSkill(ctx, name, params)
}

func (a *Agent) SkillDelete(name string) error { return a.Skills.Delete(name) }

func (a *Agent) SkillSetEnabled(name string, enabled bool) (map[string]any, error) {
	s, err := a.Skills.SetDisabled(name, !enabled)
	if err != nil {
		return nil, err
	}
	list := a.SkillList()
	return map[string]any{
		"name": s.Name, "enabled": !s.Disabled, "version": s.Version,
		"count": len(list), "skills": list,
	}, nil
}

func (a *Agent) ScheduleList() []types.ScheduleJobView {
	if a.Sched == nil {
		return nil
	}
	jobs := a.Sched.ListJobs()
	out := make([]types.ScheduleJobView, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, jobToScheduleView(j))
	}
	return out
}

func (a *Agent) ScheduleCreate(name, cron string, intervalSec int, goal, mode string) (types.ScheduleJobView, error) {
	return a.ScheduleCreateNatural(name, goal, mode, "", cron, intervalSec)
}

func (a *Agent) ScheduleCreateNatural(name, goal, mode, when, cron string, intervalSec int) (types.ScheduleJobView, error) {
	return a.ScheduleCreateWithNotify(name, goal, mode, when, cron, intervalSec, "")
}

func (a *Agent) ScheduleCreateWithNotify(name, goal, mode, when, cron string, intervalSec int, notify string) (types.ScheduleJobView, error) {
	if a.Sched == nil {
		return types.ScheduleJobView{}, fmt.Errorf("调度器未启用")
	}
	policy, err := scheduler.NormalizeNotify(notify)
	if err != nil {
		return types.ScheduleJobView{}, err
	}
	cron, intervalSec, summary, err := nlcron.Resolve(cron, intervalSec, when)
	if err != nil {
		return types.ScheduleJobView{}, err
	}
	j, err := a.Sched.AddJobDetailed(name, cron, intervalSec, goal, mode, strings.TrimSpace(when), summary)
	if err != nil {
		return types.ScheduleJobView{}, err
	}
	if strings.TrimSpace(notify) != "" {
		if err := a.Sched.SetNotify(j.Name, policy); err != nil {
			return types.ScheduleJobView{}, err
		}
	}
	return a.scheduleView(j.Name), nil
}

func (a *Agent) ScheduleSetNotify(name, policy string) (types.ScheduleJobView, error) {
	if a.Sched == nil {
		return types.ScheduleJobView{}, fmt.Errorf("调度器未启用")
	}
	if err := a.Sched.SetNotify(name, policy); err != nil {
		return types.ScheduleJobView{}, err
	}
	return a.scheduleView(name), nil
}

func (a *Agent) ScheduleSetEnabled(name string, enabled bool) (types.ScheduleJobView, error) {
	if a.Sched == nil {
		return types.ScheduleJobView{}, fmt.Errorf("调度器未启用")
	}
	if err := a.Sched.SetEnabled(name, enabled); err != nil {
		return types.ScheduleJobView{}, err
	}
	return a.scheduleView(name), nil
}

func (a *Agent) ScheduleDelete(name string) (bool, error) {
	if a.Sched == nil {
		return false, fmt.Errorf("调度器未启用")
	}
	return a.Sched.DeleteJob(name)
}

func (a *Agent) ScheduleTrigger(name string) (bool, error) {
	if a.Sched == nil {
		return false, fmt.Errorf("调度器未启用")
	}
	return a.Sched.TriggerNow(name)
}

var settingsStyles = map[string]bool{"rigorous": true, "gentle": true, "efficient": true}

var settingsModes = map[string]bool{"auto": true, "plan_first": true, "interactive": true}

func (a *Agent) SettingsView() map[string]any {
	cfg := a.Cfg
	var apiKeySet bool
	var keyHost string
	if def := a.defaultModelEntry(); def != nil {
		key, kh := a.llmKeyFor(def.BaseURL, def.APIKey)
		apiKeySet = key != ""
		keyHost = kh
	} else {
		activeKey, kh := a.llmKeyFor(cfg.LLM.BaseURL, "")
		apiKeySet = activeKey != ""
		keyHost = kh
	}
	fastModel := cfg.LLM.FastModel
	if fast := a.fastModelEntry(); fast != nil {
		fastModel = fast.Model
	}
	return map[string]any{
		"persona": map[string]any{"name": cfg.Persona.Name, "style": cfg.Persona.Style},
		"git": map[string]any{
			"branch_prefix":       cfg.Git.BranchPrefix,
			"force_push":          cfg.Git.ForcePush,
			"commit_instructions": cfg.Git.CommitInstructions,
		},
		"worktrees": map[string]any{
			"enabled":             cfg.Worktrees.Enabled,
			"fetch_before_create": cfg.Worktrees.FetchBeforeCreate,
			"auto_delete":         cfg.Worktrees.AutoDelete,
			"max_count":           cfg.Worktrees.MaxCount,
		},
		"network": map[string]any{
			"proxy_mode": cfg.Network.ProxyModeOrDefault(),
			"proxy_url":  cfg.Network.ProxyURL,
		},
		"safety": map[string]any{
			"mode":                     cfg.Safety.Mode,
			"approval_timeout_seconds": cfg.Safety.ApprovalTimeoutSecs,
			"ai_review":                cfg.Safety.AIReview,
			"deep_review":              cfg.Safety.DeepReview,
		},
		"agent": map[string]any{
			"max_replans":               cfg.Agent.MaxReplans,
			"max_steps":                 cfg.Agent.MaxSteps,
			"step_timeout_seconds":      cfg.Agent.StepTimeoutSecs,
			"step_retries":              cfg.Agent.StepRetries,
			"done_threshold":            cfg.Agent.DoneThreshold,
			"max_concurrency":           cfg.Agent.MaxConcurrency,
			"context_compress":          cfg.Agent.ContextCompress,
			"skill_auto_optimize":       cfg.Agent.SkillAutoOptimize,
			"geo_enabled":               cfg.Agent.GEOEnabled,
			"dedupe_calls":              cfg.Agent.DedupeCalls,
			"max_llm_calls_per_task":    cfg.Agent.MaxLLMCallsPerTask,
			"max_tokens_per_task":       cfg.Agent.MaxTokensPerTask,
			"max_task_duration_seconds": cfg.Agent.MaxTaskDurationSecs,
			"stuck_threshold":           cfg.Agent.StuckThreshold,
			"max_output_runes":          cfg.Agent.MaxOutputRunes,
			"max_tool_schemas":          cfg.Agent.MaxToolSchemas,
			"chat_acceptance":           cfg.Agent.ChatAcceptance,
		},
		"memory": map[string]any{
			"short_term_capacity": cfg.Memory.ShortTermCap,
			"vector_dim":          cfg.Memory.VectorDim,
			"max_items":           cfg.Memory.MaxItems,
			"project_scope":       cfg.Memory.ProjectScope,
		},
		"llm": map[string]any{
			"provider":         cfg.LLM.Provider,
			"protocol":         cfg.LLM.Protocol,
			"provider_id":      cfg.LLM.ProviderID,
			"plan":             cfg.LLM.Plan,
			"model":            cfg.LLM.Model,
			"fast_model":       fastModel,
			"tiers":            tiersView(cfg.LLM.Tiers),
			"base_url":         cfg.LLM.BaseURL,
			"temperature":      cfg.LLM.Temperature,
			"max_tokens":       cfg.LLM.MaxTokens,
			"timeout_seconds":  cfg.LLM.TimeoutSecs,
			"context_window":   cfg.LLM.ContextWindow,
			"api_key_set":      apiKeySet,
			"api_key_host":     keyHost,
			"api_key_host_cur": llm.KeyScope(cfg.LLM.BaseURL),
			"models":           a.modelsView(cfg.LLM.Models),
		},
		"scheduler": map[string]any{"enabled": cfg.Scheduler.Enabled},
		"workspace": cfg.Workspace,
		"data_dir":  cfg.DataDir,
	}
}

func tiersView(tiers map[string]string) map[string]any {
	out := map[string]any{}
	for name, model := range tiers {
		out[name] = model
	}
	return out
}

func (a *Agent) modelsView(models []config.ModelEntry) []map[string]any {
	out := make([]map[string]any, 0, len(models))
	for _, m := range models {
		out = append(out, a.modelEntryView(m))
	}
	return out
}

func (a *Agent) modelEntryView(m config.ModelEntry) map[string]any {
	key, _ := a.llmKeyFor(m.BaseURL, m.APIKey)
	return map[string]any{
		"id": m.ID, "name": m.Name,
		"provider_id": m.ProviderID, "protocol": m.Protocol,
		"base_url": m.BaseURL, "model": m.Model, "plan": m.Plan,
		"is_default": m.IsDefault, "is_fast": m.IsFast,
		"api_key_set": strings.TrimSpace(key) != "",
	}
}

func maskAPIKey(key string) string {
	n := len([]rune(key))
	if n <= 6 {
		return "****"
	}
	runes := []rune(key)
	return string(runes[:4]) + "****" + string(runes[n-2:])
}

func modelEntryFromMap(m map[string]any) (config.ModelEntry, error) {
	e := config.ModelEntry{}
	str := func(k string) string {
		if v, ok := m[k].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	e.ID = str("id")
	e.Name = str("name")
	e.ProviderID = str("provider_id")
	e.Protocol = str("protocol")
	e.BaseURL = str("base_url")
	e.Model = str("model")
	e.Plan = str("plan")
	e.APIKey = str("api_key")
	if b, ok := m["is_default"].(bool); ok {
		e.IsDefault = b
	}
	if b, ok := m["is_fast"].(bool); ok {
		e.IsFast = b
	}
	if e.Name == "" {
		return e, fmt.Errorf("显示名不能为空")
	}
	if e.Model == "" {
		return e, fmt.Errorf("模型 ID 不能为空")
	}
	if strings.TrimSpace(e.BaseURL) == "" && llm.FindProvider(e.ProviderID) == nil {
		return e, fmt.Errorf("接口地址不能为空：选择厂商预设或手动填写 Base URL")
	}
	if e.Protocol != "" && !llm.ValidProtocol(e.Protocol) {
		return e, fmt.Errorf("不支持的 API 类型 %q", e.Protocol)
	}
	return e, nil
}

func (a *Agent) modelIndex(id string) int {
	for i, m := range a.Cfg.LLM.Models {
		if m.ID == id {
			return i
		}
	}
	return -1
}

func (a *Agent) defaultModelEntry() *config.ModelEntry {
	for i := range a.Cfg.LLM.Models {
		if a.Cfg.LLM.Models[i].IsDefault {
			return &a.Cfg.LLM.Models[i]
		}
	}
	if len(a.Cfg.LLM.Models) > 0 {
		return &a.Cfg.LLM.Models[0]
	}
	return nil
}

func (a *Agent) fastModelEntry() *config.ModelEntry {
	for i := range a.Cfg.LLM.Models {
		if a.Cfg.LLM.Models[i].IsFast {
			return &a.Cfg.LLM.Models[i]
		}
	}
	return nil
}

func (a *Agent) clearFastFlag() {
	for i := range a.Cfg.LLM.Models {
		a.Cfg.LLM.Models[i].IsFast = false
	}
}

// bindModelKey writes the model's own API key into the encrypted credential
// file; when credentials are unavailable it stays in memory (tests or read-only
// setups), so the key never lands in a read-only config.
func (a *Agent) bindModelKey(m *config.ModelEntry) error {
	key := strings.TrimSpace(m.APIKey)
	if key == "" || a.Creds == nil {
		return nil
	}
	if _, err := BindLLMKey(a.Cfg, a.Creds, m.BaseURL, key, false); err != nil {
		return err
	}
	m.APIKey = ""
	return nil
}

// InitModelRuntime runs once the credential store is ready: migrate legacy
// plaintext keys and rebuild the primary, auxiliary and tier clients from the
// default model.
func (a *Agent) InitModelRuntime() {
	a.migrateModelKeys()
	a.syncModelRuntime()
}

// migrateModelKeys moves model keys written to settings.yaml by older versions
// into the credential file.
func (a *Agent) migrateModelKeys() {
	if a == nil || a.Creds == nil {
		return
	}
	changed := false
	for i := range a.Cfg.LLM.Models {
		m := &a.Cfg.LLM.Models[i]
		if strings.TrimSpace(m.APIKey) == "" {
			continue
		}
		if _, err := BindLLMKey(a.Cfg, a.Creds, m.BaseURL, m.APIKey, false); err != nil {
			continue
		}
		m.APIKey = ""
		changed = true
	}
	if changed {
		_ = a.saveModelsOverlay()
	}
}

func (a *Agent) ModelsList() []map[string]any {
	return a.modelsView(a.Cfg.LLM.Models)
}

func (a *Agent) ModelAdd(raw map[string]any) (map[string]any, error) {
	entry, err := modelEntryFromMap(raw)
	if err != nil {
		return nil, err
	}
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("model-%d", time.Now().UnixNano()%100000)
	}
	if a.modelIndex(entry.ID) >= 0 {
		return nil, fmt.Errorf("模型 ID %q 已存在", entry.ID)
	}
	if err := a.bindModelKey(&entry); err != nil {
		return nil, err
	}
	if entry.IsDefault || !a.hasDefaultModel() {
		a.dropStaleTiersFor(entry)
		a.setDefaultFlag(entry.ID)
		entry.IsDefault = true
	}
	if entry.IsFast {
		a.clearFastFlag()
		a.Cfg.LLM.FastModel = ""
	}
	a.Cfg.LLM.Models = append(a.Cfg.LLM.Models, entry)
	if err := a.saveModelsOverlay(); err != nil {
		return nil, err
	}
	a.syncModelRuntime()
	return map[string]any{"model": a.modelEntryView(entry), "models": a.ModelsList(), "active": a.ActiveModel()}, nil
}

func (a *Agent) hasDefaultModel() bool {
	for i := range a.Cfg.LLM.Models {
		if a.Cfg.LLM.Models[i].IsDefault {
			return true
		}
	}
	return false
}

func (a *Agent) setDefaultFlag(id string) {
	for i := range a.Cfg.LLM.Models {
		a.Cfg.LLM.Models[i].IsDefault = a.Cfg.LLM.Models[i].ID == id
	}
}

func (a *Agent) ModelUpdate(id string, raw map[string]any) (map[string]any, error) {
	idx := a.modelIndex(id)
	if idx < 0 {
		return nil, fmt.Errorf("模型 %q 不存在", id)
	}
	entry, err := modelEntryFromMap(raw)
	if err != nil {
		return nil, err
	}
	entry.ID = id
	if _, hasKey := raw["api_key"]; !hasKey {
		entry.APIKey = a.Cfg.LLM.Models[idx].APIKey
	}
	if err := a.bindModelKey(&entry); err != nil {
		return nil, err
	}
	if entry.IsDefault {
		a.dropStaleTiersFor(entry)
		a.setDefaultFlag(id)
	}
	if entry.IsFast {
		a.clearFastFlag()
		a.Cfg.LLM.FastModel = ""
	}
	a.Cfg.LLM.Models[idx] = entry
	if err := a.saveModelsOverlay(); err != nil {
		return nil, err
	}
	a.syncModelRuntime()
	return map[string]any{"model": a.modelEntryView(entry), "models": a.ModelsList(), "active": a.ActiveModel()}, nil
}

func (a *Agent) ModelDelete(id string) (map[string]any, error) {
	idx := a.modelIndex(id)
	if idx < 0 {
		return nil, fmt.Errorf("模型 %q 不存在", id)
	}
	wasDefault := a.Cfg.LLM.Models[idx].IsDefault
	a.Cfg.LLM.Models = append(a.Cfg.LLM.Models[:idx], a.Cfg.LLM.Models[idx+1:]...)
	if wasDefault {
		if entry := a.defaultModelEntry(); entry != nil {
			a.dropStaleTiersFor(*entry)
		}
	}
	if err := a.saveModelsOverlay(); err != nil {
		return nil, err
	}
	a.syncModelRuntime()
	return map[string]any{"deleted": id, "models": a.ModelsList(), "active": a.ActiveModel()}, nil
}

func (a *Agent) ModelSetDefault(id string) (map[string]any, error) {
	idx := a.modelIndex(id)
	if idx < 0 {
		return nil, fmt.Errorf("模型 %q 不存在", id)
	}
	a.dropStaleTiersFor(a.Cfg.LLM.Models[idx])
	a.setDefaultFlag(id)
	if err := a.saveModelsOverlay(); err != nil {
		return nil, err
	}
	a.syncModelRuntime()
	return map[string]any{"model": a.modelEntryView(a.Cfg.LLM.Models[idx]), "models": a.ModelsList(), "active": a.ActiveModel()}, nil
}

func (a *Agent) SetActiveModel(id string) (map[string]any, error) {
	idx := a.modelIndex(id)
	if idx < 0 {
		return nil, fmt.Errorf("模型 %q 不存在", id)
	}
	a.dropStaleTiersFor(a.Cfg.LLM.Models[idx])
	a.setDefaultFlag(id)
	if err := a.saveModelsOverlay(); err != nil {
		return nil, err
	}
	a.syncModelRuntime()
	return map[string]any{"active": a.modelEntryView(a.Cfg.LLM.Models[idx]), "models": a.ModelsList()}, nil
}

func (a *Agent) ActiveModel() map[string]any {
	entry := a.defaultModelEntry()
	if entry == nil {
		return nil
	}
	return a.modelEntryView(*entry)
}

// dropStaleTiersFor forgets tier and legacy auxiliary model names when the
// connection host changes. Those names belong to the previous vendor and would
// otherwise be requested from the new endpoint.
func (a *Agent) dropStaleTiersFor(entry config.ModelEntry) {
	if a == nil || a.Cfg == nil {
		return
	}
	baseURL, _, _ := llm.ResolveTarget(entry.ProviderID, entry.Plan, entry.BaseURL, entry.Model, entry.Protocol)
	oldScope, nextScope := llm.KeyScope(a.Cfg.LLM.BaseURL), llm.KeyScope(baseURL)
	if oldScope == "" || nextScope == "" || oldScope == nextScope {
		return
	}
	a.Cfg.LLM.FastModel = ""
	a.Cfg.LLM.Tiers = nil
	a.clearFastFlag()
}

func (a *Agent) applyModelEntry(entry config.ModelEntry) error {
	if a.Cfg.LLM.Provider == "mock" {
		return nil
	}
	baseURL, model, protocol := llm.ResolveTarget(entry.ProviderID, entry.Plan, entry.BaseURL, entry.Model, entry.Protocol)
	key := strings.TrimSpace(entry.APIKey)
	if key == "" {
		key, _ = a.llmKeyFor(baseURL, "")
	}
	a.LLM = llm.New(protocol, baseURL, key, model,
		a.Cfg.LLM.Temperature, a.Cfg.LLM.MaxTokens, a.Cfg.LLM.TimeoutSecs)
	a.Cfg.LLM.BaseURL = baseURL
	a.Cfg.LLM.Model = model
	a.Cfg.LLM.Protocol = protocol
	a.Cfg.LLM.APIKey = key
	a.Cfg.LLM.APIKeyScope = llm.KeyScope(baseURL)
	if entry.ProviderID != "" {
		a.Cfg.LLM.ProviderID = entry.ProviderID
	}
	if entry.Plan != "" {
		a.Cfg.LLM.Plan = entry.Plan
	}
	return nil
}

func (a *Agent) syncModelRuntime() {
	if a == nil || a.Cfg == nil {
		return
	}
	if entry := a.defaultModelEntry(); entry != nil {
		if err := a.applyModelEntry(*entry); err != nil {
			return
		}
	}
	a.RebuildFastClient()
	a.RebuildTierClients()
}

func (a *Agent) saveModelsOverlay() error {
	if err := a.Cfg.SaveOverlay(filepath.Join(a.Cfg.DataDir, config.OverlayFile)); err != nil {
		return fmt.Errorf("已生效但保存失败: %w", err)
	}
	return nil
}

// hasRedactedProxyUser detects the placeholder the network view shows in place
// of real proxy credentials. Saving it would overwrite the password with ***.
func hasRedactedProxyUser(raw string) bool {
	if u, err := url.Parse(raw); err == nil && u.User != nil {
		return u.User.Username() == "***"
	}
	return strings.HasPrefix(raw, "***@")
}

func (a *Agent) ApplySettings(patch map[string]any) (map[string]any, error) {
	sanitized := map[string]any{}
	sub := func(section string) map[string]any {
		m, ok := sanitized[section].(map[string]any)
		if !ok {
			m = map[string]any{}
			sanitized[section] = m
		}
		return m
	}
	asInt := func(v any) (int, bool) {
		switch n := v.(type) {
		case float64:
			return int(n), true
		case int:
			return n, true
		case int64:
			return int(n), true
		case string:
			var i int
			if _, err := fmt.Sscanf(strings.TrimSpace(n), "%d", &i); err == nil {
				return i, true
			}
		}
		return 0, false
	}
	var outOfRange []string
	asFloat := func(v any) (float64, bool) {
		switch n := v.(type) {
		case float64:
			return n, true
		case int:
			return float64(n), true
		case int64:
			return float64(n), true
		}
		return 0, false
	}
	posInt := func(section, key string, v any, min, max int) {
		i, ok := asInt(v)
		if !ok {
			return
		}
		if i < min || i > max {
			outOfRange = append(outOfRange, fmt.Sprintf("%s=%d（允许 %d–%d）", key, i, min, max))
			return
		}
		sub(section)[key] = i
	}
	zeroOr := func(section, key string, v any, min, max int) {
		if i, ok := asInt(v); ok && i == 0 {
			sub(section)[key] = 0
			return
		}
		posInt(section, key, v, min, max)
	}

	if pm, ok := patch["persona"].(map[string]any); ok {
		if v, ok := pm["name"].(string); ok && strings.TrimSpace(v) != "" {
			sub("persona")["name"] = strings.TrimSpace(v)
		}
		if v, ok := pm["style"].(string); ok && settingsStyles[v] {
			sub("persona")["style"] = v
		}
	}
	if sm, ok := patch["safety"].(map[string]any); ok {
		if v, ok := sm["mode"].(string); ok && settingsModes[v] {
			sub("safety")["mode"] = v
		}
		if v, ok := sm["approval_timeout_seconds"]; ok {
			posInt("safety", "approval_timeout_seconds", v, 5, 3600)
		}
		if b, ok := sm["ai_review"].(bool); ok {
			sub("safety")["ai_review"] = b
		}
		if b, ok := sm["deep_review"].(bool); ok {
			sub("safety")["deep_review"] = b
		}
	}
	if am, ok := patch["agent"].(map[string]any); ok {
		for _, f := range []struct {
			key      string
			min, max int
		}{
			{"max_replans", 0, 20},
			{"max_steps", 0, 512},
			{"step_timeout_seconds", 0, 3600},
			{"step_retries", 0, 10},
			{"done_threshold", 0, 100},
			{"max_concurrency", 0, 16},
		} {
			if v, ok := am[f.key]; ok {
				posInt("agent", f.key, v, f.min, f.max)
			}
		}
		if b, ok := am["context_compress"].(bool); ok {
			sub("agent")["context_compress"] = b
		}
		if b, ok := am["skill_auto_optimize"].(bool); ok {
			sub("agent")["skill_auto_optimize"] = b
		}
		if b, ok := am["geo_enabled"].(bool); ok {
			sub("agent")["geo_enabled"] = b
		}
		if b, ok := am["dedupe_calls"].(bool); ok {
			sub("agent")["dedupe_calls"] = b
		}
		if i, ok := am["max_llm_calls_per_task"]; ok {
			zeroOr("agent", "max_llm_calls_per_task", i, 1, 10000)
		}
		if i, ok := am["max_tokens_per_task"]; ok {
			zeroOr("agent", "max_tokens_per_task", i, 1000, 100000000)
		}
		if i, ok := am["max_task_duration_seconds"]; ok {
			zeroOr("agent", "max_task_duration_seconds", i, 10, 86400)
		}
		if i, ok := am["stuck_threshold"]; ok {
			zeroOr("agent", "stuck_threshold", i, 2, 100)
		}
		if i, ok := am["max_output_runes"]; ok {
			zeroOr("agent", "max_output_runes", i, 500, 1000000)
		}
		if i, ok := am["max_tool_schemas"]; ok {
			zeroOr("agent", "max_tool_schemas", i, 5, 1000)
		}
		if b, ok := am["chat_acceptance"].(bool); ok {
			sub("agent")["chat_acceptance"] = b
		}
	}
	if mm, ok := patch["memory"].(map[string]any); ok {
		if v, ok := mm["short_term_capacity"]; ok {
			posInt("memory", "short_term_capacity", v, 2, 1000)
		}
		if v, ok := mm["vector_dim"]; ok {
			posInt("memory", "vector_dim", v, 32, 8192)
		}
		if v, ok := mm["max_items"]; ok {
			posInt("memory", "max_items", v, 10, 100000)
		}
		if b, ok := mm["project_scope"].(bool); ok {
			sub("memory")["project_scope"] = b
		}
	}
	if lm, ok := patch["llm"].(map[string]any); ok {
		if v, ok := lm["protocol"].(string); ok && llm.ValidProtocol(v) {
			sub("llm")["protocol"] = v
		} else if v, ok := lm["protocol"].(string); ok && strings.TrimSpace(v) == "" {
			sub("llm")["protocol"] = ""
		}
		if v, ok := lm["provider_id"].(string); ok {
			v = strings.TrimSpace(v)
			if v == "" || llm.FindProvider(v) != nil {
				sub("llm")["provider_id"] = v
			}
		}
		if v, ok := lm["plan"].(string); ok {
			v = strings.TrimSpace(v)
			switch v {
			case "", llm.PlanToken, llm.PlanCoding, llm.PlanAgent:
				sub("llm")["plan"] = v
			}
		}
		if v, ok := lm["base_url"].(string); ok {
			sub("llm")["base_url"] = strings.TrimSpace(v)
		}
		if v, ok := lm["model"].(string); ok && strings.TrimSpace(v) != "" {
			sub("llm")["model"] = strings.TrimSpace(v)
		}
		if v, ok := lm["fast_model"].(string); ok {
			sub("llm")["fast_model"] = strings.TrimSpace(v)
		}
		if v, ok := lm["tiers"].(map[string]any); ok {
			tiers := map[string]any{}
			for name, model := range v {
				name = strings.TrimSpace(name)
				s, _ := model.(string)
				s = strings.TrimSpace(s)
				if name != "" && s != "" {
					tiers[name] = s
				}
			}
			sub("llm")["tiers"] = tiers
		}
		if cv, ok := lm["clear_api_key"].(bool); ok && cv {
			sub("llm")["api_key"] = ""
			sub("llm")["clear_api_key"] = true
		} else if v, ok := lm["api_key"].(string); ok && strings.TrimSpace(v) != "" {
			sub("llm")["api_key"] = strings.TrimSpace(v)
		}
		if v, ok := asFloat(lm["temperature"]); ok {
			if v < 0 || v > 2 {
				outOfRange = append(outOfRange, fmt.Sprintf("temperature=%g（允许 0–2）", v))
			} else {
				sub("llm")["temperature"] = v
			}
		}
		if v, ok := lm["max_tokens"]; ok {
			posInt("llm", "max_tokens", v, 64, 1000000)
		}
		if v, ok := lm["timeout_seconds"]; ok {
			posInt("llm", "timeout_seconds", v, 3, 600)
		}
		if raw, ok := lm["context_window"]; ok {
			zeroOr("llm", "context_window", raw, 4096, 10000000)
		}
	}
	if v, ok := patch["scheduler"].(map[string]any); ok {
		if b, ok := v["enabled"].(bool); ok {
			sub("scheduler")["enabled"] = b
		}
	}
	if v, ok := patch["git"].(map[string]any); ok {
		if s2, ok := v["branch_prefix"].(string); ok {
			sub("git")["branch_prefix"] = strings.TrimSpace(s2)
		}
		if b, ok := v["force_push"].(bool); ok {
			sub("git")["force_push"] = b
		}
		if s2, ok := v["commit_instructions"].(string); ok {
			sub("git")["commit_instructions"] = strings.TrimSpace(s2)
		}
	}
	if v, ok := patch["worktrees"].(map[string]any); ok {
		for _, k := range []string{"enabled", "fetch_before_create", "auto_delete"} {
			if b, ok := v[k].(bool); ok {
				sub("worktrees")[k] = b
			}
		}
		if raw, ok := v["max_count"]; ok {
			zeroOr("worktrees", "max_count", raw, 1, 100)
		}
	}
	if v, ok := patch["network"].(map[string]any); ok {
		if mode, ok := v["proxy_mode"].(string); ok {
			switch mode {
			case "system", "manual", "none":
				sub("network")["proxy_mode"] = mode
			default:
				return a.SettingsView(), fmt.Errorf("代理方式只能是 system / manual / none，收到 %q", mode)
			}
		}
		if raw, ok := v["proxy_url"].(string); ok {
			raw = strings.TrimSpace(raw)
			if hasRedactedProxyUser(raw) {
				return a.SettingsView(), fmt.Errorf("代理地址里的 *** 只是显示占位符，请重新输入完整地址（含账号密码）后再保存")
			}
			sub("network")["proxy_url"] = raw
		}
		if m := sub("network"); m["proxy_mode"] == "manual" {
			raw, _ := m["proxy_url"].(string)
			if err := llm.SetProxyMode("manual", raw); err != nil {
				return a.SettingsView(), err
			}
		}
	}
	if len(outOfRange) > 0 {
		return a.SettingsView(), fmt.Errorf("参数越界：%s", strings.Join(outOfRange, "；"))
	}
	if len(sanitized) == 0 {
		return a.SettingsView(), fmt.Errorf("没有可应用的有效设置")
	}

	var targetBase, targetModel, targetProtocol string
	if lm, ok := sanitized["llm"].(map[string]any); ok {
		targetBase, targetModel, targetProtocol, _ = a.llmFormTarget(lm)
		if a.Cfg.LLM.Provider != "mock" && strings.TrimSpace(targetModel) == "" {
			return a.SettingsView(), fmt.Errorf("模型 ID 不能为空：选择厂商后请填写要使用的模型")
		}
	}

	prevProviderID := a.Cfg.LLM.ProviderID
	prevPlan := a.Cfg.LLM.Plan
	prevBaseScope := llm.KeyScope(a.Cfg.LLM.BaseURL)
	a.Cfg.Apply(sanitized)
	if _, changed := sanitized["scheduler"]; changed && a.Sched != nil {
		a.Sched.SetPaused(!a.Cfg.Scheduler.Enabled)
	}

	if lm, ok := sanitized["llm"].(map[string]any); ok {
		pid, _ := lm["provider_id"].(string)
		plan, _ := lm["plan"].(string)
		pidChanged := pid != "" && pid != prevProviderID
		planChanged := plan != "" && plan != prevPlan
		if _, hasProto := lm["protocol"]; !hasProto && (pidChanged || planChanged) {
			// Without an explicit protocol, do not carry the old vendor/plan
			// protocol over to the new endpoint.
			a.Cfg.LLM.Protocol = ""
		}
		if pidChanged {
			// Auxiliary and tier model names belong to the old vendor and do
			// not exist after switching.
			a.Cfg.LLM.FastModel = ""
			a.Cfg.LLM.Tiers = nil
			a.clearFastFlag()
		} else if nextScope := llm.KeyScope(targetBase); prevBaseScope != "" && nextScope != "" && nextScope != prevBaseScope {
			// The provider preset stayed the same but the endpoint moved: tier
			// names are just as vendor-specific.
			a.Cfg.LLM.FastModel = ""
			a.Cfg.LLM.Tiers = nil
			a.clearFastFlag()
		} else if v, has := lm["fast_model"].(string); has {
			// The advanced settings field is the legacy entry: when it is
			// explicitly cleared or changed, a checkbox in the model list must
			// not keep overriding it.
			if strings.TrimSpace(v) == "" {
				a.clearFastFlag()
			} else if fast := a.fastModelEntry(); fast != nil && fast.Model != strings.TrimSpace(v) {
				a.clearFastFlag()
			}
		}
	}
	if lm, ok := sanitized["llm"].(map[string]any); ok {
		formKey, _ := lm["api_key"].(string)
		clearKey, _ := lm["clear_api_key"].(bool)
		if _, err := BindLLMKey(a.Cfg, a.Creds, targetBase, formKey, clearKey); err != nil {
			return a.SettingsView(), err
		}
	}

	if a.Cfg.LLM.Provider != "mock" {
		if _, hasLLM := sanitized["llm"]; hasLLM {
			a.Cfg.LLM.BaseURL, a.Cfg.LLM.Model, a.Cfg.LLM.Protocol = targetBase, targetModel, targetProtocol
			a.LLM = llm.New(targetProtocol, targetBase, a.Cfg.LLM.APIKey, targetModel,
				a.Cfg.LLM.Temperature, a.Cfg.LLM.MaxTokens, a.Cfg.LLM.TimeoutSecs)
			a.RebuildFastClient()
			a.RebuildTierClients()
			// The default entry drives the runtime at startup; leaving its old
			// connection here made the next launch restore the previous model.
			if entry := a.defaultModelEntry(); entry != nil {
				entry.ProviderID = a.Cfg.LLM.ProviderID
				entry.Plan = a.Cfg.LLM.Plan
				entry.BaseURL = targetBase
				entry.Model = targetModel
				entry.Protocol = targetProtocol
				if lm, ok := sanitized["llm"].(map[string]any); ok {
					formKey, _ := lm["api_key"].(string)
					clearKey, _ := lm["clear_api_key"].(bool)
					if strings.TrimSpace(formKey) != "" || clearKey {
						entry.APIKey = ""
					}
				}
			}
		}
	}
	if a.Gate != nil {
		if m, ok := sanitized["safety"].(map[string]any); ok {
			if v, ok := m["mode"].(string); ok {
				a.Gate.SetMode(v)
			}
			if v, ok := m["approval_timeout_seconds"].(int); ok {
				a.Gate.SetApprovalTimeout(time.Duration(v) * time.Second)
			}
		}
	}

	if err := a.Cfg.SaveOverlay(filepath.Join(a.Cfg.DataDir, config.OverlayFile)); err != nil {
		return a.SettingsView(), fmt.Errorf("设置已生效但保存失败: %w", err)
	}
	return a.SettingsView(), nil
}

func (a *Agent) llmFormTarget(override map[string]any) (base, model, protocol, key string) {
	str := func(k string) string {
		if v, ok := override[k].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	pid := firstNonEmpty(str("provider_id"), a.Cfg.LLM.ProviderID)
	plan := firstNonEmpty(str("plan"), a.Cfg.LLM.Plan)
	providerChanged := str("provider_id") != "" && str("provider_id") != a.Cfg.LLM.ProviderID
	base = str("base_url")
	if _, ok := override["base_url"].(string); !ok && !providerChanged {
		base = a.Cfg.LLM.BaseURL
	}
	model = str("model")
	if _, ok := override["model"].(string); !ok && !providerChanged {
		model = a.Cfg.LLM.Model
	}
	protocol = str("protocol")
	if _, explicit := override["protocol"]; !explicit && !providerChanged && plan == a.Cfg.LLM.Plan {
		protocol = a.Cfg.LLM.Protocol
	}
	base, model, protocol = llm.ResolveTarget(pid, plan, base, model, protocol)
	key, _ = a.llmKeyFor(base, str("api_key"))
	return
}

func probeHasExplicitTarget(override map[string]any) bool {
	v, ok := override["base_url"].(string)
	return ok && strings.TrimSpace(v) != ""
}

func (a *Agent) TestLLMConnection(override map[string]any) map[string]any {
	if a.Cfg.LLM.Provider == "mock" && !probeHasExplicitTarget(override) {
		return map[string]any{"ok": true, "kind": "mock", "message": "当前为离线 Mock 模型，未发起真实网络调用"}
	}
	if _, hasKey := override["api_key"]; !hasKey {
		if mid, ok := override["model_id"].(string); ok && mid != "" {
			idx := a.modelIndex(mid)
			if idx >= 0 && strings.TrimSpace(a.Cfg.LLM.Models[idx].APIKey) != "" {
				override["api_key"] = a.Cfg.LLM.Models[idx].APIKey
			}
		}
	}
	base, model, protocol, key := a.llmFormTarget(override)
	if strings.TrimSpace(base) == "" || strings.TrimSpace(model) == "" {
		return map[string]any{"ok": false, "kind": "config", "message": "base_url 或模型名为空：选择厂商预设或直接填写"}
	}
	client := llm.New(protocol, base, key, model, 0.1, 16, 15)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := time.Now()
	reply, err := client.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "ping，请只回复 ok"}},
	})
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return map[string]any{
			"ok": false, "kind": classifyLLMError(ctx, err),
			"http_status": llm.StatusCode(err), "latency_ms": latency,
			"message": err.Error(),
		}
	}
	preview := reply
	if len([]rune(preview)) > 40 {
		preview = string([]rune(preview)[:40]) + "…"
	}
	return map[string]any{
		"ok": true, "kind": "ok", "latency_ms": latency,
		"model": model, "base_url": base, "protocol": protocol,
		"message": "连接成功", "reply_preview": preview,
	}
}

func (a *Agent) ListLLMModels(override map[string]any) map[string]any {
	if a.Cfg.LLM.Provider == "mock" && !probeHasExplicitTarget(override) {
		return map[string]any{"ok": true, "kind": "mock", "models": []llm.ModelInfo{}, "message": "离线 Mock 模型没有在线模型列表"}
	}
	if _, hasKey := override["api_key"]; !hasKey {
		if mid, ok := override["model_id"].(string); ok && mid != "" {
			idx := a.modelIndex(mid)
			if idx >= 0 && strings.TrimSpace(a.Cfg.LLM.Models[idx].APIKey) != "" {
				override["api_key"] = a.Cfg.LLM.Models[idx].APIKey
			}
		}
	}
	base, _, protocol, key := a.llmFormTarget(override)
	if strings.TrimSpace(base) == "" {
		return map[string]any{"ok": false, "kind": "config", "message": "base_url 为空：选择厂商预设或直接填写 API 地址"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := llm.ListModels(ctx, protocol, base, key, 15*time.Second)
	if err != nil {
		return map[string]any{
			"ok": false, "kind": classifyLLMError(ctx, err),
			"http_status": llm.StatusCode(err), "message": err.Error(),
		}
	}
	return map[string]any{
		"ok": true, "kind": "ok", "count": len(models), "models": models,
		"base_url": base, "protocol": protocol, "message": "模型列表获取成功",
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func classifyLLMError(ctx context.Context, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	switch code := llm.StatusCode(err); {
	case code == 401 || code == 403:
		return "auth"
	case code == 404:
		return "not_found"
	case code == 429:
		return "rate_limited"
	case code >= 500:
		return "provider"
	case code > 0:
		return "api"
	default:
		return "network"
	}
}

func (a *Agent) ContextView() map[string]any {
	if a.Mem == nil {
		return map[string]any{"enabled": false}
	}
	st := a.Mem.Stats()
	rd := a.windowReading()
	return map[string]any{
		"enabled":          a.Cfg.Agent.ContextCompress,
		"short_turns":      st.ShortTurns,
		"short_cap":        st.ShortCap,
		"fill_pct":         st.FillPct,
		"overflow":         st.Overflow,
		"summary":          st.Summary,
		"summary_chars":    st.SummaryRunes,
		"est_tokens_saved": st.SavedTokens,
		"prompt_tokens":    rd.LastTokens,
		"prompt_estimated": rd.Estimated,
		"window_tokens":    rd.Window,
		"window_source":    rd.Source,
		"window_note":      rd.Note,
		"window_pct":       rd.Pct,
		"carry_turns":      rd.CarryTurns,
		"compress_end_pct": compressAtEndPct,
		"compress_mid_pct": compressMidPct,
	}
}

func (a *Agent) ResetConversation() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Mem != nil && a.idleLocked() == nil {
		a.Mem.ResetConversation()
	}
}

func (a *Agent) AuthSession() map[string]any {
	if a.Auth == nil {
		return map[string]any{"signed_in": false, "configured": false}
	}
	return a.Auth.Session()
}

func (a *Agent) AuthConfigure(supabaseURL, anonKey string) error {
	if a.Auth == nil {
		return errors.New("云端账号模块未启用")
	}
	return a.Auth.Configure(supabaseURL, anonKey)
}

func (a *Agent) AuthSignUp(email, password string) error {
	if a.Auth == nil {
		return errors.New("云端账号模块未启用")
	}
	return a.Auth.SignUp(email, password)
}

func (a *Agent) AuthSignIn(email, password string) error {
	if a.Auth == nil {
		return errors.New("云端账号模块未启用")
	}
	return a.Auth.SignIn(email, password)
}

func (a *Agent) AuthSignOut() error {
	if a.Auth == nil {
		return nil
	}
	return a.Auth.SignOut()
}

func (a *Agent) AuthOAuth(provider string) (string, error) {
	if a.Auth == nil {
		return "", errors.New("云端账号模块未启用")
	}
	return a.Auth.StartOAuth(provider)
}

func (a *Agent) DataDir() string { return a.Cfg.DataDir }

func (a *Agent) CredentialsFile() string {
	if a.Creds == nil {
		return filepath.Join(a.Cfg.DataDir, "credentials.json")
	}
	return a.Creds.Path()
}

func (a *Agent) scheduleView(name string) types.ScheduleJobView {
	for _, j := range a.Sched.ListJobs() {
		if j.Name == name {
			return jobToScheduleView(j)
		}
	}
	return types.ScheduleJobView{}
}

func jobToScheduleView(j scheduler.Job) types.ScheduleJobView {
	v := types.ScheduleJobView{
		Name: j.Name, Cron: j.Cron, IntervalSec: j.IntervalSec, Goal: j.Goal,
		Mode: j.Mode, Enabled: j.Enabled, WhenText: j.WhenText, ScheduleText: j.ScheduleText,
	}
	if policy, err := scheduler.NormalizeNotify(j.Notify); err == nil {
		v.Notify = policy
	} else {
		v.Notify = j.Notify
	}
	if j.NextRun != nil {
		v.NextRun = j.NextRun.Format(time.RFC3339)
	}
	if j.LastRun != nil {
		v.LastRun = j.LastRun.Format(time.RFC3339)
	}
	return v
}

func (a *Agent) ProvidersView() []llm.ProviderPreset { return llm.Providers }

func (a *Agent) mcpIndex(name string) int {
	for i, s := range a.Cfg.MCP {
		if s.Name == name {
			return i
		}
	}
	return -1
}

func (a *Agent) MCPInstalledByArgs(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	for _, s := range a.Cfg.MCP {
		for _, arg := range s.Args {
			if arg == ref || strings.Contains(arg, ref) {
				return true
			}
		}
	}
	return false
}

func (a *Agent) MarketMCP(q string) []map[string]any {
	presets := market.SearchMCP(q)
	out := make([]map[string]any, 0, len(presets))
	for _, p := range presets {
		out = append(out, map[string]any{
			"id": p.ID, "name": p.Name, "desc": p.Desc, "command": p.Command,
			"params": p.Params, "trust": p.Trust, "tags": p.Tags,
			"installed": a.mcpIndex(p.ID) >= 0,
		})
	}
	return out
}

func (a *Agent) MarketSkills(q string) []map[string]any {
	presets := market.SearchSkill(q)
	out := make([]map[string]any, 0, len(presets))
	for _, s := range presets {
		_, err := a.Skills.Get(s.Name)
		out = append(out, map[string]any{
			"name": s.Name, "description": s.Description, "category": s.Category,
			"params": s.Params, "steps": s.Steps, "tags": s.Tags, "installed": err == nil,
		})
	}
	return out
}

func (a *Agent) SkillInstallPreset(name string, force bool) (int, error) {
	p, err := market.FindSkill(name)
	if err != nil {
		return 0, err
	}
	_, existed := a.Skills.Get(p.Name)
	if existed == nil && !force {
		return 0, fmt.Errorf("%w：重装会把它恢复成市场里的模板，你改过的步骤会被替换；只想临时别用它，请改用「停用」", ErrAlreadyInstalled)
	}
	return a.SkillSave(skill.Skill{
		Name: p.Name, Description: p.Description, Params: p.Params, Steps: p.Steps,
	})
}

func (a *Agent) MCPList() []map[string]any {
	out := make([]map[string]any, 0, len(a.Cfg.MCP))
	names := a.Reg.Names()
	for _, s := range a.Cfg.MCP {
		_, connected := a.MCP.Get(s.Name)
		tools := 0
		prefix := mcpToolPrefix(s.Name)
		for _, n := range names {
			if strings.HasPrefix(n, prefix) {
				tools++
			}
		}
		kind := "stdio"
		if strings.TrimSpace(s.URL) != "" {
			kind = "http"
		}
		out = append(out, map[string]any{
			"name": s.Name, "command": s.Command, "args": s.Args,
			"trust": s.Trust, "enabled": s.Enabled,
			"connected": connected, "tools": tools,
			"kind": kind, "url": s.URL,
			"header_count": len(s.Headers),
		})
	}
	return out
}

func (a *Agent) MCPInstallPreset(id string, params map[string]string, trust string, force bool) (map[string]any, error) {
	p, err := market.FindMCP(id)
	if err != nil {
		return nil, err
	}
	argVals, envVals := market.SplitParams(p.Params, params)
	command, args, err := market.BuildCommand(p, argVals)
	if err != nil {
		return nil, err
	}
	env, err := market.BuildEnv(p, envVals)
	if err != nil {
		return nil, err
	}
	if trust == "" {
		trust = p.Trust
	}
	return a.mcpInstall(mcpSpec{Name: p.ID, Display: p.Name, Command: command, Args: args, Env: env, Trust: trust}, force)
}

func (a *Agent) MCPInstallCustom(name, command string, args []string, env map[string]string, trust string, force bool) (map[string]any, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("command 不能为空")
	}
	display := strings.TrimSpace(name)
	if display == "" {
		base := filepath.Base(command)
		if i := strings.LastIndexByte(base, '.'); i > 0 {
			base = base[:i]
		}
		display = sanitizeMCPName(base)
		if display == "" {
			display = "custom"
		}
	}
	return a.mcpInstall(mcpSpec{Name: display, Display: display, Command: command, Args: args, Env: env, Trust: trust}, force)
}

func (a *Agent) MCPInstallRemote(name, url string, headers map[string]string, trust string, force bool) (map[string]any, error) {
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("远端地址为空")
	}
	return a.mcpInstall(mcpSpec{Name: name, Display: name, URL: url, Headers: headers, Trust: trust}, force)
}

var mcpNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_\-]+`)

func sanitizeMCPName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(mcpNameUnsafe.ReplaceAllString(s, "-"), "-")
	if len(s) > 32 {
		s = strings.Trim(s[:32], "-")
	}
	return s
}

var ErrAlreadyInstalled = errors.New("已经安装过了")

type mcpSpec struct {
	Name    string
	Display string
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Headers map[string]string
	Trust   string
}

func (a *Agent) mcpInstall(spec mcpSpec, force bool) (map[string]any, error) {
	name, display, command, args := spec.Name, spec.Display, spec.Command, spec.Args
	if !mcpNameRe.MatchString(name) {
		return nil, fmt.Errorf("名称 %q 非法（仅限字母数字下划线连字符，≤32 字符）", name)
	}
	if strings.TrimSpace(spec.URL) == "" && strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("启动命令不能为空")
	}
	idx := a.mcpIndex(name)
	if idx >= 0 && !force {
		return nil, fmt.Errorf("%w：重装会替换当前的命令与参数；只想临时别跑它，请改用「停用」", ErrAlreadyInstalled)
	}
	trust := spec.Trust
	if trust != "readonly" && trust != "user_approved" && trust != "full_access" {
		trust = "user_approved"
	}
	srv := config.MCPServerConfig{
		Name: name, Command: command, Args: args, Env: spec.Env,
		URL: spec.URL, Headers: spec.Headers, Trust: trust, Enabled: true,
	}
	next := append([]config.MCPServerConfig{}, a.Cfg.MCP...)
	if idx >= 0 {
		next[idx] = srv
		a.mcpDetach(name)
	} else {
		next = append(next, srv)
	}
	prev := a.Cfg.MCP
	a.Cfg.MCP = next
	if err := a.Cfg.SaveOverlay(filepath.Join(a.Cfg.DataDir, config.OverlayFile)); err != nil {
		a.Cfg.MCP = prev
		return nil, fmt.Errorf("配置未保存（%v），已还原为安装前的状态", err)
	}
	tools, err := a.mcpConnect(srv)
	res := map[string]any{
		"name": name, "display": display, "installed": true, "replaced": idx >= 0,
		"connected": err == nil, "tools": tools, "mcp": a.MCPList(),
	}
	if err != nil {
		hint := "首次 npx/uvx 需联网下载，可稍后在工具页查看"
		if strings.TrimSpace(srv.URL) != "" {
			hint = "远端地址、网络或请求头里的凭据要再核一下"
		}
		res["warning"] = fmt.Sprintf("已保存配置，但连接失败（%s）: %v", hint, err)
	}
	return res, nil
}

func (a *Agent) mcpDetach(name string) int {
	removed := 0
	prefix := mcpToolPrefix(name)
	for _, n := range a.Reg.Names() {
		if strings.HasPrefix(n, prefix) {
			if a.Reg.Unregister(n) {
				removed++
			}
		}
	}
	a.MCP.Remove(name)
	return removed
}

func (a *Agent) MCPSetEnabled(name string, enabled bool) (map[string]any, error) {
	idx := a.mcpIndex(name)
	if idx < 0 {
		return nil, fmt.Errorf("MCP 服务器 %q 未安装", name)
	}
	if a.Cfg.MCP[idx].Enabled == enabled {
		return map[string]any{"name": name, "enabled": enabled, "unchanged": true, "mcp": a.MCPList()}, nil
	}
	prev := a.Cfg.MCP[idx].Enabled
	a.Cfg.MCP[idx].Enabled = enabled
	if err := a.Cfg.SaveOverlay(filepath.Join(a.Cfg.DataDir, config.OverlayFile)); err != nil {
		a.Cfg.MCP[idx].Enabled = prev
		return nil, fmt.Errorf("状态未保存（%v），已还原", err)
	}
	tools := 0
	a.mcpDetach(name)
	warning := ""
	if enabled {
		n, err := a.mcpConnect(a.Cfg.MCP[idx])
		tools = n
		if err != nil {
			warning = fmt.Sprintf("已启用，但连接失败: %v", err)
		}
	}
	res := map[string]any{
		"name": name, "enabled": enabled, "connected": enabled && warning == "",
		"tools": tools, "mcp": a.MCPList(),
	}
	if warning != "" {
		res["warning"] = warning
	}
	return res, nil
}

func (a *Agent) mcpConnect(srv config.MCPServerConfig) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := mcp.Start(ctx, mcp.ServerConfig{
		Name: srv.Name, Command: srv.Command, Args: srv.Args, Env: srv.Env,
		URL: srv.URL, Headers: srv.Headers,
		Trust: srv.Trust, Enabled: true,
	}, func(f string, xs ...any) { fmt.Fprintf(os.Stderr, "[mcp] "+f+"\n", xs...) })
	if err != nil {
		return 0, err
	}
	perm := mcp.PermissionFromTrust(srv.Trust)
	for _, def := range client.Tools {
		a.Reg.Replace(mcp.NewTool(client, def, perm))
	}
	a.MCP.Add(client)
	return len(client.Tools), nil
}

func (a *Agent) MCPRemove(name string) (map[string]any, error) {
	idx := a.mcpIndex(name)
	if idx < 0 {
		return nil, fmt.Errorf("MCP 服务器 %q 未安装", name)
	}
	prev := append([]config.MCPServerConfig{}, a.Cfg.MCP...)
	a.Cfg.MCP = append(a.Cfg.MCP[:idx], a.Cfg.MCP[idx+1:]...)
	if err := a.Cfg.SaveOverlay(filepath.Join(a.Cfg.DataDir, config.OverlayFile)); err != nil {
		a.Cfg.MCP = prev
		return nil, fmt.Errorf("已移除但持久化失败（重启后它会回来）: %w", err)
	}
	removed := a.mcpDetach(name)
	return map[string]any{"name": name, "removed_tools": removed, "mcp": a.MCPList()}, nil
}

func (a *Agent) MCPRetry(name string) (map[string]any, error) {
	idx := a.mcpIndex(name)
	if idx < 0 {
		return nil, fmt.Errorf("MCP 服务器 %q 未安装", name)
	}
	srv := &a.Cfg.MCP[idx]
	if !srv.Enabled {
		return nil, fmt.Errorf("MCP 服务器 %q 已停用，请先启用", name)
	}
	a.MCP.Remove(name)
	tools, err := a.mcpConnect(*srv)
	res := map[string]any{
		"name": name, "connected": err == nil, "tools": tools, "mcp": a.MCPList(),
	}
	if err != nil {
		res["warning"] = fmt.Sprintf("连接仍失败: %v", err)
	}
	return res, nil
}

func permFromStr(s string) types.Permission {
	switch s {
	case "readonly":
		return types.PermissionReadOnly
	case "full_access":
		return types.PermissionFullAccess
	default:
		return types.PermissionUserApproved
	}
}

func (a *Agent) PermissionOf(name string) types.Permission {
	if t, ok := a.Reg.Get(name); ok {
		return a.Gate.EffectivePermission(t)
	}
	return types.PermissionUserApproved
}

func (a *Agent) WorkspaceView() map[string]any {
	return map[string]any{
		"workspace":  a.Cfg.Workspace,
		"recents":    a.Cfg.WorkspaceRecents,
		"git_branch": gitBranch(a.Cfg.Workspace),
	}
}

func gitBranch(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return ""
	}
	return branch
}

func (a *Agent) WorkspaceBrowse(path string) (map[string]any, error) {
	path = strings.TrimSpace(path)
	type dirEntry struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
	}
	if path == "" {
		roots := []dirEntry{}
		if runtime.GOOS == "windows" {
			for c := 'C'; c <= 'Z'; c++ {
				drive := string(c) + `:\`
				if info, err := os.Stat(drive); err == nil && info.IsDir() {
					roots = append(roots, dirEntry{Name: string(c) + " 盘", Path: drive, IsDir: true})
				}
			}
		} else {
			roots = append(roots, dirEntry{Name: "/（系统根）", Path: "/", IsDir: true})
			if home, err := os.UserHomeDir(); err == nil {
				roots = append(roots, dirEntry{Name: "主目录 " + home, Path: home, IsDir: true})
			}
		}
		return map[string]any{"parent": "", "dirs": roots}, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("路径非法: %w", err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("目录不存在: %s", abs)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s 不是文件夹", abs)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	dirs := make([]dirEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dirs = append(dirs, dirEntry{Name: e.Name(), Path: filepath.Join(abs, e.Name()), IsDir: true})
		if len(dirs) >= 200 {
			break
		}
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		parent = ""
	}
	return map[string]any{"parent": parent, "current": abs, "dirs": dirs}, nil
}

func (a *Agent) WorkspaceSet(path string) (map[string]any, error) {
	if err := a.applyWorkspace(path); err != nil {
		return nil, err
	}
	return a.WorkspaceView(), nil
}

func (a *Agent) WorkspaceClear() (map[string]any, error) {
	a.Cfg.Workspace = ""
	if a.FileTools != nil {
		a.FileTools.Roots = nil
	}
	if a.Gate != nil {
		a.Gate.SetWorkspaceRoots(nil)
	}
	if err := a.Cfg.SaveOverlay(filepath.Join(a.Cfg.DataDir, config.OverlayFile)); err != nil {
		return nil, fmt.Errorf("已取消但持久化失败: %w", err)
	}
	return a.WorkspaceView(), nil
}

func (a *Agent) applyWorkspace(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("路径为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("路径非法: %w", err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("目录不存在: %s", abs)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s 不是文件夹", abs)
	}

	a.Cfg.Workspace = abs
	if a.FileTools != nil {
		a.FileTools.Roots = []string{abs}
	}
	if a.Gate != nil {
		a.Gate.SetWorkspaceRoots([]string{abs})
	}
	recents := []string{abs}
	for _, r := range a.Cfg.WorkspaceRecents {
		if r != abs {
			recents = append(recents, r)
		}
	}
	if len(recents) > 8 {
		recents = recents[:8]
	}
	a.Cfg.WorkspaceRecents = recents

	if err := a.Cfg.SaveOverlay(filepath.Join(a.Cfg.DataDir, config.OverlayFile)); err != nil {
		return fmt.Errorf("已切换但持久化失败: %w", err)
	}
	return nil
}

type changeRef struct {
	Path string
	Head preImage
	Recs []preImage
}

func (a *Agent) workspaceRoots() []string {
	if a.FileTools != nil && len(a.FileTools.Roots) > 0 {
		return a.FileTools.Roots
	}
	if strings.TrimSpace(a.Cfg.Workspace) != "" {
		return []string{a.Cfg.Workspace}
	}
	return nil
}

func (a *Agent) changeTarget(taskID, rawPath string) (changeRef, error) {
	if SafeTaskName(taskID) == "" {
		return changeRef{}, fmt.Errorf("任务标识 %q 不合法", types.Shorten(taskID, 40))
	}
	if strings.TrimSpace(a.Cfg.DataDir) == "" {
		return changeRef{}, fmt.Errorf("未配置数据目录，写前快照无处可存，也就无从还原")
	}
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return changeRef{}, fmt.Errorf("没有指定路径")
	}
	if !filepath.IsAbs(path) {
		return changeRef{}, fmt.Errorf("请填写清单里的完整路径")
	}
	if m := a.worktreeManager(); m != nil {
		if wtDir := m.Dir(taskID); toolutil.Within(wtDir, path) {
			if _, err := os.Stat(wtDir); os.IsNotExist(err) {
				return changeRef{}, fmt.Errorf("这个任务的副本（%s）已经被删掉了：副本里的改动无从还原。任务分支还在，可用 git 工具查看或合并", wtDir)
			}
		}
	}
	roots := a.workspaceRoots()
	if m := a.worktreeManager(); m != nil {
		if meta, ok := m.Get(taskID); ok {
			roots = append(append([]string(nil), roots...), meta.Path)
		}
	}
	if len(roots) == 0 {
		return changeRef{}, fmt.Errorf("尚未设定工作区，无法判断这个路径能不能动")
	}
	target, err := toolutil.ResolveInRoots(path, roots)
	if err != nil {
		return changeRef{}, err
	}
	recs, err := taskPreImages(a.Cfg.DataDir, taskID)
	if err != nil {
		return changeRef{}, err
	}
	if len(recs) == 0 {
		return changeRef{}, fmt.Errorf("这次任务没有留下写前快照（没动文件，或当时未配置数据目录）")
	}
	head, ok := firstPreImageOf(recs, target)
	if !ok {
		return changeRef{}, fmt.Errorf("这个路径不在本次任务的改动清单里，不能动")
	}
	return changeRef{Path: target, Head: head, Recs: recs}, nil
}

const diffReadMax = snapshotMaxBytes

func (a *Agent) TaskDiff(taskID, path string) (map[string]any, error) {
	ref, err := a.changeTarget(taskID, path)
	if err != nil {
		return nil, err
	}
	head := ref.Head
	view := map[string]any{
		"task_id":    taskID,
		"path":       ref.Path,
		"reversible": head.reversible(),
	}
	switch {
	case head.IsDir:
		return nil, fmt.Errorf("这是个目录，没有内容可对比")
	case head.Exists && head.File == "":
		return nil, fmt.Errorf("写前内容未留存，无从对比：%s", diffBlame(head))
	}

	var before string
	if head.Exists {
		b, err := preImageContent(a.Cfg.DataDir, taskID, head)
		if err != nil {
			return nil, fmt.Errorf("读写前内容失败: %w", err)
		}
		before = string(b)
	}
	after, afterBytes, note := readForDiff(ref.Path)
	view["before_bytes"] = head.Bytes
	view["after_bytes"] = afterBytes
	if note != "" {
		view["diff"] = DiffResult{Truncated: true, Note: note}
		return view, nil
	}
	view["diff"] = DiffText(before, after)
	return view, nil
}

func readForDiff(path string) (text string, bytes int64, note string) {
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, ""
	}
	if info.IsDir() {
		return "", info.Size(), "现在这里是个目录，没有内容可对比"
	}
	if info.Size() > diffReadMax {
		return "", info.Size(), fmt.Sprintf("现在有 %s，超过对比上限，只给出体量对比", humanSize(info.Size()))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", info.Size(), "现在这份读不出来：" + types.Shorten(err.Error(), 80)
	}
	return string(b), info.Size(), ""
}

func diffBlame(rec preImage) string {
	if strings.TrimSpace(rec.Reason) != "" {
		return rec.Reason
	}
	return "写前内容未留存"
}

func (a *Agent) TaskRevert(taskID, path string) (map[string]any, error) {
	ref, err := a.changeTarget(taskID, path)
	if err != nil {
		return nil, err
	}
	group := preImageGroup(ref.Recs, ref.Path)
	if len(group) == 0 {
		return nil, fmt.Errorf("这个路径不在本次任务的改动清单里，不能动")
	}

	type revertedRow struct {
		Path string `json:"path"`
		Note string `json:"note"`
	}
	done := make([]revertedRow, 0, len(group))
	donePaths := make([]string, 0, len(group))
	failed := make([]string, 0, len(group))
	for _, rec := range group {
		if !rec.reversible() {
			failed = append(failed, fmt.Sprintf("%s：%s", filepath.Base(rec.Path), diffBlame(rec)))
			continue
		}
		note, err := restorePreImage(a.Cfg.DataDir, taskID, rec)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s：%s", filepath.Base(rec.Path), types.Shorten(err.Error(), 120)))
			continue
		}
		done = append(done, revertedRow{Path: rec.Path, Note: note})
		donePaths = append(donePaths, rec.Path)
	}
	if len(done) == 0 {
		return nil, fmt.Errorf("没有还原成功任何路径：%s", strings.Join(failed, "；"))
	}

	a.markChangesReverted(taskID, donePaths)
	if a.Gate != nil {
		a.Gate.Record(safety.AuditEntry{
			Tool:   "file.revert",
			Risk:   "medium",
			Action: "manual_revert",
			Reason: "用户在改动清单里点了还原",
			Detail: fmt.Sprintf("%d 个路径：%s", len(done), strings.Join(donePaths, ", ")),
		})
	}
	return map[string]any{
		"task_id":  taskID,
		"reverted": done,
		"failed":   failed,
		"pairs":    len(group) > 1,
	}, nil
}

func (a *Agent) markChangesReverted(taskID string, paths []string) {
	g, err := ReadTaskResult(a.Cfg.DataDir, taskID)
	if err != nil || g == nil || len(g.Changes) == 0 {
		return
	}
	want := make(map[string]bool, len(paths))
	for _, p := range paths {
		want[pathKey(p)] = true
	}
	changed := false
	for i := range g.Changes {
		if want[pathKey(g.Changes[i].Path)] && !g.Changes[i].Reverted {
			g.Changes[i].Reverted = true
			if info, err := os.Stat(g.Changes[i].Path); err == nil && !info.IsDir() {
				g.Changes[i].Bytes = info.Size()
			} else {
				g.Changes[i].Bytes = 0
			}
			changed = true
		}
	}
	if !changed {
		return
	}
	if err := SaveTaskResult(a.Cfg.DataDir, g); err != nil {
		fmt.Fprintf(os.Stderr, "[gleam] 改动清单已还原但归档没更新（下次读档案会看不到这一位）：%v\n", err)
	}
}
