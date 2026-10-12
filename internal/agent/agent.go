package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gleam/internal/config"
	"gleam/internal/harness/conversation"
	"gleam/internal/harness/credentials"
	"gleam/internal/agent/geo"
	"gleam/internal/harness/growth"
	"gleam/internal/harness/memory"
	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/harness/scheduler"
	"gleam/internal/harness/skill"
	"gleam/internal/harness/space"
	"gleam/internal/llm"
	"gleam/internal/tools/file"
	"gleam/pkg/types"
)

// Agent Gleam 核心引擎：Plan-Execute-Reflect 循环 + 任务登记。
type Agent struct {
	LLM       llm.Client
	Reg       *registry.Registry
	Mem       *memory.Manager
	Gate      *safety.Gate
	Skills    *skill.Store
	Sched     *scheduler.Scheduler
	Cfg       *config.Config
	Notifier  Notifier
	MCP       *MCPManager
	FileTools *file.Tools         // 文件工具集（工作区热切换时更新 Roots）
	Growth    *growth.Log         // 成长日志
	Convos    *conversation.Store // 多会话持久化（左侧会话列表）
	Spaces    *space.Store        // 微光空间（按工作文件夹隔离对话）
	Creds     *credentials.Store  // 本地敏感凭证（LLM Key / 云端会话，0600）
	Auth      AuthProvider        // 云端账号（可选）
	GEO       *geo.Store          // GEO 分析历史（创作产出自动分析的留档，供 GEO 板块回看）
	FastLLM   llm.Client          // 可选：辅助调用（压缩/GEO/技能优化）用的便宜模型，nil 时用主模型

	mu    sync.Mutex
	tasks map[string]*taskHandle

	// 消耗统计（消耗看板）：按 taskID 归集模型调用与工具调用开销
	usageMu  sync.Mutex
	usage    map[string]*types.TaskUsage
	budget   map[string]*taskBudget    // 用户确认后追加的预算额度
	stuck    map[string]map[string]int // taskID -> 「工具+参数」指纹 -> 出现在多少轮里（防打转）
	hookOnce sync.Once

	// 模型档位客户端缓存：档位名 -> 客户端（按配置惰性构建，设置保存后由 RebuildTierClients 重建）
	tierMu      sync.Mutex
	tierClients map[string]llm.Client

	// 上下文窗口的最近读数：最近一次请求的输入 token 规模（见 ctxwindow.go）。
	// 它不属于某个任务：memory 的短窗口是这台机器上所有任务共用的那一份。
	ctxMu               sync.Mutex
	lastPromptTokens    int
	lastPromptEstimated bool
}

// taskBudget 用户批准后追加的额度（在配置上限之上再加）。
type taskBudget struct {
	extraCalls  int
	extraTokens int
}

// AuthProvider 云端账号能力（由 internal/harness/auth.Manager 实现），agent 不直接依赖 auth 包。
type AuthProvider interface {
	Configured() bool
	Session() map[string]any
	Configure(supabaseURL, anonKey string) error
	SignUp(email, password string) error
	SignIn(email, password string) error
	SignOut() error
	StartOAuth(provider string) (string, error)
}

type taskHandle struct {
	id      string
	goal    string
	mode    string
	cancel  context.CancelFunc
	status  types.GoalStatus
	result  *types.GoalResult
	started time.Time
}

// New 创建 Agent（各子系统由运行时装配）。
func New(cfg *config.Config, client llm.Client, reg *registry.Registry, mem *memory.Manager,
	gate *safety.Gate, skills *skill.Store, sched *scheduler.Scheduler, notifier Notifier) *Agent {
	if notifier == nil {
		notifier = NopNotifier{}
	}
	return &Agent{
		LLM: client, Reg: reg, Mem: mem, Gate: gate,
		Skills: skills, Sched: sched, Cfg: cfg, Notifier: notifier,
		tasks: map[string]*taskHandle{},
	}
}

// ValidateGoalRequest 校验 Web/JSON-RPC 等外部入口的目标参数。
// 空模式使用兼容默认值；非空未知值必须显式拒绝，避免静默降级。
func ValidateGoalRequest(req types.GoalRequest) error {
	if err := types.ValidateReferences(req.References); err != nil {
		return err
	}
	if req.Mode != "" && req.Mode != string(types.ModeAuto) && req.Mode != string(types.ModePlanFirst) && req.Mode != string(types.ModeInteractive) {
		return fmt.Errorf("不支持的安全模式 %q", req.Mode)
	}
	if req.TaskMode != "" && req.TaskMode != types.TaskChat && req.TaskMode != types.TaskWork && req.TaskMode != types.TaskCode {
		return fmt.Errorf("不支持的任务模式 %q", req.TaskMode)
	}
	if req.Role != "" && !HasRole(req.Role) {
		return fmt.Errorf("不支持的专家角色 %q", req.Role)
	}
	// task_id 会被拼成任务归档的文件名（tasks/<id>.json），所以它的形状在**入口**就得定：
	// 放过去的话，任务会照常跑完、照常花 token，最后归档那一步默默失败——
	// 人在 `gleam replay` 报"读不到"的时候才发现，而那次的凭据已经没了。
	if req.TaskID != "" && SafeTaskName(req.TaskID) == "" {
		return fmt.Errorf("task_id %q 不能作文件名：只允许字母、数字与 - _，长度不超过 128", req.TaskID)
	}
	return nil
}

// maxRetainedTasks 已完成任务保留上限（防止长期运行内存无限增长）。
const maxRetainedTasks = 200

// pruneTasksLocked 淘汰最旧的已完成任务（须持有 a.mu）。
func (a *Agent) pruneTasksLocked() {
	if len(a.tasks) <= maxRetainedTasks {
		return
	}
	type rec struct {
		id      string
		started time.Time
	}
	var done []rec
	for id, h := range a.tasks {
		if h.status != types.GoalRunning {
			done = append(done, rec{id, h.started})
		}
	}
	sort.Slice(done, func(i, j int) bool { return done[i].started.Before(done[j].started) })
	for i := 0; i < len(done) && len(a.tasks) > maxRetainedTasks; i++ {
		delete(a.tasks, done[i].id)
		a.dropUsage(done[i].id)
	}
}

// BuildPlanner 按一次目标请求装配规划器。
//
// 与 RunGoal 内部走的是**同一段逻辑**——评测与排障需要「只规划不执行」时调它，
// 而不是自己再拼一个 Planner：两处各写一份，迟早改一边忘一边，
// 那样评测出来的就不是真实运行时的行为了。
func (a *Agent) BuildPlanner(req types.GoalRequest) *Planner {
	taskID := req.TaskID
	if taskID == "" {
		taskID = types.NewID()
	}
	pl := &Planner{
		LLM: a.TierLLM(req.Role), Reg: a.Reg,
		MaxSteps:   a.Cfg.Agent.MaxSteps,
		Style:      a.Cfg.Persona.Style,
		TaskMode:   string(normalizeTaskMode(req.TaskMode)),
		CommitHint: a.Cfg.Git.CommitInstructions,
		Role:       req.Role,
		TaskID:     taskID,
		// 生效档位：规则表据此决定"要不要给这一档换一版规则"（见 rules.go）。
		// 未配档位时为空，规则一律落到通用版——保守优先，能力未知就不动规则。
		Tier: a.EffectiveTier(req.Role),
		// 薄 Harness：工具多了只给能力菜单 + 按需筛选出的 schema。
		// Helper 只在真配了辅助模型时才给——否则快筛会白白多花一次主模型的钱。
		MaxToolSchemas: a.Cfg.Agent.MaxToolSchemas,
		Helper:         a.FastLLM,
		// 场景模板带来的差异：核心工具（保证在）、产出格式与模型档位（见 buildSystemPrompt / TierLLM）。
		RoleTools: FindRole(req.Role).Tools,
	}
	// 钉住区（P2-1）：不可压缩的用户约束，注入时排易变段最前、绝不重排。
	if a.Mem != nil {
		pl.Pinned = a.Mem.Pinned()
	}
	return pl
}

func (a *Agent) workspaceOf(req types.GoalRequest) string {
	if v, ok := req.Context["cwd"].(string); ok && v != "" {
		return v
	}
	if a.Cfg != nil && a.Cfg.Workspace != "" {
		return a.Cfg.Workspace
	}
	cwd, _ := os.Getwd()
	return cwd
}

func normalizeMode(m string) string {
	switch m {
	case "plan_first":
		return string(types.ModePlanFirst)
	case "interactive":
		return string(types.ModeInteractive)
	default:
		return string(types.ModeAuto)
	}
}

func planDescriptions(plan types.Plan) []string {
	out := make([]string, 0, len(plan.Steps))
	for _, st := range plan.Steps {
		out = append(out, fmt.Sprintf("%s. %s（%s）", st.ID, st.Describe(), st.Tool))
	}
	return out
}

func planMaxRisk(reg *registry.Registry, plan types.Plan) string {
	risk := "low"
	for _, st := range plan.Steps {
		t, ok := reg.Get(st.Tool)
		if !ok {
			continue
		}
		switch t.Permission() {
		case types.PermissionFullAccess:
			return "high"
		case types.PermissionUserApproved:
			risk = "medium"
		}
	}
	return risk
}

// SetNotifier 替换事件出口（服务层装配时调用）。
func (a *Agent) SetNotifier(n Notifier) {
	a.Notifier = n
}

// ApprovalTimeout 安全门控的审批等待时长。
func (a *Agent) ApprovalTimeout() time.Duration {
	if a.Gate == nil {
		return 5 * time.Minute
	}
	return a.Gate.ApprovalTimeoutDuration()
}

// CancelTask 取消运行中的任务。
func (a *Agent) CancelTask(taskID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	h, ok := a.tasks[taskID]
	if !ok || h.cancel == nil {
		return false
	}
	h.cancel()
	return true
}

// TaskStatus 查询任务状态。
func (a *Agent) TaskStatus(taskID string) (types.GoalStatus, *types.GoalResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h, ok := a.tasks[taskID]
	if !ok {
		return "", nil
	}
	return h.status, h.result
}

// RunningTasks 运行中任务数。
func (a *Agent) RunningTasks() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, h := range a.tasks {
		if h.status == types.GoalRunning {
			n++
		}
	}
	return n
}

// DataDir 保证数据目录存在。
func DataDir(cfg *config.Config) string {
	_ = os.MkdirAll(filepath.Join(cfg.DataDir, "memory"), 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.DataDir, "skills"), 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.DataDir, "tasks"), 0o755)
	return cfg.DataDir
}
