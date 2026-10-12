package agent

import (
	"strings"
	"testing"

	"gleam/internal/harness/memory"
	"gleam/internal/harness/registry"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

func bigRegistry(n int) *registry.Registry {
	r := registry.New()
	r.MustRegister(&funcTool{name: "reply", perm: types.PermissionReadOnly})
	r.MustRegister(&funcTool{name: "schedule.create", perm: types.PermissionReadOnly})
	for i := 0; i < n; i++ {
		name := "tool" + string(rune('a'+i%26))
		if i >= 26 {
			name = "toolx" + string(rune('a'+i%26))
		}
		r.MustRegister(&funcTool{name: name, perm: types.PermissionReadOnly})
	}
	return r
}

func TestPromptCache_StablePrefixSurvivesGoalChange(t *testing.T) {
	p := &Planner{Reg: bigRegistry(25), MaxToolSchemas: 12, Role: "analyst", TaskMode: "work", Style: "efficient"}
	a := p.buildSystemPrompt("把销售数据整理成表格并汇总", "D:/ws", nil, nil, "", "")
	b := p.buildSystemPrompt("帮我写一封给客户的道歉邮件", "D:/ws", nil, nil, "", "")

	stable, _, _ := llm.SplitCacheBoundary(a, b)
	n := llm.StablePrefixLen(a, b)

	for _, want := range []string{
		"## 输出格式",
		"## 规划规则",
		"## 验收标准（acceptance）",
		"## 能力菜单",
		"## 定时/周期任务",
		"## 专家角色：数据分析师",
	} {
		if !strings.Contains(stable, want) {
			t.Errorf("稳定段应覆盖 %q，实际公共前缀只有 %d 字", want, n)
		}
	}
	if strings.Contains(stable, "把销售数据整理成表格") || strings.Contains(stable, "道歉邮件") {
		t.Error("目标属于易变内容，不该出现在公共前缀里")
	}
	if n < llm.CacheFriendlyMinChars {
		t.Errorf("公共前缀只有 %d 字，低于值得缓存的量级（%d）——布局被易变内容打断了",
			n, llm.CacheFriendlyMinChars)
	}
	t.Logf("换目标后公共前缀 %d 字（提示词全长 %d / %d）", n, len([]rune(a)), len([]rune(b)))
}

func TestPromptCache_VolatileContentNeverBreaksPrefix(t *testing.T) {
	p := &Planner{Reg: bigRegistry(25), MaxToolSchemas: 12, Role: "coder", TaskMode: "code", Style: "efficient"}
	recentA := []memory.Turn{{Role: "user", Content: "上次聊到执行器"}}
	recentB := []memory.Turn{{Role: "user", Content: "另一次完全不同的对话"}, {Role: "assistant", Content: "好的"}}
	a := p.buildSystemPrompt("重构执行器", "D:/ws", recentA, []memory.Hit{{Content: "偏好最小 diff"}}, "摘要甲", "")
	b := p.buildSystemPrompt("重构执行器", "E:/另一个工作区", recentB, nil, "摘要乙", "上次计划无效，请修正")

	stable, _, _ := llm.SplitCacheBoundary(a, b)
	if !strings.Contains(stable, "## 输出格式") || !strings.Contains(stable, "## 编程模式准则") {
		t.Errorf("稳定段被易变内容打断了，实际前缀：%.60s", stable)
	}
	for _, volatile := range []string{
		"D:/ws", "E:/另一个工作区",
		"摘要甲", "摘要乙",
		"上次聊到执行器", "另一次完全不同的对话",
		"偏好最小 diff", "上次计划无效",
	} {
		if strings.Contains(stable, volatile) {
			t.Errorf("易变内容 %q 混进了公共前缀", volatile)
		}
	}
	if n := llm.StablePrefixLen(a, b); n < llm.CacheFriendlyMinChars {
		t.Errorf("公共前缀只有 %d 字，低于值得缓存的量级（%d）", n, llm.CacheFriendlyMinChars)
	}
}

func TestPromptCache_SchemaSectionStaysVolatile(t *testing.T) {
	p := &Planner{Reg: bigRegistry(25), MaxToolSchemas: 12, Role: "coder"}
	a := p.buildSystemPrompt("做点什么", "D:/ws", nil, nil, "", "")
	q := *p
	q.ForceTools = []string{"reply"}
	b := q.buildSystemPrompt("做点什么", "D:/ws", nil, nil, "", "")

	stable, _, _ := llm.SplitCacheBoundary(a, b)
	if !strings.Contains(stable, "## 能力菜单") {
		t.Error("能力菜单不随任务历史变化，应留在稳定段里")
	}
	if strings.Contains(stable, "本任务此前已用过") {
		t.Error("随任务历史变化的 schema 段不该混进稳定段")
	}
}

func TestPromptCache_FullInjectionIsGoalIndependent(t *testing.T) {
	p := &Planner{Reg: bigRegistry(25), MaxToolSchemas: 0, Role: "general", TaskMode: "work", Style: "efficient"}
	a := p.buildSystemPrompt("把销售数据整理成表格并汇总", "D:/ws", nil, nil, "", "")
	b := p.buildSystemPrompt("帮我写一封给客户的道歉邮件", "D:/ws", nil, nil, "", "")

	if a != b {
		n := llm.StablePrefixLen(a, b)
		t.Errorf("全量注入下换目标应得到完全相同的提示词，实际公共前缀只有 %d 字（总长 %d/%d）——"+
			"schema 段混进了随目标变化的内容，可缓存前缀被打断",
			n, len([]rune(a)), len([]rune(b)))
	}
	if strings.Contains(a, "## 能力菜单") {
		t.Error("全量注入时不应再输出能力菜单（重复内容，且会把稳定段撑长）")
	}
}

func TestPromptCache_IdentityComesFirst(t *testing.T) {
	p := &Planner{Reg: bigRegistry(25), MaxToolSchemas: 12, Role: "writer"}
	got := p.buildSystemPrompt("随便写点什么", "D:/ws", nil, nil, "摘要", "反馈")
	if !strings.HasPrefix(got, promptIdentity) {
		t.Errorf("提示词应以稳定的身份段开头，实际开头是：%.60s", got)
	}
}

func TestPromptCache_OutputReminderAtEnd(t *testing.T) {
	p := &Planner{Reg: bigRegistry(25), MaxToolSchemas: 12}
	got := strings.TrimSpace(p.buildSystemPrompt("做点什么", "D:/ws", nil, nil, "", ""))
	if !strings.HasSuffix(got, "不要任何其他文字、不要代码块。") {
		t.Errorf("末尾应保留输出格式提醒，实际结尾：%.80s", got[len(got)-80:])
	}
}

func TestPromptCache_ReflectorStableInstructionFirst(t *testing.T) {
	acc := []string{"文件里有 3 行", "合计正确"}
	execA := &Result{Total: 1, Succeeded: 1, Steps: []types.StepResult{{StepID: "s1", Tool: "file.read", Status: types.StepSucceeded}}}
	execB := &Result{Total: 2, Succeeded: 2, Steps: []types.StepResult{{StepID: "s1", Tool: "shell.exec", Status: types.StepSucceeded}}}
	a := reflectorSystemPrompt(true, buildDigest("目标甲", execA, 1, acc, nil))
	b := reflectorSystemPrompt(true, buildDigest("目标乙", execB, 2, acc, nil))

	stable, restA, _ := llm.SplitCacheBoundary(a, b)
	if !strings.Contains(stable, "## 安全约定") || !strings.Contains(stable, "## 判定要求") {
		t.Errorf("反思器的稳定段应包含安全约定与判定指令，实际前缀：%.80s", stable)
	}
	if strings.Contains(stable, "目标甲") || strings.Contains(stable, "目标乙") {
		t.Error("目标文本属于易变内容，不该出现在公共前缀里")
	}
	if strings.Contains(stable, "file.read") || strings.Contains(stable, "shell.exec") {
		t.Error("执行结果属于易变内容，不该出现在公共前缀里")
	}
	if !strings.Contains(restA, "file.read") {
		t.Error("易变段应包含本次的执行结果")
	}
	if n := llm.StablePrefixLen(a, b); n < 400 {
		t.Errorf("反思器公共前缀只有 %d 字，稳定段偏短", n)
	}
}
