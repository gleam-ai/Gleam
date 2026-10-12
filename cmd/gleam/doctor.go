package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"gleam/internal/agent"
	"gleam/internal/harness/growth"
)

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	workspace := fs.String("workspace", "", "工作区根目录")
	dataDir := fs.String("data-dir", "", "数据目录")
	mockLLM := fs.Bool("mock-llm", false, "使用 Mock 模型")
	mockScript := fs.String("mock-script", "", "Mock 响应脚本 JSON 路径")
	asJSON := fs.Bool("json", false, "输出结构化 JSON 报告")
	strict := fs.Bool("strict", false, "存在不合格项时以非零退出（供 CI/上线前门禁使用）")
	flagArgs, positionals := splitFlagArgs(args, fs)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positionals) > 0 {
		return fmt.Errorf("doctor 不接受位置参数 %q", positionals[0])
	}

	rt, err := buildRuntime(*configPath, *workspace, *dataDir, *mockLLM, *mockScript, newConsoleNotifier())
	if err != nil {
		return err
	}
	defer rt.cleanup()

	rep := rt.agent.Readiness()
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
		return strictErr(rep, *strict)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "就绪体检（九坑自检）  %s\n\n", rep.GeneratedAt.Format("2006-01-02 15:04"))

	for _, it := range rep.Items {
		fmt.Fprintf(&b, "  %s %d. %s\n", readinessMark(it.Status), it.Index, it.Title)
		fmt.Fprintf(&b, "          %s\n", it.Summary)
		for _, ev := range it.Evidence {
			fmt.Fprintf(&b, "          · %s\n", ev)
		}
		if it.Fix != "" {
			fmt.Fprintf(&b, "          → %s\n", it.Fix)
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintf(&b, "合计：%d 通过 / %d 待改进 / %d 不合格 —— %s\n",
		rep.Passed, rep.Warned, rep.Failed, rep.Verdict)

	fmt.Fprint(&b, stateReport(rt.cfg.DataDir))

	if g := rt.agent.Growth; g != nil {
		fmt.Fprint(&b, userSignalReport(g.Stats()))
	}

	var todo []string
	for _, it := range rep.Items {
		if it.Status != agent.ReadyPass && it.Fix != "" {
			todo = append(todo, fmt.Sprintf("    %s %d. %s → %s", readinessMark(it.Status), it.Index, it.Title, it.Fix))
		}
	}
	if len(todo) > 0 {
		fmt.Fprintf(&b, "\n待处理：\n%s\n", strings.Join(todo, "\n"))
	}

	fmt.Print(b.String())
	return strictErr(rep, *strict)
}

const userSignalMinSample = 5

func userSignalReport(st growth.Stats) string {
	if st.TotalTasks+st.AbortedTasks < userSignalMinSample {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n用户信号（口径：分母 = 完成 %d + 中断 %d）\n", st.TotalTasks, st.AbortedTasks)
	fmt.Fprintf(&b, "  用户重试率 %.0f%%（%d 次）——重试的任务质量打折，原始 trace 可用 gleam eval --emit-case 回流\n",
		st.RetryRate*100, st.RetryCount)
	fmt.Fprintf(&b, "  用户中断率 %.0f%%（%d 次）——中断要修的是等待体验或能力缺口，与失败不同因\n",
		st.AbortRate*100, st.AbortedTasks)
	if st.FirstPassBase > 0 {
		fmt.Fprintf(&b, "  首次验收通过率 %.0f%%（%d/%d，仅工作模式）——返工一次才成也是成功，但不是一次就合格\n",
			st.FirstPassRate*100, st.FirstPassCount, st.FirstPassBase)
		fmt.Fprintf(&b, "  累计返工 %d 轮（含中断前已发生的）——重试三次才凑出来与一次做对，成功率上一样、成本上两回事\n",
			st.ReworksTotal)
	}
	return b.String()
}

func strictErr(rep agent.ReadinessReport, strict bool) error {
	if !strict || rep.Failed == 0 {
		return nil
	}
	var bad []string
	for _, it := range rep.Items {
		if it.Status == agent.ReadyFail {
			bad = append(bad, fmt.Sprintf("%d. %s", it.Index, it.Title))
		}
	}
	return fmt.Errorf("体检不合格 %d 项：%s（去掉 --strict 可只看报告）", rep.Failed, strings.Join(bad, "；"))
}

func readinessMark(s agent.ReadinessStatus) string {
	switch s {
	case agent.ReadyPass:
		return "[通过]  "
	case agent.ReadyWarn:
		return "[待改进]"
	default:
		return "[不合格]"
	}
}
