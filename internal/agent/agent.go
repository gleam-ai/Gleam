package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gleam/internal/agent/geo"
	"gleam/internal/config"
	"gleam/internal/harness/conversation"
	"gleam/internal/harness/credentials"
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

// Agent is the Gleam core engine: the Plan-Execute-Reflect loop plus the task
// registry.
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
	FileTools *file.Tools         // file toolset (Roots updated on workspace hot-switch)
	Growth    *growth.Log         // growth log
	Convos    *conversation.Store // multi-conversation persistence (left sidebar list)
	Spaces    *space.Store        // Gleam spaces (conversations isolated per working folder)
	Creds     *credentials.Store  // local sensitive credentials (LLM key / cloud session, 0600)
	Auth      AuthProvider        // cloud account (optional)
	GEO       *geo.Store          // GEO analysis history (archive of auto-analyzed creative output)
	FastLLM   llm.Client          // optional: cheap model for auxiliary calls (compression/GEO/skill tuning); main model when nil

	mu    sync.Mutex
	tasks map[string]*taskHandle

	// Usage accounting (usage dashboard): model and tool call costs grouped
	// by taskID.
	usageMu  sync.Mutex
	usage    map[string]*types.TaskUsage
	budget   map[string]*taskBudget    // extra quota granted after user confirmation
	stuck    map[string]map[string]int // taskID -> "tool+args" fingerprint -> rounds it appeared in (anti-spin)
	hookOnce sync.Once

	// Model tier client cache: tier name -> client (built lazily per config,
	// rebuilt by RebuildTierClients after a settings save).
	tierMu      sync.Mutex
	tierClients map[string]llm.Client

	// Latest context window reading: input token size of the most recent
	// request (see ctxwindow.go). It belongs to no task: memory's short window
	// is the one shared by all tasks on this machine.
	ctxMu               sync.Mutex
	lastPromptTokens    int
	lastPromptEstimated bool
}

// taskBudget is extra quota granted after user approval (on top of the config
// cap).
type taskBudget struct {
	extraCalls  int
	extraTokens int
}

// AuthProvider is the cloud account capability (implemented by
// internal/harness/auth.Manager); agent does not depend on the auth package
// directly.
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

// New creates an Agent (subsystems are wired by the runtime).
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

// ValidateGoalRequest validates goal parameters from external entry points
// such as Web/JSON-RPC. An empty mode uses the compatible default; a non-empty
// unknown value must be rejected explicitly, avoiding silent downgrade.
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
	// task_id becomes part of the task archive filename (tasks/<id>.json), so
	// its shape must be fixed at the entry: letting it through, the task runs
	// normally and spends tokens as usual, and the archive step fails
	// silently -- the user only notices when `gleam replay` reports "not
	// found", and by then the evidence is gone.
	if req.TaskID != "" && SafeTaskName(req.TaskID) == "" {
		return fmt.Errorf("task_id %q 不能作文件名：只允许字母、数字与 - _，长度不超过 128", req.TaskID)
	}
	return nil
}

// maxRetainedTasks caps retained completed tasks (prevents unbounded memory
// growth in long runs).
const maxRetainedTasks = 200

// pruneTasksLocked evicts the oldest completed tasks (a.mu must be held).
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

// BuildPlanner assembles a planner for one goal request.
//
// It is the same logic RunGoal uses internally -- call it when evaluation or
// troubleshooting needs "plan only, no execute", instead of building another
// Planner: two copies drift apart sooner or later, and then what is evaluated
// is no longer the real runtime behavior.
func (a *Agent) BuildPlanner(req types.GoalRequest) *Planner {
	return a.buildPlanner(req, a.TierLLM(req.Role), a.FastLLM)
}

func (a *Agent) buildPlanner(req types.GoalRequest, tier, helper llm.Client) *Planner {
	taskID := req.TaskID
	if taskID == "" {
		taskID = types.NewID()
	}
	pl := &Planner{
		LLM: tier, Reg: a.Reg,
		MaxSteps:   a.Cfg.Agent.MaxSteps,
		Style:      a.Cfg.Persona.Style,
		TaskMode:   string(normalizeTaskMode(req.TaskMode)),
		CommitHint: a.Cfg.Git.CommitInstructions,
		Role:       req.Role,
		TaskID:     taskID,
		// Effective tier: the rule table uses it to decide "whether to swap in
		// a different rule version for this tier" (see rules.go). Empty when
		// no tier is configured, and rules fall to the generic version --
		// conservative first: unknown capability means no rule changes.
		Tier: a.EffectiveTier(req.Role),
		// Thin harness: with many tools, only the capability menu plus
		// on-demand filtered schemas. Helper is given only when an auxiliary
		// model is really configured -- otherwise screening wastes an extra
		// main-model call.
		MaxToolSchemas: a.Cfg.Agent.MaxToolSchemas,
		Helper:         helper,
		// Differences from scenario templates: core tools (always present),
		// output format and model tier (see buildSystemPrompt / TierLLM).
		RoleTools: FindRole(req.Role).Tools,
	}
	// Pinned zone (P2-1): incompressible user constraints, placed before the
	// volatile segment at injection and never reordered.
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

// SetNotifier replaces the event sink (called when the service layer wires up).
func (a *Agent) SetNotifier(n Notifier) {
	a.Notifier = n
}

// ApprovalTimeout is the safety gate's approval wait duration.
func (a *Agent) ApprovalTimeout() time.Duration {
	if a.Gate == nil {
		return 5 * time.Minute
	}
	return a.Gate.ApprovalTimeoutDuration()
}

// CancelTask cancels a running task.
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

// TaskStatus queries a task's status.
func (a *Agent) TaskStatus(taskID string) (types.GoalStatus, *types.GoalResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h, ok := a.tasks[taskID]
	if !ok {
		return "", nil
	}
	return h.status, h.result
}

// RunningTasks counts running tasks.
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

// DataDir ensures the data directories exist.
func DataDir(cfg *config.Config) string {
	_ = os.MkdirAll(filepath.Join(cfg.DataDir, "memory"), 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.DataDir, "skills"), 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.DataDir, "tasks"), 0o755)
	return cfg.DataDir
}
