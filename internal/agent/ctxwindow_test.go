package agent

import (
	"context"
	"strings"
	"testing"

	"gleam/internal/harness/memory"
	"gleam/internal/llm"
)

func TestWindowPct(t *testing.T) {
	cases := []struct{ last, window, want int }{
		{0, 128000, 0},
		{115200, 128000, 90},
		{115100, 128000, 90},
		{128000, 128000, 100},
		{200000, 128000, 100},
		{1000, 0, 0},
		{1000, 128000, 1},
	}
	for _, c := range cases {
		if got := windowPct(c.last, c.window); got != c.want {
			t.Errorf("windowPct(%d, %d) = %d，应为 %d", c.last, c.window, got, c.want)
		}
	}
}

func TestContextWindowResolution(t *testing.T) {
	f := newFixture(t, nil)

	f.a.Cfg.LLM.Model = "claude-sonnet-4"
	if got, src, _ := f.a.contextWindow(); got != 200000 || src != llm.SourceTable {
		t.Errorf("内置表：得到 %d/%s", got, src)
	}
	f.a.Cfg.LLM.Model = "谁也没见过的模型"
	if got, src, _ := f.a.contextWindow(); got != llm.DefaultContextWindow || src != llm.SourceDefault {
		t.Errorf("兜底：得到 %d/%s", got, src)
	}
	f.a.Cfg.LLM.ContextWindow = 32000
	if got, src, _ := f.a.contextWindow(); got != 32000 || src != llm.SourceManual {
		t.Errorf("手填应当压过内置表：得到 %d/%s", got, src)
	}
}

func TestCompressAfterTask(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000
	ctx := context.Background()

	f.a.notePromptSize(llm.Usage{PromptTokens: 500})
	if c, tn := f.a.compressAfterTask(ctx, "t1"); c || tn {
		t.Errorf("水位 50%% 且无溢出时不该动上下文（得到 %v/%v）", c, tn)
	}
	if got := f.a.Mem.CarryTurns(); got != memory.CarryFull {
		t.Errorf("不该收紧，实际携带 %d 轮", got)
	}

	f.a.notePromptSize(llm.Usage{PromptTokens: 900})
	c, tn := f.a.compressAfterTask(ctx, "t1")
	if !tn {
		t.Error("水位 90% 应当收紧下一轮携带")
	}
	_ = c
	if got := f.a.Mem.CarryTurns(); got != memory.CarryTight {
		t.Errorf("应当收紧到 %d 轮，实际 %d", memory.CarryTight, got)
	}

	f2 := newFixture(t, nil)
	f2.a.Cfg.LLM.ContextWindow = 1000
	f2.a.notePromptSize(llm.Usage{PromptTokens: 100})
	for i := 0; i < f2.a.Cfg.Memory.ShortTermCap+3; i++ {
		f2.a.Mem.AddTurn("user", "第几轮不重要")
	}
	if f2.a.Mem.Stats().Overflow == 0 {
		t.Fatal("前提不成立：应当已经攒出溢出轮")
	}
	if c, _ := f2.a.compressAfterTask(ctx, "t2"); !c {
		t.Error("有溢出轮就该汇总，与水位无关")
	}
}

func TestCompressMidTask(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000
	ctx := context.Background()

	f.a.notePromptSize(llm.Usage{PromptTokens: 950})
	if _, tn := f.a.compressMidTask(ctx, "t1"); tn {
		t.Error("95% 还不该在任务中动手（96% 才是那条线）")
	}
	f.a.notePromptSize(llm.Usage{PromptTokens: 960})
	if _, tn := f.a.compressMidTask(ctx, "t1"); !tn {
		t.Error("96% 应当收紧")
	}
	if got := f.a.Mem.CarryTurns(); got != memory.CarryTight {
		t.Errorf("应当收紧到 %d 轮，实际 %d", memory.CarryTight, got)
	}
	f.a.notePromptSize(llm.Usage{PromptTokens: 400})
	f.a.compressMidTask(ctx, "t1")
	if got := f.a.Mem.CarryTurns(); got != memory.CarryFull {
		t.Errorf("水位掉回 40%% 应当松回 %d 轮，实际 %d", memory.CarryFull, got)
	}
}

func TestCompressionOffDoesNothing(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000
	f.a.Cfg.Agent.ContextCompress = false
	f.a.notePromptSize(llm.Usage{PromptTokens: 999})
	ctx := context.Background()

	if c, tn := f.a.compressMidTask(ctx, "t1"); c || tn {
		t.Error("自动压缩关着时任务中不该动上下文")
	}
	if c, tn := f.a.compressAfterTask(ctx, "t1"); c || tn {
		t.Error("自动压缩关着时任务结束也不该动上下文")
	}
}

func TestContextViewCarriesWindowReading(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000
	f.a.notePromptSize(llm.Usage{PromptTokens: 123, Estimated: true})

	v := f.a.ContextView()
	if v["window_tokens"] != 1000 || v["window_source"] != llm.SourceManual {
		t.Errorf("分母与来源不对：%v / %v", v["window_tokens"], v["window_source"])
	}
	if v["prompt_tokens"] != 123 || v["prompt_estimated"] != true {
		t.Errorf("读数应当是 123（估算）：%v / %v", v["prompt_tokens"], v["prompt_estimated"])
	}
	if v["window_pct"] != 13 {
		t.Errorf("占用应为 13%%，实际 %v", v["window_pct"])
	}
	if v["carry_turns"] != memory.CarryFull {
		t.Errorf("应当带上携带轮数，实际 %v", v["carry_turns"])
	}
	if v["compress_end_pct"] != compressAtEndPct || v["compress_mid_pct"] != compressMidPct {
		t.Errorf("阈值没带出去：%v / %v", v["compress_end_pct"], v["compress_mid_pct"])
	}
	if _, ok := v["fill_pct"]; !ok {
		t.Error("轮数口径不该被这次改动顶掉")
	}
}

func TestContextActionText(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000
	f.a.notePromptSize(llm.Usage{PromptTokens: 970})
	f.a.Mem.SetCarryTurns(memory.CarryTight)

	got := f.a.contextActionText(true, true, true)
	for _, want := range []string{"97%", "1k", "2 轮最近对话", "任务继续"} {
		if !strings.Contains(got, want) {
			t.Errorf("这句话里应当有 %q：%s", want, got)
		}
	}
}
