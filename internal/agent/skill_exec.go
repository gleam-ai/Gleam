package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gleam/internal/harness/growth"
	"gleam/internal/harness/skill"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

// RunSkill 执行已固化的技能（一键复用）。
func (a *Agent) RunSkill(ctx context.Context, name string, params map[string]string) (map[string]any, error) {
	if a.Skills == nil {
		return nil, fmt.Errorf("技能系统未启用")
	}
	sk, err := a.Skills.Get(name)
	if err != nil {
		return nil, err
	}
	if sk.Disabled {
		// 停用的技能连参数都留着，就是要"先别跑但别忘"。让它跑等于把停掉的东西重新挂上。
		return nil, fmt.Errorf("技能 %q 已停用，请先在技能页启用它", name)
	}
	plan := types.Plan{Goal: "运行技能 " + name}
	for i, st := range sk.Steps {
		args, err := skill.SubstituteParams(st.Args, params)
		if err != nil {
			return nil, fmt.Errorf("技能参数替换失败（步骤 %s）: %w", st.ID, err)
		}
		plan.Steps = append(plan.Steps, types.Step{ID: st.ID, Description: st.Description, Tool: st.Tool, Args: args, DependsOn: st.DependsOn})
		_ = i
	}
	taskID := types.NewID()
	a.Notifier.OnProgress(types.ProgressEvent{TaskID: taskID, Phase: "execute", Message: fmt.Sprintf("运行技能 %s v%d", name, sk.Version), Progress: 10, Kind: "info"})
	executor := &Executor{
		Reg: a.Reg, Gate: a.Gate, Notifier: a.Notifier,
		MaxConcurrency: a.Cfg.Agent.MaxConcurrency,
		StepTimeout:    time.Duration(a.Cfg.Agent.StepTimeoutSecs) * time.Second,
		StepRetries:    a.Cfg.Agent.StepRetries,
		Dedupe:         a.Cfg.Agent.DedupeCalls,
		MaxOutputRunes: a.Cfg.Agent.MaxOutputRunes,
		DataDir:        a.Cfg.DataDir,
		OnUsage:        a.BumpUsage,
	}
	exec := executor.Execute(ctx, plan, taskID, string(types.ModeAuto), false)
	ok := exec.Failed == 0 && exec.Succeeded > 0
	if err := a.Skills.RecordRun(name, ok); err != nil {
		// 统计写不进去 = 面板上的次数与成功率偏少，而没有任何东西会报这个错。必须说出来。
		a.Notifier.OnProgress(types.ProgressEvent{
			TaskID: taskID, Phase: "reflect",
			Message:  fmt.Sprintf("技能 %s 的运行统计未写入（次数与成功率会偏少）: %v", name, err),
			Progress: 95, Kind: "warn",
		})
	}
	// 成长日志：只记跑通的复用。跑坏一次也算 5 分，等级就成了「用了多少次技能」而不是
	// 「攒下多少能用的做法」——那正是这套体系要回答的反面。
	if ok && a.Growth != nil {
		a.Growth.Record(growth.Entry{
			Type: "skill_used", SkillName: name, Goal: "复用技能 " + name,
			Steps: exec.Succeeded,
		})
	}
	if !ok && !exec.Cancelled {
		a.autoOptimizeSkill(ctx, taskID, name, sk, exec)
	}
	// 技能计划目前不带验收标准（上面只填了 Steps），所以这里 hasChecks 恒为 false。
	// 仍按契约传进去：万一以后技能也声明验收标准，这条路径不该悄悄退回"全成功即完成"。
	result := a.buildResult("技能 "+name, exec, heuristicReflection(exec, "", len(plan.Acceptance) > 0), plan.Acceptance, types.ContextBreakdown{})
	result.TaskID = taskID
	result.StartedAt = time.Now()
	result.FinishedAt = time.Now()
	result.Status = statusOfExec(exec)
	if result.Summary == "" {
		result.Summary = fmt.Sprintf("技能 %s 执行完成（成功 %d/%d）", name, exec.Succeeded, exec.Total)
	}
	return map[string]any{
		"task_id": taskID,
		"status":  string(result.Status),
		"score":   result.Score,
		"summary": result.Summary,
		"steps":   result.Steps,
	}, nil
}

// autoOptimizeSkill 技能运行失败后的自动参数优化（设计文档 §4.2.2）：
// 把失败摘要交给 LLM 修订步骤，经规划校验后保存为新版本。Mock 模型下跳过，保证离线自测确定性。
func (a *Agent) autoOptimizeSkill(ctx context.Context, taskID, name string, sk *skill.Skill, exec *Result) {
	if a.LLM == nil || llm.IsMockClient(a.LLM) || !a.Cfg.Agent.SkillAutoOptimize {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## 技能 %s（当前 v%d）运行失败\n目标：%s\n\n## 现有步骤\n", name, sk.Version, sk.Description)
	for _, st := range sk.Steps {
		args, _ := json.Marshal(st.Args)
		fmt.Fprintf(&b, "- {\"id\":%q,\"description\":%q,\"tool\":%q,\"args\":%s,\"depends_on\":%s}\n",
			st.ID, st.Description, st.Tool, args, stepDepsJSON(st.DependsOn))
	}
	fmt.Fprintf(&b, "\n## 失败详情\n")
	for _, r := range exec.Steps {
		if r.Status == types.StepFailed {
			fmt.Fprintf(&b, "- [%s] %s: %s\n", r.StepID, r.Tool, types.Shorten(r.Error, 160))
		}
	}
	sys := strings.Join([]string{
		"你是 Gleam 的技能优化器。" + llm.MarkerSkillFix,
		"",
		b.String(),
		`
## 输出（只输出严格 JSON，不要 markdown 代码块）
{"steps":[{"id":"s1","description":"一句话说明","tool":"工具名","args":{},"depends_on":[]}]}

针对失败原因修订：调整参数取值、修正步骤引用（"$ref:s2"、"{ref:s2}"）、或删除多余步骤。
只能使用原技能中出现过的工具，保持技能语义不变。`,
	}, "\n")
	req := llm.ChatRequest{System: sys, Messages: []llm.Message{{Role: llm.RoleUser, Content: "请输出修订后的步骤。"}}, Temperature: 0.2, TaskID: taskID}
	text, err := a.HelperLLM().Chat(ctx, req)
	if err != nil {
		return
	}
	raw, err := extractJSON(text)
	if err != nil {
		return
	}
	validator := &Planner{LLM: a.LLM, Reg: a.Reg, MaxSteps: a.Cfg.Agent.MaxSteps}
	plan, err := validator.validate(raw, "技能 "+name)
	if err != nil || len(plan.Steps) == 0 {
		return
	}
	optimized := *sk
	optimized.Steps = plan.Steps
	if v, err := a.SkillSave(optimized); err == nil {
		a.Notifier.OnProgress(types.ProgressEvent{
			TaskID: taskID, Phase: "reflect",
			Message:  fmt.Sprintf("技能 %s 运行失败，已自动优化参数并保存为 v%d，可重试", name, v),
			Progress: 90, Kind: "warn",
		})
	}
}

// stepDepsJSON 步骤依赖列表的 JSON 表示（用于优化器提示词）。
func stepDepsJSON(deps []string) string {
	if len(deps) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(deps)
	return string(b)
}

// delegateTimeout 委派调用的硬超时：子任务应该聚焦，60 秒足够。
const delegateTimeout = 60 * time.Second

// Delegate 把子目标交给独立 LLM 调用处理，不污染主对话上下文。
// role 为空时使用通用助手角色；非空时注入对应角色的系统提示词。
func (a *Agent) Delegate(ctx context.Context, goal string, role string) (string, error) {
	dctx, cancel := context.WithTimeout(ctx, delegateTimeout)
	defer cancel()

	sys := "你是一个专注的子任务执行者。根据用户目标，直接给出完整、可独立使用的结果。\n" +
		"要求：\n" +
		"- 结果自包含，不依赖主对话上下文\n" +
		"- 如果目标涉及代码，给出可直接运行的代码\n" +
		"- 如果目标涉及分析，给出结论和关键数据\n" +
		"- 不要解释你在做什么，直接给结果"

	if role != "" && role != "general" {
		if r := FindRole(role); r.ID != "general" {
			sys = r.SystemPrompt + "\n\n" + sys
		}
	}

	req := llm.ChatRequest{
		System:      sys,
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: goal}},
		Temperature: 0.3,
		MaxTokens:   2000,
	}
	client := a.LLM
	if a.FastLLM != nil {
		client = a.FastLLM
	}
	text, err := client.Chat(dctx, req)
	if err != nil {
		return "", fmt.Errorf("委派调用失败: %w", err)
	}
	return text, nil
}
