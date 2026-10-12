package eval

import (
	"context"
	"time"
	"unicode/utf8"

	"gleam/internal/agent"
	"gleam/pkg/types"
)

const DefaultTimeout = 60 * time.Second

type Runner struct {
	Agent   *agent.Agent
	Depth   Depth
	CWD     string
	Timeout time.Duration
	Tier    string
	OnCase  func(CaseResult)
	Repeat  int
}

func (r *Runner) plannerFor(req types.GoalRequest) *agent.Planner {
	p := r.Agent.BuildPlanner(req)
	if r.Tier != "" {
		p.Tier = r.Tier
	}
	return p
}

func (r *Runner) Run(ctx context.Context, cases []Case) Report {
	rep := Report{
		GeneratedAt:  time.Now(),
		Depth:        r.Depth,
		Model:        r.modelName(),
		LocalSelect:  r.Depth == DepthSelect,
		RelevantOnly: r.Depth == DepthSelect,
		Tier:         r.Tier,
		RuleSet:      agent.RuleSetVersion(),
		Total:        len(cases),
	}
	for _, c := range cases {
		res := r.runCase(ctx, c)
		tally(&rep, c, &res)
		rep.PromptChars += res.PromptChars
		rep.RetryOK += res.RetryOK
		if res.Usage != nil {
			if rep.Usage == nil {
				rep.Usage = &CaseUsage{}
			}
			rep.Usage.add(*res.Usage)
		}
		for k, n := range res.FailureKinds {
			if rep.FailureBreakdown == nil {
				rep.FailureBreakdown = map[string]int{}
			}
			rep.FailureBreakdown[k] += n
		}
		rep.Cases = append(rep.Cases, res)
		if r.OnCase != nil {
			r.OnCase(res)
		}
	}
	if r.Repeat > 1 && len(rep.Cases) > 0 {
		rep.RepeatN = r.Repeat
		for _, cr := range rep.Cases {
			if cr.Stable {
				rep.StableCases++
				continue
			}
			rep.UnstableCases = append(rep.UnstableCases, cr.ID)
		}
		rate := float64(rep.StableCases) / float64(len(rep.Cases))
		rep.Repeatability = &rate
	}
	return rep
}

func tally(rep *Report, c Case, res *CaseResult) {
	if LayerOf(c) == LayerHoldout {
		rep.HoldoutTotal++
		if res.Passed {
			rep.HoldoutPassed++
		}
	}
	if c.Unsolvable && rep.Depth == DepthFull {
		rep.Unsolvable++
		if res.Passed {
			rep.UnsolvableHandled++
		} else {
			rep.Failed++
		}
		rep.Attempted = rep.Total - rep.Unsolvable
		return
	}
	switch {
	case res.Passed:
		rep.Passed++
		if c.KnownIssue {
			rep.Resolved = append(rep.Resolved, c.ID)
		}
	case c.KnownIssue:
		res.Known = true
		rep.Known++
	default:
		rep.Failed++
	}
	rep.Attempted = rep.Total - rep.Unsolvable
}

func (r *Runner) modelName() string {
	if r.Agent == nil || r.Agent.Cfg == nil {
		return "unknown"
	}
	if r.Agent.Cfg.LLM.Provider == "mock" {
		return "mock"
	}
	if m := r.Agent.Cfg.LLM.Model; m != "" {
		return m
	}
	if p := r.Agent.Cfg.LLM.Provider; p != "" {
		return p
	}
	return "unknown"
}

func (r *Runner) runCase(ctx context.Context, c Case) CaseResult {
	n := r.Repeat
	if n < 1 {
		n = 1
	}
	res := r.runCaseOnce(ctx, c)
	if n == 1 {
		res.Runs, res.Agreed, res.Stable = 1, 1, true
		return res
	}
	var total CaseUsage
	var hasUsage bool
	acc := func(u *CaseUsage) {
		if u == nil {
			return
		}
		total.add(*u)
		hasUsage = true
	}
	acc(res.Usage)
	sigs := []string{runSignature(res.Passed, res.Steps, res.Tools)}
	for i := 1; i < n; i++ {
		again := r.runCaseOnce(ctx, c)
		acc(again.Usage)
		sigs = append(sigs, runSignature(again.Passed, again.Steps, again.Tools))
	}
	if hasUsage {
		res.Usage = &total
	}
	_, agree := modalVariant(sigs)
	res.Runs, res.Agreed, res.Stable = n, agree, agree == n
	if !res.Stable {
		res.Variants = describeVariants(sigs)
	}
	return res
}

func (r *Runner) runCaseOnce(ctx context.Context, c Case) CaseResult {
	start := time.Now()
	res := CaseResult{ID: c.ID, Goal: c.Goal, Note: c.Note, Holdout: LayerOf(c) == LayerHoldout}

	cwd := c.CWD
	if cwd == "" {
		cwd = r.CWD
	}
	req := types.GoalRequest{
		Goal:     c.Goal,
		Role:     c.Role,
		TaskMode: types.TaskMode(c.TaskMode),
		Mode:     string(types.ModeAuto),
		Context:  map[string]any{"cwd": cwd},
	}

	to := r.Timeout
	if to <= 0 {
		to = DefaultTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	var obs observation
	switch r.Depth {
	case DepthPlan:
		p := r.plannerFor(req)
		res.Rules = p.ActiveRules()
		res.PromptChars = utf8.RuneCountInString(p.SystemPromptFor(c.Goal, cwd))
		plan, err := p.Plan(cctx, c.Goal, cwd, nil, nil, "", "")
		if err != nil {
			obs.Error = err.Error()
		} else {
			obs.Tools = planTools(plan)
			obs.Steps = len(plan.Steps)
			obs.Acceptance = len(plan.Acceptance)
		}
	case DepthFull:
		p := r.plannerFor(req)
		res.Rules = p.ActiveRules()
		res.PromptChars = utf8.RuneCountInString(p.SystemPromptFor(c.Goal, cwd))
		g := r.Agent.RunGoal(cctx, req)
		obs.Tools = stepTools(g.Steps)
		obs.Steps = len(g.Steps)
		obs.Acceptance = len(g.Acceptance)
		obs.Status = string(g.Status)
		obs.Error = g.Error
		res.Status = string(g.Status)
		res.Score = g.Score
		res.Usage = usageFromTask(g.Usage)
		for k, n := range g.FailureBreakdown {
			if res.FailureKinds == nil {
				res.FailureKinds = map[string]int{}
			}
			res.FailureKinds[string(k)] += n
		}
		for _, st := range g.Steps {
			if st.Retried && st.Status == types.StepSucceeded {
				res.RetryOK++
			}
		}
	default:
		p := r.plannerFor(req)
		res.Rules = p.ActiveRules()
		p.Helper = nil
		res.PromptChars = utf8.RuneCountInString(p.SystemPromptFor(c.Goal, cwd))
		obs.Tools = p.RelevantTools(c.Goal)
	}

	res.Tools = obs.Tools
	res.Steps = obs.Steps
	res.Acceptance = obs.Acceptance
	res.Error = obs.Error
	res.Checks = judge(c, obs, r.Depth)
	res.Passed = allPassed(res.Checks)
	res.DurationMS = time.Since(start).Milliseconds()
	return res
}

func planTools(p types.Plan) []string {
	names := make([]string, 0, len(p.Steps))
	seen := map[string]bool{}
	for _, s := range p.Steps {
		if s.Tool == "" || seen[s.Tool] {
			continue
		}
		seen[s.Tool] = true
		names = append(names, s.Tool)
	}
	return names
}

func stepTools(steps []types.StepResult) []string {
	names := make([]string, 0, len(steps))
	seen := map[string]bool{}
	for _, s := range steps {
		if s.Tool == "" || seen[s.Tool] {
			continue
		}
		seen[s.Tool] = true
		names = append(names, s.Tool)
	}
	return names
}

func allPassed(checks []Check) bool {
	for _, c := range checks {
		if !c.Passed {
			return false
		}
	}
	return true
}
