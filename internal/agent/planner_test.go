package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gleam/internal/config"
	"gleam/internal/llm"
)

func testCtx() context.Context { return context.Background() }

func TestValidateFallbackSharesConfigDefault(t *testing.T) {
	p := &Planner{Reg: menuRegistry(2)}

	planJSON := func(n int) string {
		ids := make([]string, 0, n)
		for i := 0; i < n; i++ {
			ids = append(ids, fmt.Sprintf(`{"id":"s%d","tool":"reply","args":{"text":"x"}}`, i+1))
		}
		return `{"steps":[` + strings.Join(ids, ",") + `]}`
	}

	if _, err := p.validate([]byte(planJSON(config.DefaultMaxSteps)), "g"); err != nil {
		t.Fatalf("恰好 %d 步应当通过，实际 %v", config.DefaultMaxSteps, err)
	}
	_, err := p.validate([]byte(planJSON(config.DefaultMaxSteps+1)), "g")
	if err == nil {
		t.Fatalf("超过 %d 步应当被拒", config.DefaultMaxSteps)
	}
	if !strings.Contains(err.Error(), fmt.Sprint(config.DefaultMaxSteps)) {
		t.Errorf("报错里应写出上限 %d（说明用的是共享常量），实际 %v", config.DefaultMaxSteps, err)
	}
}

func TestPromptStatesTheStepLimitItEnforces(t *testing.T) {
	planJSON := func(n int) string {
		steps := make([]string, 0, n)
		for i := 0; i < n; i++ {
			steps = append(steps, fmt.Sprintf(`{"id":"s%d","tool":"reply","args":{"text":"x"}}`, i+1))
		}
		return `{"steps":[` + strings.Join(steps, ",") + `]}`
	}
	// limit=6 mirrors the subtask cap; limit=0 falls back to the config default.
	for _, limit := range []int{6, 0} {
		p := &Planner{Reg: menuRegistry(2), MaxSteps: limit}
		want := p.stepLimit()
		if prompt := p.SystemPromptFor("g", "."); !strings.Contains(prompt, fmt.Sprintf("最多 %d 个步骤", want)) {
			t.Errorf("MaxSteps=%d：提示词应告知上限 %d，实际未找到", limit, want)
		}
		if _, err := p.validate([]byte(planJSON(want)), "g"); err != nil {
			t.Errorf("MaxSteps=%d：恰好 %d 步应通过，实际 %v", limit, want, err)
		}
		if _, err := p.validate([]byte(planJSON(want+1)), "g"); err == nil {
			t.Errorf("MaxSteps=%d：%d 步应被拒", limit, want+1)
		}
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1}`, `{"a":1}`},
		{"```json\n{\"a\": 1}\n```", `{"a": 1}`},
		{"前置说明 {\"a\":{\"b\":\"}\"}} 后缀", `{"a":{"b":"}"}}`},
		{"{\"text\":\"引号\\\"与括号{后继续\"}", `{"text":"引号\"与括号{后继续"}`},
	}
	for i, c := range cases {
		got, err := extractJSON(c.in)
		if err != nil {
			t.Errorf("case %d: %v", i, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("case %d: got %q want %q", i, got, c.want)
		}
	}
	if _, err := extractJSON("没有 JSON"); err == nil {
		t.Error("应报错")
	}
	if _, err := extractJSON("{\"未闭合\": 1"); err == nil {
		t.Error("未闭合应报错")
	}
}

func TestPlanner_ValidPlan(t *testing.T) {
	p := &Planner{
		LLM: mockReturning(`{"steps":[
			{"id":"s1","description":"列目录","tool":"echo","args":{"in":"x"}},
			{"id":"s2","description":"回复","tool":"reply","args":{"text":"完成"},"depends_on":["s1"]}
		],"estimated_time":"short"}`),
		Reg: newTestRegistry(),
	}
	plan, err := p.Plan(testCtx(), "目标", "/ws", nil, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 || plan.Steps[1].DependsOn[0] != "s1" {
		t.Errorf("plan = %+v", plan)
	}
	if plan.Steps[0].ID != "s1" || plan.Steps[0].Tool != "echo" {
		t.Errorf("step0 = %+v", plan.Steps[0])
	}
}

func TestPlanner_AutoAssignIDs(t *testing.T) {
	p := &Planner{
		LLM: mockReturning(`{"steps":[
			{"tool":"echo","args":{"in":"a"}},
			{"tool":"echo","args":{"in":"b"}}
		]}`),
		Reg: newTestRegistry(),
	}
	plan, err := p.Plan(testCtx(), "g", "", nil, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].ID != "s1" || plan.Steps[1].ID != "s2" {
		t.Errorf("自动 ID = %s,%s", plan.Steps[0].ID, plan.Steps[1].ID)
	}
}

func TestPlanner_ValidationFailures(t *testing.T) {
	cases := []struct {
		name   string
		output string
		expect string
	}{
		{"非JSON", "抱歉我无法输出 JSON", "JSON"},
		{"未知工具", `{"steps":[{"id":"s1","tool":"nope"}]}`, "不存在"},
		{"缺必填参数", `{"steps":[{"id":"s1","tool":"echo","args":{}}]}`, "缺少"},
		{"空步骤", `{"steps":[]}`, "没有步骤"},
		{"循环依赖", `{"steps":[{"id":"s1","tool":"echo","args":{"in":"a"},"depends_on":["s2"]},{"id":"s2","tool":"echo","args":{"in":"b"},"depends_on":["s1"]}]}`, "循环"},
		{"自依赖", `{"steps":[{"id":"s1","tool":"echo","args":{"in":"a"},"depends_on":["s1"]}]}`, "自身"},
		{"依赖不存在", `{"steps":[{"id":"s1","tool":"echo","args":{"in":"a"},"depends_on":["s9"]}]}`, "不存在"},
		{"类型错误", `{"steps":[{"id":"s1","tool":"echo","args":{"in":123}}]}`, "字符串"},
	}
	for _, c := range cases {
		p := &Planner{LLM: mockReturning(c.output), Reg: newTestRegistry()}
		_, err := p.Plan(testCtx(), "g", "", nil, nil, "", "")
		if err == nil {
			t.Errorf("%s: 应报错", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.expect) {
			t.Errorf("%s: 错误 %q 不含 %q", c.name, err.Error(), c.expect)
		}
	}
}

func TestPlanner_RejectsReferenceGraphs(t *testing.T) {
	for _, tc := range []struct {
		name, steps, want string
	}{
		{"exact self", `[{"id":"s1","tool":"reply","args":{"text":"$ref:s1.text"}}]`, "自身"},
		{"interpolated self", `[{"tool":"reply","args":{"text":"before {ref:s1.text} after"}}]`, "自身"},
		{"nested cycle", `[{"id":"s1","tool":"reply","args":{"nested":[{"text":"$ref:s2.text"}]}},{"id":"s2","tool":"reply","args":{"text":"{ref:s1.text}"}}]`, "循环"},
		{"mixed cycle", `[{"id":"s1","tool":"reply","depends_on":["s2"]},{"id":"s2","tool":"reply","args":{"text":"$ref:s1.text"}}]`, "循环"},
		{"unknown ref", `[{"id":"s1","tool":"reply","args":{"text":"{ref:missing.text}"}}]`, "不存在"},
		{"duplicate default", `[{"tool":"reply"},{"id":"s1","tool":"reply"}]`, "重复"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Planner{Reg: newTestRegistry()}
			_, err := p.validate([]byte(`{"steps":`+tc.steps+`}`), "reference graph")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPlanner_FencedOutput(t *testing.T) {
	out := "好的，下面是计划：\n```json\n{\"steps\":[{\"id\":\"s1\",\"tool\":\"reply\",\"args\":{\"text\":\"hi\"}}]}\n```\n以上。"
	p := &Planner{LLM: mockReturning(out), Reg: newTestRegistry()}
	plan, err := p.Plan(testCtx(), "g", "", nil, nil, "", "")
	if err != nil {
		t.Fatalf("应容忍代码块: %v", err)
	}
	if plan.Steps[0].Tool != "reply" {
		t.Error("步骤错误")
	}
}

// eval A3: planning must stream so a thinking model's long generation is not
// cut off by a fixed non-streaming deadline. The progress callback is wired by
// the caller but only fires via ChatStream — non-streaming Chat never invokes
// it, so a zero delta count means planning regressed to the blocking path.
func TestPlanner_StreamsProgress(t *testing.T) {
	out := "```json\n{\"steps\":[{\"id\":\"s1\",\"tool\":\"reply\",\"args\":{\"text\":\"hi\"}}]}\n```"
	var deltaChars int
	p := &Planner{LLM: mockReturning(out), Reg: newTestRegistry(), OnLLMDelta: func(s string) {
		deltaChars += len([]rune(s))
	}}
	plan, err := p.Plan(testCtx(), "g", "", nil, nil, "", "")
	if err != nil {
		t.Fatalf("流式规划应成功: %v", err)
	}
	if deltaChars == 0 {
		t.Error("规划应经 ChatStream 触发 OnLLMDelta 进度（非流式 Chat 不会调用它）")
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Tool != "reply" {
		t.Errorf("计划解析错误: %+v", plan.Steps)
	}
}

type mockOneShot struct{ text string }

func (m *mockOneShot) Chat(_ context.Context, _ llm.ChatRequest) (string, error) { return m.text, nil }
func (m *mockOneShot) ChatStream(_ context.Context, _ llm.ChatRequest, onDelta func(string)) (string, error) {
	if onDelta != nil {
		onDelta(m.text)
	}
	return m.text, nil
}
func (m *mockOneShot) Name() string { return "mock-one-shot" }

func mockReturning(text string) *mockOneShot { return &mockOneShot{text: text} }

type mockCaptureReq struct {
	text string
	got  llm.ChatRequest
}

func (m *mockCaptureReq) Chat(_ context.Context, r llm.ChatRequest) (string, error) {
	m.got = r
	return m.text, nil
}
func (m *mockCaptureReq) ChatStream(_ context.Context, r llm.ChatRequest, onDelta func(string)) (string, error) {
	m.got = r
	if onDelta != nil {
		onDelta(m.text)
	}
	return m.text, nil
}
func (m *mockCaptureReq) Name() string { return "mock-capture" }

// eval probe: the 4096 default is spent on reasoning and yields 0 steps; only
// thinking mode raises the planning budget, so small-output models keep it.
func TestPlanner_ThinkingRaisesPlanBudget(t *testing.T) {
	out := `{"steps":[{"id":"s1","tool":"reply","args":{"text":"hi"}}]}`
	for _, tc := range []struct {
		thinking bool
		want     int
	}{
		{true, thinkingPlanMaxTokens},
		{false, 0},
	} {
		m := &mockCaptureReq{text: out}
		p := &Planner{LLM: m, Reg: newTestRegistry(), Thinking: tc.thinking}
		if _, err := p.Plan(testCtx(), "g", "", nil, nil, "", ""); err != nil {
			t.Fatalf("thinking=%v: 规划应成功: %v", tc.thinking, err)
		}
		if m.got.MaxTokens != tc.want {
			t.Errorf("thinking=%v: 规划 MaxTokens=%d，期望 %d", tc.thinking, m.got.MaxTokens, tc.want)
		}
	}
}
