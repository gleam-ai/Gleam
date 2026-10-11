package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/config"
	"gleam/pkg/types"
)

func TestRenderTrace_DeliveryLine(t *testing.T) {
	cases := []struct {
		name string
		res  types.GoalResult
		want string
	}{
		{"一次通过", types.GoalResult{Status: types.GoalSuccess, FirstPass: true}, "返工：0 轮（一次通过）"},
		{"返工两轮", types.GoalResult{Status: types.GoalSuccess, Reworks: 2}, "返工：2 轮（非一次通过）"},
		{"没走到验收", types.GoalResult{Status: types.GoalCancelled}, ""},
	}
	for _, c := range cases {
		out := renderTrace(&c.res)
		if c.want == "" {
			if strings.Contains(out, "返工：") {
				t.Errorf("%s：不该印返工行，实际：\n%s", c.name, out)
			}
			continue
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("%s：输出应包含 %q，实际：\n%s", c.name, c.want, out)
		}
	}
}

func TestRenderTrace_ConfigSnapshotLine(t *testing.T) {
	withSnap := types.GoalResult{
		Status: types.GoalSuccess,
		ConfigSnapshot: &types.ConfigSnapshot{
			Provider: "openai", Model: "gpt-x", MaxConcurrency: 4,
			StepTimeoutSecs: 45, DedupeCalls: true, MaxOutputRunes: 6000,
		},
	}
	out := renderTrace(&withSnap)
	for _, want := range []string{"配置快照：", "openai/gpt-x", "并发 4", "单步超时 45s", "去重 开", "输出预算 6000 字符"} {
		if !strings.Contains(out, want) {
			t.Errorf("快照行缺少 %q，实际：\n%s", want, out)
		}
	}

	noSnap := types.GoalResult{Status: types.GoalSuccess}
	out = renderTrace(&noSnap)
	if !strings.Contains(out, "配置快照：无") {
		t.Errorf("没有快照时必须如实说明，实际：\n%s", out)
	}
	if !strings.Contains(out, "回退当前配置") {
		t.Errorf("没有快照时要讲清 --rerun 会回退当前配置，否则差异会被误读，实际：\n%s", out)
	}
}

func TestResolveRerunParams_PrefersSnapshotOverLive(t *testing.T) {
	snap := &types.ConfigSnapshot{
		MaxConcurrency: 2, StepTimeoutSecs: 11, StepRetries: 3,
		DedupeCalls: true, MaxOutputRunes: 700,
	}
	live := config.Default()
	live.Agent.MaxConcurrency = 64
	live.Agent.StepTimeoutSecs = 999
	live.Agent.StepRetries = 9
	live.Agent.DedupeCalls = false
	live.Agent.MaxOutputRunes = 1

	p := resolveRerunParams(snap, live)
	if !p.FromSnapshot {
		t.Error("有快照时 FromSnapshot 应为 true——否则输出里会说成回退，读的人会以为这次不是复现")
	}
	if p.MaxConcurrency != 2 || p.StepTimeout != 11*time.Second || p.StepRetries != 3 || !p.Dedupe || p.MaxOutputRunes != 700 {
		t.Errorf("没用快照的参数: %+v", p)
	}
	if !strings.Contains(p.Origin(), "快照") {
		t.Errorf("来源说明应点明取自快照，实际 %q", p.Origin())
	}
}

func TestResolveRerunParams_FallsBackToLiveConfig(t *testing.T) {
	live := config.Default()
	live.Agent.MaxConcurrency = 64
	live.Agent.StepTimeoutSecs = 999
	live.Agent.StepRetries = 9
	live.Agent.DedupeCalls = false
	live.Agent.MaxOutputRunes = 1

	p := resolveRerunParams(nil, live)
	if p.FromSnapshot {
		t.Error("没有快照时 FromSnapshot 应为 false")
	}
	if p.MaxConcurrency != 64 || p.StepTimeout != 999*time.Second || p.StepRetries != 9 || p.Dedupe || p.MaxOutputRunes != 1 {
		t.Errorf("回退值不对: %+v", p)
	}
	origin := p.Origin()
	if !strings.Contains(origin, "回退") {
		t.Errorf("来源说明必须点明是回退，实际 %q", origin)
	}
	if !strings.Contains(origin, "不能全归给环境") {
		t.Errorf("来源说明要点明后果（差异里混着配置变化），实际 %q", origin)
	}
}

func TestResolveRerunParams_NilBoth(t *testing.T) {
	p := resolveRerunParams(nil, nil)
	if p.FromSnapshot || p.MaxConcurrency != 0 || p.StepTimeout != 0 {
		t.Errorf("应得零值参数，实际 %+v", p)
	}
}

func TestRerunExecutor_TakesParamsFromSnapshot(t *testing.T) {
	g := &types.GoalResult{
		Goal: "当时的任务",
		ConfigSnapshot: &types.ConfigSnapshot{
			MaxConcurrency: 2, StepTimeoutSecs: 11, StepRetries: 3,
			DedupeCalls: true, MaxOutputRunes: 700,
		},
	}
	live := config.Default()
	live.Agent.MaxConcurrency = 64
	live.Agent.StepTimeoutSecs = 999
	live.Agent.StepRetries = 9
	live.Agent.DedupeCalls = false
	live.Agent.MaxOutputRunes = 1

	ex, params := rerunExecutor(g.Goal, g.ConfigSnapshot, live, nil, nil, "", "")
	if !params.FromSnapshot {
		t.Error("有快照时来源应记成快照")
	}
	if ex.MaxConcurrency != 2 || ex.StepTimeout != 11*time.Second || ex.StepRetries != 3 || !ex.Dedupe || ex.MaxOutputRunes != 700 {
		t.Errorf("执行器没用快照的参数: %+v", ex)
	}
	if ex.Goal != "当时的任务" {
		t.Errorf("目标应原样带过去，实际 %q", ex.Goal)
	}

	ex2, params2 := rerunExecutor("老任务", nil, live, nil, nil, "", "")
	if params2.FromSnapshot {
		t.Error("没有快照时应记成回退")
	}
	if ex2.MaxConcurrency != 64 || ex2.StepTimeout != 999*time.Second {
		t.Errorf("老任务应回退活配置: %+v", ex2)
	}
}

func TestRerunExecutor_WiresRunLogSink(t *testing.T) {
	dir := t.TempDir()
	ex, _ := rerunExecutor("任务", nil, config.Default(), nil, nil, dir, "replay-t1")
	if ex.Sink == nil {
		t.Fatal("回放执行器必须接上运行日志，否则中途退出什么都查不到")
	}
	ex.Sink.RecordStep("replay-t1", types.StepResult{StepID: "s1", Status: types.StepSucceeded, Outcome: types.OutcomeOK})
	if _, err := os.Stat(filepath.Join(dir, "runs", "replay-t1.jsonl")); err != nil {
		t.Errorf("运行日志应写在 runs/replay-t1.jsonl（与源任务分开）: %v", err)
	}
	if ex2, _ := rerunExecutor("任务", nil, config.Default(), nil, nil, "", "x"); ex2.Sink != nil {
		t.Error("没有数据目录时不该接运行日志")
	}
}

func TestDescribeSnapshot_LimitLabelsAndStableOrder(t *testing.T) {
	s := &types.ConfigSnapshot{
		MaxOutputRunes: 0, MaxToolSchemas: 0, StepTimeoutSecs: 0,
		Tiers: map[string]string{"aa": "1", "bb": "2", "cc": "3", "dd": "4", "ee": "5"},
	}
	out := describeSnapshot(s)
	for _, want := range []string{"单步超时 不限", "输出预算 不限", "工具菜单 不限"} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q，实际：%s", want, out)
		}
	}
	if strings.Contains(out, "输出预算 0") {
		t.Errorf("0 表示不限，不能印成 0：%s", out)
	}

	if strings.Index(out, "aa=") > strings.Index(out, "ee=") {
		t.Errorf("档位应按名字升序，实际：%s", out)
	}

	first := describeSnapshot(s)
	for i := 0; i < 20; i++ {
		if got := describeSnapshot(s); got != first {
			t.Fatalf("同一份快照印出了不同顺序（第 %d 次）——档位必须排序：\n%s\n%s", i, first, got)
		}
	}
}
