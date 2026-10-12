package agent

import (
	"context"
	"strings"
	"testing"

	"gleam/pkg/types"
)

// multiPropTool serves a schema whose properties live in a map, so rendering it
// exercises the ordering discipline this file tests.
type multiPropTool struct {
	name     string
	props    map[string]any
	required []string
}

func (m *multiPropTool) Name() string                 { return m.name }
func (m *multiPropTool) Description() string          { return "多参数工具" }
func (m *multiPropTool) Permission() types.Permission { return types.PermissionReadOnly }
func (m *multiPropTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": m.props, "required": m.required}
}
func (m *multiPropTool) Execute(context.Context, map[string]any) (any, error) { return "ok", nil }

// The capability menu is part of the stable prompt prefix. Rendering the property
// map directly would shuffle a tool's parameter list on every build, cut the
// cacheable prefix at the menu, and flip the readiness verdict between calls.
func TestCompactToolSchema_ParameterOrderIsStable(t *testing.T) {
	tool := &multiPropTool{
		name: "file.delete",
		props: map[string]any{
			"path":      map[string]any{"type": "string", "description": "目标路径"},
			"recursive": map[string]any{"type": "boolean", "description": "删除目录时必须为 true"},
			"limit":     map[string]any{"type": "number"},
		},
		required: []string{"path"},
	}

	want := compactToolSchema(tool)
	for i := 0; i < 200; i++ {
		if got := compactToolSchema(tool); got != want {
			t.Fatalf("第 %d 次渲染的参数列表变了（map 遍历顺序漏进了提示词）：\n%s\n%s", i, want, got)
		}
	}

	// Determinism alone is not enough: required still marks, and the order is
	// by name so the menu reads the same on every machine.
	if !strings.Contains(want, "path(string, 目标路径)[必填]") {
		t.Errorf("必填参数应带 [必填] 标记：%s", want)
	}
	if !strings.Contains(want, "limit(number)") ||
		!strings.Contains(want, "recursive(boolean, 删除目录时必须为 true)") {
		t.Errorf("参数渲染缺项：%s", want)
	}
	pathIdx := strings.Index(want, "- file.delete")
	limitIdx := strings.Index(want, "limit(")
	recIdx := strings.Index(want, "recursive(")
	if !(pathIdx < limitIdx && limitIdx < recIdx) {
		t.Errorf("参数应按名称排序 path < limit < recursive：%s", want)
	}
}
