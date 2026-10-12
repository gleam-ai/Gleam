package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gleam/internal/harness/growth"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

func TestRuleSetVersion_IsStableAndShort(t *testing.T) {
	a, b := RuleSetVersion(), RuleSetVersion()
	if a != b {
		t.Fatalf("同一份规则表两次算出不同指纹：%q vs %q", a, b)
	}
	if len(a) != 10 {
		t.Fatalf("指纹应为 10 位，实际 %q（%d 位）", a, len(a))
	}
	if strings.Trim(a, "0123456789abcdef") != "" {
		t.Fatalf("指纹应为小写十六进制，实际 %q", a)
	}
}

func TestRuleSetVersion_SensitiveToEveryField(t *testing.T) {
	base := RuleSetVersion()
	mutations := []struct {
		name string
		mod  func(rs []Rule) []Rule
	}{
		{"改条目正文", func(rs []Rule) []Rule {
			i := ruleIndex(rs, "output.rules")
			items := append([]string(nil), rs[i].Items...)
			items[0] += "（多一个字）"
			rs[i].Items = items
			return rs
		}},
		{"改标题", func(rs []Rule) []Rule {
			i := ruleIndex(rs, "output.rules")
			rs[i].Title += "（改了标题）"
			return rs
		}},
		{"改适用范围", func(rs []Rule) []Rule {
			i := ruleIndex(rs, "schedule.hint")
			rs[i].Scope = Scope{Modes: []string{"code"}}
			return rs
		}},
		{"改槽位", func(rs []Rule) []Rule {
			i := ruleIndex(rs, "schedule.hint")
			rs[i].Slot = SlotContract
			return rs
		}},
		{"改规则 ID", func(rs []Rule) []Rule {
			i := ruleIndex(rs, "code.diff")
			rs[i].ID = "code.diff2"
			return rs
		}},
		{"新增一条规则", func(rs []Rule) []Rule {
			return append(rs, Rule{ID: "tmp.probe", Slot: SlotContract, Items: []string{"探针"}})
		}},
		{"删掉一条规则", func(rs []Rule) []Rule {
			out := append([]Rule(nil), rs...)
			return out[:len(out)-1]
		}},
		{"调换渲染顺序", func(rs []Rule) []Rule {
			out := append([]Rule(nil), rs...)
			out[0], out[1] = out[1], out[0]
			return out
		}},
	}
	for _, m := range mutations {
		orig := stableRules
		cp := append([]Rule(nil), orig...)
		stableRules = m.mod(cp)
		got := RuleSetVersion()
		stableRules = orig
		if got == base {
			t.Errorf("%s：指纹没变（%s），这条改动会悄悄逃过归因", m.name, got)
		}
	}
	if RuleSetVersion() != base {
		t.Fatal("变异后规则表没有复原")
	}
}

func ruleIndex(rs []Rule, id string) int {
	for i, r := range rs {
		if r.ID == id {
			return i
		}
	}
	panic("测试写错了：规则表里没有 " + id)
}

func TestActiveRuleIDs_ConsistentWithRenderedPrompt(t *testing.T) {
	scenarios := []struct {
		mode, tier, role string
	}{
		{"work", "", ""},
		{"work", TierOffice, "writer"},
		{"work", TierReasoning, "analyst"},
		{"code", "", "coder"},
		{"code", TierEconomy, "coder"},
		{"code", TierCoding, "coder"},
		{"code", TierReasoning, "ops"},
	}
	for _, sc := range scenarios {
		rendered := map[string]string{}
		for _, slot := range []string{SlotContract, SlotCode, SlotSchedule} {
			rendered[slot] = renderRules(slot, sc.mode, sc.tier, sc.role)
		}

		chosen := map[string]bool{}
		var want []string
		for _, r := range stableRules {
			if chosen[r.ID] || !r.Scope.allows(sc.mode, sc.tier, sc.role) {
				continue
			}
			chosen[r.ID] = true
			want = append(want, r.ID)
		}

		got := ActiveRuleIDs(sc.mode, sc.tier, sc.role)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("场景 %+v：生效 ID 为 [%s]，期望 [%s]", sc, strings.Join(got, " "), strings.Join(want, " "))
		}

		seen := map[string]bool{}
		for _, r := range stableRules {
			if seen[r.ID] {
				continue
			}
			hit := r.Scope.allows(sc.mode, sc.tier, sc.role)
			if hit {
				seen[r.ID] = true
			}
			inPrompt := strings.Contains(rendered[r.Slot], r.Items[0])
			if inPrompt != hit {
				t.Errorf("场景 %+v：规则 %s 的记录与渲染不一致（记为生效=%v，正文在提示词里=%v）",
					sc, r.ID, hit, inPrompt)
			}
		}
	}
}

func TestActiveRuleIDs_CannotDistinguishVariants(t *testing.T) {
	withTier := ActiveRuleIDs("code", TierCoding, "coder")
	without := ActiveRuleIDs("code", "", "coder")
	if strings.Join(withTier, ",") != strings.Join(without, ",") {
		t.Fatalf("两个档位下生效 ID 不同了（%v vs %v）——若变体改用不同 ID，记录与归因都要重新设计",
			withTier, without)
	}
	if renderRules(SlotCode, "code", TierCoding, "coder") == renderRules(SlotCode, "code", "", "coder") {
		t.Fatal("两个档位下的编程准则完全相同——按档位条件化失效了")
	}
}

func TestActiveRuleIDs_ChatUsesNoRules(t *testing.T) {
	if got := ActiveRuleIDs(string(types.TaskChat), "", ""); len(got) != 0 {
		t.Errorf("对话模式不该记任何规则，实际 %v", got)
	}
	if got := ActiveRuleIDs("work", "", ""); len(got) == 0 {
		t.Error("工作模式至少要记下契约段的规则")
	}
}

func TestRuleTable_IsSnapshotOfStableRules(t *testing.T) {
	tab := RuleTable()
	if len(tab) != len(stableRules) {
		t.Fatalf("回查到 %d 条，规则表里是 %d 条", len(tab), len(stableRules))
	}
	for i, r := range tab {
		if r.ID != stableRules[i].ID || r.Slot != stableRules[i].Slot {
			t.Errorf("第 %d 条对不上：视图 %s/%s，实际 %s/%s", i, r.Slot, r.ID, stableRules[i].Slot, stableRules[i].ID)
		}
		if r.Items != len(stableRules[i].Items) {
			t.Errorf("规则 %s 条目数记成 %d，实际 %d", r.ID, r.Items, len(stableRules[i].Items))
		}
		chars := 0
		for _, it := range stableRules[i].Items {
			chars += len(it)
		}
		if r.Chars != chars {
			t.Errorf("规则 %s 字符数记成 %d，实际 %d", r.ID, r.Chars, chars)
		}
	}
	tab[0].ID = "改坏了"
	if RuleTable()[0].ID == "改坏了" {
		t.Error("回查视图与内部表共用底层数组，改视图会改坏规则表")
	}
}

func TestDescribeRuleSet_PartitionsRuleIDs(t *testing.T) {
	for _, sc := range []struct{ mode, tier, role string }{
		{"work", "", ""},
		{"code", TierCoding, "coder"},
	} {
		rs := DescribeRuleSet(sc.mode, sc.tier, sc.role)
		if rs.Version != RuleSetVersion() {
			t.Errorf("场景 %+v 指纹对不上", sc)
		}
		all := map[string]bool{}
		for _, r := range stableRules {
			all[r.ID] = true
		}
		covered := map[string]bool{}
		for _, id := range rs.Active {
			if !all[id] {
				t.Errorf("生效列表里有表里不存在的 ID %q", id)
			}
			if covered[id] {
				t.Errorf("ID %q 在生效列表里重复", id)
			}
			covered[id] = true
		}
		for _, id := range rs.Inactive {
			if !all[id] {
				t.Errorf("未生效列表里有表里不存在的 ID %q", id)
			}
			if covered[id] {
				t.Errorf("ID %q 同时出现在生效与未生效列表里", id)
			}
			covered[id] = true
		}
		if len(covered) != len(all) {
			t.Errorf("场景 %+v：两份列表合计 %d 个 ID，表里是 %d 个", sc, len(covered), len(all))
		}
		if sc.mode == "code" {
			for _, id := range rs.Inactive {
				if id == "code.verify" {
					t.Error("code 模式下 code.verify 已生效，不该再出现在未生效列表里")
				}
			}
		}
	}
}

func TestRunGoal_RecordsRuleSet(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(92, "done", "已回复"),
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "打个招呼", Role: "coder"})
	if res.Status != types.GoalSuccess {
		t.Fatalf("任务未成功: %+v", res)
	}
	if res.RuleSet != RuleSetVersion() {
		t.Errorf("结果里的规则集指纹是 %q，当前规则表是 %q", res.RuleSet, RuleSetVersion())
	}
	want := ActiveRuleIDs(string(types.TaskWork), f.a.EffectiveTier("coder"), "coder")
	if strings.Join(res.Rules, ",") != strings.Join(want, ",") {
		t.Errorf("结果里记的生效规则是 %v，期望 %v", res.Rules, want)
	}
	if len(res.Rules) == 0 {
		t.Error("工作模式的产出不该记成「没用规则」")
	}
}

func TestRunGoal_RuleSetReachesGrowthLog(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(92, "done", "已回复"),
	})
	g, err := growth.Open(filepath.Join(t.TempDir(), "growth"))
	if err != nil {
		t.Fatal(err)
	}
	f.a.Growth = g
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "打个招呼"})
	if res.Status != types.GoalSuccess {
		t.Fatalf("任务未成功: %+v", res)
	}
	entries := g.Recent(1)
	if len(entries) != 1 {
		t.Fatalf("成长日志里有 %d 条，期望 1 条", len(entries))
	}
	if entries[0].RuleSet != RuleSetVersion() {
		t.Errorf("成长日志里的规则集是 %q，当前规则表是 %q", entries[0].RuleSet, RuleSetVersion())
	}
}

func TestRunGoal_ChatRecordsNoRuleSet(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		{Kind: "chat_mode", Texts: []string{"你好呀！我是 Gleam。"}},
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{
		Goal: "你好", TaskMode: types.TaskChat,
	})
	if res.Status != types.GoalSuccess {
		t.Fatalf("对话未成功: %+v", res)
	}
	if res.RuleSet != "" || len(res.Rules) != 0 {
		t.Errorf("对话模式不该记规则集，实际 RuleSet=%q Rules=%v", res.RuleSet, res.Rules)
	}
}
