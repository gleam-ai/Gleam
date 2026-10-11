package eval

import (
	"context"
	"strings"
	"testing"

	"gleam/internal/agent"
	"gleam/internal/config"
	"gleam/internal/harness/registry"
	"gleam/internal/llm"
	"gleam/internal/tools/std"
)

type planLLM struct {
	vary bool
	n    int
}

func (m *planLLM) Chat(_ context.Context, req llm.ChatRequest) (string, error) {
	if llm.KindOf(req.System) != "plan" {
		return `{"score":85,"verdict":"done","reason":"ok"}`, nil
	}
	m.n++
	if m.vary && m.n%2 == 0 {
		return `{"steps":[{"id":"s1","tool":"reply","args":{"text":"a"}}],"estimated_time":"short"}`, nil
	}
	return `{"steps":[{"id":"s1","tool":"reply","args":{"text":"a"}},` +
		`{"id":"s2","tool":"reply","args":{"text":"b"}}],"estimated_time":"short"}`, nil
}

func (m *planLLM) ChatStream(ctx context.Context, req llm.ChatRequest, _ func(string)) (string, error) {
	return m.Chat(ctx, req)
}

func (m *planLLM) Name() string { return "plan-stub" }

func planDepthRunner(t *testing.T, client llm.Client, repeat int) *Runner {
	t.Helper()
	cfg := config.Default()
	cfg.LLM.Provider = "mock"
	reg := registry.New()
	reg.MustRegister(std.NewReply())
	a := agent.New(cfg, client, reg, nil, nil, nil, nil, nil)
	return &Runner{Agent: a, Depth: DepthPlan, CWD: t.TempDir(), Repeat: repeat}
}

func TestRepeatFlagsUnstableCase(t *testing.T) {
	r := planDepthRunner(t, &planLLM{vary: true}, 3)
	rep := r.Run(context.Background(), []Case{{ID: "u1", Goal: "随便做点什么"}})

	if rep.RepeatN != 3 {
		t.Fatalf("报告应记下重跑遍数，实际 %d", rep.RepeatN)
	}
	c := rep.Cases[0]
	if c.Runs != 3 {
		t.Fatalf("用例应记下重跑遍数，实际 %d", c.Runs)
	}
	if c.Stable {
		t.Fatal("三次运行步数不同，不能判为稳定")
	}
	if c.Agreed != 2 {
		t.Errorf("应记下与最常见结果一致的遍数 2，实际 %d", c.Agreed)
	}
	if len(c.Variants) != 2 {
		t.Fatalf("应报出 2 种结果（2 步 ×2、1 步 ×1），实际 %v", c.Variants)
	}
	if !strings.Contains(c.Variants[0], "×2") {
		t.Errorf("次数多的应排前面，实际 %v", c.Variants)
	}
	if rep.Repeatability == nil || *rep.Repeatability != 0 {
		t.Errorf("这条用例不稳，一致率应为 0，实际 %v", rep.Repeatability)
	}
	if len(rep.UnstableCases) != 1 || rep.UnstableCases[0] != "u1" {
		t.Errorf("应报出不稳的用例 ID，实际 %v", rep.UnstableCases)
	}
	if !strings.Contains(rep.SummaryLine(), "可重复性") {
		t.Errorf("一行摘要里也要有可重复性，实际 %q", rep.SummaryLine())
	}
}

func TestRepeatStableCase(t *testing.T) {
	r := planDepthRunner(t, &planLLM{}, 3)
	rep := r.Run(context.Background(), []Case{{ID: "s1", Goal: "随便做点什么"}})

	c := rep.Cases[0]
	if !c.Stable || c.Agreed != 3 {
		t.Errorf("结果一致时应 Stable/Agreed=3，实际 %+v", c)
	}
	if len(c.Variants) != 0 {
		t.Errorf("稳定用例不该报差异明细，实际 %v", c.Variants)
	}
	if rep.Repeatability == nil || *rep.Repeatability != 1 {
		t.Errorf("一致率应为 1，实际 %v", rep.Repeatability)
	}
	if rep.StableCases != 1 || len(rep.UnstableCases) != 0 {
		t.Errorf("稳定用例计数不对：stable=%d unstable=%v", rep.StableCases, rep.UnstableCases)
	}
}

func TestRepeatOffKeepsOldShape(t *testing.T) {
	for _, n := range []int{0, 1} {
		r := planDepthRunner(t, &planLLM{}, n)
		rep := r.Run(context.Background(), []Case{{ID: "s1", Goal: "随便做点什么"}})
		if rep.Repeatability != nil || rep.RepeatN != 0 {
			t.Errorf("Repeat=%d 时不该报可重复性，实际 n=%d rate=%v", n, rep.RepeatN, rep.Repeatability)
		}
		if rep.Cases[0].Runs != 1 || !rep.Cases[0].Stable {
			t.Errorf("Repeat=%d 时单遍运行应记 Runs=1/Stable=true，实际 %+v", n, rep.Cases[0])
		}
	}
}

func TestRunSignatureOnlyCountsRealDifferences(t *testing.T) {
	base := runSignature(true, 3, []string{"file.read", "file.list"})

	if got := runSignature(true, 3, []string{"file.list", "file.read"}); got != base {
		t.Errorf("工具顺序不该算差异：%q vs %q", got, base)
	}
	if runSignature(true, 3, []string{"file.read"}) == base {
		t.Error("工具集合少了东西必须算差异")
	}
	if runSignature(false, 3, []string{"file.read", "file.list"}) == base {
		t.Error("通过与否不同必须算差异")
	}
	if runSignature(true, 4, []string{"file.read", "file.list"}) == base {
		t.Error("步数不同必须算差异")
	}
}

func TestDescribeVariantsOrdersByCount(t *testing.T) {
	got := describeVariants([]string{"b", "a", "b", "c", "b", "a"})
	want := []string{"b ×3", "a ×2", "c ×1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("差异明细排序不对：实际 %v，期望 %v", got, want)
	}
}
