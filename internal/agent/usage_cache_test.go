package agent

import (
	"testing"

	"gleam/internal/config"
	"gleam/internal/llm"
)

func TestUsage_CachedTokensAccumulated(t *testing.T) {
	a := &Agent{Cfg: config.Default()}
	a.initUsage("t1")
	a.addUsage("t1", llm.Usage{PromptTokens: 1000, CompletionTokens: 50, CachedTokens: 800})
	a.addUsage("t1", llm.Usage{PromptTokens: 500, CompletionTokens: 30, CachedTokens: 100})
	a.addUsage("t1", llm.Usage{PromptTokens: 200, CompletionTokens: 10})

	u := a.usageOf("t1")
	if u.LLMCalls != 3 || u.PromptTokens != 1700 || u.CompletionTokens != 90 {
		t.Fatalf("基础累加不对: %+v", u)
	}
	if u.CachedTokens != 900 {
		t.Errorf("缓存命中应累加为 900，实际 %d", u.CachedTokens)
	}
	if u.CachedCalls != 2 {
		t.Errorf("报告了缓存的调用应为 2 次，实际 %d", u.CachedCalls)
	}
	if rate := u.CacheHitRate(); rate < 0.52 || rate > 0.54 {
		t.Errorf("命中率应约 0.529，实际 %v", rate)
	}
}

func TestUsage_NoCacheDataIsNotZeroPercent(t *testing.T) {
	a := &Agent{Cfg: config.Default()}
	a.initUsage("t2")
	a.addUsage("t2", llm.Usage{PromptTokens: 1000, CompletionTokens: 10, Estimated: true})

	u := a.usageOf("t2")
	if u.CachedTokens != 0 || u.CachedCalls != 0 {
		t.Errorf("无缓存数据时不该凭空产生数字: %+v", u)
	}
	if u.EstimatedCalls != 1 {
		t.Error("估算次数应被记录，前端据此标注 ≈")
	}
	if rate := u.CacheHitRate(); rate != 0 {
		t.Errorf("无数据时命中率为 0（表示未知），实际 %v", rate)
	}
}

func TestUsage_CacheHitRateClamped(t *testing.T) {
	a := &Agent{Cfg: config.Default()}
	a.initUsage("t3")
	a.addUsage("t3", llm.Usage{PromptTokens: 100, CompletionTokens: 5, CachedTokens: 500})
	if rate := a.usageOf("t3").CacheHitRate(); rate != 1 {
		t.Errorf("命中率应被夹到 1，实际 %v", rate)
	}
}
