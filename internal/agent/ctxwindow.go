package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gleam/internal/harness/memory"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

const (
	compressAtEndPct = 90
	compressMidPct   = 96
	relaxPct         = 70
)

func (a *Agent) notePromptSize(u llm.Usage) {
	if u.PromptTokens <= 0 {
		return
	}
	a.ctxMu.Lock()
	a.lastPromptTokens = u.PromptTokens
	a.lastPromptEstimated = u.Estimated
	a.ctxMu.Unlock()
}

func (a *Agent) lastPrompt() (int, bool) {
	a.ctxMu.Lock()
	defer a.ctxMu.Unlock()
	return a.lastPromptTokens, a.lastPromptEstimated
}

func (a *Agent) contextWindow() (tokens int, source, note string) {
	if a.Cfg != nil && a.Cfg.LLM.ContextWindow > 0 {
		return a.Cfg.LLM.ContextWindow, llm.SourceManual, "按设置里选的窗口大小算"
	}
	model := ""
	if a.Cfg != nil {
		model = a.Cfg.LLM.Model
	}
	return llm.ContextWindowFor(model)
}

func windowPct(lastTokens, windowTokens int) int {
	if windowTokens <= 0 || lastTokens <= 0 {
		return 0
	}
	p := (100*lastTokens + windowTokens - 1) / windowTokens
	if p > 100 {
		return 100
	}
	return p
}

type windowReading struct {
	LastTokens int    `json:"prompt_tokens"`
	Estimated  bool   `json:"prompt_estimated"`
	Window     int    `json:"window_tokens"`
	Source     string `json:"window_source"`
	Note       string `json:"window_note"`
	Pct        int    `json:"window_pct"`
	CarryTurns int    `json:"carry_turns"`
}

func (a *Agent) windowReading() windowReading {
	last, est := a.lastPrompt()
	win, src, note := a.contextWindow()
	rd := windowReading{LastTokens: last, Estimated: est, Window: win, Source: src, Note: note, Pct: windowPct(last, win)}
	if a.Mem != nil {
		rd.CarryTurns = a.Mem.CarryTurns()
	}
	return rd
}

func (a *Agent) compressAfterTask(ctx context.Context, taskID string, gen int) (compressed, tightened bool) {
	if !a.compressionOn() {
		return false, false
	}
	rd := a.windowReading()
	return a.applyCompression(ctx, taskID, gen, rd.Pct >= compressAtEndPct)
}

func (a *Agent) compressMidTask(ctx context.Context, taskID string) (compressed, tightened bool) {
	if !a.compressionOn() {
		return false, false
	}
	gen := a.Mem.Generation()
	if a.windowReading().Pct < compressMidPct {
		a.Mem.SetCarryTurnsForGeneration(gen, memory.CarryFull)
		return false, false
	}
	return a.applyCompression(ctx, taskID, gen, true)
}

func (a *Agent) compressionOn() bool {
	return a.Cfg != nil && a.Cfg.Agent.ContextCompress && a.Mem != nil
}

func (a *Agent) contextActionText(compressed, tightened, mid bool) string {
	return contextActionText(a.windowReading(), compressed, tightened, mid)
}

func contextActionText(rd windowReading, compressed, tightened, mid bool) string {
	var b strings.Builder
	switch {
	case compressed && tightened:
		b.WriteString("已把早期上下文汇总成摘要，并把下一轮携带的对话收紧")
	case compressed:
		b.WriteString("已把早期上下文汇总成摘要")
	case tightened:
		b.WriteString("已把下一轮携带的对话收紧")
	}
	fmt.Fprintf(&b, "（窗口占用 %d%%，窗口 %s，下一轮带 %d 轮最近对话）",
		rd.Pct, humanTokens(rd.Window), rd.CarryTurns)
	if mid {
		b.WriteString("；任务继续")
	}
	return b.String()
}

func (a *Agent) notifyContextAction(taskID string, gen int, compressed, tightened, mid bool) {
	if a.Notifier == nil || a.Mem == nil || (!compressed && !tightened) {
		return
	}
	rd := a.windowReading()
	carry, current := a.Mem.CarryTurnsForGeneration(gen)
	if !current {
		return
	}
	rd.CarryTurns = carry
	// Notifier may lock task admission; never call it while holding memory's lock.
	a.Notifier.OnProgress(types.ProgressEvent{
		TaskID: taskID, Phase: "context", Message: contextActionText(rd, compressed, tightened, mid), Progress: 100, Kind: "info",
	})
}

func humanTokens(n int) string {
	if n >= 1000 {
		return strconv.Itoa(n/1000) + "k"
	}
	return strconv.Itoa(n)
}

func (a *Agent) applyCompression(ctx context.Context, taskID string, gen int, tighten bool) (compressed, tightened bool) {
	compressed = a.Mem.CompressForGeneration(gen, a.CompressSummarizer(ctx, taskID))
	if tighten {
		tightened = a.Mem.SetCarryTurnsForGeneration(gen, memory.CarryTight)
	}
	return compressed, tightened
}
