package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"gleam/internal/agent"
	"gleam/internal/eval"
)

func cmdEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	workspace := fs.String("workspace", "", "工作区根目录")
	dataDir := fs.String("data-dir", "", "数据目录")
	mockLLM := fs.Bool("mock-llm", false, "使用 Mock 模型")
	mockScript := fs.String("mock-script", "", "Mock 响应脚本 JSON 路径")
	depth := fs.String("depth", string(eval.DepthSelect), "评测深度 select|plan|full")
	tier := fs.String("tier", "", "模拟生效模型档位（只改提示词构建，不切换模型）")
	casesPath := fs.String("cases", "", "自定义用例集 JSON（默认用内置用例）")
	layer := fs.String("layer", "", "只跑指定分层的用例 smoke|regression|edge|adversarial|holdout（默认全部）")
	emitCase := fs.String("emit-case", "", "把指定 taskID 的失败任务转成用例草稿（badcase 回流；读取 <data-dir>/tasks/<taskID>.json）")
	asJSON := fs.Bool("json", false, "输出结构化 JSON 报告")
	repeat := fs.Int("repeat", 1, "每条用例重跑遍数（>1 时额外报出可重复性：通过率回答「对不对」，可重复性回答「稳不稳」）")
	save := fs.String("save", "", "把本次报告写成基线文件")
	baseline := fs.String("baseline", "", "与基线文件对比并报出回归")
	strict := fs.Bool("strict", false, "有回归则以非零退出（未给基线时：有用例未过即非零）")
	flagArgs, positionals := splitFlagArgs(args, fs)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positionals) > 0 {
		return fmt.Errorf("eval 不接受位置参数 %q", positionals[0])
	}
	if *repeat < 1 {
		return fmt.Errorf("--repeat 至少为 1（当前 %d）", *repeat)
	}

	d := eval.Depth(*depth)
	if !eval.ValidDepth(d) {
		return fmt.Errorf("未知深度 %q（可选 select|plan|full）", *depth)
	}

	var tempDirs []string
	if d == eval.DepthFull {
		if *workspace == "" {
			dir, err := os.MkdirTemp("", "gleam-eval-ws-")
			if err != nil {
				return fmt.Errorf("创建临时工作区失败: %w", err)
			}
			*workspace, tempDirs = dir, append(tempDirs, dir)
		}
		if *dataDir == "" {
			dir, err := os.MkdirTemp("", "gleam-eval-data-")
			if err != nil {
				return fmt.Errorf("创建临时数据目录失败: %w", err)
			}
			*dataDir, tempDirs = dir, append(tempDirs, dir)
		}
	}
	defer func() {
		for _, dir := range tempDirs {
			_ = os.RemoveAll(dir)
		}
	}()

	cases, err := loadEvalCases(*casesPath)
	if err != nil {
		return err
	}

	rt, err := buildRuntime(*configPath, *workspace, *dataDir, *mockLLM, *mockScript, agent.NopNotifier{})
	if err != nil {
		return err
	}
	defer rt.cleanup()

	if *emitCase != "" {
		return emitCaseDraft(rt.cfg.DataDir, *emitCase)
	}

	cases = eval.FilterLayer(cases, eval.Layer(*layer))
	if len(cases) == 0 {
		return fmt.Errorf("分层 %q 下没有用例（内置用例分层：smoke / regression / holdout）", *layer)
	}

	if d != eval.DepthSelect && !*asJSON {
		if rt.cfg.LLM.Provider == "mock" {
			fmt.Fprintln(os.Stderr, "[gleam] 注意：当前是 Mock 模型，plan/full 深度只能验证管道通畅，验证不了质量。")
		} else if rt.cfg.LLM.APIKey == "" {
			fmt.Fprintln(os.Stderr, "[gleam] 注意：没有可用的模型 API Key，plan/full 深度的用例会因调用失败而未过。")
		}
	}

	runner := &eval.Runner{
		Agent:  rt.agent,
		Depth:  d,
		CWD:    *workspace,
		Tier:   *tier,
		Repeat: *repeat,
		OnCase: func(res eval.CaseResult) {
			if *asJSON {
				return
			}
			if res.Holdout {
				return
			}
			mark := evalMark(res)
			if !res.Stable {
				mark += " [不稳]"
			}
			fmt.Fprintf(os.Stderr, "  %s %s\n", mark, res.ID)
		},
	}
	rep := runner.Run(context.Background(), cases)

	if *save != "" {
		if err := eval.SaveBaseline(*save, rep); err != nil {
			return err
		}
		if !*asJSON {
			fmt.Fprintf(os.Stderr, "[gleam] 已写入基线 %s\n", *save)
		}
	}

	hasBaseline := *baseline != ""
	if hasBaseline {
		base, err := eval.LoadBaseline(*baseline)
		if err != nil {
			return fmt.Errorf("读取基线失败: %w", err)
		}
		diff, err := eval.Compare(rep, base)
		if err != nil {
			return err
		}
		rep.Diff = diff
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	} else {
		fmt.Print(renderEvalReport(rep, len(cases)))
	}

	return evalGateErr(rep, *strict, hasBaseline)
}

func loadEvalCases(path string) ([]eval.Case, error) {
	if path != "" {
		return eval.LoadCases(path)
	}
	return eval.BuiltinCases()
}

func emitCaseDraft(dataDir, taskID string) error {
	path := agent.TaskArchivePath(dataDir, taskID)
	if path == "" {
		return fmt.Errorf("非法任务 ID: %q（含分隔符或过长——这种名字既不会写出去，也读不回来）", taskID)
	}
	g, err := agent.ReadTaskResult(dataDir, taskID)
	if err != nil {
		return err
	}
	if g == nil {
		return fmt.Errorf("读取任务记录失败: 没有 %s（任务归档由 gleam goal / serve 自动写入）", path)
	}

	draft := eval.Case{
		ID:       "bc-" + taskID,
		Goal:     g.Goal,
		Role:     "",
		TaskMode: "",
		Note: fmt.Sprintf(
			"badcase 回流自任务 %s（状态 %s，trace %s）。【先回答】：换更强模型这个问题会不会消失？会→研究问题，不进回归集；不会→工程问题，补齐期望后入库。期望行为写成可判定的结构（要什么/不要什么），不要写期望文本。",
			taskID, g.Status, g.TraceID),
	}
	seen := map[string]bool{}
	for _, st := range g.Steps {
		if st.Tool == "" || seen[st.Tool] {
			continue
		}
		seen[st.Tool] = true
		draft.WantTools = append(draft.WantTools, st.Tool)
	}

	out, err := json.MarshalIndent(map[string]any{
		"note":       "用例草稿——期望字段补齐、删掉本说明后并入用例集",
		"case":       draft,
		"task_id":    taskID,
		"status":     string(g.Status),
		"error":      g.Error,
		"trace_id":   g.TraceID,
		"ruleset":    g.RuleSet,
		"steps_hint": "原始步骤与失败归因见 " + path + "（gleam replay " + taskID + " 可回放）",
	}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	fmt.Fprintf(os.Stderr, "[gleam] 已生成用例草稿 bc-%s：补齐期望后并入用例集；只有工程问题进回归集。\n", taskID)
	return nil
}

func evalGateErr(rep eval.Report, strict, hasBaseline bool) error {
	if !strict {
		return nil
	}
	if hasBaseline {
		if rep.Diff.Regressed() {
			return fmt.Errorf("评测出现回归 %d 项：%s（详见上方对比；确认是有意为之可去掉 --strict）",
				len(rep.Diff.Broke), strings.Join(rep.Diff.Broke, "、"))
		}
		return nil
	}
	if rep.Failed > 0 {
		return fmt.Errorf("评测 %d 项未过（%d/%d 通过）；想只卡回归请加 --baseline 指定基线文件",
			rep.Failed, rep.Passed, rep.Total)
	}
	return nil
}

func renderEvalReport(rep eval.Report, total int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "提示词与行为回归评测  %s 深度  %s\n",
		rep.Depth, rep.GeneratedAt.Format("2006-01-02 15:04"))

	model := rep.Model
	if rep.Depth == eval.DepthSelect && rep.LocalSelect {
		model += "（本地关键词筛选，未调用模型）"
	}
	fmt.Fprintf(&b, "用例 %d 条  模型 %s\n", total, model)
	if rep.Tier != "" {
		fmt.Fprintf(&b, "模拟生效档位：%s（只改提示词构建，不切换模型）\n", rep.Tier)
	}
	fmt.Fprintln(&b)

	for _, c := range rep.Cases {
		if c.Holdout {
			continue
		}
		mark := evalMark(c)
		if !c.Stable {
			mark += " [不稳]"
		}
		fmt.Fprintf(&b, "  %s %s\n", mark, c.ID)
		if !c.Stable {
			fmt.Fprintf(&b, "          重跑 %d 遍得到 %d 种结果：%s\n",
				c.Runs, len(c.Variants), strings.Join(c.Variants, "；"))
		}
		if c.Passed {
			continue
		}
		fmt.Fprintf(&b, "          目标：%s\n", c.Goal)
		for _, ck := range c.Checks {
			if ck.Passed {
				continue
			}
			line := "          · " + ck.Name
			if ck.Detail != "" {
				line += " —— " + ck.Detail
			}
			fmt.Fprintln(&b, line)
		}
		if c.Error != "" {
			fmt.Fprintf(&b, "          · 执行错误：%s\n", c.Error)
		}
		if c.Note != "" {
			fmt.Fprintf(&b, "          （这条在守什么：%s）\n", c.Note)
		}
	}

	fmt.Fprintf(&b, "\n合计：%d 通过 / %d 未过", rep.Passed, rep.Failed)
	if rep.Known > 0 {
		fmt.Fprintf(&b, " / %d 已知问题", rep.Known)
	}
	fmt.Fprintf(&b, "（提示词合计 %d 字符）\n", rep.PromptChars)
	switch {
	case rep.Usage != nil:
		fmt.Fprintf(&b, "用量：%d 次模型调用，%d token（输入 %d / 输出 %d",
			rep.Usage.LLMCalls, rep.Usage.TotalTokens(), rep.Usage.PromptTokens, rep.Usage.CompletionTokens)
		if rep.Usage.CachedTokens > 0 {
			fmt.Fprintf(&b, "，缓存命中 %d", rep.Usage.CachedTokens)
		}
		fmt.Fprintf(&b, "），%d 次工具调用", rep.Usage.ToolCalls)
		if rep.Usage.EstimatedCalls > 0 {
			fmt.Fprintf(&b, "；其中 %d 次调用的 token 为估算值", rep.Usage.EstimatedCalls)
		}
		fmt.Fprintln(&b)
		if rep.RepeatN > 1 {
			fmt.Fprintf(&b, "  含 --repeat %d 的每一遍：重跑也真的调了模型，所以一并计入\n", rep.RepeatN)
		}
	case rep.Depth == eval.DepthPlan:
		fmt.Fprintln(&b, "用量：未计量（plan 深度的规划调用不经过 RunGoal，没有归集到用例上）")
	}
	if rep.HoldoutTotal > 0 {
		fmt.Fprintf(&b, "保留池：%d/%d 通过（%d 条，单条结果不展示——防止调参过拟合到看得见的用例）\n",
			rep.HoldoutPassed, rep.HoldoutTotal, rep.HoldoutTotal)
	}
	if rep.Repeatability != nil {
		fmt.Fprintf(&b, "可重复性：%.0f%%（%d/%d 条用例重跑 %d 遍结果完全一致）\n",
			*rep.Repeatability*100, rep.StableCases, rep.Total, rep.RepeatN)
		if len(rep.UnstableCases) > 0 {
			fmt.Fprintf(&b, "  ⚠ 不稳的用例：%s\n", strings.Join(rep.UnstableCases, "、"))
			fmt.Fprintln(&b, "  不稳要么是断言太细（该放宽），要么是提示词有歧义（该修）；"+
				"在它变稳之前，这条用例的绿不能当作改动有效的证据。")
		} else {
			fmt.Fprintln(&b, "  通过率回答「对不对」，可重复性回答「稳不稳」——两者都达标，结论才可归因。")
		}
	}
	if rep.Unsolvable > 0 {
		rate := 0.0
		if rep.Attempted > 0 {
			rate = float64(rep.Passed) / float64(rep.Attempted) * 100
		}
		urate := 0.0
		if rep.Unsolvable > 0 {
			urate = float64(rep.UnsolvableHandled) / float64(rep.Unsolvable) * 100
		}
		fmt.Fprintf(&b, "口径：可解样本通过率 %.0f%%（%d/%d，分母不含 %d 条不可解用例）；不可解处理率 %.0f%%（%d/%d，识别为不可解、不硬猜）\n",
			rate, rep.Passed, rep.Attempted, rep.Unsolvable, urate, rep.UnsolvableHandled, rep.Unsolvable)
	}
	if len(rep.FailureBreakdown) > 0 {
		fmt.Fprintf(&b, "失败归因分布（过程失败，含被重规划救回的）：")
		kinds := make([]string, 0, len(rep.FailureBreakdown))
		for k := range rep.FailureBreakdown {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			fmt.Fprintf(&b, " %s=%d", k, rep.FailureBreakdown[k])
		}
		fmt.Fprintln(&b)
	}
	if rep.RetryOK > 0 {
		fmt.Fprintf(&b, "重试后才成功的步骤：%d 个（这些不是\"一次就对\"，是隐患位置）\n", rep.RetryOK)
	}

	if len(rep.Resolved) > 0 {
		fmt.Fprintf(&b, "\n✓ 下列用例标着「已知问题」但本次通过了，根因多半已修，可以摘掉标记：%s\n",
			strings.Join(rep.Resolved, "、"))
	}

	if rep.Diff != nil {
		fmt.Fprintf(&b, "\n与基线（%s）对比：\n", rep.Diff.BaselineAt.Format("2006-01-02 15:04"))
		for _, line := range rep.Diff.Describe() {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	return b.String()
}

func evalMark(c eval.CaseResult) string {
	switch {
	case c.Passed:
		return "[通过]"
	case c.Known:
		return "[已知]"
	default:
		return "[未过]"
	}
}
