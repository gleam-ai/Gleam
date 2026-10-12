package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gleam/internal/llm"
	"gleam/pkg/types"
)

type failingLLM struct{}

var errReflectorDown = errors.New("模拟反思器不可用")

func (failingLLM) Chat(context.Context, llm.ChatRequest) (string, error) {
	return "", errReflectorDown
}

func (failingLLM) ChatStream(context.Context, llm.ChatRequest, func(string)) (string, error) {
	return "", errReflectorDown
}

func (failingLLM) Name() string { return "failing" }

func reflectChecksScript(jsonText string) llm.Scripted {
	return llm.Scripted{Kind: "reflect", Texts: []string{jsonText}}
}

func TestAcceptance_Normalized(t *testing.T) {
	got := normalizeAcceptance([]string{
		"  回复里给出不少于 3 条建议  ",
		"回复里给出不少于 3 条建议",
		"",
		"第二条",
		"第三条",
		"第四条",
		"第五条",
	})
	if len(got) != 4 {
		t.Fatalf("去重去空后最多保留 4 条，实际 %d：%v", len(got), got)
	}
	if got[0] != "回复里给出不少于 3 条建议" {
		t.Errorf("应去掉首尾空白，实际 %q", got[0])
	}
}

func TestAcceptance_EmptyWhenAbsent(t *testing.T) {
	if got := normalizeAcceptance(nil); len(got) != 0 {
		t.Errorf("没有验收标准时应为空，实际 %v", got)
	}
}

func TestAcceptance_PlannerParsesIt(t *testing.T) {
	p := &Planner{Reg: menuRegistry(2), MaxSteps: 5}
	plan, err := p.validate([]byte(`{"steps":[{"id":"s1","tool":"reply","args":{"text":"hi"}}],
		"acceptance":["回复里包含 3 条建议","语气保持简洁"]}`), "给点建议")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Acceptance) != 2 {
		t.Fatalf("应保留 2 条验收标准，实际 %v", plan.Acceptance)
	}
}

func TestAcceptance_OptionalForBackwardCompat(t *testing.T) {
	p := &Planner{Reg: menuRegistry(2), MaxSteps: 5}
	plan, err := p.validate([]byte(`{"steps":[{"id":"s1","tool":"reply","args":{"text":"hi"}}]}`), "x")
	if err != nil {
		t.Fatalf("缺少 acceptance 不应导致校验失败: %v", err)
	}
	if len(plan.Acceptance) != 0 {
		t.Errorf("应为空，实际 %v", plan.Acceptance)
	}
}

func TestAcceptance_ScoreFromChecks(t *testing.T) {
	cases := []struct {
		name    string
		checks  []types.CheckResult
		wantSco int
	}{
		{"全部通过", []types.CheckResult{{Passed: true}, {Passed: true}}, 100},
		{"对半", []types.CheckResult{{Passed: true}, {Passed: false}}, 50},
		{"三缺一", []types.CheckResult{{Passed: true}, {Passed: true}, {Passed: false}}, 66},
		{"全不过", []types.CheckResult{{Passed: false}, {Passed: false}}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scoreFromChecks(tc.checks); got != tc.wantSco {
				t.Errorf("score = %d, want %d", got, tc.wantSco)
			}
		})
	}
}

func TestAcceptance_MissingCheckCountsAsFailed(t *testing.T) {
	acc := []string{"标准A", "标准B", "标准C"}
	got := alignChecks([]types.CheckResult{{Criterion: "标准A", Passed: true}}, acc)
	if len(got) != 3 {
		t.Fatalf("应补齐到 3 条，实际 %d", len(got))
	}
	if !got[0].Passed {
		t.Error("标准A 判定通过")
	}
	if got[1].Passed || got[2].Passed {
		t.Error("未判定的标准应按未通过计")
	}
	if scoreFromChecks(got) != 33 {
		t.Errorf("1/3 通过应为 33 分，实际 %d", scoreFromChecks(got))
	}
}

func TestAcceptance_ExtraChecksDropped(t *testing.T) {
	got := alignChecks([]types.CheckResult{
		{Criterion: "标准A", Passed: true},
		{Criterion: "我临时加的", Passed: true},
	}, []string{"标准A"})
	if len(got) != 1 || got[0].Criterion != "标准A" {
		t.Errorf("应只保留定下的标准，实际 %+v", got)
	}
}

func TestAcceptance_ReflectorIgnoresSelfScore(t *testing.T) {
	m := llm.NewMock()
	m.Enqueue("reflect", `{"score":95,"verdict":"done","reason":"我觉得很好",
		"checks":[{"criterion":"标准A","passed":true},{"criterion":"标准B","passed":true},{"criterion":"标准C","passed":false,"note":"缺第 3 条建议"}]}`)
	r := &Reflector{LLM: m, DoneThreshold: 80, Acceptance: []string{"标准A", "标准B", "标准C"}}
	refl := r.Evaluate(context.Background(), "给建议", mkExec(1, 0, 0), 1)
	if refl.Score != 66 {
		t.Errorf("分数应由判定结果算出（2/3=66），实际 %d", refl.Score)
	}
	if len(refl.Checks) != 3 {
		t.Fatalf("应保留 3 条判定，实际 %d", len(refl.Checks))
	}
	if refl.Checks[2].Note == "" {
		t.Error("未通过的原因要具体到能照着改")
	}
}

func TestAcceptance_NoCriteriaKeepsModelScore(t *testing.T) {
	m := llm.NewMock()
	m.Enqueue("reflect", `{"score":88,"verdict":"done","reason":"ok"}`)
	r := &Reflector{LLM: m, DoneThreshold: 80}
	refl := r.Evaluate(context.Background(), "g", mkExec(1, 0, 0), 1)
	if refl.Score != 88 {
		t.Errorf("无验收标准时应沿用模型评分，实际 %d", refl.Score)
	}
	if len(refl.Checks) != 0 {
		t.Errorf("无验收标准时不应产生判定，实际 %+v", refl.Checks)
	}
}

func TestAcceptance_DigestListsCriteria(t *testing.T) {
	digest := buildDigest("目标", mkExec(1, 0, 0), 1, []string{"第一条", "第二条"}, nil)
	if !strings.Contains(digest, "验收标准") || !strings.Contains(digest, "第一条") || !strings.Contains(digest, "第二条") {
		t.Errorf("digest 应列出验收标准：\n%s", digest)
	}
}

func TestAcceptance_ReachesGoalResult(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"建议一二三"}}],
			"acceptance":["回复里给出不少于 3 条建议"]}`),
		reflectChecksScript(`{"checks":[{"criterion":"回复里给出不少于 3 条建议","passed":true}],
			"verdict":"done","reason":"ok"}`),
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "给我三条建议", Mode: "auto"})
	if res.Status != types.GoalSuccess {
		t.Fatalf("全部通过应判成功，实际 %s（%s）", res.Status, res.Error)
	}
	if len(res.Acceptance) != 1 {
		t.Fatalf("验收标准应带到结果里，实际 %v", res.Acceptance)
	}
	if len(res.Checks) != 1 {
		t.Fatalf("判定结果应带到结果里，实际 %v", res.Checks)
	}
	if res.Checks[0].Criterion != "回复里给出不少于 3 条建议" {
		t.Errorf("判定应对齐到原标准，实际 %q", res.Checks[0].Criterion)
	}
}

func TestAcceptance_DoneRequiresAllPassed(t *testing.T) {
	m := llm.NewMock()
	m.Enqueue("reflect", `{"verdict":"done","reason":"我觉得没问题",
		"checks":[{"criterion":"A","passed":true},{"criterion":"B","passed":false,"note":"缺 B"}]}`)
	r := &Reflector{LLM: m, DoneThreshold: 80, Acceptance: []string{"A", "B"}}
	refl := r.Evaluate(context.Background(), "g", mkExec(1, 0, 0), 1)
	if refl.Verdict == "done" {
		t.Fatalf("有标准未通过时不能判 done，实际 %q（分数 %d）", refl.Verdict, refl.Score)
	}
	if refl.Reason == "" {
		t.Error("降级后要留下原因，否则用户不知道为什么要重试")
	}
}

func TestAcceptance_DoneIndependentOfThreshold(t *testing.T) {
	m := llm.NewMock()
	m.Enqueue("reflect", `{"verdict":"done","reason":"ok",
		"checks":[{"criterion":"A","passed":true},{"criterion":"B","passed":false}]}`)
	r := &Reflector{LLM: m, DoneThreshold: 30, Acceptance: []string{"A", "B"}}
	refl := r.Evaluate(context.Background(), "g", mkExec(1, 0, 0), 1)
	if refl.Verdict == "done" {
		t.Fatalf("阈值调低不应让未全过的验收算完成，实际 %q", refl.Verdict)
	}
	if acceptanceSatisfied([]string{"A", "B"}, refl.Checks) {
		t.Error("半通过不应被判定为验收达标")
	}
}

func TestAcceptance_AllPassedKeepsDone(t *testing.T) {
	m := llm.NewMock()
	m.Enqueue("reflect", `{"verdict":"done","reason":"ok",
		"checks":[{"criterion":"A","passed":true},{"criterion":"B","passed":true}]}`)
	r := &Reflector{LLM: m, DoneThreshold: 80, Acceptance: []string{"A", "B"}}
	refl := r.Evaluate(context.Background(), "g", mkExec(1, 0, 0), 1)
	if refl.Verdict != "done" || refl.Score != 100 {
		t.Fatalf("全部通过应保持 done/100，实际 %q/%d", refl.Verdict, refl.Score)
	}
	if !acceptanceSatisfied([]string{"A", "B"}, refl.Checks) {
		t.Error("全部通过应判定为验收达标")
	}
}

func TestAcceptance_MissingChecksBlocksCompletion(t *testing.T) {
	if !acceptanceSatisfied(nil, nil) {
		t.Error("没有验收标准时不应阻断完成")
	}
	if acceptanceSatisfied([]string{"A"}, nil) {
		t.Error("有验收标准却没有判定，不能算通过（判定缺失 ≠ 没问题）")
	}
	if acceptanceSatisfied([]string{"A"}, []types.CheckResult{{Criterion: "A", Passed: false}}) {
		t.Error("有未通过项时应判定为未达标")
	}
}

func TestAcceptance_HeuristicNeverDoneWithChecks(t *testing.T) {
	exec := mkExec(2, 0, 0)
	refl := heuristicReflection(exec, "（反思器调用失败）", true)
	if refl.Verdict == "done" {
		t.Fatalf("有验收标准时兜底不能判 done，实际 %q", refl.Verdict)
	}
	if refl.Score != 0 {
		t.Errorf("没核对过就没有完成度可言，分数应为 0，实际 %d", refl.Score)
	}
	if !strings.Contains(refl.Reason, "未能核对") {
		t.Errorf("原因里要写明验收没能核对，实际 %q", refl.Reason)
	}
	refl = heuristicReflection(exec, "", false)
	if refl.Verdict != "done" || refl.Score != 85 {
		t.Fatalf("没有验收标准时兜底应保持 done/85，实际 %q/%d", refl.Verdict, refl.Score)
	}
}

func TestAcceptance_ReflectorFailureNeverReportsSuccess(t *testing.T) {
	acceptance := []string{"产出里含 4 个字段", "文件名符合规范", "数值可对账", "末尾有结论"}
	exec := mkExec(3, 0, 0)

	r := &Reflector{LLM: failingLLM{}, Acceptance: acceptance}
	refl := r.Evaluate(context.Background(), "生成季度报表", exec, 1)

	if refl.Verdict == "done" {
		t.Fatalf("反思器不可用时不能判 done，实际 %q（分数 %d）", refl.Verdict, refl.Score)
	}
	if refl.Score == 85 {
		t.Errorf("反思器不可用时不该给出 85 分（那是「全部成功」的旧口径），实际 %d", refl.Score)
	}
	if len(refl.Checks) != 0 {
		t.Errorf("反思器不可用时应无判定，实际 %+v", refl.Checks)
	}
	if acceptanceSatisfied(acceptance, refl.Checks) {
		t.Error("有验收标准却没判定，循环门禁不能放行")
	}
	res := &types.GoalResult{
		Status: types.GoalSuccess, Score: refl.Score,
		Acceptance: acceptance, Checks: refl.Checks,
	}
	applyAcceptanceVerdict(res, acceptance)
	if res.Status == types.GoalSuccess {
		t.Errorf("验收未核对却报告成功：%+v", res)
	}
	if res.Status != types.GoalPartial {
		t.Errorf("步骤其实都跑成功了，应落「部分完成」而不是 %s", res.Status)
	}
	if !strings.Contains(res.Error, "未能核对") {
		t.Errorf("结果里要说明验收没能核对，实际 %q", res.Error)
	}
}

func TestAcceptance_UncheckedStatusIsPartial(t *testing.T) {
	res := &types.GoalResult{Status: types.GoalSuccess, Acceptance: []string{"A", "B"}}
	applyAcceptanceVerdict(res, []string{"A", "B"})
	if res.Status != types.GoalPartial {
		t.Fatalf("验收未核对应判部分完成，实际 %s", res.Status)
	}
	if !strings.Contains(res.Error, "未能核对") {
		t.Errorf("要说明差的是核对这一步，实际 %q", res.Error)
	}
	res = &types.GoalResult{Status: types.GoalSuccess}
	applyAcceptanceVerdict(res, nil)
	if res.Status != types.GoalSuccess {
		t.Errorf("没有验收标准时不应改状态，实际 %s", res.Status)
	}
}

func TestAcceptance_StatusCorrectedToPartial(t *testing.T) {
	res := &types.GoalResult{Status: types.GoalSuccess, Checks: []types.CheckResult{
		{Criterion: "给出 3 条建议", Passed: true},
		{Criterion: "每条写明负责人", Passed: false},
	}}
	applyAcceptanceVerdict(res, []string{"给出 3 条建议", "每条写明负责人"})
	if res.Status != types.GoalPartial {
		t.Fatalf("应判部分完成，实际 %s", res.Status)
	}
	if !strings.Contains(res.Error, "每条写明负责人") {
		t.Errorf("要说明哪条没达标，实际 %q", res.Error)
	}
	if strings.Contains(res.Error, "给出 3 条建议") {
		t.Errorf("已通过的条目不该出现在未达标列表里：%q", res.Error)
	}
}

func TestAcceptance_StatusCorrectedToFailed(t *testing.T) {
	res := &types.GoalResult{Status: types.GoalSuccess, Checks: []types.CheckResult{
		{Criterion: "A", Passed: false},
		{Criterion: "B", Passed: false},
	}}
	applyAcceptanceVerdict(res, []string{"A", "B"})
	if res.Status != types.GoalFailed {
		t.Fatalf("全不过应判失败，实际 %s", res.Status)
	}
}

func TestAcceptance_StatusUntouched(t *testing.T) {
	res := &types.GoalResult{Status: types.GoalSuccess, Checks: []types.CheckResult{{Criterion: "A"}}}
	applyAcceptanceVerdict(res, nil)
	if res.Status != types.GoalSuccess {
		t.Errorf("没有验收标准时不应改状态，实际 %s", res.Status)
	}
	res = &types.GoalResult{Status: types.GoalFailed, Error: "工具报错", Checks: []types.CheckResult{
		{Criterion: "A", Passed: true},
	}}
	applyAcceptanceVerdict(res, []string{"A"})
	if res.Status != types.GoalFailed || res.Error != "工具报错" {
		t.Errorf("已判失败的任务不该被改写，实际 %s / %q", res.Status, res.Error)
	}
	res = &types.GoalResult{Status: types.GoalCancelled, Checks: []types.CheckResult{{Criterion: "A"}}}
	applyAcceptanceVerdict(res, []string{"A"})
	if res.Status != types.GoalCancelled {
		t.Errorf("取消状态不该被改写，实际 %s", res.Status)
	}
}

func TestAcceptance_NotDoneTriggersReplan(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"只给了一条建议"}}],
			"acceptance":["回复里给出不少于 3 条建议"]}`),
		reflectChecksScript(`{"verdict":"done","reason":"我尽力了",
			"checks":[{"criterion":"回复里给出不少于 3 条建议","passed":false,"note":"只给了 1 条"}]}`),
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "给我三条建议", Mode: "auto"})

	plans := 0
	for _, c := range f.llm.Calls {
		if llm.KindOf(c.System) == "plan" {
			plans++
		}
	}
	if plans < 2 {
		t.Fatalf("验收未通过应触发重规划，实际只规划了 %d 次", plans)
	}
	if res.Status == types.GoalSuccess {
		t.Errorf("验收始终未通过却报告成功：%+v", res)
	}
}
