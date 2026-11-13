package toolutil

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"gleam/pkg/types"
)

func Str(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return trimFloat(t)
	case int64:
		return fmt.Sprintf("%d", t)
	case int:
		return fmt.Sprintf("%d", t)
	case bool:
		return fmt.Sprintf("%v", t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func trimFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func RequireStr(args map[string]any, key string) (string, error) {
	s := Str(args, key)
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("缺少必填参数 %q", key)
	}
	return s, nil
}

func Bool(args map[string]any, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1" || v == "yes"
	default:
		return false
	}
}

func Int(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		if n, ok := intFromFloat(v); ok {
			return n
		}
	case int64:
		if n, ok := intFromInt64(v); ok {
			return n
		}
	case int:
		return v
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err == nil {
			return n
		}
	}
	return def
}

// Model-provided numbers arrive as float64/int64 and may exceed int range
// (especially on 32-bit builds); an unchecked conversion wraps or is undefined,
// so out-of-range values fall back to the default instead.
func intFromInt64(v int64) (int, bool) {
	if v < math.MinInt || v > math.MaxInt {
		return 0, false
	}
	return int(v), true
}

func intFromFloat(f float64) (int, bool) {
	if math.IsNaN(f) || f < math.MinInt || f >= math.MaxInt {
		return 0, false
	}
	return int(f), true
}

func Map(args map[string]any, key string) map[string]any {
	if v, ok := args[key].(map[string]any); ok {
		return v
	}
	return nil
}

// StrSlice reads a string-array argument: after JSON deserialization it is
// []any, so items are asserted one by one. Non-string items are dropped
// silently -- numbers mixed into model-provided choices should not fail the
// whole call.
func StrSlice(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func IntOf(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		if r, ok := intFromInt64(n); ok {
			return r
		}
	case float64:
		if r, ok := intFromFloat(n); ok {
			return r
		}
	case string:
		var r int
		if _, err := fmt.Sscanf(strings.TrimSpace(n), "%d", &r); err == nil {
			return r
		}
	}
	return 0
}

func EmptyIfNone(out any) (types.StepOutcome, string) {
	m, ok := out.(map[string]any)
	if !ok {
		return types.OutcomeOK, ""
	}
	if IntOf(m["count"]) == 0 {
		return types.OutcomeEmpty, ""
	}
	return types.OutcomeOK, ""
}

func SchemaProp(desc, typ string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

func Schema(desc string, required []string, props map[string]any) map[string]any {
	return map[string]any{
		"type":        "object",
		"description": desc,
		"properties":  props,
		"required":    required,
	}
}

// ValidateArgs checks required fields and declared property types. It is the
// single boundary check shared by planning, task execution and direct calls.
func ValidateArgs(t types.Tool, args map[string]any) error {
	if n, ok := t.(types.ArgNormalizer); ok {
		n.NormalizeArgs(args)
	}
	schema := t.Schema()
	props, _ := schema["properties"].(map[string]any)
	for _, key := range requiredKeys(schema["required"]) {
		v, present := args[key]
		if s, isStr := v.(string); isStr {
			if !present || strings.TrimSpace(s) == "" {
				return fmt.Errorf("缺少必填参数 %q", key)
			}
			continue
		}
		if !present || v == nil {
			return fmt.Errorf("缺少必填参数 %q", key)
		}
	}
	for k, v := range args {
		prop, ok := props[k].(map[string]any)
		if !ok {
			continue
		}
		typ, _ := prop["type"].(string)
		if err := checkType(k, v, typ); err != nil {
			return err
		}
	}
	return nil
}

func requiredKeys(v any) []string {
	switch req := v.(type) {
	case []string:
		return req
	case []any:
		out := make([]string, 0, len(req))
		for _, r := range req {
			if key, ok := r.(string); ok {
				out = append(out, key)
			}
		}
		return out
	default:
		return nil
	}
}

func checkType(key string, v any, typ string) error {
	switch typ {
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("参数 %q 应为字符串", key)
		}
	case "number", "integer":
		switch v.(type) {
		case float64, int64, int:
		default:
			return fmt.Errorf("参数 %q 应为数字", key)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("参数 %q 应为布尔值", key)
		}
	case "array":
		if _, ok := v.([]any); !ok {
			return fmt.Errorf("参数 %q 应为数组", key)
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return fmt.Errorf("参数 %q 应为对象", key)
		}
	}
	return nil
}
