package agent

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/config"
	"gleam/internal/harness/conversation"
	"gleam/internal/harness/growth"
	"gleam/internal/harness/memory"
	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/harness/scheduler"
	"gleam/internal/harness/skill"
	"gleam/internal/harness/space"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

type bareTool struct{ funcTool }

func (b *bareTool) Schema() map[string]any { return nil }

type testNotifier struct{}

func (testNotifier) OnProgress(types.ProgressEvent) {}
func (testNotifier) OnApproval(types.ApprovalRequest) types.ApprovalResponse {
	return types.ApprovalResponse{Approved: true}
}
func (testNotifier) OnSuggestion(string, string)             {}
func (testNotifier) OnSuggestSkill(string, types.SkillDraft) {}
func (testNotifier) OnTaskDone(types.TaskDoneEvent)          {}

func readyItem(t *testing.T, rep ReadinessReport, key string) ReadinessItem {
	t.Helper()
	for _, it := range rep.Items {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("体检报告里没有 %q 这一项", key)
	return ReadinessItem{}
}

func fullKitAgent(t *testing.T) *Agent {
	t.Helper()
	dir := t.TempDir()

	reg := registry.New()
	reg.MustRegister(&funcTool{name: "file.read", perm: types.PermissionReadOnly})
	reg.MustRegister(&funcTool{name: "file.write", perm: types.PermissionUserApproved})
	reg.MustRegister(&funcTool{name: "shell.exec", perm: types.PermissionFullAccess})
	reg.MustRegister(&funcTool{name: "reply", perm: types.PermissionReadOnly})

	gate := safety.New("auto", nil, nil, []string{dir}, time.Minute)
	mem, err := memory.Open(dir, 8, 64, 100)
	if err != nil {
		t.Fatalf("memory.Open: %v", err)
	}
	skills, err := skill.Open(filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("skill.Open: %v", err)
	}
	if _, err := skills.Save(skill.Skill{
		Name: "整理表格", Description: "把 CSV 汇总成表",
		Steps: []types.Step{{ID: "s1", Tool: "file.read", Args: map[string]any{"path": "a.csv"}}},
	}); err != nil {
		t.Fatalf("skills.Save: %v", err)
	}
	sched, err := scheduler.Open(filepath.Join(dir, "schedule.json"), func(scheduler.Job) {})
	if err != nil {
		t.Fatalf("scheduler.Open: %v", err)
	}
	grow, err := growth.Open(dir)
	if err != nil {
		t.Fatalf("growth.Open: %v", err)
	}
	grow.Record(growth.Entry{Type: "task_completed", Goal: "整理销售数据", Score: 88, Steps: 3, LLMCalls: 4})
	convos, err := conversation.Open(dir)
	if err != nil {
		t.Fatalf("conversation.Open: %v", err)
	}
	spaces, err := space.Open(dir, dir)
	if err != nil {
		t.Fatalf("space.Open: %v", err)
	}

	cfg := config.Default()
	cfg.Workspace = dir
	cfg.LLM.Tiers = map[string]string{"coding": "m-coder", "office": "m-office"}

	return &Agent{
		LLM: llm.NewMock(), Reg: reg, Mem: mem, Gate: gate,
		Skills: skills, Sched: sched, Growth: grow, Convos: convos, Spaces: spaces,
		Cfg: cfg, Notifier: testNotifier{},
	}
}

func TestReadiness_NineItemsInArticleOrder(t *testing.T) {
	a := &Agent{Cfg: config.Default()}
	rep := a.Readiness()

	if rep.Total != 9 || len(rep.Items) != 9 {
		t.Fatalf("应有 9 项，实际 total=%d items=%d", rep.Total, len(rep.Items))
	}
	wantKeys := []string{
		"runtime", "delivery", "model_lock", "audit_split", "practice_reuse",
		"business_gap", "cache_control", "iteration_arch", "extensible_memory",
	}
	for i, it := range rep.Items {
		if it.Index != i+1 {
			t.Errorf("第 %d 项编号应为 %d，实际 %d", i+1, i+1, it.Index)
		}
		if it.Key != wantKeys[i] {
			t.Errorf("第 %d 项 key 应为 %q，实际 %q", i+1, wantKeys[i], it.Key)
		}
		if it.Title == "" {
			t.Errorf("第 %d 项缺少标题", i+1)
		}
		if it.Summary == "" {
			t.Errorf("第 %d 项缺少结论描述", i+1)
		}
		if it.Evidence == nil {
			t.Errorf("第 %d 项的证据应为空数组而不是 null", i+1)
		}
		if it.Status != ReadyPass && it.Fix == "" {
			t.Errorf("第 %d 项不是 pass 却没有给补齐建议：%s", i+1, it.Summary)
		}
	}
	if rep.Passed+rep.Warned+rep.Failed != 9 {
		t.Errorf("计数不自洽：%d+%d+%d != 9", rep.Passed, rep.Warned, rep.Failed)
	}
	if !strings.Contains(rep.SummaryLine(), "9") {
		t.Errorf("一行摘要应含总数，实际：%s", rep.SummaryLine())
	}
}

func TestReadiness_FullKitIsReady(t *testing.T) {
	a := fullKitAgent(t)
	rep := a.Readiness()

	if rep.Verdict != "ready" {
		for _, it := range rep.Items {
			if it.Status != ReadyPass {
				t.Errorf("第 %d 项 %s 未通过：%s（%v）", it.Index, it.Title, it.Summary, it.Evidence)
			}
		}
		t.Fatalf("应判定 ready，实际 %s", rep.Verdict)
	}
	if rep.Passed != 9 {
		t.Errorf("应 9 项全过，实际 %d", rep.Passed)
	}
}

func TestReadiness_BareAgentIsNotReady(t *testing.T) {
	a := &Agent{Cfg: config.Default()}
	rep := a.Readiness()

	if rep.Verdict != "not_ready" {
		t.Fatalf("空壳 Agent 应判定 not_ready，实际 %s", rep.Verdict)
	}
	for _, key := range []string{"runtime", "delivery", "model_lock", "audit_split", "cache_control", "iteration_arch", "extensible_memory"} {
		if got := readyItem(t, rep, key).Status; got != ReadyFail {
			t.Errorf("%s 应为 fail，实际 %s", key, got)
		}
	}
}

func TestReadiness_NoSchemaToolFailsKernel(t *testing.T) {
	reg := registry.New()
	reg.MustRegister(&funcTool{name: "file.read", perm: types.PermissionReadOnly})
	reg.MustRegister(&bareTool{funcTool{name: "mystery", perm: types.PermissionReadOnly}})

	a := &Agent{
		LLM: llm.NewMock(), Reg: reg,
		Gate: safety.New("auto", nil, nil, nil, time.Minute),
		Cfg:  config.Default(),
	}
	it := readyItem(t, a.Readiness(), "runtime")
	if it.Status != ReadyFail {
		t.Fatalf("有裸工具时应 fail，实际 %s：%s", it.Status, it.Summary)
	}
	if !strings.Contains(it.Summary, "1") {
		t.Errorf("结论应点出数量，实际：%s", it.Summary)
	}
	if it.Fix == "" {
		t.Error("fail 项必须给补齐建议")
	}
}

func TestReadiness_ReadonlyOnlyWarnsDelivery(t *testing.T) {
	reg := registry.New()
	reg.MustRegister(&funcTool{name: "file.read", perm: types.PermissionReadOnly})
	reg.MustRegister(&funcTool{name: "web.fetch", perm: types.PermissionReadOnly})

	a := &Agent{
		LLM: llm.NewMock(), Reg: reg,
		Gate: safety.New("auto", nil, nil, nil, time.Minute),
		Cfg:  config.Default(),
	}
	it := readyItem(t, a.Readiness(), "delivery")
	if it.Status != ReadyWarn {
		t.Fatalf("只有只读工具应 warn，实际 %s：%s", it.Status, it.Summary)
	}
}

func TestReadiness_SingleTierWarnsModelLock(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Tiers = nil
	a := &Agent{LLM: llm.NewMock(), Cfg: cfg}

	if got := readyItem(t, a.Readiness(), "model_lock").Status; got != ReadyWarn {
		t.Errorf("未配档位应 warn，实际 %s", got)
	}

	cfg.LLM.Tiers = map[string]string{"coding": "m1", "office": "m2", "reasoning": "m3"}
	if got := readyItem(t, a.Readiness(), "model_lock").Status; got != ReadyPass {
		t.Errorf("配了两档以上应 pass，实际 %s", got)
	}
}

func TestReadiness_HighRiskWithoutApprovalChannelWarns(t *testing.T) {
	reg := registry.New()
	reg.MustRegister(&funcTool{name: "file.read", perm: types.PermissionReadOnly})
	reg.MustRegister(&funcTool{name: "shell.exec", perm: types.PermissionFullAccess})

	base := func(n Notifier) *Agent {
		return &Agent{
			LLM: llm.NewMock(), Reg: reg,
			Gate: safety.New("auto", nil, nil, nil, time.Minute),
			Cfg:  config.Default(), Notifier: n,
		}
	}
	it := readyItem(t, base(NopNotifier{}).Readiness(), "audit_split")
	if it.Status != ReadyWarn {
		t.Fatalf("高风险工具 + 无审批通道应 warn，实际 %s：%s", it.Status, it.Summary)
	}
	if !strings.Contains(it.Summary, "审批") {
		t.Errorf("结论应点出审批通道问题，实际：%s", it.Summary)
	}

	if got := readyItem(t, base(testNotifier{}).Readiness(), "audit_split").Status; got != ReadyPass {
		t.Errorf("接了审批通道应 pass，实际 %s", got)
	}
}

func TestReadiness_NoSkillsWarnsPracticeReuse(t *testing.T) {
	dir := t.TempDir()
	skills, err := skill.Open(filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("skill.Open: %v", err)
	}
	a := &Agent{LLM: llm.NewMock(), Skills: skills, Cfg: config.Default()}

	it := readyItem(t, a.Readiness(), "practice_reuse")
	if it.Status != ReadyWarn {
		t.Fatalf("没有技能应 warn，实际 %s：%s", it.Status, it.Summary)
	}
	if !strings.Contains(it.Summary, "技能") {
		t.Errorf("结论应点出技能，实际：%s", it.Summary)
	}
}

func TestReadiness_UnsetWorkspaceWarnsBusinessGap(t *testing.T) {
	cfg := config.Default()
	cfg.Workspace = ""
	a := &Agent{LLM: llm.NewMock(), Cfg: cfg}

	it := readyItem(t, a.Readiness(), "business_gap")
	if it.Status != ReadyWarn {
		t.Fatalf("工作区未设置应 warn，实际 %s：%s", it.Status, it.Summary)
	}

	cfg.Workspace = "D:/proj"
	if got := readyItem(t, a.Readiness(), "business_gap").Status; got != ReadyPass {
		t.Errorf("工作区已设置应 pass，实际 %s", got)
	}
}

func TestReadiness_CacheUnmeasurableIsNotMisattributed(t *testing.T) {
	a := &Agent{Cfg: config.Default(), LLM: llm.NewMock()}
	it := readyItem(t, a.Readiness(), "cache_control")

	if it.Status != ReadyFail {
		t.Fatalf("无法实测应 fail，实际 %s", it.Status)
	}
	if !strings.Contains(it.Summary, "无法实测") {
		t.Errorf("结论应说明是「测不了」，实际：%s", it.Summary)
	}
	if strings.Contains(it.Summary, "布局退化") {
		t.Errorf("不该把「测不了」归因成「布局退化」，实际：%s", it.Summary)
	}
	if it.Fix == "" {
		t.Error("fail 项必须给补齐建议")
	}
}

func TestReadiness_CacheLayoutMeasured(t *testing.T) {
	cfg := config.Default()
	cfg.Workspace = "D:/ws"
	a := &Agent{LLM: llm.NewMock(), Reg: bigRegistry(25), Cfg: cfg}

	stable, shortest, lenA, lenB, layoutOK := a.measurePromptPrefix()
	if lenA == 0 || lenB == 0 {
		t.Fatal("实测样本为空，说明提示词构建没跑起来")
	}
	if !layoutOK {
		t.Error("稳定段应完整落在公共前缀之内")
	}
	if stable < llm.CacheFriendlyMinChars {
		t.Errorf("公共前缀只有 %d 字，低于值得缓存的量级（%d）", stable, llm.CacheFriendlyMinChars)
	}
	if ratio := float64(stable) / float64(shortest); ratio < cacheCoverageWarn {
		t.Errorf("公共前缀占比 %.2f，低于 warn 阈值 %.2f", ratio, cacheCoverageWarn)
	}

	it := readyItem(t, a.Readiness(), "cache_control")
	if it.Status != ReadyPass {
		t.Fatalf("当前布局应 pass，实际 %s：%s", it.Status, it.Summary)
	}
	if !strings.Contains(it.Summary, "%") {
		t.Errorf("结论应给出实测占比，实际：%s", it.Summary)
	}
}

func TestReadiness_CacheLayoutDetectsBreak(t *testing.T) {
	brokenA := "你是助手。\n\n## 上下文\n- 工作目录: D:/ws\n\n## 能力菜单\n- file.read\n\n## 输出格式\n只输出 JSON。"
	brokenB := "你是助手。\n\n## 上下文\n- 工作目录: D:/ws/sub\n\n## 能力菜单\n- file.read\n\n## 输出格式\n只输出 JSON。"

	prefix := brokenA[:llm.StablePrefixLen(brokenA, brokenB)]
	if layoutCoversStable(prefix, brokenA) {
		t.Fatal("稳定段被易变的工作目录截断，布局校验应判定为不通过")
	}
	if got := cacheStatus(false, 5000, 0.9); got != ReadyFail {
		t.Errorf("布局坏掉时应 fail，实际 %s", got)
	}
}

func TestCacheStatus_Precedence(t *testing.T) {
	big := llm.CacheFriendlyMinChars * 3
	cases := []struct {
		name     string
		layoutOK bool
		stable   int
		ratio    float64
		want     ReadinessStatus
	}{
		{"布局坏掉，其余再好也 fail", false, big, 0.99, ReadyFail},
		{"布局坏掉且占比极低", false, 10, 0.01, ReadyFail},
		{"稳定段太小，占比再高也 warn", true, 100, 1.0, ReadyWarn},
		{"稳定段够大但覆盖不足", true, big, 0.1, ReadyWarn},
		{"稳定段够大且覆盖足够", true, big, 0.25, ReadyPass},
		{"真实场景：21 个工具、布局正确、覆盖 46%", true, 2448, 0.46, ReadyPass},
		{"理想情况", true, big, 0.9, ReadyPass},
	}
	for _, c := range cases {
		if got := cacheStatus(c.layoutOK, c.stable, c.ratio); got != c.want {
			t.Errorf("%s：cacheStatus(%v,%d,%.2f) = %s，期望 %s",
				c.name, c.layoutOK, c.stable, c.ratio, got, c.want)
		}
	}
}

func TestReadiness_IsReadOnlyAndIdempotent(t *testing.T) {
	a := fullKitAgent(t)
	before := *a.Cfg
	beforeTiers := len(a.Cfg.LLM.Tiers)

	first := a.Readiness()
	second := a.Readiness()

	if first.Verdict != second.Verdict || first.Passed != second.Passed {
		t.Errorf("两次体检结果不一致：%s/%d vs %s/%d", first.Verdict, first.Passed, second.Verdict, second.Passed)
	}
	if len(a.Cfg.LLM.Tiers) != beforeTiers {
		t.Error("体检修改了模型档位配置")
	}
	if a.Cfg.Workspace != before.Workspace || a.Cfg.Agent.ContextCompress != before.Agent.ContextCompress {
		t.Error("体检修改了配置")
	}
	if first.GeneratedAt.IsZero() {
		t.Error("报告应带生成时间")
	}
}

func TestReadiness_AggregateUsage(t *testing.T) {
	a := fullKitAgent(t)
	a.initUsage("t1")
	a.addUsage("t1", llm.Usage{PromptTokens: 1000, CompletionTokens: 50, CachedTokens: 800})
	a.initUsage("t2")
	a.addUsage("t2", llm.Usage{PromptTokens: 500, CompletionTokens: 20})

	u := a.aggregateUsage()
	if u.LLMCalls != 2 || u.PromptTokens != 1500 || u.CachedTokens != 800 || u.CachedCalls != 1 {
		t.Fatalf("用量汇总不对：%+v", u)
	}

	it := readyItem(t, a.Readiness(), "cache_control")
	joined := strings.Join(it.Evidence, "\n")
	if !strings.Contains(joined, "累计缓存命中") {
		t.Errorf("证据应包含缓存命中统计，实际：%v", it.Evidence)
	}
	if !strings.Contains(joined, "1 / 2") {
		t.Errorf("证据应给出命中次数/调用次数，实际：%v", it.Evidence)
	}
}

func TestReadiness_UsageNoCacheReportedIsNotALie(t *testing.T) {
	a := fullKitAgent(t)
	a.initUsage("t1")
	a.addUsage("t1", llm.Usage{PromptTokens: 1000, CompletionTokens: 50})

	joined := strings.Join(readyItem(t, a.Readiness(), "cache_control").Evidence, "\n")
	if !strings.Contains(joined, "未报告") {
		t.Errorf("无缓存数据时应说明未报告，实际：%s", joined)
	}
	if strings.Contains(joined, "命中率 0%") {
		t.Error("不该编造 0% 命中率")
	}
}

func TestReadiness_DoesNotTouchNetworkOrDisk(t *testing.T) {
	a := &Agent{Cfg: config.Default(), LLM: llm.NewMock()}
	done := make(chan ReadinessReport, 1)
	go func() { done <- a.Readiness() }()
	select {
	case rep := <-done:
		if rep.Total != 9 {
			t.Errorf("应返回 9 项，实际 %d", rep.Total)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("体检超时：说明它在做网络或磁盘等待，而它应该是纯内存的")
	}
}
