package webui

import (
	"strings"
	"testing"
)

func TestContext_CarriesWaterLevel(t *testing.T) {
	f := newFixture(t, nil)

	first := f.call("GET", "/api/context", nil)
	pct, ok := first["fill_pct"]
	if !ok {
		t.Fatal(`/api/context 没有 fill_pct 字段：水位条没有数据来源，会永远停在"0%"`)
	}
	p, ok := pct.(float64)
	if !ok {
		t.Fatalf("fill_pct 应为数字，实际 %T", pct)
	}
	if p != 0 {
		t.Errorf("空会话的水位 = %v，应为 0", p)
	}

	for i := 0; i < 5; i++ {
		f.agent.Mem.AddTurn("user", "填满窗口的一轮")
	}
	got := f.call("GET", "/api/context", nil)
	turns, _ := got["short_turns"].(float64)
	cap_, _ := got["short_cap"].(float64)
	want := 100 * turns / cap_
	if v, _ := got["fill_pct"].(float64); v != want {
		t.Errorf("水位 = %v%%，按回传的 %v/%v 轮应为 %v%%", got["fill_pct"], turns, cap_, want)
	}
	if v, _ := got["fill_pct"].(float64); v < 0 || v > 100 {
		t.Errorf("水位 = %v%%，越界会把水位条画溢出那一栏", v)
	}
}

func TestSettings_ModelOnlyPatchIsAccepted(t *testing.T) {
	f := newFixture(t, nil)

	f.call("POST", "/api/settings", map[string]any{
		"llm": map[string]any{
			"fast_model": "cheap-helper",
			"tiers":      map[string]any{"coding": "deepseek-coder", "office": "glm-5.3-flash"},
		},
	})

	before := f.call("GET", "/api/settings", nil)
	baseBefore, _ := dot(before, "llm.base_url").(string)
	if baseBefore == "" {
		t.Fatal("夹具的 base_url 为空，下面的断言会虚假通过")
	}
	tiersBefore, _ := dot(before, "llm.tiers").(map[string]any)
	if len(tiersBefore) == 0 {
		t.Fatal("夹具的档位表为空，「清空档位」那条断言会虚假通过")
	}

	out := f.call("POST", "/api/settings", map[string]any{
		"llm": map[string]any{"model": "just-switching-model"},
	})
	if got := dot(out, "llm.model"); got != "just-switching-model" {
		t.Fatalf("补丁后 llm.model = %v，应为 just-switching-model（切了没生效，界面还显示旧名字）", got)
	}
	if baseAfter, _ := dot(out, "llm.base_url").(string); baseAfter != baseBefore {
		t.Errorf("只发 model 就把 base_url 从 %q 改成了 %q：补丁被当成整段替换", baseBefore, baseAfter)
	}
	if fmAfter, _ := dot(out, "llm.fast_model").(string); fmAfter != "cheap-helper" {
		t.Errorf("只发 model 就把辅助模型从 %q 改成了 %q：压缩/GEO 会悄悄退回主模型，贵一档", "cheap-helper", fmAfter)
	}
	tiersAfter, _ := dot(out, "llm.tiers").(map[string]any)
	if len(tiersAfter) != len(tiersBefore) {
		t.Errorf("只发 model 就把档位表从 %d 条变成 %d 条", len(tiersBefore), len(tiersAfter))
	}

	if got := dot(f.call("GET", "/api/settings", nil), "llm.model"); got != "just-switching-model" {
		t.Errorf("重新 GET 的 llm.model = %v，改动没留在运行时配置里", got)
	}
}

func dot(m map[string]any, path string) any {
	cur := any(m)
	for _, part := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = mm[part]
		if !ok {
			return nil
		}
	}
	return cur
}
