package agent

import (
	"fmt"
	"strings"
	"testing"

	"gleam/internal/harness/registry"
	"gleam/internal/textmatch"
)

func routingRegistry(tools ...*fakeTool) *registry.Registry {
	r := registry.New()
	for _, t := range tools {
		r.MustRegister(t)
	}
	return r
}

func TestRoutingIgnoresManualTail(t *testing.T) {
	const goal = "你好，简单介绍一下你自己"

	tail := &fakeTool{name: "schedule.create", desc: strings.Repeat("说明文字。", 20) + "不要自己拼 cron"}
	p := &Planner{Reg: routingRegistry(tail), MaxToolSchemas: 12}
	if got := p.RelevantTools(goal); len(got) != 0 {
		t.Fatalf("描述尾部的用法提醒不该参与路由，却选到了 %v", got)
	}

	near := &fakeTool{name: "schedule.create", desc: "不要自己拼 cron"}
	p2 := &Planner{Reg: routingRegistry(near), MaxToolSchemas: 12}
	if got := p2.RelevantTools(goal); len(got) == 0 {
		t.Fatal("关键词落在描述开头时必须命中")
	}
}

func TestAlnumTermsMatchWholeWords(t *testing.T) {
	tool := &fakeTool{name: "alpha.tool", desc: "inspect goals field"}
	p := &Planner{Reg: routingRegistry(tool), MaxToolSchemas: 12}

	if got := p.RelevantTools("goal"); len(got) != 0 {
		t.Fatalf("goal 不该命中 goals，实际选到了 %v", got)
	}
	if got := p.RelevantTools("inspect"); len(got) == 0 {
		t.Fatal("整词命中必须生效，否则上面那条只是'永远选不到'")
	}
}

func TestShortAlnumTermsAreDropped(t *testing.T) {
	for _, w := range []string{"go", "in", "is", "id"} {
		if textmatch.Terms(w)[w] {
			t.Errorf("两字母词 %q 不该进入检索词表", w)
		}
	}
	if !textmatch.Terms("goa")["goa"] {
		t.Error("三字母词应当保留")
	}
}

func TestRoutingTextLimitIsEnforced(t *testing.T) {
	long := &fakeTool{name: "x.y", desc: strings.Repeat("甲", 300)}
	got := routingText(long)
	if n, limit := len([]rune(got)), len("x.y")+1+routingTextLimit+1; n > limit {
		t.Fatalf("路由文本没有被截断：%d 字，上限 %d", n, limit)
	}
	if !strings.Contains(got, "…") {
		t.Error("被截断时应当带省略号，排障时一眼能看出截过")
	}

	short := &fakeTool{name: "x.y", desc: "短描述"}
	if strings.Contains(routingText(short), "…") {
		t.Error("没超限就不该出现省略号")
	}
}

func TestRelevantToolsIsNotTheMenu(t *testing.T) {
	p := &Planner{
		Reg:            routingRegistry(&fakeTool{name: "reply", desc: "直接向用户回复文字"}),
		MaxToolSchemas: 12,
	}
	if got := p.SelectTools("随便聊聊"); len(got) != 1 {
		t.Fatalf("菜单里应当有被强制保留的 reply，实际 %v", got)
	}
	if got := p.RelevantTools("随便聊聊"); len(got) != 0 {
		t.Fatalf("打分相关集里不该有 reply（它不参与打分），实际 %v", got)
	}
}

func TestURLGoalKeepsFetchAndCUA(t *testing.T) {
	tools := []*fakeTool{
		{name: "reply", desc: "直接向用户回复文字"},
		{name: "web.fetch", desc: "抓取网页内容并提取正文文本（只读）"},
		{name: "computer_use", desc: "控制桌面应用：截屏、点击屏幕坐标、输入文本、启动应用或打开网址。"},
	}
	for i := 0; i < 15; i++ {
		tools = append(tools, &fakeTool{name: fmt.Sprintf("filler.%02d", i), desc: "与目标无关的工具"})
	}
	p := &Planner{Reg: routingRegistry(tools...), MaxToolSchemas: 12}

	goal := "看看 https://example.com/article 这篇文章讲了什么"
	got := p.SelectTools(goal)
	has := func(name string) bool {
		for _, n := range got {
			if n == name {
				return true
			}
		}
		return false
	}
	if !has("web.fetch") {
		t.Fatalf("目标含 URL 时必须附 web.fetch 的 schema（辅助筛选超时也不能丢），实际 %v", got)
	}
	if !has("computer_use") {
		t.Fatalf("web.fetch 撞反爬墙时的兜底是 computer_use，两者必须同时附上，实际 %v", got)
	}

	plain := p.SelectTools("写一首关于春天的诗")
	for _, n := range plain {
		if n == "web.fetch" {
			t.Fatalf("无 URL 的目标不该强塞 web.fetch，实际 %v", plain)
		}
	}
}

func TestNameHitWeighsMoreThanDescription(t *testing.T) {
	byName := &fakeTool{name: "file.write", desc: "unrelated"}
	byDesc := &fakeTool{name: "zzz.tool", desc: "write text files"}
	p := &Planner{Reg: routingRegistry(byName, byDesc), MaxToolSchemas: 12}

	got := p.RelevantTools("write")
	if len(got) != 2 {
		t.Fatalf("两个工具都该命中，实际 %v", got)
	}
	if got[0] != "file.write" {
		t.Fatalf("名称命中应排在描述命中之前，实际顺序 %v", got)
	}
}
