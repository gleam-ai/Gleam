package toolutil

import (
	"context"
	"math"
	"strings"
	"testing"

	"gleam/pkg/types"
)

func TestStr(t *testing.T) {
	args := map[string]any{
		"s": "text", "n": 3.5, "i": int64(7), "b": true, "nil": nil,
	}
	if Str(args, "s") != "text" {
		t.Error("字符串")
	}
	if Str(args, "n") != "3.5" {
		t.Errorf("数字 = %q", Str(args, "n"))
	}
	if Str(args, "i") != "7" {
		t.Errorf("整数 = %q", Str(args, "i"))
	}
	if Str(args, "b") != "true" {
		t.Errorf("布尔 = %q", Str(args, "b"))
	}
	if Str(args, "missing") != "" {
		t.Error("缺失应为空")
	}
}

func TestRequireStr(t *testing.T) {
	if _, err := RequireStr(map[string]any{"k": "v"}, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := RequireStr(map[string]any{"k": "  "}, "k"); err == nil {
		t.Error("空白应报错")
	}
	if _, err := RequireStr(map[string]any{}, "k"); err == nil {
		t.Error("缺失应报错")
	}
}

func TestBoolIntMap(t *testing.T) {
	if !Bool(map[string]any{"b": true}, "b") {
		t.Error("bool true")
	}
	if Bool(map[string]any{"b": "false"}, "b") {
		t.Error("字符串 false")
	}
	if Bool(map[string]any{}, "b") {
		t.Error("缺失默认 false")
	}
	if Int(map[string]any{"n": float64(5)}, "n", 1) != 5 {
		t.Error("float64 整数")
	}
	if Int(map[string]any{"n": "8"}, "n", 1) != 8 {
		t.Error("字符串整数")
	}
	if Int(map[string]any{}, "n", 9) != 9 {
		t.Error("默认值")
	}
	m := Map(map[string]any{"m": map[string]any{"a": 1}}, "m")
	if m == nil || m["a"] != 1 {
		t.Error("Map 取值")
	}
	if Map(map[string]any{}, "m") != nil {
		t.Error("缺失 Map 应为 nil")
	}
}

func TestSchema(t *testing.T) {
	s := Schema("描述", []string{"path"}, map[string]any{
		"path": SchemaProp("路径", "string"),
	})
	if s["type"] != "object" || s["description"] != "描述" {
		t.Errorf("schema = %v", s)
	}
	props := s["properties"].(map[string]any)
	if !strings.Contains(props["path"].(map[string]any)["type"].(string), "string") {
		t.Error("属性类型")
	}
}

type schemaOnlyTool struct{ schema map[string]any }

func (s schemaOnlyTool) Name() string                                         { return "schema-only" }
func (s schemaOnlyTool) Description() string                                  { return "" }
func (s schemaOnlyTool) Schema() map[string]any                               { return s.schema }
func (s schemaOnlyTool) Permission() types.Permission                         { return types.PermissionUserApproved }
func (s schemaOnlyTool) Execute(context.Context, map[string]any) (any, error) { return nil, nil }

func TestValidateArgs_RequiredStringSlice(t *testing.T) {
	tool := schemaOnlyTool{schema: Schema("写文件", []string{"path", "content"}, map[string]any{
		"path": SchemaProp("路径", "string"), "content": SchemaProp("内容", "string"),
	})}
	if err := ValidateArgs(tool, map[string]any{"path": "a.txt"}); err == nil || !strings.Contains(err.Error(), "content") {
		t.Fatalf("缺少必填 content 应报错，实际 %v", err)
	}
	if err := ValidateArgs(tool, map[string]any{"path": "a.txt", "content": "  "}); err == nil {
		t.Error("空白必填字符串应报错")
	}
	if err := ValidateArgs(tool, map[string]any{"path": "a.txt", "content": 42}); err == nil {
		t.Error("content 类型错误应报错")
	}
	if err := ValidateArgs(tool, map[string]any{"path": "a.txt", "content": "ok"}); err != nil {
		t.Fatalf("合法参数被拒: %v", err)
	}
}

// eval A5: a type error must tell the model what it sent wrong and how to fix
// it — field, expected type, the actual received kind, and a concrete example —
// otherwise it spins on unrelated tools instead of correcting the argument.
func TestValidateArgs_TypeErrorIsActionable(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		val     any
		want    string
		actual  string
		example string
	}{
		{"string 收到 object", "string", map[string]any{"text": "x"}, "字符串", "object", `"f": "文本内容"`},
		{"integer 收到 string", "integer", "10", "数字", "string", `"f": 10`},
		{"boolean 收到 string", "boolean", "true", "布尔值", "string", `"f": true`},
		{"array 收到 string", "array", "x", "数组", "string", `"f": ["a", "b"]`},
		{"object 收到 array", "object", []any{"x"}, "对象", "array", `"f": {"key": "value"}`},
	}
	for _, tc := range cases {
		tool := schemaOnlyTool{schema: Schema("工具", []string{"f"}, map[string]any{
			"f": SchemaProp("参数", tc.typ),
		})}
		err := ValidateArgs(tool, map[string]any{"f": tc.val})
		if err == nil {
			t.Errorf("%s: 应报错", tc.name)
			continue
		}
		msg := err.Error()
		for _, want := range []string{"f", tc.want, tc.actual, tc.example} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: 错误信息缺少 %q，实际 %q", tc.name, want, msg)
			}
		}
	}
}

func TestIntOutOfRangeFallsBack(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"float64 超上界", float64(math.MaxInt64) * 2},
		{"float64 超下界", float64(math.MinInt64) * 2},
		{"float64 NaN", math.NaN()},
		{"float64 正无穷", math.Inf(1)},
		{"int64 超界", int64(math.MaxInt64)},
	}
	for _, c := range cases {
		if c.name == "int64 超界" && math.MaxInt == math.MaxInt64 {
			continue // 64-bit: int64(max) is a valid int
		}
		if got := Int(map[string]any{"n": c.v}, "n", 7); got != 7 {
			t.Errorf("%s: Int = %d, 应回落默认 7", c.name, got)
		}
		if got := IntOf(c.v); got != 0 {
			t.Errorf("%s: IntOf = %d, 应回落 0", c.name, got)
		}
	}
	if got := Int(map[string]any{"n": float64(-5)}, "n", 7); got != -5 {
		t.Errorf("负整数应保留: %d", got)
	}
	if got := IntOf(int64(42)); got != 42 {
		t.Errorf("int64 正常值: %d", got)
	}
}
