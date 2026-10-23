package agent

import (
	"context"
	"testing"
	"time"

	"gleam/internal/harness/growth"
	"gleam/internal/harness/skill"
	"gleam/pkg/types"
)

func countType(g *growth.Log, typ string) int {
	n := 0
	for _, e := range g.All() {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func withGrowth(t *testing.T, f *agentFixture) *growth.Log {
	t.Helper()
	g, err := growth.Open(f.a.Cfg.DataDir)
	if err != nil {
		t.Fatalf("growth.Open: %v", err)
	}
	f.a.Growth = g
	return g
}

func oneStepSkill(name string) skill.Skill {
	return skill.Skill{
		Name:        name,
		Description: "钉一个 " + name,
		Steps:       []types.Step{{ID: "s1", Tool: "reply", Args: map[string]any{"text": "ok"}}},
	}
}

func TestSkillGrowth_CreatedFiresOncePerSkill(t *testing.T) {
	f := newFixture(t, nil)
	g := withGrowth(t, f)

	if v, err := f.a.SkillSave(oneStepSkill("alpha")); err != nil || v != 1 {
		t.Fatalf("首存 v%d err%v", v, err)
	}
	if n := countType(g, "skill_created"); n != 1 {
		t.Fatalf("首存应记 1 条，记了 %d", n)
	}
	if v, err := f.a.SkillSave(oneStepSkill("alpha")); err != nil || v != 2 {
		t.Fatalf("改版应 v2，got v%d err%v", v, err)
	}
	if _, err := f.a.SkillSave(oneStepSkill("beta")); err != nil {
		t.Fatal(err)
	}
	if n := countType(g, "skill_created"); n != 2 {
		t.Fatalf("两条技能应共记 2 条，记了 %d", n)
	}
	if v, err := f.a.SkillInstallPreset("quick-note", false); err != nil || v != 1 {
		t.Fatalf("装模板应 v1，got v%d err%v", v, err)
	}
	if n := countType(g, "skill_created"); n != 3 {
		t.Fatalf("装模板也是一条新技能入库，应记 3 条，记了 %d", n)
	}
	if v, err := f.a.SkillInstallPreset("quick-note", true); err != nil || v != 2 {
		t.Fatalf("强制重装应 v2，got v%d err%v", v, err)
	}
	if n := countType(g, "skill_created"); n != 3 {
		t.Fatalf("重装不该再记入库，应仍是 3 条，记了 %d", n)
	}
	if got := g.Stats().TotalSkills; got != 3 {
		t.Errorf("Stats.TotalSkills = %d，want 3", got)
	}
}

func TestSkillGrowth_AutoOptimizeDoesNotRecount(t *testing.T) {
	f := newFixture(t, nil)
	g := withGrowth(t, f)
	f.a.LLM = mockReturning(`{"steps":[
		{"id":"s1","description":"回复","tool":"reply","args":{"text":"v2"}}
	]}`)
	if _, err := f.a.SkillSave(skill.Skill{
		Name:        "broken",
		Description: "指向不存在的工具",
		Steps:       []types.Step{{ID: "s1", Tool: "no-such-tool"}},
	}); err != nil {
		t.Fatal(err)
	}
	if n := countType(g, "skill_created"); n != 1 {
		t.Fatalf("入库应记 1 条，记了 %d", n)
	}
	if _, err := f.a.RunSkill(context.Background(), "broken", nil); err != nil {
		t.Fatal(err)
	}
	if n := countType(g, "skill_used"); n != 0 {
		t.Fatalf("跑坏的技能不该算复用，记了 %d", n)
	}
	if v, _ := f.store.Get("broken"); v.Version != 2 {
		t.Fatalf("自动优化应存 v2，got v%d", v.Version)
	}
	if n := countType(g, "skill_created"); n != 1 {
		t.Fatalf("自动优化不该算新技能入库，应仍是 1 条，记了 %d", n)
	}
}

func TestSkillGrowth_UsedOnlyOnSuccessfulRun(t *testing.T) {
	f := newFixture(t, nil)
	g := withGrowth(t, f)
	if _, err := f.store.Save(skill.Skill{
		Name:        "make-notes",
		Description: "创建笔记",
		Params:      []string{"title"},
		Steps: []types.Step{
			{ID: "s1", Tool: "file.write", Args: map[string]any{"path": "notes/{{title}}.txt", "content": "笔记：{{title}}"}},
			{ID: "s2", Tool: "file.read", Args: map[string]any{"path": "notes/{{title}}.txt"}, DependsOn: []string{"s1"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.RunSkill(context.Background(), "make-notes", nil); err == nil {
		t.Fatal("缺参数应报错")
	}
	if n := countType(g, "skill_used"); n != 0 {
		t.Fatalf("没跑起来不该记复用，记了 %d", n)
	}
	if _, err := f.a.RunSkill(context.Background(), "make-notes", map[string]string{"title": "购物清单"}); err != nil {
		t.Fatal(err)
	}
	entries := g.All()
	if n := countType(g, "skill_used"); n != 1 {
		t.Fatalf("跑通一次应记 1 条，记了 %d（现有 %d 条）", n, len(entries))
	}
	for _, e := range entries {
		if e.Type == "skill_used" {
			if e.SkillName != "make-notes" || e.Steps != 2 {
				t.Errorf("复用记录字段错误: %+v", e)
			}
		}
	}
	if got := g.Stats().SkillUses; got != 1 {
		t.Errorf("Stats.SkillUses = %d，want 1", got)
	}
}

func TestSkillSlug_NilStoreTerminates(t *testing.T) {
	done := make(chan string, 1)
	go func() { done <- skillSlug("整理 项目 文件", nil) }()
	select {
	case got := <-done:
		if got == "" {
			t.Error("技能名不应为空")
		}
	case <-time.After(time.Second):
		t.Fatal("store 为 nil 时不应陷入死循环")
	}
}

func TestSkillSlug_AvoidsExistingAndStaysValid(t *testing.T) {
	st, err := skill.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(oneStepSkill("demo")); err != nil {
		t.Fatal(err)
	}
	if got := skillSlug("demo", st); got == "demo" {
		t.Errorf("已存在的技能名不应重复：%q", got)
	}
	got := skillSlug("写一份周报 🚀", nil)
	for _, r := range got {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || (r >= 0x4e00 && r <= 0x9fff)) {
			t.Fatalf("技能名含非法字符：%q", got)
		}
	}
}
