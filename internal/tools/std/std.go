package std

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gleam/internal/nlcron"
	"gleam/internal/textmatch"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

type ReplyTool struct{}

func NewReply() *ReplyTool { return &ReplyTool{} }

func (t *ReplyTool) Name() string { return "reply" }
func (t *ReplyTool) Description() string {
	return "直接向用户回复文字。对话、咨询、无需工具的目标以此收尾"
}
func (t *ReplyTool) Permission() types.Permission {
	return types.PermissionReadOnly
}
func (t *ReplyTool) Schema() map[string]any {
	return toolutil.Schema("回复用户", []string{"text"}, map[string]any{
		"text": toolutil.SchemaProp("回复内容（面向用户的完整回答）", "string"),
	})
}
func (t *ReplyTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	text, err := toolutil.RequireStr(args, "text")
	if err != nil {
		return nil, err
	}
	return map[string]any{"text": text}, nil
}

type SearchHit struct {
	ID      string   `json:"id"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
	Score   float32  `json:"score"`
}

type MemoryStore interface {
	Remember(content string, tags []string) (string, error)
	Search(query string, k int) []SearchHit
	Count() int
}

type memSaveTool struct{ store MemoryStore }

func NewMemSave(store MemoryStore) types.Tool { return &memSaveTool{store} }

func (t *memSaveTool) Name() string { return "memory.save" }
func (t *memSaveTool) Description() string {
	return "记住重要信息：写入长期记忆，跨会话可用"
}
func (t *memSaveTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *memSaveTool) Schema() map[string]any {
	return toolutil.Schema("保存长期记忆", []string{"content"}, map[string]any{
		"content": toolutil.SchemaProp("要记住的内容", "string"),
		"tags":    toolutil.SchemaProp("标签列表，如 [\"偏好\"]", "array"),
	})
}
func (t *memSaveTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	content, err := toolutil.RequireStr(args, "content")
	if err != nil {
		return nil, err
	}
	var tags []string
	if arr, ok := args["tags"].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				tags = append(tags, s)
			}
		}
	}
	id, err := t.store.Remember(content, tags)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "saved": true}, nil
}

type memSearchTool struct{ store MemoryStore }

func NewMemSearch(store MemoryStore) types.Tool { return &memSearchTool{store} }

func (t *memSearchTool) Name() string                 { return "memory.search" }
func (t *memSearchTool) Description() string          { return "按语义相关度检索长期记忆" }
func (t *memSearchTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *memSearchTool) Schema() map[string]any {
	return toolutil.Schema("检索长期记忆", []string{"query"}, map[string]any{
		"query": toolutil.SchemaProp("检索词", "string"),
		"k":     toolutil.SchemaProp("返回条数（默认 5）", "integer"),
	})
}
func (t *memSearchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	query, err := toolutil.RequireStr(args, "query")
	if err != nil {
		return nil, err
	}
	hits := t.store.Search(query, toolutil.Int(args, "k", 5))
	return map[string]any{"query": query, "count": len(hits), "hits": hits}, nil
}

func (t *memSearchTool) Outcome(args map[string]any, out any) (types.StepOutcome, string) {
	return toolutil.EmptyIfNone(out)
}

type MemoryDeleter interface {
	SoftDelete(id string) bool
}

type memDeleteTool struct{ store MemoryStore }

func NewMemDelete(store MemoryStore) types.Tool { return &memDeleteTool{store} }

func (t *memDeleteTool) Name() string { return "memory.delete" }
func (t *memDeleteTool) Description() string {
	return "删除一条长期记忆（按 memory.search 返回的 id）；用户要求忘掉某条信息时使用"
}
func (t *memDeleteTool) Permission() types.Permission {
	return types.PermissionUserApproved
}
func (t *memDeleteTool) Schema() map[string]any {
	return toolutil.Schema("删除长期记忆", []string{"id"}, map[string]any{
		"id": toolutil.SchemaProp("要删除的记忆条目 ID（来自 memory.search 的结果）", "string"),
	})
}
func (t *memDeleteTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	id, err := toolutil.RequireStr(args, "id")
	if err != nil {
		return nil, err
	}
	d, ok := t.store.(MemoryDeleter)
	if !ok {
		return nil, fmt.Errorf("当前记忆存储不支持删除")
	}
	if !d.SoftDelete(id) {
		return nil, fmt.Errorf("记忆条目不存在或已删除: %s", id)
	}
	return map[string]any{"id": id, "deleted": true}, nil
}

type JobDef struct {
	Name         string `json:"name"`
	Cron         string `json:"cron,omitempty"`
	IntervalSec  int    `json:"interval_sec,omitempty"`
	Goal         string `json:"goal"`
	Mode         string `json:"mode,omitempty"`
	Enabled      bool   `json:"enabled"`
	WhenText     string `json:"when_text,omitempty"`
	ScheduleText string `json:"schedule_text,omitempty"`
	NextRun      string `json:"next_run,omitempty"`
	LastRun      string `json:"last_run,omitempty"`
}

type ScheduleManager interface {
	AddJob(name, cron string, intervalSec int, goal, mode string) (JobDef, error)
	DeleteJob(name string) (bool, error)
	ListJobs() []JobDef
}

type ScheduleManagerDetailed interface {
	AddJobDetailed(name, cron string, intervalSec int, goal, mode, whenText, scheduleText string) (JobDef, error)
}

type scheduleCreateTool struct{ mgr ScheduleManager }

func NewScheduleCreate(mgr ScheduleManager) types.Tool { return &scheduleCreateTool{mgr} }

func (t *scheduleCreateTool) Name() string { return "schedule.create" }
func (t *scheduleCreateTool) Description() string {
	return "创建定时/周期任务：到点自动执行一个目标。支持用中文自然语言描述时间（推荐 when），也支持 cron 或固定间隔。" +
		"当用户说“每天/每周/每月/工作日/周末/每隔多久/到点提醒我 + 做某事”时调用此工具。" +
		"name 取简短中文任务名（如“每日下载目录整理”），goal 是到点真正要执行的事，二者都不能为空；" +
		"时间用中文原话放进 when（如“每天早上9点”“工作日18:30”），不要自己拼 cron。" +
		"创建成功后用 reply 向用户确认“已按什么节奏、执行什么事、下次大约什么时候”。"
}
func (t *scheduleCreateTool) Permission() types.Permission { return types.PermissionUserApproved }
func (t *scheduleCreateTool) Schema() map[string]any {
	return toolutil.Schema("创建定时任务", []string{"name", "goal"}, map[string]any{
		"name":         toolutil.SchemaProp("任务名（唯一、简短）", "string"),
		"goal":         toolutil.SchemaProp("到点要执行的目标描述", "string"),
		"when":         toolutil.SchemaProp("自然语言时间，如“每天早上9点”“工作日18:30”“每周一和周五晚8点”“每月1号9点”“每隔30分钟”。与 cron、interval_sec 三选一，优先用 when", "string"),
		"cron":         toolutil.SchemaProp("Cron 表达式（5 段：分 时 日 月 周），与 when/interval_sec 三选一", "string"),
		"interval_sec": toolutil.SchemaProp("间隔秒数（与 when/cron 三选一，最小 5 秒）", "integer"),
		"mode":         toolutil.SchemaProp("执行模式 auto|plan_first|interactive（默认 auto）", "string"),
	})
}
func (t *scheduleCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, err := toolutil.RequireStr(args, "name")
	if err != nil {
		return nil, err
	}
	goal, err := toolutil.RequireStr(args, "goal")
	if err != nil {
		return nil, err
	}
	when := toolutil.Str(args, "when")
	cron := toolutil.Str(args, "cron")
	intervalSec := toolutil.Int(args, "interval_sec", 0)
	cron, intervalSec, summary, err := nlcron.Resolve(cron, intervalSec, when)
	if err != nil {
		return nil, err
	}
	mode := toolutil.Str(args, "mode")
	var job JobDef
	if dm, ok := t.mgr.(ScheduleManagerDetailed); ok {
		job, err = dm.AddJobDetailed(name, cron, intervalSec, goal, mode, strings.TrimSpace(when), summary)
	} else {
		job, err = t.mgr.AddJob(name, cron, intervalSec, goal, mode)
	}
	if err != nil {
		return nil, err
	}
	if job.WhenText == "" {
		job.WhenText = strings.TrimSpace(when)
	}
	if job.ScheduleText == "" && summary != "" {
		job.ScheduleText = summary
	}
	return job, nil
}

type scheduleListTool struct{ mgr ScheduleManager }

func NewScheduleList(mgr ScheduleManager) types.Tool { return &scheduleListTool{mgr} }

func (t *scheduleListTool) Name() string                 { return "schedule.list" }
func (t *scheduleListTool) Description() string          { return "列出全部定时任务" }
func (t *scheduleListTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *scheduleListTool) Schema() map[string]any {
	return toolutil.Schema("列出定时任务", nil, map[string]any{})
}
func (t *scheduleListTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	jobs := t.mgr.ListJobs()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
	return map[string]any{"count": len(jobs), "jobs": jobs}, nil
}

type scheduleDeleteTool struct{ mgr ScheduleManager }

func NewScheduleDelete(mgr ScheduleManager) types.Tool { return &scheduleDeleteTool{mgr} }

func (t *scheduleDeleteTool) Name() string { return "schedule.delete" }
func (t *scheduleDeleteTool) Description() string {
	return "删除定时任务（高风险：影响后续自动化，需要用户批准）"
}
func (t *scheduleDeleteTool) Permission() types.Permission { return types.PermissionFullAccess }
func (t *scheduleDeleteTool) Schema() map[string]any {
	return toolutil.Schema("删除定时任务", []string{"name"}, map[string]any{
		"name": toolutil.SchemaProp("任务名", "string"),
	})
}
func (t *scheduleDeleteTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, err := toolutil.RequireStr(args, "name")
	if err != nil {
		return nil, err
	}
	ok, err := t.mgr.DeleteJob(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("定时任务 %q 不存在", name)
	}
	return map[string]any{"name": name, "deleted": true}, nil
}

type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     int    `json:"version"`
	Runs        int    `json:"runs"`
	Successes   int    `json:"successes"`
}

type SkillStore interface {
	ListSummaries() []SkillSummary
	GetParams(name string) ([]string, error)
}

type SkillRunner interface {
	RunSkill(ctx context.Context, name string, params map[string]string) (map[string]any, error)
}

type skillListTool struct{ store SkillStore }

func NewSkillList(store SkillStore) types.Tool { return &skillListTool{store} }

func (t *skillListTool) Name() string { return "skill.list" }
func (t *skillListTool) Description() string {
	return "列出已固化的技能（可复用的工作流）"
}
func (t *skillListTool) Permission() types.Permission { return types.PermissionReadOnly }

func (t *skillListTool) Schema() map[string]any {
	return toolutil.Schema("列出技能", nil, map[string]any{
		"query": toolutil.SchemaProp("按目标做相关度筛选，只返回最相关的几条；不填则列出全部", "string"),
		"limit": toolutil.SchemaProp("query 筛选时最多返回几条（默认 10，上限 50）", "integer"),
	})
}

const (
	skillQueryLimit    = 10
	skillQueryMaxLimit = 50
)

func (t *skillListTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	all := t.store.ListSummaries()
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	query := strings.TrimSpace(toolutil.Str(args, "query"))
	if query == "" {
		return map[string]any{"count": len(all), "skills": all}, nil
	}

	limit := toolutil.Int(args, "limit", skillQueryLimit)
	if limit <= 0 {
		limit = skillQueryLimit
	}
	if limit > skillQueryMaxLimit {
		limit = skillQueryMaxLimit
	}

	hits := rankSkills(query, all)
	matched := len(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := map[string]any{"count": len(hits), "total": len(all), "skills": hits}
	if matched == 0 {
		out["note"] = fmt.Sprintf("没有与目标相关的技能（共 %d 个）。不带 query 可列出全部", len(all))
	} else {
		out["note"] = fmt.Sprintf("已按目标筛选：共 %d 个技能，相关 %d 个，返回前 %d 个。要看全部可再调一次不带 query 的 skill.list",
			len(all), matched, len(hits))
	}
	return out, nil
}

func rankSkills(query string, list []SkillSummary) []SkillSummary {
	terms := textmatch.Terms(query)
	type scored struct {
		s SkillSummary
		n int
	}
	var hits []scored
	for _, s := range list {
		n := textmatch.Score(terms, s.Description) + 2*textmatch.Score(terms, s.Name)
		if n > 0 {
			hits = append(hits, scored{s, n})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].n != hits[j].n {
			return hits[i].n > hits[j].n
		}
		return hits[i].s.Name < hits[j].s.Name
	})
	out := make([]SkillSummary, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.s)
	}
	return out
}

type skillRunTool struct {
	store  SkillStore
	runner SkillRunner
}

func NewSkillRun(store SkillStore, runner SkillRunner) types.Tool {
	return &skillRunTool{store, runner}
}

func (t *skillRunTool) Name() string { return "skill.run" }
func (t *skillRunTool) Description() string {
	return "执行一个已固化的技能（一键复用历史工作流）"
}
func (t *skillRunTool) Permission() types.Permission { return types.PermissionUserApproved }
func (t *skillRunTool) Schema() map[string]any {
	return toolutil.Schema("运行技能", []string{"name"}, map[string]any{
		"name":   toolutil.SchemaProp("技能名", "string"),
		"params": toolutil.SchemaProp("参数映射（技能声明的占位符）", "object"),
	})
}
func (t *skillRunTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, err := toolutil.RequireStr(args, "name")
	if err != nil {
		return nil, err
	}
	paramsRaw := toolutil.Map(args, "params")
	params := map[string]string{}
	for k, v := range paramsRaw {
		params[k] = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	if declared, err := t.store.GetParams(name); err == nil {
		for _, p := range declared {
			if _, ok := params[p]; !ok {
				return nil, fmt.Errorf("缺少技能参数 %q（该技能声明了参数：%v）", p, declared)
			}
		}
	}
	return t.runner.RunSkill(ctx, name, params)
}

type Delegator interface {
	Delegate(ctx context.Context, goal string, role string) (string, error)
}

type agentDelegateTool struct{ d Delegator }

func NewAgentDelegate(d Delegator) types.Tool { return &agentDelegateTool{d} }

func (t *agentDelegateTool) Name() string { return "agent.delegate" }
func (t *agentDelegateTool) Description() string {
	return "把子任务委派给独立 LLM 调用处理，适合并行探索、信息检索或隔离上下文的子目标。返回文本结果。"
}
func (t *agentDelegateTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *agentDelegateTool) Schema() map[string]any {
	return toolutil.Schema("委派子任务", []string{"goal"}, map[string]any{
		"goal": toolutil.SchemaProp("子目标描述：要解决什么、期望什么产出", "string"),
		"role": toolutil.SchemaProp("可选：专家角色 ID（coder/analyst/writer/researcher/pm/ops），默认 general", "string"),
	})
}
func (t *agentDelegateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	goal, err := toolutil.RequireStr(args, "goal")
	if err != nil {
		return nil, err
	}
	role := toolutil.Str(args, "role")
	text, err := t.d.Delegate(ctx, goal, role)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": text}, nil
}
