package llm

import "testing"

func TestContextWindowFor_Table(t *testing.T) {
	cases := map[string]int{
		"claude-sonnet-4":      200000,
		"claude-3-5-sonnet":    200000,
		"gpt-4o":               128000,
		"gpt-4o-mini":          128000,
		"gpt-4-turbo":          128000,
		"glm-4-plus":           128000,
		"glm-5.3-flash":        128000,
		"qwen3-coder":          131072,
		"Qwen2.5-72B-Instruct": 131072,
	}
	for model, want := range cases {
		got, src, _ := ContextWindowFor(model)
		if got != want || src != SourceTable {
			t.Errorf("ContextWindowFor(%q) = %d/%s，应为 %d/%s", model, got, src, want, SourceTable)
		}
	}
}

func TestContextWindowFor_StripsVendorPrefix(t *testing.T) {
	for _, model := range []string{"zhipu/glm-4-plus", "anthropic.claude-sonnet-4", "openai:gpt-4o"} {
		if got, src, _ := ContextWindowFor(model); src != SourceTable {
			t.Errorf("ContextWindowFor(%q) 没认出来（得到 %d/%s）", model, got, src)
		}
	}
}

func TestContextWindowFor_FallsBack(t *testing.T) {
	for _, model := range []string{"", "   ", "some-local-llama-70b"} {
		got, src, note := ContextWindowFor(model)
		if got != DefaultContextWindow || src != SourceDefault {
			t.Errorf("ContextWindowFor(%q) = %d/%s，应为 %d/%s", model, got, src, DefaultContextWindow, SourceDefault)
		}
		if note == "" {
			t.Errorf("ContextWindowFor(%q) 的说明不能是空的：界面要拿它解释分母从哪来", model)
		}
	}
}

func TestContextWindowFor_NewFamilies(t *testing.T) {
	cases := map[string]int{
		"claude-fable-5-1":       1000000,
		"claude-opus-5-5":        1000000,
		"claude-sonnet-5-5":      1000000,
		"claude-haiku-5-5":       1000000,
		"ernie-5.1":              128000,
		"ernie-4.5-turbo-128k":   128000,
		"ernie-4.5-turbo-32k":    32000,
		"ernie-4.5-turbo-vl":     128000,
		"ernie-4.5-turbo-vl-32k": 32000,
		"ernie-x1.1":             64000,
		"claude-sonnet-4-5":      200000,
		"glm-5.3-flash":          128000,
	}
	for model, want := range cases {
		got, src, note := ContextWindowFor(model)
		if got != want || src != SourceTable {
			t.Errorf("ContextWindowFor(%q) = %d/%s，应为 %d/%s", model, got, src, want, SourceTable)
		}
		if note == "" {
			t.Errorf("ContextWindowFor(%q) 的出处不能是空的", model)
		}
	}
}
