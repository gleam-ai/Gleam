package eval

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"gleam/internal/agent"
	"gleam/internal/config"
	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/llm"
	"gleam/internal/tools/std"
	"gleam/pkg/types"
)

type usageLLM struct {
	n atomic.Int64
}

func (m *usageLLM) Chat(_ context.Context, req llm.ChatRequest) (string, error) {
	text := `{"score":90,"verdict":"done","reason":"ok"}`
	if llm.KindOf(req.System) == "plan" {
		text = `{"steps":[{"id":"s1","tool":"reply","args":{"text":"好了"}}],"estimated_time":"short"}`
	}
	m.n.Add(1)
	llm.ReportUsage(req, text, llm.Usage{PromptTokens: 100, CompletionTokens: 20})
	return text, nil
}

func (m *usageLLM) ChatStream(ctx context.Context, req llm.ChatRequest, _ func(string)) (string, error) {
	return m.Chat(ctx, req)
}

func (m *usageLLM) Name() string { return "usage-stub" }

func (m *usageLLM) calls() int { return int(m.n.Load()) }

func fullDepthRunner(t *testing.T, client llm.Client, repeat int) *Runner {
	t.Helper()
	cfg := config.Default()
	cfg.LLM.Provider = "mock"
	cfg.DataDir = t.TempDir()
	reg := registry.New()
	reg.MustRegister(std.NewReply())
	gate := safety.New("auto", nil, nil, nil, time.Second)
	a := agent.New(cfg, client, reg, nil, gate, nil, nil, nil)
	return &Runner{Agent: a, Depth: DepthFull, CWD: t.TempDir(), Repeat: repeat}
}

func TestFullDepthReportsUsage(t *testing.T) {
	stub := &usageLLM{}
	rep := fullDepthRunner(t, stub, 1).Run(context.Background(), []Case{{ID: "u1", Goal: "打个招呼"}})

	if rep.Usage == nil {
		t.Fatal("full 深度的报告必须有用量合计")
	}
	if stub.calls() == 0 {
		t.Fatal("桩一次都没被调用，这条测试没测到东西")
	}
	if rep.Usage.LLMCalls != stub.calls() {
		t.Errorf("报告记了 %d 次模型调用，桩实际调用 %d 次（有调用没被计入）", rep.Usage.LLMCalls, stub.calls())
	}
	if rep.Usage.PromptTokens != stub.calls()*100 || rep.Usage.CompletionTokens != stub.calls()*20 {
		t.Errorf("token 合计 = 输入%d/输出%d，应为 输入%d/输出%d",
			rep.Usage.PromptTokens, rep.Usage.CompletionTokens, stub.calls()*100, stub.calls()*20)
	}
	c := rep.Cases[0]
	if c.Usage == nil {
		t.Fatal("用例级也要有用量——只看合计答不出'哪条贵'")
	}
	if c.Usage.TotalTokens() != rep.Usage.TotalTokens() {
		t.Errorf("单条合计 %d 与报告合计 %d 不一致（本次只有一条用例）",
			c.Usage.TotalTokens(), rep.Usage.TotalTokens())
	}
}

func TestRepeatUsageCountsEveryRun(t *testing.T) {
	one := &usageLLM{}
	rep1 := fullDepthRunner(t, one, 1).Run(context.Background(), []Case{{ID: "u1", Goal: "打个招呼"}})

	three := &usageLLM{}
	rep3 := fullDepthRunner(t, three, 3).Run(context.Background(), []Case{{ID: "u1", Goal: "打个招呼"}})

	if rep1.Usage == nil || rep3.Usage == nil {
		t.Fatal("两边的报告都该有用量")
	}
	if three.calls() <= one.calls() {
		t.Fatalf("repeat 3 的模型调用（%d）应多于 repeat 1（%d）", three.calls(), one.calls())
	}
	if rep3.Usage.LLMCalls != three.calls() {
		t.Errorf("repeat 3 报告记了 %d 次调用，桩实际 %d 次（重跑的遍数被漏计了）",
			rep3.Usage.LLMCalls, three.calls())
	}
	if rep3.Passed != 1 {
		t.Errorf("通过率口径应仍取首遍，实得 Passed=%d", rep3.Passed)
	}
}

func TestSelectDepthHasNoUsage(t *testing.T) {
	rep := (&Runner{Depth: DepthSelect}).Run(context.Background(), nil)
	if rep.Usage != nil {
		t.Errorf("select 深度不该有用量合计，实得 %+v", rep.Usage)
	}
}

func TestUsageFromTaskKeepsEveryCostField(t *testing.T) {
	in := types.TaskUsage{
		LLMCalls:         11,
		PromptTokens:     12,
		CompletionTokens: 13,
		EstimatedCalls:   14,
		CachedTokens:     15,
		CachedCalls:      16,
		ToolCalls:        17,
		Retries:          18,
		Deduped:          19,
		DurationMs:       20,
	}
	got := usageFromTask(in)
	want := &CaseUsage{
		LLMCalls: 11, PromptTokens: 12, CompletionTokens: 13,
		EstimatedCalls: 14, CachedTokens: 15, CachedCalls: 16, ToolCalls: 17,
	}
	if *got != *want {
		t.Errorf("转换结果 = %+v，应为 %+v", *got, *want)
	}
	if got.TotalTokens() != 25 {
		t.Errorf("TotalTokens = %d，应为 25", got.TotalTokens())
	}
}
