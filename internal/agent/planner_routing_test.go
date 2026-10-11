package agent

import (
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
