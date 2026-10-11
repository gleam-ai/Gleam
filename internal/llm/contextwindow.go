package llm

import "strings"

const DefaultContextWindow = 128000

const (
	SourceManual  = "manual"
	SourceTable   = "table"
	SourceDefault = "default"
)

type contextWindowEntry struct {
	prefix string
	tokens int
	note   string
}

var contextWindows = []contextWindowEntry{
	{prefix: "claude-fable-5", tokens: 1000000, note: "Anthropic Claude Fable 5 1M"},
	{prefix: "claude-opus-5", tokens: 1000000, note: "Anthropic Claude Opus 5 1M"},
	{prefix: "claude-sonnet-5", tokens: 1000000, note: "Anthropic Claude Sonnet 5 1M"},
	{prefix: "claude-haiku-5", tokens: 1000000, note: "Anthropic Claude Haiku 5 1M"},
	{prefix: "claude", tokens: 200000, note: "Anthropic 旧代标准窗口 200k"},
	{prefix: "ernie-4.5-turbo-128k", tokens: 128000, note: "百度 ERNIE 4.5 Turbo 128k"},
	{prefix: "ernie-4.5-turbo-vl-32k", tokens: 32000, note: "百度 ERNIE 4.5 Turbo VL 32k"},
	{prefix: "ernie-4.5-turbo-32k", tokens: 32000, note: "百度 ERNIE 4.5 Turbo 32k"},
	{prefix: "ernie-4.5-turbo", tokens: 128000, note: "百度 ERNIE 4.5 Turbo 128k"},
	{prefix: "ernie-5", tokens: 128000, note: "百度 ERNIE 5 128k"},
	{prefix: "ernie-x1.1", tokens: 64000, note: "百度 ERNIE X1.1 64k"},
	{prefix: "gpt-4o", tokens: 128000, note: "OpenAI GPT-4o 128k"},
	{prefix: "gpt-4-turbo", tokens: 128000, note: "OpenAI GPT-4 Turbo 128k"},
	{prefix: "glm-4", tokens: 128000, note: "智谱 GLM-4 128k"},
	{prefix: "glm-5", tokens: 128000, note: "智谱 GLM-5 128k"},
	{prefix: "qwen", tokens: 131072, note: "通义千问 128k（131072）"},
}

func ContextWindowFor(model string) (tokens int, source, note string) {
	name := strings.ToLower(strings.TrimSpace(model))
	if name == "" {
		return DefaultContextWindow, SourceDefault, "没填模型名，按兜底窗口算"
	}
	for _, e := range contextWindows {
		if hasModelToken(name, e.prefix) {
			return e.tokens, SourceTable, e.note
		}
	}
	return DefaultContextWindow, SourceDefault, "这个模型名不在内置表里，按兜底窗口算（可在设置 → 模型里手填）"
}

func hasModelToken(name, prefix string) bool {
	for i := 0; ; {
		j := strings.Index(name[i:], prefix)
		if j < 0 {
			return false
		}
		at := i + j
		if at == 0 || !isASCIIAlnum(name[at-1]) {
			return true
		}
		i = at + 1
	}
}

func isASCIIAlnum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
