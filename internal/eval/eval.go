package eval

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gleam/pkg/types"
)

type Depth string

const (
	DepthSelect Depth = "select"
	DepthPlan   Depth = "plan"
	DepthFull   Depth = "full"
)

func ValidDepth(d Depth) bool {
	switch d {
	case DepthSelect, DepthPlan, DepthFull:
		return true
	}
	return false
}

type Layer string

const (
	LayerSmoke       Layer = "smoke"
	LayerRegression  Layer = "regression"
	LayerEdge        Layer = "edge"
	LayerAdversarial Layer = "adversarial"
	LayerHoldout     Layer = "holdout"
)

func ValidLayer(l Layer) bool {
	switch l {
	case "", LayerSmoke, LayerRegression, LayerEdge, LayerAdversarial, LayerHoldout:
		return true
	}
	return false
}

func LayerOf(c Case) Layer {
	if c.Layer == "" {
		return LayerRegression
	}
	return Layer(c.Layer)
}

type Case struct {
	ID       string `json:"id"`
	Goal     string `json:"goal"`
	Role     string `json:"role,omitempty"`
	TaskMode string `json:"task_mode,omitempty"`
	CWD      string `json:"cwd,omitempty"`
	Note     string `json:"note,omitempty"`

	WantTools      []string `json:"want_tools,omitempty"`
	NotWantTools   []string `json:"not_want_tools,omitempty"`
	TopN           int      `json:"top_n,omitempty"`
	MinSteps       int      `json:"min_steps,omitempty"`
	MaxSteps       int      `json:"max_steps,omitempty"`
	WantAcceptance bool     `json:"want_acceptance,omitempty"`
	WantStatus     []string `json:"want_status,omitempty"`

	KnownIssue bool `json:"known_issue,omitempty"`
	Unsolvable bool `json:"unsolvable,omitempty"`

	Layer string `json:"layer,omitempty"`
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type observation struct {
	Tools      []string
	Steps      int
	Acceptance int
	Status     string
	Error      string
}

type CaseUsage struct {
	LLMCalls         int `json:"llm_calls,omitempty"`
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	EstimatedCalls   int `json:"estimated_calls,omitempty"`
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CachedCalls      int `json:"cached_calls,omitempty"`
	ToolCalls        int `json:"tool_calls,omitempty"`
}

func (u CaseUsage) TotalTokens() int { return u.PromptTokens + u.CompletionTokens }

func (u CaseUsage) Empty() bool {
	return u.LLMCalls == 0 && u.PromptTokens == 0 && u.CompletionTokens == 0 &&
		u.EstimatedCalls == 0 && u.CachedTokens == 0 && u.CachedCalls == 0 && u.ToolCalls == 0
}

func (u *CaseUsage) add(o CaseUsage) {
	u.LLMCalls += o.LLMCalls
	u.PromptTokens += o.PromptTokens
	u.CompletionTokens += o.CompletionTokens
	u.EstimatedCalls += o.EstimatedCalls
	u.CachedTokens += o.CachedTokens
	u.CachedCalls += o.CachedCalls
	u.ToolCalls += o.ToolCalls
}

func usageFromTask(t types.TaskUsage) *CaseUsage {
	return &CaseUsage{
		LLMCalls:         t.LLMCalls,
		PromptTokens:     t.PromptTokens,
		CompletionTokens: t.CompletionTokens,
		EstimatedCalls:   t.EstimatedCalls,
		CachedTokens:     t.CachedTokens,
		CachedCalls:      t.CachedCalls,
		ToolCalls:        t.ToolCalls,
	}
}

type CaseResult struct {
	ID           string         `json:"id"`
	Goal         string         `json:"goal"`
	Passed       bool           `json:"passed"`
	Checks       []Check        `json:"checks"`
	Tools        []string       `json:"tools,omitempty"`
	Steps        int            `json:"steps,omitempty"`
	Acceptance   int            `json:"acceptance,omitempty"`
	Status       string         `json:"status,omitempty"`
	Score        int            `json:"score,omitempty"`
	Note         string         `json:"note,omitempty"`
	Known        bool           `json:"known,omitempty"`
	Holdout      bool           `json:"holdout,omitempty"`
	PromptChars  int            `json:"prompt_chars,omitempty"`
	DurationMS   int64          `json:"duration_ms"`
	Error        string         `json:"error,omitempty"`
	Rules        []string       `json:"rules,omitempty"`
	FailureKinds map[string]int `json:"failure_kinds,omitempty"`
	RetryOK      int            `json:"retry_ok,omitempty"`
	Runs         int            `json:"runs,omitempty"`
	Agreed       int            `json:"agreed,omitempty"`
	Stable       bool           `json:"stable,omitempty"`
	Variants     []string       `json:"variants,omitempty"`
	Usage        *CaseUsage     `json:"usage,omitempty"`
}

type Diff struct {
	BaselineAt  time.Time `json:"baseline_at"`
	Fixed       []string  `json:"fixed,omitempty"`
	Broke       []string  `json:"broke,omitempty"`
	Added       []string  `json:"added,omitempty"`
	Removed     []string  `json:"removed,omitempty"`
	PromptDelta int       `json:"prompt_delta"`
	RuleSetFrom string    `json:"rule_set_from,omitempty"`
	RuleSetTo   string    `json:"rule_set_to,omitempty"`
}

type Report struct {
	GeneratedAt       time.Time      `json:"generated_at"`
	Depth             Depth          `json:"depth"`
	Model             string         `json:"model"`
	LocalSelect       bool           `json:"local_select"`
	RelevantOnly      bool           `json:"relevant_only,omitempty"`
	Tier              string         `json:"tier,omitempty"`
	RuleSet           string         `json:"rule_set,omitempty"`
	Total             int            `json:"total"`
	Passed            int            `json:"passed"`
	Failed            int            `json:"failed"`
	Known             int            `json:"known,omitempty"`
	Resolved          []string       `json:"resolved,omitempty"`
	Unsolvable        int            `json:"unsolvable,omitempty"`
	UnsolvableHandled int            `json:"unsolvable_handled,omitempty"`
	Attempted         int            `json:"attempted,omitempty"`
	RetryOK           int            `json:"retry_ok,omitempty"`
	HoldoutTotal      int            `json:"holdout_total,omitempty"`
	HoldoutPassed     int            `json:"holdout_passed,omitempty"`
	FailureBreakdown  map[string]int `json:"failure_breakdown,omitempty"`
	PromptChars       int            `json:"prompt_chars"`
	Usage             *CaseUsage     `json:"usage,omitempty"`
	RepeatN           int            `json:"repeat_n,omitempty"`
	StableCases       int            `json:"stable_cases,omitempty"`
	UnstableCases     []string       `json:"unstable_cases,omitempty"`
	Repeatability     *float64       `json:"repeatability,omitempty"`
	Cases             []CaseResult   `json:"cases"`
	Diff              *Diff          `json:"diff,omitempty"`
}

func (r Report) SummaryLine() string {
	s := fmt.Sprintf("评测（%s）%d/%d 通过，%d 项未过", r.Depth, r.Passed, r.Total, r.Failed)
	if r.Known > 0 {
		s += fmt.Sprintf("，%d 项已知问题", r.Known)
	}
	if r.Attempted > 0 && r.Attempted != r.Total {
		s += fmt.Sprintf("；通过率分母 %d（不含 %d 条不可解用例）", r.Attempted, r.Total-r.Attempted)
	}
	if r.Diff != nil && len(r.Diff.Broke) > 0 {
		s += fmt.Sprintf("；⚠ 回归 %d 项", len(r.Diff.Broke))
	}
	if r.Repeatability != nil {
		s += fmt.Sprintf("；可重复性 %.0f%%（%d 条 ×%d 遍，%d 条不稳）",
			*r.Repeatability*100, r.Total, r.RepeatN, len(r.UnstableCases))
	}
	return s
}

const DefaultTopN = 5

func judge(c Case, obs observation, d Depth) []Check {
	var out []Check

	if c.Unsolvable && d == DepthFull {
		handled := obs.Status != "success"
		detail := "实际 " + obs.Status
		if handled {
			detail = "如实报告为 " + obs.Status + "（没有硬猜成功）"
		} else {
			detail = "对不可解目标报了 success——指标这样记会激励硬猜"
		}
		out = append(out, Check{Name: "识别为不可解（不硬猜）", Passed: handled, Detail: detail})
		return out
	}

	if obs.Error != "" {
		out = append(out, Check{Name: "执行无错误", Passed: false, Detail: obs.Error})
	} else {
		out = append(out, Check{Name: "执行无错误", Passed: true})
	}

	window := obs.Tools
	if d == DepthSelect {
		n := c.TopN
		if n <= 0 {
			n = DefaultTopN
		}
		if len(window) > n {
			window = window[:n]
		}
	}

	for _, want := range c.WantTools {
		if idx := indexOf(window, want); idx >= 0 {
			detail := ""
			if d == DepthSelect {
				detail = fmt.Sprintf("相关度第 %d 名", idx+1)
			}
			out = append(out, Check{Name: "应选到 " + want, Passed: true, Detail: detail})
		} else {
			out = append(out, Check{
				Name: "应选到 " + want, Passed: false,
				Detail: whyMissing(want, obs.Tools, window, d),
			})
		}
	}

	for _, bad := range c.NotWantTools {
		if idx := indexOf(window, bad); idx >= 0 {
			detail := "误路由——目标与这个工具无关，选到它多半会做错事"
			if d == DepthSelect {
				detail = fmt.Sprintf("相关度第 %d 名，进了前 %d 名：%s",
					idx+1, len(window), joinOrNone(window))
			}
			out = append(out, Check{Name: "不该选到 " + bad, Passed: false, Detail: detail})
		} else {
			out = append(out, Check{Name: "不该选到 " + bad, Passed: true})
		}
	}

	if d == DepthPlan || d == DepthFull {
		if c.MinSteps > 0 || c.MaxSteps > 0 {
			ok := true
			detail := fmt.Sprintf("实际 %d 步", obs.Steps)
			if c.MinSteps > 0 && obs.Steps < c.MinSteps {
				ok = false
			}
			if c.MaxSteps > 0 && obs.Steps > c.MaxSteps {
				ok = false
			}
			name := fmt.Sprintf("步数在 %d..%d 之间", c.MinSteps, c.MaxSteps)
			if c.MinSteps == 0 {
				name = fmt.Sprintf("步数不超过 %d", c.MaxSteps)
			}
			out = append(out, Check{Name: name, Passed: ok, Detail: detail})
		}
		if c.WantAcceptance {
			ok := obs.Acceptance > 0
			detail := fmt.Sprintf("实际 %d 条", obs.Acceptance)
			if !ok {
				detail = "没给验收标准——规划时就该定好「做到什么算完成」，否则反思阶段只能凭感觉打分"
			}
			out = append(out, Check{Name: "给出验收标准", Passed: ok, Detail: detail})
		}
		if len(c.WantStatus) > 0 {
			ok := contains(c.WantStatus, obs.Status)
			out = append(out, Check{
				Name:   "目标状态属于 " + strings.Join(c.WantStatus, "|"),
				Passed: ok,
				Detail: "实际 " + obs.Status,
			})
		}
	}

	return out
}

func contains(list []string, want string) bool {
	return indexOf(list, want) >= 0
}

func indexOf(list []string, want string) int {
	for i, s := range list {
		if s == want {
			return i
		}
	}
	return -1
}

func whyMissing(want string, all, window []string, d Depth) string {
	if d != DepthSelect {
		return fmt.Sprintf("实际用到 %s", joinOrNone(all))
	}
	if idx := indexOf(all, want); idx >= 0 {
		return fmt.Sprintf("相关度第 %d 名，落在前 %d 名之外；前 %d 名：%s",
			idx+1, len(window), len(window), joinOrNone(window))
	}
	return fmt.Sprintf("本地打分完全没命中；前 %d 名：%s", len(window), joinOrNone(window))
}

func joinOrNone(list []string) string {
	if len(list) == 0 {
		return "（空）"
	}
	return strings.Join(list, "、")
}

func runSignature(passed bool, steps int, tools []string) string {
	sorted := append([]string(nil), tools...)
	sort.Strings(sorted)
	return fmt.Sprintf("%s|%d步|%s", passMark(passed), steps, strings.Join(sorted, ","))
}

func passMark(passed bool) string {
	if passed {
		return "通过"
	}
	return "未过"
}

func modalVariant(sigs []string) (string, int) {
	counts := map[string]int{}
	order := make([]string, 0, len(sigs))
	for _, s := range sigs {
		if counts[s] == 0 {
			order = append(order, s)
		}
		counts[s]++
	}
	best, bestN := "", 0
	for _, s := range order {
		if counts[s] > bestN {
			best, bestN = s, counts[s]
		}
	}
	return best, bestN
}

func describeVariants(sigs []string) []string {
	counts := map[string]int{}
	order := make([]string, 0, len(sigs))
	for _, s := range sigs {
		if counts[s] == 0 {
			order = append(order, s)
		}
		counts[s]++
	}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	out := make([]string, 0, len(order))
	for _, s := range order {
		out = append(out, fmt.Sprintf("%s ×%d", s, counts[s]))
	}
	return out
}
