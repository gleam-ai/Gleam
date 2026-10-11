package agent

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"gleam/internal/llm"
	"gleam/internal/tools/std"
)

func TestScheduleSectionIsCompact(t *testing.T) {
	sec := renderRules(SlotSchedule, "work", "", "")
	if n := len(sec); n > 300 {
		t.Errorf("定时段膨胀到 %d 字节（上限 300）——细节该放 schedule.create 的说明里，别搬回稳定段", n)
	}
	if !strings.Contains(sec, "## 定时/周期任务") {
		t.Error("段落标题不能改：就绪体检与缓存布局测试都按这个标记定位稳定段")
	}
	if !strings.Contains(sec, "不要只在回复里答应") {
		t.Error("「不要只在回复里答应」不能删——它是这段唯一不可替代的行为约束")
	}
}

func TestScheduleGuidanceNotLost(t *testing.T) {
	tool := std.NewScheduleCreate(nil)
	visible := renderRules(SlotSchedule, "work", "", "") + "\n" + tool.Description() + "\n" + fmt.Sprint(tool.Schema())

	for _, want := range []struct{ what, needle string }{
		{"触发条件「每天」", "每天"},
		{"触发条件「到点提醒我」", "到点提醒我"},
		{"行为约束「不要只在回复里答应」", "不要只在回复里答应"},
		{"时间放 when", "when"},
		{"不要自己拼 cron", "不要自己拼 cron"},
		{"name 必填", "name"},
		{"goal 必填", "goal"},
		{"创建后用 reply 确认", "reply"},
	} {
		if !strings.Contains(visible, want.needle) {
			t.Errorf("%s（%q）在提示词段和工具说明里都找不到——减法把信息弄丢了", want.what, want.needle)
		}
	}
}

func TestScheduleDetailStaysOutOfMenu(t *testing.T) {
	line := firstLine(std.NewScheduleCreate(nil).Description())
	if n := utf8.RuneCountInString(line); n > 61 {
		t.Errorf("菜单行 %d 字，超出「60 字 + 省略号」的约定", n)
	}
	for _, detail := range []string{"到点提醒我", "不要自己拼 cron", "reply", "name 取简短"} {
		if strings.Contains(line, detail) {
			t.Errorf("细节 %q 混进了能力菜单（稳定段）——等于没做减法", detail)
		}
	}
	if !strings.Contains(line, "定时") {
		t.Error("菜单行应保留「定时」，否则模型无法从菜单发现这个工具")
	}
}

func TestScheduleSectionStaysInStablePrefix(t *testing.T) {
	p := &Planner{Reg: bigRegistry(25), MaxToolSchemas: 12, Role: "analyst", TaskMode: "work", Style: "efficient"}
	a := p.buildSystemPrompt("把上季度销售数据整理成表格并汇总", "D:/ws", nil, nil, "", "")
	b := p.buildSystemPrompt("帮我写一封给客户的道歉邮件", "D:/ws/sub", nil, nil, "", "")

	stable, _, _ := llm.SplitCacheBoundary(a, b)
	if !strings.Contains(stable, "## 定时/周期任务") {
		t.Error("定时段应仍落在跨请求公共前缀内")
	}
	if !strings.Contains(stable, "不要只在回复里答应") {
		t.Error("定时段的行为约束应仍落在公共前缀内")
	}
}
