package types

import (
	"fmt"
	"sort"
	"strings"
)

// ValidateStepGraph checks explicit dependencies and argument references together.
// Callers assign default IDs before validation; the steps are not modified.
func ValidateStepGraph(steps []Step) error {
	deps := make(map[string][]string, len(steps))
	for _, st := range steps {
		if st.ID == "" {
			return fmt.Errorf("步骤 ID 不能为空")
		}
		if _, exists := deps[st.ID]; exists {
			return fmt.Errorf("步骤 ID 重复: %s", st.ID)
		}
		deps[st.ID] = append(append([]string(nil), st.DependsOn...), argumentDependencies(st.Args)...)
	}
	for _, st := range steps {
		for _, dep := range deps[st.ID] {
			if dep == st.ID {
				return fmt.Errorf("步骤 %s 依赖自身", st.ID)
			}
			if _, exists := deps[dep]; !exists {
				return fmt.Errorf("步骤 %s 依赖了不存在的步骤 %q", st.ID, dep)
			}
		}
	}
	const (
		white = iota
		gray
		black
	)
	color := make(map[string]int, len(steps))
	var visit func(string, []string) error
	visit = func(id string, stack []string) error {
		color[id] = gray
		stack = append(stack, id)
		for _, dep := range deps[id] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("计划存在循环依赖: %s", strings.Join(append(stack, dep), " → "))
			case white:
				if err := visit(dep, stack); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}
	for _, st := range steps {
		if color[st.ID] == white {
			if err := visit(st.ID, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func argumentDependencies(v any) []string {
	var deps []string
	add := func(ref string) {
		id, _, _ := strings.Cut(ref, ".")
		deps = append(deps, id)
	}
	switch v := v.(type) {
	case string:
		if strings.HasPrefix(v, "$ref:") && len(v) > len("$ref:") {
			add(v[len("$ref:"):])
			return deps
		}
		for {
			start := strings.Index(v, "{ref:")
			if start < 0 {
				break
			}
			end := strings.Index(v[start:], "}")
			if end < 0 {
				break
			}
			end += start
			add(v[start+len("{ref:") : end])
			v = v[end+1:]
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			deps = append(deps, argumentDependencies(v[k])...)
		}
	case []any:
		for _, item := range v {
			deps = append(deps, argumentDependencies(item)...)
		}
	}
	return deps
}
