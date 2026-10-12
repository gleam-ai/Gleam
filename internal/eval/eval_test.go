package eval

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/agent"
)

func TestJudgeIgnoresPlanChecksAtSelectDepth(t *testing.T) {
	c := Case{
		ID: "x", Goal: "g",
		WantTools:      []string{"file.write"},
		MinSteps:       3,
		MaxSteps:       5,
		WantAcceptance: true,
		WantStatus:     []string{"success"},
	}
	obs := observation{Tools: []string{"file.write"}, Steps: 0, Acceptance: 0, Status: ""}

	checks := judge(c, obs, DepthSelect)
	for _, ck := range checks {
		if strings.Contains(ck.Name, "步数") || strings.Contains(ck.Name, "验收") ||
			strings.Contains(ck.Name, "状态") {
			t.Fatalf("select 深度不该判计划类断言，却出现了 %q", ck.Name)
		}
	}
	if !allPassed(checks) {
		t.Fatalf("select 深度下这条应当通过，实际 checks=%+v", checks)
	}

	planChecks := judge(c, obs, DepthPlan)
	if allPassed(planChecks) {
		t.Fatal("plan 深度下步数/验收标准都不满足，却判通过了——断言没生效")
	}
}

func TestJudgeSelectUsesTopNWindow(t *testing.T) {
	c := Case{
		ID: "x", Goal: "g",
		WantTools:    []string{"file.list"},
		NotWantTools: []string{"schedule.create"},
		TopN:         3,
	}
	obs := observation{Tools: []string{
		"file.list", "file.read", "file.write", "file.move",
		"file.delete", "file.mkdir", "web.fetch", "memory.save",
		"skill.list", "reply", "schedule.create",
	}}
	if !allPassed(judge(c, obs, DepthSelect)) {
		t.Fatalf("窗口外的名次不该判红，实际 checks=%+v", judge(c, obs, DepthSelect))
	}

	obs.Tools = []string{"file.list", "schedule.create", "file.read"}
	checks := judge(c, obs, DepthSelect)
	if allPassed(checks) {
		t.Fatal("误路由工具进了窗口却没判红")
	}
	var detail string
	for _, ck := range checks {
		if !ck.Passed && strings.Contains(ck.Name, "schedule.create") {
			detail = ck.Detail
		}
	}
	if !strings.Contains(detail, "第 2 名") {
		t.Fatalf("失败详情该指出实际名次，实际为 %q", detail)
	}
}

func TestJudgeDistinguishesRankedOutFromMissing(t *testing.T) {
	c := Case{ID: "x", Goal: "g", WantTools: []string{"file.write"}, TopN: 1}

	rankedOut := judge(c, observation{Tools: []string{"file.read", "file.write"}}, DepthSelect)
	if !strings.Contains(rankedOut[1].Detail, "落在前 1 名之外") {
		t.Fatalf("排到窗口外应说明是名次问题，实际 %q", rankedOut[1].Detail)
	}

	missing := judge(c, observation{Tools: []string{"file.read"}}, DepthSelect)
	if !strings.Contains(missing[1].Detail, "完全没命中") {
		t.Fatalf("没命中应说明是没命中，实际 %q", missing[1].Detail)
	}
}

func TestJudgeTopNDefault(t *testing.T) {
	c := Case{ID: "x", Goal: "g", NotWantTools: []string{"bad"}}
	obs := observation{Tools: []string{"a", "b", "c", "d", "bad"}}
	if allPassed(judge(c, obs, DepthSelect)) {
		t.Fatalf("第 %d 名应落在默认窗口内", DefaultTopN)
	}
	obs.Tools = []string{"a", "b", "c", "d", "e", "bad"}
	if !allPassed(judge(c, obs, DepthSelect)) {
		t.Fatalf("第 %d 名应落在默认窗口外", DefaultTopN+1)
	}
}

func TestJudgePlanDepthDoesNotWindow(t *testing.T) {
	c := Case{ID: "x", Goal: "g", NotWantTools: []string{"bad"}, TopN: 1}
	obs := observation{Tools: []string{"a", "b", "bad"}, Steps: 1, Acceptance: 1}
	if allPassed(judge(c, obs, DepthPlan)) {
		t.Fatal("plan 深度不该按前 N 名截断——第 3 名也是模型真的选出来的")
	}
}

func TestValidateRejectsVacuousCase(t *testing.T) {
	err := Validate([]Case{{ID: "empty", Goal: "随便做点什么"}})
	if err == nil || !strings.Contains(err.Error(), "没有任何期望") {
		t.Fatalf("应当拒绝没有期望的用例，实际 err=%v", err)
	}
}

func TestValidateRejectsBadCases(t *testing.T) {
	cases := map[string][]Case{
		"缺 id":   {{Goal: "g", WantTools: []string{"a"}}},
		"缺 goal": {{ID: "a", WantTools: []string{"x"}}},
		"id 重复":  {{ID: "a", Goal: "g", WantTools: []string{"x"}}, {ID: "a", Goal: "g", WantTools: []string{"y"}}},
		"步数区间颠倒": {{ID: "a", Goal: "g", MinSteps: 5, MaxSteps: 2}},
	}
	for name, cs := range cases {
		if err := Validate(cs); err == nil {
			t.Errorf("%s：应当报错，却通过了", name)
		}
	}
}

func TestBuiltinCasesAreValid(t *testing.T) {
	cases, err := BuiltinCases()
	if err != nil {
		t.Fatalf("内置用例集不合法: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("内置用例集为空")
	}
	for _, c := range cases {
		if strings.TrimSpace(c.Note) == "" {
			t.Errorf("用例 %s 缺 note——红的用例必须能自己解释自己", c.ID)
		}
		if c.KnownIssue && !strings.Contains(c.Note, "已知问题") {
			t.Errorf("用例 %s 标了 known_issue，note 里却没说明根因", c.ID)
		}
	}
}

func rep(depth Depth, passed map[string]bool) Report {
	r := Report{GeneratedAt: time.Now(), Depth: depth}
	for id, ok := range passed {
		r.Cases = append(r.Cases, CaseResult{ID: id, Passed: ok})
	}
	return r
}

func TestCompareDetectsRegression(t *testing.T) {
	base := rep(DepthSelect, map[string]bool{"a": true, "b": true, "c": false})
	now := rep(DepthSelect, map[string]bool{"a": true, "b": false, "c": true})

	d, err := Compare(now, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Broke) != 1 || d.Broke[0] != "b" {
		t.Fatalf("应当报出 b 回归，实际 %v", d.Broke)
	}
	if len(d.Fixed) != 1 || d.Fixed[0] != "c" {
		t.Fatalf("应当报出 c 修好，实际 %v", d.Fixed)
	}
	if !d.Regressed() {
		t.Fatal("有回归时 Regressed 必须为真——它决定退出码")
	}
}

func TestCompareRejectsDepthMismatch(t *testing.T) {
	_, err := Compare(rep(DepthPlan, map[string]bool{"a": true}), rep(DepthSelect, map[string]bool{"a": true}))
	if err == nil || !strings.Contains(err.Error(), "深度不一致") {
		t.Fatalf("跨深度对比应当直接报错，实际 err=%v", err)
	}
}

func TestCompareRejectsTierMismatch(t *testing.T) {
	base := rep(DepthSelect, map[string]bool{"a": true})
	now := rep(DepthSelect, map[string]bool{"a": true})
	now.Tier = "coding"
	if _, err := Compare(now, base); err == nil || !strings.Contains(err.Error(), "模拟档位不一致") {
		t.Fatalf("档位不同时应当直接报错，实际 err=%v", err)
	}
	if _, err := Compare(rep(DepthSelect, map[string]bool{"a": true}), base); err != nil {
		t.Fatalf("未模拟档位时不该报错，实际 err=%v", err)
	}
}

func TestRunnerTierReachesReport(t *testing.T) {
	r := &Runner{Depth: DepthSelect, Tier: "reasoning"}
	rep := r.Run(context.Background(), nil)
	if rep.Tier != "reasoning" {
		t.Fatalf("报告应记下模拟档位，实际 %q", rep.Tier)
	}
	if (&Runner{Depth: DepthSelect}).Run(context.Background(), nil).Tier != "" {
		t.Fatal("未模拟时档位应为空")
	}
}

func TestCompareReportsRemovedCases(t *testing.T) {
	base := rep(DepthSelect, map[string]bool{"a": true, "b": false})
	now := rep(DepthSelect, map[string]bool{"a": true})

	d, err := Compare(now, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Removed) != 1 || d.Removed[0] != "b" {
		t.Fatalf("应当报出 b 被移出用例集，实际 %v", d.Removed)
	}
}

func TestRunnerReportsRuleSet(t *testing.T) {
	r := &Runner{Depth: DepthSelect}
	rep := r.Run(context.Background(), nil)
	if rep.RuleSet == "" {
		t.Fatal("报告里没有规则集指纹")
	}
	if rep.RuleSet != agent.RuleSetVersion() {
		t.Fatalf("报告里的指纹 %q 与当前规则表 %q 不一致——记的不是渲染用的那一份",
			rep.RuleSet, agent.RuleSetVersion())
	}
}

func TestCompareReportsRuleSetChangeButDoesNotFail(t *testing.T) {
	base := rep(DepthSelect, map[string]bool{"a": true})
	base.RuleSet = "aaaaaaaaaa"
	now := rep(DepthSelect, map[string]bool{"a": true})
	now.RuleSet = "bbbbbbbbbb"

	d, err := Compare(now, base)
	if err != nil {
		t.Fatal(err)
	}
	if d.RuleSetFrom != "aaaaaaaaaa" || d.RuleSetTo != "bbbbbbbbbb" {
		t.Fatalf("规则集变化没记全：%q → %q", d.RuleSetFrom, d.RuleSetTo)
	}
	var line string
	for _, l := range d.Describe() {
		if strings.Contains(l, "规则集") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("对比说明里没有规则集变化——回归时最该先看的线索丢了")
	}
	if !strings.Contains(line, "先怀疑规则改动") {
		t.Errorf("说明该指出归因方向，实际 %q", line)
	}
	if d.Regressed() {
		t.Fatal("规则集变了不该判成回归（会逼人摘掉 --strict）")
	}
}

func TestCompareRuleSetUnchangedStaysQuiet(t *testing.T) {
	base := rep(DepthSelect, map[string]bool{"a": true})
	base.RuleSet = "same123456"
	now := rep(DepthSelect, map[string]bool{"a": true})
	now.RuleSet = "same123456"
	d, err := Compare(now, base)
	if err != nil {
		t.Fatal(err)
	}
	if d.RuleSetFrom != "" || d.RuleSetTo != "" {
		t.Errorf("规则集没变却记了差异：%q → %q", d.RuleSetFrom, d.RuleSetTo)
	}
	for _, l := range d.Describe() {
		if strings.Contains(l, "规则集") {
			t.Errorf("规则集没变却报了：%q", l)
		}
	}
}

func TestCompareSkipsKnownIssues(t *testing.T) {
	base := rep(DepthSelect, map[string]bool{"a": true, "known": true})
	now := rep(DepthSelect, map[string]bool{"a": true, "known": false})
	for i := range now.Cases {
		if now.Cases[i].ID == "known" {
			now.Cases[i].Known = true
		}
	}

	d, err := Compare(now, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Broke) != 0 {
		t.Fatalf("已知问题不该算回归，实际 %v", d.Broke)
	}
	if d.Regressed() {
		t.Fatal("只有已知问题变红时，不该判定为回归")
	}
}

func TestTallyKnownIssueAccounting(t *testing.T) {
	var r Report
	tally(&r, Case{ID: "p", KnownIssue: false}, &CaseResult{ID: "p", Passed: true})
	tally(&r, Case{ID: "f", KnownIssue: false}, &CaseResult{ID: "f", Passed: false})
	tally(&r, Case{ID: "k", KnownIssue: true}, &CaseResult{ID: "k", Passed: false})
	tally(&r, Case{ID: "kr", KnownIssue: true}, &CaseResult{ID: "kr", Passed: true})

	if r.Passed != 2 || r.Failed != 1 || r.Known != 1 {
		t.Fatalf("归集错误：passed=%d failed=%d known=%d", r.Passed, r.Failed, r.Known)
	}
	if len(r.Resolved) != 1 || r.Resolved[0] != "kr" {
		t.Fatalf("标着已知问题却通过了的应当报进 Resolved，实际 %v", r.Resolved)
	}
}

func TestSummaryLineMentionsKnown(t *testing.T) {
	r := Report{Depth: DepthSelect, Total: 4, Passed: 2, Failed: 1, Known: 1}
	if !strings.Contains(r.SummaryLine(), "已知问题") {
		t.Fatalf("摘要该提到已知问题，实际 %q", r.SummaryLine())
	}
}

func TestBaselineRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "base.json")
	in := rep(DepthSelect, map[string]bool{"a": true})
	in.PromptChars = 123
	if err := SaveBaseline(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.Depth != in.Depth || out.PromptChars != in.PromptChars || len(out.Cases) != 1 {
		t.Fatalf("基线往返丢信息：%+v", out)
	}
	if _, err := LoadBaseline(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("读不存在的基线应当报错")
	}
}

func TestValidDepth(t *testing.T) {
	for _, d := range []Depth{DepthSelect, DepthPlan, DepthFull} {
		if !ValidDepth(d) {
			t.Errorf("%q 应当是合法深度", d)
		}
	}
	if ValidDepth("bogus") || ValidDepth("") {
		t.Error("非法深度不该通过校验")
	}
}
