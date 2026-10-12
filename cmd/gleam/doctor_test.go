package main

import (
	"strings"
	"testing"

	"gleam/internal/agent"
	"gleam/internal/harness/growth"
)

func report(passed, warned, failed int, items ...agent.ReadinessItem) agent.ReadinessReport {
	return agent.ReadinessReport{
		Total: passed + warned + failed, Passed: passed, Warned: warned, Failed: failed,
		Items: items,
	}
}

func TestStrictErr_OnlyFailsBlock(t *testing.T) {
	pass := agent.ReadinessItem{Index: 1, Title: "Runtime 重复建设", Status: agent.ReadyPass}
	warn := agent.ReadinessItem{Index: 3, Title: "模型与能力锁定", Status: agent.ReadyWarn}
	fail := agent.ReadinessItem{Index: 7, Title: "模型调用缓存不可控", Status: agent.ReadyFail}

	cases := []struct {
		name   string
		rep    agent.ReadinessReport
		strict bool
		want   bool
	}{
		{"不开启 strict 永不报错", report(1, 1, 1, pass, warn, fail), false, false},
		{"全部通过", report(3, 0, 0, pass), true, false},
		{"只有待改进：放行", report(2, 1, 0, pass, warn), true, false},
		{"有不合格：拦住", report(2, 0, 1, pass, fail), true, true},
	}
	for _, c := range cases {
		err := strictErr(c.rep, c.strict)
		if (err != nil) != c.want {
			t.Errorf("%s：strictErr = %v，期望报错=%v", c.name, err, c.want)
		}
	}
}

func TestStrictErr_MessageIsActionable(t *testing.T) {
	rep := report(1, 0, 2,
		agent.ReadinessItem{Index: 1, Title: "Runtime 重复建设", Status: agent.ReadyFail},
		agent.ReadinessItem{Index: 7, Title: "模型调用缓存不可控", Status: agent.ReadyFail},
	)
	err := strictErr(rep, true)
	if err == nil {
		t.Fatal("应报错")
	}
	msg := err.Error()
	for _, want := range []string{"2", "1. Runtime 重复建设", "7. 模型调用缓存不可控", "--strict"} {
		if !strings.Contains(msg, want) {
			t.Errorf("报错信息应包含 %q，实际：%s", want, msg)
		}
	}
}

func TestUserSignalReport_DeliveryLine(t *testing.T) {
	out := userSignalReport(growth.Stats{
		TotalTasks: 6, AbortedTasks: 1, RetryCount: 1,
		RetryRate: 1.0 / 7, AbortRate: 1.0 / 7,
		FirstPassCount: 4, FirstPassBase: 6, FirstPassRate: 4.0 / 6,
		ReworksTotal: 3,
	})
	for _, want := range []string{
		"分母 = 完成 6 + 中断 1",
		"首次验收通过率 67%（4/6，仅工作模式）",
		"累计返工 3 轮",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q，实际：\n%s", want, out)
		}
	}
}

func TestUserSignalReport_NoDeliveryBase(t *testing.T) {
	out := userSignalReport(growth.Stats{TotalTasks: 5, AbortRate: 0})
	if strings.Contains(out, "首次验收通过率") {
		t.Errorf("没有记过交付口径的任务时不该报通过率（0/0 不是 0%%），实际：\n%s", out)
	}
	if !strings.Contains(out, "用户重试率") {
		t.Errorf("用户信号应照常报，实际：\n%s", out)
	}
}

func TestUserSignalReport_SmallSampleIsQuiet(t *testing.T) {
	if out := userSignalReport(growth.Stats{TotalTasks: 2, AbortedTasks: 1}); out != "" {
		t.Errorf("样本不足时不该报比率，实际输出：%q", out)
	}
}
