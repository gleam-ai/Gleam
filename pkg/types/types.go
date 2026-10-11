package types

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func NewID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

type Permission int

const (
	PermissionReadOnly Permission = iota
	PermissionUserApproved
	PermissionFullAccess
)

func (p Permission) String() string {
	switch p {
	case PermissionReadOnly:
		return "readonly"
	case PermissionUserApproved:
		return "user_approved"
	default:
		return "full_access"
	}
}

type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Permission() Permission
	Execute(ctx context.Context, args map[string]any) (any, error)
}

type PathAware interface {
	Paths(ctx context.Context, args map[string]any) []string
}

type StepOutcome string

const (
	OutcomeOK     StepOutcome = "ok"
	OutcomeEmpty  StepOutcome = "empty"
	OutcomeFailed StepOutcome = "failed"
)

type OutcomeReporter interface {
	Outcome(args map[string]any, out any) (StepOutcome, string)
}

type ErrorKind string

const (
	ErrParam      ErrorKind = "param"
	ErrFormat     ErrorKind = "format"
	ErrBusiness   ErrorKind = "business"
	ErrPermission ErrorKind = "permission"
	ErrNotFound   ErrorKind = "not_found"
	ErrTimeout    ErrorKind = "timeout"
	ErrUpstream   ErrorKind = "upstream"
	ErrUnknown    ErrorKind = "unknown"
	ErrInternal   ErrorKind = "internal"
)

func ClassifyError(msg string) ErrorKind {
	s := strings.ToLower(msg)
	switch {
	case containsAny(s, "timeout", "deadline", "超时", "限流", "rate limit", "too many requests", "429"):
		return ErrTimeout
	case containsAny(s, "5xx", "502", "503", "504", "bad gateway", "service unavailable", "internal server error", "上游"):
		return ErrUpstream
	case containsAny(s, "permission", "denied", "forbidden", "unauthorized", "401", "403", "权限", "拒绝执行", "审批", "未授权"):
		return ErrPermission
	case containsAny(s, "not found", "no such", "不存在", "404", "找不到", "无法找到"):
		return ErrNotFound
	case containsAny(s, "invalid argument", "invalid param", "missing", "缺少参数", "参数", "引用的步骤"):
		return ErrParam
	case containsAny(s, "invalid json", "unmarshal", "解析失败", "格式"):
		return ErrFormat
	case containsAny(s, "退出码", "exit code", "业务", "已拒绝", "rejected"):
		return ErrBusiness
	default:
		return ErrUnknown
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

type Step struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	Tool        string         `json:"tool"`
	Args        map[string]any `json:"args,omitempty"`
	DependsOn   []string       `json:"depends_on,omitempty"`
}

type Plan struct {
	Goal          string   `json:"goal"`
	Steps         []Step   `json:"steps"`
	EstimatedTime string   `json:"estimated_time,omitempty"`
	Acceptance    []string `json:"acceptance,omitempty"`
}

type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepSucceeded StepStatus = "succeeded"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
)

type StepResult struct {
	StepID     string         `json:"step_id"`
	Tool       string         `json:"tool"`
	Status     StepStatus     `json:"status"`
	Outcome    StepOutcome    `json:"outcome,omitempty"`
	Output     any            `json:"output,omitempty"`
	Error      string         `json:"error,omitempty"`
	ErrorKind  ErrorKind      `json:"error_kind,omitempty"`
	FinalArgs  map[string]any `json:"final_args,omitempty"`
	StartedAt  time.Time      `json:"started_at,omitempty"`
	FinishedAt time.Time      `json:"finished_at,omitempty"`
	DurationMS int64          `json:"duration_ms"`
	Deduped    bool           `json:"deduped,omitempty"`
	Attempt    int            `json:"attempt,omitempty"`
	Retried    bool           `json:"retried,omitempty"`
	Carried    bool           `json:"carried,omitempty"`
}

type ContextBreakdown struct {
	Instruction int `json:"instruction"`
	Capability  int `json:"capability"`
	Knowledge   int `json:"knowledge"`
	State       int `json:"state"`
	Total       int `json:"total"`
}

func (c ContextBreakdown) StateShare() float64 {
	if c.Total <= 0 {
		return 0
	}
	return float64(c.State) / float64(c.Total)
}

type CheckResult struct {
	Criterion string `json:"criterion"`
	Passed    bool   `json:"passed"`
	Note      string `json:"note,omitempty"`
}

type Reflection struct {
	Score      int           `json:"score"`
	Verdict    string        `json:"verdict"`
	Reason     string        `json:"reason"`
	Suggestion string        `json:"suggestion,omitempty"`
	Checks     []CheckResult `json:"checks,omitempty"`
}

type ProgressEvent struct {
	TaskID   string `json:"task_id"`
	Phase    string `json:"phase"`
	Message  string `json:"message"`
	Progress int    `json:"progress"`
	Kind     string `json:"kind"`
}

type ToolEvent struct {
	TaskID     string    `json:"task_id"`
	StepID     string    `json:"step_id"`
	Tool       string    `json:"tool"`
	Phase      string    `json:"phase"`
	Title      string    `json:"title"`
	Detail     string    `json:"detail,omitempty"`
	Status     string    `json:"status,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

const (
	ToolStart = "start"
	ToolEnd   = "end"
)

const (
	ToolDone    = "done"
	ToolFailed  = "failed"
	ToolSkipped = "skipped"
)

type TaskDoneEvent struct {
	TaskID string     `json:"task_id"`
	Origin string     `json:"origin"`
	Result GoalResult `json:"result"`
}

func (e TaskDoneEvent) Succeeded() bool { return e.Result.Status == GoalSuccess }

func (e TaskDoneEvent) StateText() string {
	switch e.Result.Status {
	case GoalSuccess:
		return "完成"
	case GoalPartial:
		return "部分完成"
	case GoalCancelled:
		return "已取消"
	default:
		return "失败"
	}
}

func (e TaskDoneEvent) Title() string {
	origin := strings.TrimSpace(e.Origin)
	if origin == "" {
		origin = "后台任务"
	}
	return origin + e.StateText()
}

func (e TaskDoneEvent) Line() string {
	parts := []string{e.StateText() + "：" + Shorten(strings.TrimSpace(e.Result.Goal), 60)}
	if e.Result.Status == GoalSuccess || e.Result.Status == GoalPartial {
		parts = append(parts, fmt.Sprintf("完成度 %d", e.Result.Score))
	}
	if detail := strings.TrimSpace(e.Result.Error); detail != "" {
		parts = append(parts, Shorten(detail, 120))
	} else if e.Result.Status != GoalSuccess {
		if s := strings.TrimSpace(e.Result.Summary); s != "" {
			parts = append(parts, Shorten(s, 120))
		}
	}
	return strings.Join(parts, " · ")
}

type ApprovalRequest struct {
	TaskID    string   `json:"task_id"`
	Plan      []string `json:"plan"`
	Risk      string   `json:"risk"`
	Reason    string   `json:"reason,omitempty"`
	StepID    string   `json:"step_id,omitempty"`
	WholePlan bool     `json:"whole_plan"`
}

type ApprovalResponse struct {
	Approved bool   `json:"approved"`
	Note     string `json:"note,omitempty"`
}

type GoalMode string

const (
	ModeAuto        GoalMode = "auto"
	ModePlanFirst   GoalMode = "plan_first"
	ModeInteractive GoalMode = "interactive"
)

type TaskMode string

const (
	TaskChat TaskMode = "chat"
	TaskWork TaskMode = "work"
	TaskCode TaskMode = "code"
)

type Reference struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Value string `json:"value"`
}

const MaxReferences = 20

func ValidateReferences(refs []Reference) error {
	if len(refs) > MaxReferences {
		return fmt.Errorf("引用数量不能超过 %d", MaxReferences)
	}
	allowed := map[string]bool{"file": true, "goal": true, "skill": true, "plugin": true, "memory": true}
	for i, ref := range refs {
		if !allowed[ref.Kind] {
			return fmt.Errorf("第 %d 个引用类型 %q 不受支持", i+1, ref.Kind)
		}
		label := strings.TrimSpace(ref.Label)
		value := strings.TrimSpace(ref.Value)
		if label == "" || value == "" {
			return fmt.Errorf("第 %d 个引用的 label 和 value 不能为空", i+1)
		}
		if len([]rune(label)) > 200 || len([]rune(value)) > 2048 {
			return fmt.Errorf("第 %d 个引用过长", i+1)
		}
	}
	return nil
}

type GoalRequest struct {
	TaskID     string         `json:"task_id,omitempty"`
	Goal       string         `json:"goal"`
	Context    map[string]any `json:"context,omitempty"`
	References []Reference    `json:"references,omitempty"`
	Mode       string         `json:"mode"`
	TaskMode   TaskMode       `json:"task_mode,omitempty"`
	Role       string         `json:"role,omitempty"`

	ConversationID string `json:"conversation_id,omitempty"`
}

type GoalStatus string

const (
	GoalRunning   GoalStatus = "running"
	GoalSuccess   GoalStatus = "success"
	GoalPartial   GoalStatus = "partial"
	GoalFailed    GoalStatus = "failed"
	GoalCancelled GoalStatus = "cancelled"
)

type TaskUsage struct {
	LLMCalls         int   `json:"llm_calls"`
	PromptTokens     int   `json:"prompt_tokens"`
	CompletionTokens int   `json:"completion_tokens"`
	EstimatedCalls   int   `json:"estimated_calls"`
	CachedTokens     int   `json:"cached_tokens"`
	CachedCalls      int   `json:"cached_calls"`
	ToolCalls        int   `json:"tool_calls"`
	Retries          int   `json:"retries"`
	Deduped          int   `json:"deduped"`
	DurationMs       int64 `json:"duration_ms"`
}

func (u TaskUsage) CacheHitRate() float64 {
	if u.PromptTokens <= 0 || u.CachedTokens <= 0 {
		return 0
	}
	if u.CachedTokens > u.PromptTokens {
		return 1
	}
	return float64(u.CachedTokens) / float64(u.PromptTokens)
}

func (u TaskUsage) TotalTokens() int { return u.PromptTokens + u.CompletionTokens }

type ConfigSnapshot struct {
	Provider   string            `json:"provider,omitempty"`
	Model      string            `json:"model,omitempty"`
	FastModel  string            `json:"fast_model,omitempty"`
	Tiers      map[string]string `json:"tiers,omitempty"`
	SafetyMode string            `json:"safety_mode,omitempty"`

	DoneThreshold   int  `json:"done_threshold,omitempty"`
	MaxReplans      int  `json:"max_replans,omitempty"`
	MaxSteps        int  `json:"max_steps,omitempty"`
	StepTimeoutSecs int  `json:"step_timeout_secs,omitempty"`
	StepRetries     int  `json:"step_retries,omitempty"`
	MaxConcurrency  int  `json:"max_concurrency,omitempty"`
	DedupeCalls     bool `json:"dedupe_calls,omitempty"`
	MaxOutputRunes  int  `json:"max_output_runes,omitempty"`
	MaxToolSchemas  int  `json:"max_tool_schemas,omitempty"`
	ReflectEachStep bool `json:"reflect_each_step,omitempty"`
}

type GoalResult struct {
	TaskID           string            `json:"task_id"`
	Goal             string            `json:"goal"`
	Status           GoalStatus        `json:"status"`
	Mode             string            `json:"mode,omitempty"`
	TaskMode         TaskMode          `json:"task_mode,omitempty"`
	Score            int               `json:"score"`
	Summary          string            `json:"summary"`
	Error            string            `json:"error,omitempty"`
	Suggestion       string            `json:"suggestion,omitempty"`
	Steps            []StepResult      `json:"steps,omitempty"`
	Usage            TaskUsage         `json:"usage"`
	StartedAt        time.Time         `json:"started_at"`
	FinishedAt       time.Time         `json:"finished_at"`
	Acceptance       []string          `json:"acceptance,omitempty"`
	Checks           []CheckResult     `json:"checks,omitempty"`
	RuleSet          string            `json:"rule_set,omitempty"`
	Rules            []string          `json:"rules,omitempty"`
	ConfigSnapshot   *ConfigSnapshot   `json:"config_snapshot,omitempty"`
	TraceID          string            `json:"trace_id,omitempty"`
	ExecutedPlan     *Plan             `json:"executed_plan,omitempty"`
	PromptBreakdown  *ContextBreakdown `json:"prompt_breakdown,omitempty"`
	FailureBreakdown map[ErrorKind]int `json:"failure_breakdown,omitempty"`
	Changes          []FileChange      `json:"changes,omitempty"`
	FirstPass        bool              `json:"first_pass,omitempty"`
	Reworks          int               `json:"reworks,omitempty"`
}

type FileChange struct {
	Path         string `json:"path"`
	Kind         string `json:"kind"`
	Tool         string `json:"tool,omitempty"`
	StepID       string `json:"step_id,omitempty"`
	OK           bool   `json:"ok"`
	Note         string `json:"note,omitempty"`
	Bytes        int64  `json:"bytes"`
	PrevBytes    int64  `json:"prev_bytes,omitempty"`
	Reversible   bool   `json:"reversible"`
	Blocked      string `json:"blocked,omitempty"`
	AddedLines   int    `json:"added_lines,omitempty"`
	DeletedLines int    `json:"deleted_lines,omitempty"`
	Reverted     bool   `json:"reverted,omitempty"`
}

type ReplayKind string

const (
	ReplayRerun  ReplayKind = "rerun"
	ReplayResume ReplayKind = "resume"
	ReplayFork   ReplayKind = "fork"
)

type ReplayRecord struct {
	ReplayID     string         `json:"replay_id"`
	Kind         ReplayKind     `json:"kind"`
	SourceTaskID string         `json:"source_task_id"`
	FromStep     string         `json:"from_step,omitempty"`
	OverrideTool string         `json:"override_tool,omitempty"`
	OverrideArgs map[string]any `json:"override_args,omitempty"`

	CreatedAt      time.Time       `json:"created_at"`
	Steps          []StepResult    `json:"steps"`
	Succeeded      int             `json:"succeeded"`
	Failed         int             `json:"failed"`
	Skipped        int             `json:"skipped"`
	Empty          int             `json:"empty"`
	Carried        int             `json:"carried"`
	DurationMs     int64           `json:"duration_ms"`
	ConfigSnapshot *ConfigSnapshot `json:"config_snapshot,omitempty"`
}

type Suggestion struct {
	TaskID string      `json:"task_id,omitempty"`
	Type   string      `json:"type"`
	Text   string      `json:"text,omitempty"`
	Skill  *SkillDraft `json:"skill,omitempty"`
}

func (g *GoalResult) RetryOK() int {
	n := 0
	for _, st := range g.Steps {
		if st.Retried && st.Status == StepSucceeded {
			n++
		}
	}
	return n
}

type SkillDraft struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Steps       []Step `json:"steps"`
}

type MemoryHitView struct {
	ID      string   `json:"id"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
	Score   float32  `json:"score"`
}

type ScheduleJobView struct {
	Name         string `json:"name"`
	Cron         string `json:"cron,omitempty"`
	IntervalSec  int    `json:"interval_sec,omitempty"`
	Goal         string `json:"goal"`
	Mode         string `json:"mode,omitempty"`
	Notify       string `json:"notify,omitempty"`
	Enabled      bool   `json:"enabled"`
	WhenText     string `json:"when_text,omitempty"`
	ScheduleText string `json:"schedule_text,omitempty"`
	NextRun      string `json:"next_run,omitempty"`
	LastRun      string `json:"last_run,omitempty"`
}

var ErrStepRejected = fmt.Errorf("用户拒绝执行该操作")

func (s Step) Describe() string {
	if s.Description != "" {
		return s.Description
	}
	return fmt.Sprintf("调用 %s", s.Tool)
}

func Shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	cjk, other := 0, 0
	for _, r := range s {
		switch {
		case r >= 0x2E80 && r <= 0x9FFF || r >= 0xF900 && r <= 0xFAFF || r >= 0xFF00 && r <= 0xFFEF:
			cjk++
		default:
			other++
		}
	}
	return cjk + (other+3)/4
}

type FeedbackKind string

const (
	FeedbackBug        FeedbackKind = "bug"
	FeedbackSuggestion FeedbackKind = "suggestion"
)

type FeedbackDelivery string

const (
	FeedbackLocalOnly FeedbackDelivery = "local_only"
	FeedbackSent      FeedbackDelivery = "sent"
	FeedbackFailed    FeedbackDelivery = "failed"
)

type FeedbackAttachment struct {
	Name  string `json:"name"`
	Mime  string `json:"mime"`
	Bytes int    `json:"bytes"`
}

type FeedbackContext struct {
	AppVersion string     `json:"app_version"`
	GoVersion  string     `json:"go_version,omitempty"`
	OS         string     `json:"os,omitempty"`
	Model      string     `json:"model,omitempty"`
	LLMHost    string     `json:"llm_host,omitempty"`
	TaskID     string     `json:"task_id,omitempty"`
	TaskStatus GoalStatus `json:"task_status,omitempty"`
	FailedTool string     `json:"failed_tool,omitempty"`
}

type Feedback struct {
	ID           string               `json:"id"`
	Kind         FeedbackKind         `json:"kind"`
	Text         string               `json:"text"`
	Attachments  []FeedbackAttachment `json:"attachments,omitempty"`
	Context      FeedbackContext      `json:"context"`
	CreatedAt    time.Time            `json:"created_at"`
	Delivery     FeedbackDelivery     `json:"delivery"`
	DeliveryNote string               `json:"delivery_note,omitempty"`
}
