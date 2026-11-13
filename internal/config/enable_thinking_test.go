package config

import (
	"path/filepath"
	"testing"
)

func TestEnableThinkingParse(t *testing.T) {
	for _, tc := range []struct {
		name string
		val  any
		want *bool
	}{
		{"false", false, boolPtr(false)},
		{"true", true, boolPtr(true)},
		{"absent", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			llm := map[string]any{}
			if tc.val != nil {
				llm["enable_thinking"] = tc.val
			}
			cfg.Apply(map[string]any{"llm": llm})
			got := cfg.LLM.EnableThinking
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("未配置时应为 nil，实际 %v", *got)
			case tc.want != nil && got == nil:
				t.Errorf("应解析为 %v，实际 nil", *tc.want)
			case tc.want != nil && got != nil && *got != *tc.want:
				t.Errorf("应解析为 %v，实际 %v", *tc.want, *got)
			}
		})
	}
}

func TestEnableThinkingOverlayRoundtrip(t *testing.T) {
	dir := t.TempDir()
	cfg := Default()
	cfg.LLM.EnableThinking = boolPtr(false)

	path := filepath.Join(dir, OverlayFile)
	if err := cfg.SaveOverlay(path); err != nil {
		t.Fatal(err)
	}
	fresh := Default()
	if err := LoadOverlay(fresh, path); err != nil {
		t.Fatal(err)
	}
	if fresh.LLM.EnableThinking == nil || *fresh.LLM.EnableThinking != false {
		t.Errorf("overlay 往返后 enable_thinking 应为 false，实际 %v", fresh.LLM.EnableThinking)
	}
}

func TestEnableThinkingCloneIndependent(t *testing.T) {
	cfg := Default()
	cfg.LLM.EnableThinking = boolPtr(true)
	clone := cfg.Clone()
	*clone.LLM.EnableThinking = false
	if *cfg.LLM.EnableThinking != true {
		t.Error("Clone 必须深拷贝 EnableThinking 指针，改副本不能影响原配置")
	}
}

func boolPtr(b bool) *bool { return &b }
