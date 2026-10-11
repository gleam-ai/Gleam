package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gleam/internal/agent"
	"gleam/internal/atomicfile"
	"gleam/internal/config"
	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/pkg/types"
)

func cmdReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	dataDir := fs.String("data-dir", "", "数据目录")
	rerun := fs.Bool("rerun", false, "用当时的计划真实重跑一遍（会真实调用工具）")
	from := fs.String("from", "", "从这一步开始重跑；之前的步骤沿用当时结果，不会重新执行")
	forkTool := fs.String("tool", "", "分叉：把 --from 指定的那一步换成这个工具")
	forkArgs := fs.String("args", "", "分叉：该步骤的新参数（JSON 对象）")
	diff := fs.Bool("diff", false, "比较两条记录：gleam replay --diff <记录A> <记录B>")
	mockLLM := fs.Bool("mock-llm", false, "使用 Mock 模型（仅真实重跑时有意义）")
	flagArgs, positionals := splitFlagArgs(args, fs)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	rt, err := buildRuntime(*configPath, "", *dataDir, *mockLLM, "", agent.NopNotifier{})
	if err != nil {
		return err
	}
	defer rt.cleanup()

	if *diff {
		if len(positionals) != 2 {
			return fmt.Errorf("用法: gleam replay --diff <记录A> <记录B>")
		}
		a, err := loadRun(rt.cfg.DataDir, positionals[0])
		if err != nil {
			return err
		}
		b, err := loadRun(rt.cfg.DataDir, positionals[1])
		if err != nil {
			return err
		}
		fmt.Print(renderDiff(a.view(), b.view()))
		return nil
	}

	if len(positionals) != 1 {
		return fmt.Errorf("用法: gleam replay <taskID> [--rerun | --from <stepID> [--tool <name>] [--args <json>] | --diff <A> <B>]")
	}
	if *rerun && *from != "" {
		return fmt.Errorf("--rerun（从头原样重跑）与 --from（从某一步继续）只能选一个")
	}
	if (*forkTool != "" || *forkArgs != "") && *from == "" {
		return fmt.Errorf("分叉要指明改哪一步：--from <stepID> --tool <name>（现在只给了 --tool/--args）")
	}

	src, err := loadRun(rt.cfg.DataDir, positionals[0])
	if err != nil {
		return err
	}
	fmt.Print(renderLoaded(src))

	if src.RunLogOnly {
		return fmt.Errorf("这次运行没有跑完（没有终态记录），无法重跑——它只留下了运行中的步骤日志")
	}
	if !*rerun && *from == "" {
		fmt.Println("\n（--rerun 原样重跑；--from <stepID> 从某一步继续；--from <stepID> --tool <name> 换一种走法；--diff <A> <B> 比较两条记录。重跑会真实调用工具，请确认无副作用顾虑后再用）")
		return nil
	}
	if src.Plan() == nil || len(src.Plan().Steps) == 0 {
		return fmt.Errorf("这条任务没有落盘执行计划（ExecutedPlan 为空），无法重跑——它可能是在旧版本下产出的")
	}

	plan := *src.Plan()
	plan.Steps = append([]types.Step(nil), src.Plan().Steps...)
	kind := types.ReplayRerun
	var carried map[string]types.StepResult
	var overrideTool string
	var overrideArgs map[string]any

	if *from != "" {
		idx := -1
		for i, st := range plan.Steps {
			if st.ID == *from {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("计划里没有步骤 %q；可用：%s", *from, strings.Join(stepIDs(plan), "、"))
		}
		kind = types.ReplayResume
		carried = map[string]types.StepResult{}
		for i := 0; i < idx; i++ {
			st := plan.Steps[i]
			r, ok := src.resultOf(st.ID)
			if !ok {
				return fmt.Errorf("步骤 %q 当时没有结果记录，无法沿用；不能从 %q 恢复——"+
					"跳过它意味着把它重新执行一遍（可能有副作用）。请换一个起点，或直接用 --rerun",
					st.ID, *from)
			}
			carried[st.ID] = r
		}
		if *forkTool != "" || *forkArgs != "" {
			kind = types.ReplayFork
			if *forkTool != "" {
				plan.Steps[idx].Tool = *forkTool
				overrideTool = *forkTool
			}
			if *forkArgs != "" {
				args := map[string]any{}
				if err := json.Unmarshal([]byte(*forkArgs), &args); err != nil {
					return fmt.Errorf("--args 不是合法的 JSON 对象: %w", err)
				}
				plan.Steps[idx].Args = args
				overrideArgs = args
			}
		}
		if warn := carriedWarning(carried, plan, *from); warn != "" {
			fmt.Println(warn)
		}
	}

	replayTaskID := "replay-" + src.TaskID()
	executor, params := rerunExecutor(src.Goal(), src.Snapshot(), rt.cfg, rt.reg, rt.gate, rt.cfg.DataDir, replayTaskID)
	executor.Carried = carried

	switch kind {
	case types.ReplayFork:
		fmt.Printf("\n分叉中：从 %s 起重跑，该步换成 %s（真实调用工具）…\n", *from, forkSummary(overrideTool, overrideArgs))
	case types.ReplayResume:
		fmt.Printf("\n恢复中：从 %s 起重跑，前 %d 步沿用当时结果（不会重新执行）…\n", *from, len(carried))
	default:
		fmt.Printf("\n重跑中：%d 个步骤（真实调用工具）…\n", len(plan.Steps))
	}
	fmt.Printf("%s\n", params.Origin())

	start := time.Now()
	exec := executor.Execute(context.Background(), plan, replayTaskID, string(types.ModeAuto), false)
	wall := time.Since(start).Milliseconds()

	rec := replayRecordOf(kind, src.TaskID(), *from, overrideTool, overrideArgs, exec, wall, &params)
	path, saveErr := saveReplay(rt.cfg.DataDir, rec)
	agent.DiscardRunLog(rt.cfg.DataDir, replayTaskID)

	fmt.Print(renderDiff(src.view(), viewOfReplay(rec)))
	if saveErr != nil {
		fmt.Fprintf(os.Stderr, "\n[gleam] 回放记录未能存档：%v（本次结果只在上面，无法与别的记录比较）\n", saveErr)
	} else {
		fmt.Printf("\n本次回放已存档（%s）：gleam replay --diff %s %s 可与原任务逐步对比\n",
			path, src.TaskID(), rec.ReplayID)
	}
	if exec.Failed > 0 {
		return fmt.Errorf("重跑存在失败步骤")
	}
	return nil
}

type loadedRun struct {
	Ref        string
	Task       *types.GoalResult
	Replay     *types.ReplayRecord
	LogSteps   []types.StepResult
	RunLogOnly bool
}

func (l loadedRun) Goal() string {
	switch {
	case l.Task != nil:
		return l.Task.Goal
	case l.Replay != nil:
		return l.Replay.SourceTaskID + " 的回放"
	default:
		return ""
	}
}

func (l loadedRun) Plan() *types.Plan {
	if l.Task != nil {
		return l.Task.ExecutedPlan
	}
	return nil
}

func (l loadedRun) Snapshot() *types.ConfigSnapshot {
	switch {
	case l.Task != nil:
		return l.Task.ConfigSnapshot
	case l.Replay != nil:
		return l.Replay.ConfigSnapshot
	default:
		return nil
	}
}

func (l loadedRun) TaskID() string {
	switch {
	case l.Task != nil:
		return l.Task.TaskID
	case l.Replay != nil:
		return l.Replay.SourceTaskID
	default:
		return l.Ref
	}
}

func (l loadedRun) resultOf(stepID string) (types.StepResult, bool) {
	steps := l.LogSteps
	if l.Task != nil {
		steps = l.Task.Steps
	} else if l.Replay != nil {
		steps = l.Replay.Steps
	}
	for _, r := range steps {
		if r.StepID == stepID {
			return r, true
		}
	}
	return types.StepResult{}, false
}

func (l loadedRun) view() runView {
	switch {
	case l.Task != nil:
		v := runView{
			Ref: l.Task.TaskID, Label: "任务", TaskID: l.Task.TaskID, Goal: l.Task.Goal,
			Status: l.Task.Status, Steps: l.Task.Steps, Plan: l.Task.ExecutedPlan,
			Snapshot: l.Task.ConfigSnapshot, Usage: l.Task.Usage,
			DurationMs: l.Task.Usage.DurationMs, FirstPass: l.Task.FirstPass, Reworks: l.Task.Reworks,
		}
		v.count()
		return v
	case l.Replay != nil:
		return viewOfReplay(l.Replay)
	default:
		v := runView{Ref: l.Ref, Label: "未跑完的运行", RunLogOnly: true, Steps: l.LogSteps}
		v.count()
		return v
	}
}

func loadRun(dataDir, ref string) (loadedRun, error) {
	ref = strings.TrimSuffix(ref, ".json")
	switch {
	case strings.HasPrefix(ref, "task:"):
		return loadRunFrom(dataDir, strings.TrimPrefix(ref, "task:"), "task")
	case strings.HasPrefix(ref, "replay:"):
		return loadRunFrom(dataDir, strings.TrimPrefix(ref, "replay:"), "replay")
	}
	taskPath := agent.TaskArchivePath(dataDir, ref)
	replayPath := replayFile(dataDir, ref)
	taskOK, replayOK := fileExists(taskPath), fileExists(replayPath)
	if taskOK && replayOK {
		return loadedRun{}, fmt.Errorf("%q 在 tasks/ 与 replays/ 里都存在，无法判断读哪一条（用 task:%s 或 replay:%s 指明）", ref, ref, ref)
	}
	if taskOK {
		return loadRunFrom(dataDir, ref, "task")
	}
	if replayOK {
		return loadRunFrom(dataDir, ref, "replay")
	}
	if steps, err := agent.ReadRunLog(dataDir, ref); err == nil && len(steps) > 0 {
		return loadedRun{Ref: ref, LogSteps: steps, RunLogOnly: true}, nil
	}
	return loadedRun{}, fmt.Errorf("读不到记录 %q（数据目录 %s）：tasks/%s.json、replays/%s.json 都不存在，"+
		"运行日志 runs/%s.jsonl 也没有——这条任务可能既没跑完、也没留下运行日志", ref, dataDir, ref, ref, ref)
}

func loadRunFrom(dataDir, id, kind string) (loadedRun, error) {
	if kind != "replay" {
		g, err := agent.ReadTaskResult(dataDir, id)
		if err != nil {
			return loadedRun{}, err
		}
		if g == nil {
			return loadedRun{}, fmt.Errorf("读取%s记录失败: 没有 tasks/%s.json", kind, id)
		}
		return loadedRun{Ref: g.TaskID, Task: g}, nil
	}
	path := replayFile(dataDir, id)
	if path == "" {
		return loadedRun{}, fmt.Errorf("回放 ID %q 不能作文件名", id)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return loadedRun{}, fmt.Errorf("读取%s记录失败: %w", kind, err)
	}
	var r types.ReplayRecord
	if err := json.Unmarshal(b, &r); err != nil {
		return loadedRun{}, fmt.Errorf("解析回放记录失败: %w", err)
	}
	return loadedRun{Ref: r.ReplayID, Replay: &r}, nil
}

func replayFile(dataDir, id string) string {
	if agent.SafeTaskName(id) == "" {
		return ""
	}
	return filepath.Join(dataDir, "replays", id+".json")
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func saveReplay(dataDir string, rec *types.ReplayRecord) (string, error) {
	path := replayFile(dataDir, rec.ReplayID)
	if path == "" {
		return "", fmt.Errorf("回放 ID %q 不能作文件名，未归档", rec.ReplayID)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(rec, "", " ")
	if err != nil {
		return "", err
	}
	if err := atomicfile.Write(path, b, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func replayRecordOf(kind types.ReplayKind, srcTaskID, from, tool string, args map[string]any,
	exec *agent.Result, wallMS int64, params *rerunParams) *types.ReplayRecord {
	rec := &types.ReplayRecord{
		ReplayID:       types.NewID(),
		Kind:           kind,
		SourceTaskID:   srcTaskID,
		FromStep:       from,
		OverrideTool:   tool,
		OverrideArgs:   args,
		CreatedAt:      time.Now(),
		Steps:          exec.Steps,
		DurationMs:     wallMS,
		ConfigSnapshot: params.Snapshot,
	}
	rec.Succeeded, rec.Failed, rec.Skipped, rec.Empty, rec.Carried = countSteps(exec.Steps)
	return rec
}

type runView struct {
	Ref        string
	Label      string
	TaskID     string
	Goal       string
	Status     types.GoalStatus
	Steps      []types.StepResult
	Plan       *types.Plan
	Snapshot   *types.ConfigSnapshot
	Usage      types.TaskUsage
	DurationMs int64
	FirstPass  bool
	Reworks    int
	RunLogOnly bool

	Succeeded, Failed, Skipped, Empty, Carried int
}

func (v *runView) count() {
	v.Succeeded, v.Failed, v.Skipped, v.Empty, v.Carried = countSteps(v.Steps)
}

func countSteps(steps []types.StepResult) (succeeded, failed, skipped, empty, carried int) {
	for _, r := range steps {
		if r.Carried {
			carried++
		}
		switch {
		case r.Status == types.StepSucceeded && r.Outcome == types.OutcomeFailed:
			failed++
		case r.Status == types.StepSucceeded:
			succeeded++
			if r.Outcome == types.OutcomeEmpty {
				empty++
			}
		case r.Status == types.StepFailed:
			failed++
		default:
			skipped++
		}
	}
	return
}

func viewOfReplay(r *types.ReplayRecord) runView {
	v := runView{
		Ref: r.ReplayID, Label: "回放", TaskID: r.SourceTaskID,
		Goal: r.SourceTaskID + " 的回放", Steps: r.Steps,
		Snapshot: r.ConfigSnapshot, DurationMs: r.DurationMs,
	}
	v.count()
	return v
}

func renderLoaded(l loadedRun) string {
	switch {
	case l.Task != nil:
		return renderTrace(l.Task)
	case l.Replay != nil:
		return renderReplayTrace(l.Replay)
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "任务回放  %s\n", l.Ref)
		fmt.Fprintln(&b, "**这次运行没有跑完**：没有终态记录，以下是运行日志里恢复到的步骤。")
		fmt.Fprintln(&b, "没有落盘的计划，所以它无法重跑；能回答的是「当时跑到哪一步、每步什么结果」。")
		fmt.Fprintf(&b, "\n步骤（%d 个）：\n", len(l.LogSteps))
		for _, st := range l.LogSteps {
			fmt.Fprintf(&b, "  %s\n", stepLine(st))
		}
		return b.String()
	}
}

func renderTrace(g *types.GoalResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "任务回放  %s\n", g.TaskID)
	fmt.Fprintf(&b, "目标：%s\n", g.Goal)
	fmt.Fprintf(&b, "trace_id：%s（同输入+同模型重复出现时相同，可据此聚类）\n", g.TraceID)
	fmt.Fprintf(&b, "状态：%s  完成度 %d/100  规则集 %s\n", g.Status, g.Score, g.RuleSet)
	if g.ConfigSnapshot != nil {
		fmt.Fprintf(&b, "配置快照：%s\n", describeSnapshot(g.ConfigSnapshot))
	} else {
		fmt.Fprintf(&b, "配置快照：无（产出于本字段落地之前；--rerun 会回退当前配置，差异不能全归给环境）\n")
	}
	if g.Error != "" {
		fmt.Fprintf(&b, "错误：%s\n", g.Error)
	}

	if bd := g.PromptBreakdown; bd != nil {
		fmt.Fprintf(&b, "\n提示词分段：指令 %d / 能力 %d / 知识 %d / 状态 %d（合计 %d",
			bd.Instruction, bd.Capability, bd.Knowledge, bd.State, bd.Total)
		if bd.Total > 0 {
			fmt.Fprintf(&b, "，状态占比 %.0f%%", bd.StateShare()*100)
		}
		fmt.Fprintln(&b, "）")
	}

	if len(g.Steps) > 0 {
		fmt.Fprintf(&b, "\n步骤（%d 个）：\n", len(g.Steps))
		for _, st := range g.Steps {
			fmt.Fprintf(&b, "  %s\n", stepLine(st))
		}
	}

	if len(g.FailureBreakdown) > 0 {
		fmt.Fprintf(&b, "\n失败归因分布：")
		for k, n := range g.FailureBreakdown {
			fmt.Fprintf(&b, " %s=%d", k, n)
		}
		fmt.Fprintln(&b)
	}

	switch {
	case g.FirstPass:
		fmt.Fprintf(&b, "\n返工：0 轮（一次通过）\n")
	case g.Reworks > 0:
		fmt.Fprintf(&b, "\n返工：%d 轮（非一次通过）\n", g.Reworks)
	}

	u := g.Usage
	fmt.Fprintf(&b, "\n用量：模型调用 %d（token %d/%d）· 工具调用 %d · 重试 %d · 去重 %d · 耗时 %dms\n",
		u.LLMCalls, u.PromptTokens, u.CompletionTokens, u.ToolCalls, u.Retries, u.Deduped, u.DurationMs)

	if g.ExecutedPlan != nil && len(g.ExecutedPlan.Steps) > 0 {
		fmt.Fprintf(&b, "\n当时的计划（%d 步，可 --rerun 原样重跑）：\n", len(g.ExecutedPlan.Steps))
		for _, st := range g.ExecutedPlan.Steps {
			fmt.Fprintf(&b, "  %s → %s %s\n", st.ID, st.Tool, st.Describe())
		}
	}
	return b.String()
}

func renderReplayTrace(r *types.ReplayRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "回放记录  %s（%s）\n", r.ReplayID, replayKindLabel(r.Kind))
	fmt.Fprintf(&b, "源自任务：%s\n", r.SourceTaskID)
	if r.FromStep != "" {
		fmt.Fprintf(&b, "起点：%s 起（之前的步骤沿用当时结果，没有重新执行）\n", r.FromStep)
	}
	if r.OverrideTool != "" || len(r.OverrideArgs) > 0 {
		fmt.Fprintf(&b, "改动：%s\n", forkSummary(r.OverrideTool, r.OverrideArgs))
	}
	fmt.Fprintf(&b, "时间：%s\n", r.CreatedAt.Format("2006-01-02 15:04:05"))
	if r.ConfigSnapshot != nil {
		fmt.Fprintf(&b, "配置快照：%s\n", describeSnapshot(r.ConfigSnapshot))
	} else {
		fmt.Fprintf(&b, "配置快照：无（这次回放没有记录配置）\n")
	}
	s, f, sk, e, c := countSteps(r.Steps)
	fmt.Fprintf(&b, "\n结果：成功 %d / 失败 %d / 跳过 %d / 空结果 %d（共 %d 步）", s, f, sk, e, len(r.Steps))
	if c > 0 {
		fmt.Fprintf(&b, "，其中 %d 步沿用（这次没有执行）", c)
	}
	fmt.Fprintf(&b, "\n耗时：%dms\n", r.DurationMs)
	if len(r.Steps) > 0 {
		fmt.Fprintf(&b, "\n步骤（%d 个）：\n", len(r.Steps))
		for _, st := range r.Steps {
			fmt.Fprintf(&b, "  %s\n", stepLine(st))
		}
	}
	return b.String()
}

func stepLine(st types.StepResult) string {
	line := fmt.Sprintf("[%s] %s (%s)", st.Status, st.StepID, st.Tool)
	if st.Outcome != "" && st.Outcome != types.OutcomeOK {
		line += fmt.Sprintf(" outcome=%s", st.Outcome)
	}
	if st.Retried {
		line += fmt.Sprintf(" 第 %d 次尝试", st.Attempt)
	}
	if st.Carried {
		line += " 沿用（未执行）"
	}
	if st.Deduped {
		line += " 复用"
	}
	if st.Error != "" {
		line += "\n        " + types.Shorten(st.Error, 160)
		if st.ErrorKind != "" {
			line += fmt.Sprintf("（%s）", st.ErrorKind)
		}
	}
	return line
}

func replayKindLabel(k types.ReplayKind) string {
	switch k {
	case types.ReplayResume:
		return "恢复"
	case types.ReplayFork:
		return "分叉"
	default:
		return "原样重跑"
	}
}

func forkSummary(tool string, args map[string]any) string {
	parts := []string{}
	if tool != "" {
		parts = append(parts, "工具="+tool)
	}
	if len(args) > 0 {
		if b, err := json.Marshal(args); err == nil {
			parts = append(parts, "参数="+string(b))
		}
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, " · ")
}

func renderDiff(a, b runView) string {
	var out strings.Builder
	fmt.Fprintf(&out, "\n对比（%s → %s）：\n", describeView(a), describeView(b))
	if a.Status != "" || b.Status != "" {
		fmt.Fprintf(&out, "  状态：  %s → %s\n", orDash(string(a.Status)), orDash(string(b.Status)))
	}
	fmt.Fprintf(&out, "  结果：  成功 %d / 失败 %d / 跳过 %d / 空结果 %d（共 %d 步） → 成功 %d / 失败 %d / 跳过 %d / 空结果 %d（共 %d 步）\n",
		a.Succeeded, a.Failed, a.Skipped, a.Empty, len(a.Steps),
		b.Succeeded, b.Failed, b.Skipped, b.Empty, len(b.Steps))
	if a.Carried > 0 || b.Carried > 0 {
		fmt.Fprintf(&out, "  沿用：  %d 步 → %d 步（沿用=这次没有执行，结果取自上一次）\n", a.Carried, b.Carried)
	}
	fmt.Fprintf(&out, "  重试：  %d → %d 个步骤经历重试\n", retriedCount(a.Steps), retriedCount(b.Steps))
	fmt.Fprintf(&out, "  耗时：  %dms → %dms\n", a.DurationMs, b.DurationMs)

	fmt.Fprintf(&out, "  逐步：\n")
	old := map[string]types.StepResult{}
	for _, st := range a.Steps {
		old[st.StepID] = st
	}
	seen := map[string]bool{}
	for _, st := range b.Steps {
		seen[st.StepID] = true
		o, ok := old[st.StepID]
		if !ok {
			fmt.Fprintf(&out, "    %s：A 里不存在此步骤结果\n", st.StepID)
			continue
		}
		same := o.Status == st.Status && o.Outcome == st.Outcome && o.Tool == st.Tool
		mark := "一致"
		if !same {
			if o.Tool != st.Tool {
				mark = fmt.Sprintf("换工具（%s → %s）", o.Tool, st.Tool)
			} else {
				mark = fmt.Sprintf("变化（%s → %s）", orOK(o.Status, o.Outcome), orOK(st.Status, st.Outcome))
			}
		}
		fmt.Fprintf(&out, "    %s (%s)：%s", st.StepID, st.Tool, mark)
		if st.Carried {
			fmt.Fprintf(&out, " [沿用]")
		}
		if !same && st.Error != "" {
			fmt.Fprintf(&out, "：%s", types.Shorten(st.Error, 120))
		}
		fmt.Fprintln(&out)
	}
	for _, st := range a.Steps {
		if !seen[st.StepID] {
			fmt.Fprintf(&out, "    %s：B 里不存在此步骤结果（当时 %s）\n", st.StepID, orOK(st.Status, st.Outcome))
		}
	}
	return out.String()
}

func describeView(v runView) string {
	label := v.Label
	if label == "" {
		label = "记录"
	}
	return fmt.Sprintf("%s %s", label, v.Ref)
}

func orOK(st types.StepStatus, oc types.StepOutcome) string {
	if oc == "" {
		return string(st)
	}
	return string(st) + "/" + string(oc)
}

func retriedCount(steps []types.StepResult) int {
	n := 0
	for _, st := range steps {
		if st.Retried {
			n++
		}
	}
	return n
}

func stepIDs(plan types.Plan) []string {
	out := make([]string, 0, len(plan.Steps))
	for _, st := range plan.Steps {
		out = append(out, st.ID)
	}
	return out
}

func carriedWarning(carried map[string]types.StepResult, plan types.Plan, target string) string {
	var bad []string
	for _, st := range plan.Steps {
		if st.ID == target {
			break
		}
		r, ok := carried[st.ID]
		if !ok {
			continue
		}
		if r.Status != types.StepSucceeded || r.Outcome == types.OutcomeFailed {
			bad = append(bad, st.ID)
		}
	}
	if len(bad) == 0 {
		return ""
	}
	sort.Strings(bad)
	return fmt.Sprintf("提醒：沿用下来的步骤 %s 当时没有成功，依赖它们的步骤这次仍会被跳过——"+
		"这是当时的前提，不是恢复失败。要改这个前提，请把起点提前到那一步。", strings.Join(bad, "、"))
}

func rerunExecutor(goal string, snap *types.ConfigSnapshot, live *config.Config, reg *registry.Registry,
	gate *safety.Gate, dataDir, runID string) (*agent.Executor, rerunParams) {
	params := resolveRerunParams(snap, live)
	e := &agent.Executor{
		Reg:            reg,
		Gate:           gate,
		Notifier:       agent.NopNotifier{},
		MaxConcurrency: params.MaxConcurrency,
		StepTimeout:    params.StepTimeout,
		StepRetries:    params.StepRetries,
		Dedupe:         params.Dedupe,
		MaxOutputRunes: params.MaxOutputRunes,
		Goal:           goal,
	}
	if dataDir != "" {
		e.Sink = agent.NewRunLog(dataDir)
	}
	return e, params
}

type rerunParams struct {
	MaxConcurrency int
	StepTimeout    time.Duration
	StepRetries    int
	Dedupe         bool
	MaxOutputRunes int
	Snapshot       *types.ConfigSnapshot
	FromSnapshot   bool
}

func resolveRerunParams(snap *types.ConfigSnapshot, live *config.Config) rerunParams {
	if snap != nil {
		return rerunParams{
			MaxConcurrency: snap.MaxConcurrency,
			StepTimeout:    time.Duration(snap.StepTimeoutSecs) * time.Second,
			StepRetries:    snap.StepRetries,
			Dedupe:         snap.DedupeCalls,
			MaxOutputRunes: snap.MaxOutputRunes,
			Snapshot:       snap,
			FromSnapshot:   true,
		}
	}
	if live == nil {
		return rerunParams{}
	}
	fallback := live.Snapshot()
	return rerunParams{
		MaxConcurrency: live.Agent.MaxConcurrency,
		StepTimeout:    time.Duration(live.Agent.StepTimeoutSecs) * time.Second,
		StepRetries:    live.Agent.StepRetries,
		Dedupe:         live.Agent.DedupeCalls,
		MaxOutputRunes: live.Agent.MaxOutputRunes,
		Snapshot:       &fallback,
	}
}

func (p rerunParams) Origin() string {
	if p.FromSnapshot {
		return "参数取自任务启动时的快照（本次只动\"环境\"这一个变量）"
	}
	return "参数回退当前配置（该任务没有快照）——差异里混着配置变化，不能全归给环境"
}

func describeSnapshot(s *types.ConfigSnapshot) string {
	model := s.Model
	if model == "" {
		model = "未指定"
	}
	if s.Provider != "" {
		model = s.Provider + "/" + model
	}
	parts := []string{"模型 " + model}
	if s.FastModel != "" {
		parts = append(parts, "快模型 "+s.FastModel)
	}
	if len(s.Tiers) > 0 {
		keys := make([]string, 0, len(s.Tiers))
		for k := range s.Tiers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			pairs = append(pairs, k+"="+s.Tiers[k])
		}
		parts = append(parts, "档位 "+strings.Join(pairs, "，"))
	}
	if s.SafetyMode != "" {
		parts = append(parts, "安全模式 "+s.SafetyMode)
	}
	parts = append(parts,
		fmt.Sprintf("完成阈值 %d", s.DoneThreshold),
		fmt.Sprintf("最大重规划 %d", s.MaxReplans),
		fmt.Sprintf("最大步数 %d", s.MaxSteps),
	)
	parts = append(parts,
		fmt.Sprintf("并发 %d", s.MaxConcurrency),
		"单步超时 "+limitLabel(s.StepTimeoutSecs, "s"),
		fmt.Sprintf("单步重试 %d", s.StepRetries),
		"去重 "+onOff(s.DedupeCalls),
		"输出预算 "+limitLabel(s.MaxOutputRunes, " 字符"),
		"工具菜单 "+limitLabel(s.MaxToolSchemas, " 个"),
		"逐步反思 "+onOff(s.ReflectEachStep),
	)
	return strings.Join(parts, " · ")
}

func onOff(v bool) string {
	if v {
		return "开"
	}
	return "关"
}

func limitLabel(n int, unit string) string {
	if n <= 0 {
		return "不限"
	}
	return fmt.Sprintf("%d%s", n, unit)
}
