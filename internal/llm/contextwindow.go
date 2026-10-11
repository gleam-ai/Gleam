// contextwindow.go 模型上下文窗口的内置兜底表。
//
// **为什么需要一张表**：占用水位要有个分母，而分母不能瞎给——分母错了，
// 界面上那个百分比就是在说谎（"才 36%" 可能其实是 90%）。主流厂商里没有一家
// 提供"这个模型窗口多大"的接口，所以只能靠一张表 + 用户手填。
//
// **这张表的定位是兜底，不是权威**：值取自各厂商文档里的**标准窗口**（不是 beta 长窗、
// 也不是按套餐变动的那个），只在模型名认得出厂商与代际时命中。查不到就用手填值，
// 手填也没有就用保守默认值——**用了哪一个由调用方带回界面**（`Source`），
// 这样"这个百分比是怎么来的"始终看得见，而不是一个看起来很确定的数字。
//
// 加一条是一行的事：名字前缀 + 窗口大小 + 出处。宁可少列几条，也不要填一个记不准的数。
package llm

import "strings"

// DefaultContextWindow 认不出模型时的保守默认窗口。
// 取 128k：它是当前主流模型的下界附近，宁可把水位算得偏高（早一点提醒你），
// 也不要算成 1M 让你以为还有很多余量。
const DefaultContextWindow = 128000

// SourceManual / SourceTable / SourceDefault 窗口大小的三个来源。
// 界面必须把它显示出来：同一个百分比，"按你填的数算的"与"按兜底值算的"可信度不同。
const (
	SourceManual  = "manual"  // 设置里选的（预设档或自定义值）
	SourceTable   = "table"   // 命中内置表
	SourceDefault = "default" // 谁都认不出，用兜底值
)

// contextWindowEntry 一条兜底：模型名（小写）前缀 → 窗口大小。
// note 写的是这个数从哪来，出问题时（厂商改了窗口）要能顺着它去核。
type contextWindowEntry struct {
	prefix string
	tokens int
	note   string
}

// contextWindows 内置表。按顺序匹配，**长的前缀写在前面**（`gpt-4o` 要在 `gpt-4o-mini` 之前判定，
// 否则后者永远轮不到；这里两者同值，但规则先立着，免得以后加新条目时踩）。
var contextWindows = []contextWindowEntry{
	// Anthropic 5-5 代：官方模型页标的是 1M（2026-10-11 核）。比下面的 `claude` 更具体，
	// 必须排在它前面——否则永远轮不到，会一直按旧代的 200k 算。
	{prefix: "claude-fable-5", tokens: 1000000, note: "Anthropic Claude Fable 5 1M"},
	{prefix: "claude-opus-5", tokens: 1000000, note: "Anthropic Claude Opus 5 1M"},
	{prefix: "claude-sonnet-5", tokens: 1000000, note: "Anthropic Claude Sonnet 5 1M"},
	{prefix: "claude-haiku-5", tokens: 1000000, note: "Anthropic Claude Haiku 5 1M"},
	{prefix: "claude", tokens: 200000, note: "Anthropic 旧代标准窗口 200k"},
	// 百度千帆的 ERNIE 系列：取自百度自己的模型列表页（该页逐条标了上下文长度）。
	// 带 32k 的变体写在前面，否则会被 `ernie-4.5-turbo` 那条先命中。
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

// ContextWindowFor 认一下这个模型的窗口有多大。
//
// 返回 tokens、来源、以及一句人能读的说明（命中表时是出处，否则说明用的是兜底/手填）。
// 手填优先由调用方处理（它才知道用户填了什么），这里只管"认不认得出来"。
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

// hasModelToken 模型名里是否出现了这个家族名，且它前面是**边界**（开头或非字母数字）。
//
// 为什么按"出现"而不是"开头"：从厂商模型列表里拉回来的名字常常带厂商前缀——
// `zhipu/glm-4`、`anthropic.claude-sonnet-4`、`openai:gpt-4o`。只认开头，这几种全认不出来。
// 为什么要求前面是边界：免得 `my-claude-4` 这种自造名前缀把 `lm-4` 之类的片段配上。
// 这是**兜底表**的匹配规则，宁可宽一点：认错了也只是窗口大小取个家族值，界面会标出用的是内置表。
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
