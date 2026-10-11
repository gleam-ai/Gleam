package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/config"
	"gleam/pkg/types"
)

func cueAgent(t *testing.T) *Agent {
	t.Helper()
	return &Agent{Cfg: &config.Config{DataDir: t.TempDir()}}
}

func archive(t *testing.T, a *Agent, g *types.GoalResult) {
	t.Helper()
	if g.StartedAt.IsZero() {
		g.StartedAt = time.Now().Add(-time.Hour)
	}
	if g.FinishedAt.IsZero() {
		g.FinishedAt = g.StartedAt.Add(2 * time.Minute)
	}
	if err := SaveTaskResult(a.Cfg.DataDir, g); err != nil {
		t.Fatalf("归档失败：%v", err)
	}
}

func failedRun(taskID, goal, trace string, kinds map[types.ErrorKind]int) *types.GoalResult {
	return &types.GoalResult{
		TaskID: taskID, Goal: goal, Status: types.GoalFailed, TraceID: trace,
		Score: 20, Error: "上游返回 502，重试三次仍然失败", FailureBreakdown: kinds,
	}
}

func quietFailedRun(taskID, goal, trace string) *types.GoalResult {
	return &types.GoalResult{TaskID: taskID, Goal: goal, Status: types.GoalFailed, TraceID: trace, Score: 10}
}

func TestCues_EverySignalProducesARow(t *testing.T) {
	a := cueAgent(t)
	archive(t, a, failedRun("t1", "把报告转成表格", "traceFail", map[types.ErrorKind]int{types.ErrUpstream: 1}))
	archive(t, a, failedRun("t2", "把报告转成表格", "traceFail", map[types.ErrorKind]int{types.ErrUpstream: 1}))
	archive(t, a, &types.GoalResult{
		TaskID: "t3", Goal: "整理这一周的剪辑素材", Status: types.GoalPartial,
		TraceID: "traceHalf", Score: 55,
	})
	archive(t, a, &types.GoalResult{
		TaskID: "t4", Goal: "整理这一周的剪辑素材", Status: types.GoalPartial,
		TraceID: "traceHalf", Score: 60,
	})
	archive(t, a, failedRun("t5", "另一个目标", "traceOther", map[types.ErrorKind]int{types.ErrTimeout: 2}))
	archive(t, a, failedRun("t6", "又一个目标", "traceOther2", map[types.ErrorKind]int{types.ErrTimeout: 2}))

	got := map[string]bool{}
	for _, row := range a.CueView(false).Rows {
		got[row.Signal] = true
	}
	for _, sig := range cueSignals {
		if !got[sig] {
			t.Errorf("信号 %q 登记了却浮不出卡（派生分支或阈值坏了）", sig)
		}
	}
}

func TestCues_RepeatedFailureWinsOverHalfDone(t *testing.T) {
	a := cueAgent(t)
	archive(t, a, failedRun("t1", "同一个目标", "traceX", nil))
	archive(t, a, failedRun("t2", "同一个目标", "traceX", nil))
	archive(t, a, &types.GoalResult{TaskID: "t3", Goal: "同一个目标", Status: types.GoalPartial, TraceID: "traceX", Score: 50})
	archive(t, a, &types.GoalResult{TaskID: "t4", Goal: "同一个目标", Status: types.GoalPartial, TraceID: "traceX", Score: 40})

	var sigs []string
	for _, row := range a.CueView(false).Rows {
		sigs = append(sigs, row.Signal)
	}
	if len(sigs) != 1 || sigs[0] != "repeat_failure" {
		t.Errorf("只该浮更严重的那张，实得 %v", sigs)
	}
}

func TestCues_HalfDoneSkipsTraceThatOnceWon(t *testing.T) {
	a := cueAgent(t)
	archive(t, a, &types.GoalResult{TaskID: "t1", Goal: "写周报", Status: types.GoalSuccess, TraceID: "traceY", Score: 100})
	archive(t, a, &types.GoalResult{TaskID: "t2", Goal: "写周报", Status: types.GoalPartial, TraceID: "traceY", Score: 50})
	archive(t, a, &types.GoalResult{TaskID: "t3", Goal: "写周报", Status: types.GoalPartial, TraceID: "traceY", Score: 45})

	if rows := a.CueView(false).Rows; len(rows) != 0 {
		t.Errorf("成功过一次的目标不该提「每次只做一半」，实得 %+v", rows)
	}
}

func TestCues_ReadOnlyAndStable(t *testing.T) {
	a := cueAgent(t)
	archive(t, a, failedRun("t1", "反复失败的目标", "traceZ", map[types.ErrorKind]int{types.ErrPermission: 1}))
	archive(t, a, failedRun("t2", "反复失败的目标", "traceZ", map[types.ErrorKind]int{types.ErrPermission: 1}))

	first, err := json.Marshal(a.CueView(false))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(a.CueView(false))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("同一段历史两次算出了不同的页：\n%s\n%s", first, second)
	}
	if _, err := os.Stat(filepath.Join(a.Cfg.DataDir, "cues.json")); !os.IsNotExist(err) {
		t.Errorf("只读的一页算完却留下了 %s：卡片不该落盘", filepath.Join(a.Cfg.DataDir, "cues.json"))
	}
}

func TestCues_DismissSurvivesRestart(t *testing.T) {
	a := cueAgent(t)
	archive(t, a, failedRun("t1", "别再提我已知晓", "traceD", nil))
	archive(t, a, failedRun("t2", "别再提我已知晓", "traceD", nil))

	rows := a.CueView(false).Rows
	if len(rows) == 0 {
		t.Fatal("前置条件不成立：本该浮出一张卡")
	}
	id := rows[0].ID
	if err := a.CueDismiss(id); err != nil {
		t.Fatalf("记处置失败：%v", err)
	}
	b := &Agent{Cfg: &config.Config{DataDir: a.Cfg.DataDir}}
	if got := b.CueView(false); len(got.Rows) != 0 {
		t.Errorf("记了别再提之后仍浮出 %d 张卡", len(got.Rows))
	}
	if len(b.CueView(false).Suppressed) != 1 {
		t.Errorf("处置记录没进 suppressed，撤销入口就找不到它：%+v", b.CueView(false).Suppressed)
	}
	if err := b.CueUnsuppress(id); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	if len(b.CueView(false).Rows) == 0 {
		t.Error("撤销「别再提」之后那张卡应该回来")
	}
}

func TestCues_AdoptRecordsStateWithoutRunningAnything(t *testing.T) {
	a := cueAgent(t)
	archive(t, a, failedRun("t1", "采纳但不该跑起来", "traceA", nil))
	archive(t, a, failedRun("t2", "采纳但不该跑起来", "traceA", nil))
	id := a.CueView(false).Rows[0].ID

	before := tree(t, a.Cfg.DataDir)
	if err := a.CueAdopt(id); err != nil {
		t.Fatalf("采纳失败：%v", err)
	}
	after := tree(t, a.Cfg.DataDir)
	if diff := fileSetDiff(before, after); len(diff) != 1 || diff[0] != "cues.json" {
		t.Errorf("采纳之后盘上多出的不该只有处置记录，实得 %v", diff)
	}
	var found bool
	for _, s := range a.CueView(false).Suppressed {
		if s.Fingerprint == id && s.Reason == "adopted" {
			found = true
		}
	}
	if !found {
		t.Errorf("处置记录没标成 adopted：%+v", a.CueView(false).Suppressed)
	}
}

func TestCues_RespectsLimit(t *testing.T) {
	a := cueAgent(t)
	for i := 0; i < 5; i++ {
		trace := fmt.Sprintf("traceLimit%d", i)
		goal := fmt.Sprintf("反复失败的目标 %d", i)
		archive(t, a, quietFailedRun(fmt.Sprintf("a%d", i), goal, trace))
		archive(t, a, quietFailedRun(fmt.Sprintf("b%d", i), goal, trace))
	}
	led := a.CueView(false)
	if len(led.Rows) != cueLimit {
		t.Errorf("最多 %d 张，实得 %d", cueLimit, len(led.Rows))
	}
	if led.Overflow != 2 {
		t.Errorf("没挤进来的条数应报 2，实得 %d", led.Overflow)
	}
	if !strings.Contains(led.Truncated, "2 条") {
		t.Errorf("截断要交代没挤进来的条数，实得 %q", led.Truncated)
	}
	if !strings.Contains(led.Coverage, fmt.Sprintf("最多列 %d 张卡", cueLimit)) {
		t.Errorf("覆盖范围该交代配额：%s", led.Coverage)
	}
}

func TestCues_CoverageIsHonestAboutTrim(t *testing.T) {
	a := cueAgent(t)
	led := a.CueView(false)
	if led.Scanned != 0 || len(led.Rows) != 0 {
		t.Fatalf("前置条件：空目录应算出空页（scanned=%d rows=%d）", led.Scanned, len(led.Rows))
	}
	for _, want := range []string{"读了 0 条", "裁剪"} {
		if !strings.Contains(led.Coverage, want) {
			t.Errorf("覆盖范围缺 %q：%s", want, led.Coverage)
		}
	}
}

func TestCues_UnreadableArchiveSaysSo(t *testing.T) {
	a := cueAgent(t)
	p := filepath.Join(a.Cfg.DataDir, "tasks")
	if err := os.WriteFile(p, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	led := a.CueView(false)
	if led.Note == "" && led.Coverage == "" {
		t.Error("归档读不动时至少要留下一句可看见的说明")
	}
}

func TestCues_KindTextCoversEveryEnum(t *testing.T) {
	for _, k := range []types.ErrorKind{
		types.ErrParam, types.ErrFormat, types.ErrBusiness, types.ErrPermission,
		types.ErrNotFound, types.ErrTimeout, types.ErrUpstream, types.ErrInternal, types.ErrUnknown,
	} {
		if got := cueKindText(k, false); got == string(k) || got == "" {
			t.Errorf("失败类别 %q 没有人话映射：%q", k, got)
		}
	}
}

func TestCues_LabelsCoverEveryEnumValue(t *testing.T) {
	for _, sig := range cueSignals {
		if got := cueSignalText(sig, false); got == sig || got == "" {
			t.Errorf("线索 %q 没有人话映射：%q", sig, got)
		}
	}
	for _, r := range []string{cueDismissed, cueAdopted} {
		if got := cueReasonText(r, false); got == r || got == "" {
			t.Errorf("处置原因 %q 没有人话映射：%q", r, got)
		}
	}

	a := cueAgent(t)
	archive(t, a, failedRun("t1", "标签要跟着记录走", "traceL", nil))
	archive(t, a, failedRun("t2", "标签要跟着记录走", "traceL", nil))
	if err := a.CueDismiss(a.CueView(false).Rows[0].ID); err != nil {
		t.Fatalf("记处置失败：%v", err)
	}
	sup := a.CueView(false).Suppressed
	if len(sup) != 1 {
		t.Fatalf("应有一条处置记录，实得 %d", len(sup))
	}
	if sup[0].SignalText == "" || sup[0].SignalText == sup[0].Signal {
		t.Errorf("处置记录没带线索的人话：%+v", sup[0])
	}
	if sup[0].ReasonText != "别再提" {
		t.Errorf("处置记录的原因应印成「别再提」，实得 %q", sup[0].ReasonText)
	}
}

func TestCues_AuthoredCopyHasNoMarkdown(t *testing.T) {
	a := cueAgent(t)
	archive(t, a, failedRun("t1", "带**强调**的目标", "traceM", map[types.ErrorKind]int{types.ErrTimeout: 3}))
	archive(t, a, failedRun("t2", "带**强调**的目标", "traceM", map[types.ErrorKind]int{types.ErrTimeout: 3}))
	archive(t, a, failedRun("t3", "另一个**加粗**目标", "traceM2", map[types.ErrorKind]int{types.ErrTimeout: 3}))
	archive(t, a, failedRun("t4", "另一个**加粗**目标", "traceM2", map[types.ErrorKind]int{types.ErrTimeout: 3}))

	led := a.CueView(false)
	if len(led.Rows) == 0 {
		t.Fatal("前置条件：本该浮出卡")
	}
	checked := []string{led.Coverage, led.Note}
	for _, row := range led.Rows {
		checked = append(checked, row.SignalText, row.Why)
	}
	for _, s := range checked {
		if strings.Contains(s, "**") {
			t.Errorf("候补目标页的自作文案里出现了 markdown 强调：%q", s)
		}
	}
}

func tree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func fileSetDiff(before, after []string) []string {
	in := map[string]bool{}
	for _, p := range before {
		in[p] = true
	}
	var out []string
	for _, p := range after {
		if !in[p] {
			out = append(out, p)
		}
	}
	return out
}

func TestCues_EnglishViewHasNoCJK(t *testing.T) {
	a := cueAgent(t)
	archive(t, a, &types.GoalResult{
		TaskID: "t1", Goal: "convert the report into a table", Status: types.GoalFailed,
		TraceID: "traceFail", Score: 20, Error: "upstream returned 502, three retries failed",
		FailureBreakdown: map[types.ErrorKind]int{types.ErrUpstream: 1},
	})
	archive(t, a, &types.GoalResult{
		TaskID: "t2", Goal: "convert the report into a table", Status: types.GoalFailed,
		TraceID: "traceFail", Score: 20,
		FailureBreakdown: map[types.ErrorKind]int{types.ErrUpstream: 1},
	})
	led := a.CueView(true)
	if len(led.Rows) == 0 {
		t.Fatal("英文视图一张卡都浮不出来，没有可判的对象")
	}
	assertNoCJK(t, "cues", led)
	if err := a.CueDismiss(led.Rows[0].ID); err != nil {
		t.Fatalf("记处置失败：%v", err)
	}
	assertNoCJK(t, "cues-suppressed", a.CueView(true))
	if !hasCJK(a.CueView(false).Coverage) {
		t.Error("中文版 Coverage 不含中文：bi.pick(false) 坏了，两份说法被改成了同一份")
	}
}
