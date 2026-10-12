package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/pkg/types"
)

func TestFitRunes_KeepsHeadAndTail(t *testing.T) {
	head := strings.Repeat("甲", 400)
	tail := strings.Repeat("乙", 400)
	src := head + strings.Repeat("丙", 5000) + tail

	got := (&Executor{MaxOutputRunes: 1000}).fitRunes(src, "t1", "s1")
	if len([]rune(got)) > 1100 {
		t.Fatalf("截断后仍过长：%d 字", len([]rune(got)))
	}
	if !strings.Contains(got, "已截断") || !strings.Contains(got, "5800") {
		t.Errorf("应说明被截断及原始长度，实际片段：%s", got[len(got)-200:])
	}
	if !strings.HasPrefix(got, head) {
		t.Error("应保留开头")
	}
	if !strings.HasSuffix(got, string([]rune(tail)[100:])) {
		t.Error("应保留结尾")
	}
}

func TestFitRunes_ShortUnchanged(t *testing.T) {
	src := "很短的一段输出"
	if got := (&Executor{MaxOutputRunes: 6000}).fitRunes(src, "t1", "s1"); got != src {
		t.Fatalf("应原样返回，实际 %q", got)
	}
}

func TestCapOutput_TypesPreserved(t *testing.T) {
	small := map[string]any{"n": 1, "entries": []any{"a", "b"}}
	got := (&Executor{MaxOutputRunes: 6000}).capOutput(small, "t1", "s1")
	if _, ok := got.(map[string]any); !ok {
		t.Errorf("小结果应保持原始类型，实际 %T", got)
	} else if !reflect.DeepEqual(got, small) {
		t.Errorf("小结果应原样返回，实际 %#v", got)
	}
	if got := (&Executor{MaxOutputRunes: 0}).capOutput("abc", "t1", "s1"); got != "abc" {
		t.Errorf("预算为 0 时不应改动，实际 %#v", got)
	}
}

func TestCapOutput_LargeStructBecomesTruncatedText(t *testing.T) {
	entries := make([]any, 0, 500)
	for i := 0; i < 500; i++ {
		entries = append(entries, strings.Repeat("数据", 20))
	}
	got, ok := (&Executor{MaxOutputRunes: 1000}).capOutput(map[string]any{"entries": entries}, "t1", "s1").(string)
	if !ok {
		t.Fatal("超限的结构化输出应退化为字符串")
	}
	if len([]rune(got)) > 1100 {
		t.Fatalf("仍过长：%d 字", len([]rune(got)))
	}
	if !strings.Contains(got, "已截断") {
		t.Error("应标记已截断，避免模型误以为看到全貌")
	}
}

func TestExecutor_RefSubstitutionCapped(t *testing.T) {
	r := registry.New()
	r.MustRegister(&funcTool{name: "bigout", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, _ map[string]any) (any, error) {
			return strings.Repeat("很长的文件内容", 2000), nil
		}})
	r.MustRegister(&funcTool{name: "reply", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, args map[string]any) (any, error) {
			return map[string]any{"text": args["text"]}, nil
		}})

	e := &Executor{
		Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, StepTimeout: 2 * time.Second,
		MaxOutputRunes: 1000,
	}
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "bigout"},
		{ID: "s2", Tool: "reply", Args: map[string]any{"text": "内容：{ref:s1}"}, DependsOn: []string{"s1"}},
	}}
	res := e.Execute(context.Background(), plan, "t1", string(types.ModeAuto), false)
	if res.Succeeded != 2 {
		t.Fatalf("两步都应成功: %+v", res.Steps)
	}
	text, _ := res.ByID["s2"].FinalArgs["text"].(string)
	if got := len([]rune(text)); got > 1100 {
		t.Fatalf("注入下游的参数应被截断，实际 %d 字", got)
	}
	if !strings.Contains(text, "已截断") {
		t.Error("下游参数里应带截断说明，否则模型会以为拿到了全文")
	}
	full, _ := res.ByID["s1"].Output.(string)
	if len([]rune(full)) < 10000 {
		t.Errorf("步骤原始输出不应被裁剪，实际 %d 字", len([]rune(full)))
	}
}

func TestExecutor_RefSubstitutionUncappedWhenUnlimited(t *testing.T) {
	r := registry.New()
	r.MustRegister(&funcTool{name: "bigout", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, _ map[string]any) (any, error) {
			return strings.Repeat("长内容", 3000), nil
		}})
	r.MustRegister(&funcTool{name: "reply", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, args map[string]any) (any, error) {
			return map[string]any{"text": args["text"]}, nil
		}})
	e := &Executor{
		Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, StepTimeout: 2 * time.Second,
	}
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "bigout"},
		{ID: "s2", Tool: "reply", Args: map[string]any{"text": "{ref:s1}"}, DependsOn: []string{"s1"}},
	}}
	res := e.Execute(context.Background(), plan, "t2", string(types.ModeAuto), false)
	text, _ := res.ByID["s2"].FinalArgs["text"].(string)
	if strings.Contains(text, "已截断") {
		t.Error("预算为 0 时不应截断")
	}
}
