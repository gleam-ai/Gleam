package std

import (
	"context"
	"fmt"
	"testing"
)

func p23Skills() []SkillSummary {
	list := []SkillSummary{
		{Name: "daily-report", Description: "把当天的工作记录整理成日报"},
		{Name: "report-format", Description: "统一日报格式，整理成表格"},
		{Name: "sales-digest", Description: "把销售数据整理成摘要"},
	}
	for i := 0; i < 57; i++ {
		list = append(list, SkillSummary{
			Name:        fmt.Sprintf("filler-%02d", i),
			Description: fmt.Sprintf("第 %d 个无关技能：图片压缩与视频转码", i),
		})
	}
	return list
}

func p23Execute(t *testing.T, args map[string]any) map[string]any {
	t.Helper()
	tool := NewSkillList(&fakeSkillStore{summaries: p23Skills()})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	return out.(map[string]any)
}

func TestSkillList_NoQueryKeepsFullList(t *testing.T) {
	out := p23Execute(t, map[string]any{})
	skills := out["skills"].([]SkillSummary)
	if len(skills) != 60 || out["count"] != 60 {
		t.Fatalf("不带 query 应返回全部 60 条，实际 count=%v len=%d", out["count"], len(skills))
	}
	if _, has := out["total"]; has {
		t.Error("不带 query 时不该出现 total——它是「被筛掉了多少」的提示，没筛就别报")
	}
}

func TestSkillList_QueryFiltersAndKeepsRelevant(t *testing.T) {
	out := p23Execute(t, map[string]any{"query": "把销售数据整理成日报"})
	skills := out["skills"].([]SkillSummary)

	if out["total"] != 60 {
		t.Errorf("total = %v，应为全部 60 个", out["total"])
	}
	if len(skills) == 0 || len(skills) > skillQueryLimit {
		t.Fatalf("应返回 1..%d 条，实际 %d 条", skillQueryLimit, len(skills))
	}
	if out["count"] != len(skills) {
		t.Errorf("count(%v) 应等于实际返回条数(%d)", out["count"], len(skills))
	}

	got := map[string]bool{}
	for _, s := range skills {
		got[s.Name] = true
	}
	for _, want := range []string{"daily-report", "report-format", "sales-digest"} {
		if !got[want] {
			t.Errorf("相关技能 %q 应被筛出来，实际 %v", want, skills)
		}
	}
	for _, s := range skills {
		if s.Name != "daily-report" && s.Name != "report-format" && s.Name != "sales-digest" {
			t.Errorf("无关技能 %q 不该出现在结果里", s.Name)
		}
	}
	if _, has := out["note"]; !has {
		t.Error("筛选后应给出 note，否则模型不知道列表被筛过、也看不到全部")
	}
}

func TestSkillList_QueryNoMatchReturnsEmpty(t *testing.T) {
	out := p23Execute(t, map[string]any{"query": "量子纠缠校准"})
	skills := out["skills"].([]SkillSummary)
	if len(skills) != 0 {
		t.Errorf("无匹配时不该补齐，实际返回 %v", skills)
	}
	if out["count"] != 0 {
		t.Errorf("count = %v，应为 0", out["count"])
	}
	if out["total"] != 60 {
		t.Errorf("total = %v，应仍报全部 60 个（否则看起来像技能库是空的）", out["total"])
	}
}

func TestSkillList_QueryLimitIsClamped(t *testing.T) {
	args := map[string]any{"query": "无关技能"}
	out := p23Execute(t, args)
	if n := len(out["skills"].([]SkillSummary)); n != skillQueryLimit {
		t.Errorf("默认应返回 %d 条，实际 %d", skillQueryLimit, n)
	}

	args = map[string]any{"query": "无关技能", "limit": 3}
	if n := len(p23Execute(t, args)["skills"].([]SkillSummary)); n != 3 {
		t.Errorf("limit=3 应返回 3 条，实际 %d", n)
	}

	args = map[string]any{"query": "无关技能", "limit": 9999}
	if n := len(p23Execute(t, args)["skills"].([]SkillSummary)); n != skillQueryMaxLimit {
		t.Errorf("limit 超上限应夹到 %d，实际 %d", skillQueryMaxLimit, n)
	}

	args = map[string]any{"query": "无关技能", "limit": 0}
	if n := len(p23Execute(t, args)["skills"].([]SkillSummary)); n != skillQueryLimit {
		t.Errorf("limit=0 应回到默认 %d，实际 %d", skillQueryLimit, n)
	}
}

func TestRankSkills_OrdersByScore(t *testing.T) {
	list := []SkillSummary{
		{Name: "z-other", Description: "日报"},
		{Name: "daily-report", Description: "日报生成"},
	}
	got := rankSkills("日报", list)
	if len(got) != 2 {
		t.Fatalf("应两条都命中，实际 %v", got)
	}
	if got[0].Name != "daily-report" {
		t.Errorf("名称命中的应排前面，实际 %v", got)
	}
}
