package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/pkg/types"
)

func TestCountSteps_MatchesExecutorJudgement(t *testing.T) {
	steps := []types.StepResult{
		{StepID: "s1", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
		{StepID: "s2", Status: types.StepSucceeded, Outcome: types.OutcomeFailed},
		{StepID: "s3", Status: types.StepFailed, Outcome: types.OutcomeFailed},
		{StepID: "s4", Status: types.StepSkipped},
		{StepID: "s5", Status: types.StepSucceeded, Outcome: types.OutcomeEmpty},
		{StepID: "s6", Status: types.StepSucceeded, Outcome: types.OutcomeOK, Carried: true},
	}
	s, f, sk, e, c := countSteps(steps)
	if s != 3 {
		t.Errorf("成功 = %d，应为 3（s1、s5，以及沿用的 s6——沿用来的成功步骤确实是成功的）；"+
			"s2 跑完但业务失败不算成功", s)
	}
	if f != 2 {
		t.Errorf("失败 = %d，应为 2（s2 与 s3）", f)
	}
	if sk != 1 {
		t.Errorf("跳过 = %d，应为 1", sk)
	}
	if e != 1 {
		t.Errorf("空结果 = %d，应为 1（空结果同时计入成功——它不是错误，但要看得见）", e)
	}
	if c != 1 {
		t.Errorf("沿用 = %d，应为 1", c)
	}
	if s+f+sk != len(steps) {
		t.Errorf("三类之和 %d 应等于总步数 %d", s+f+sk, len(steps))
	}
}

func TestStepLine_DistinguishesCarriedFromDeduped(t *testing.T) {
	carried := stepLine(types.StepResult{StepID: "s1", Tool: "file.write", Status: types.StepSucceeded, Carried: true})
	deduped := stepLine(types.StepResult{StepID: "s1", Tool: "file.read", Status: types.StepSucceeded, Deduped: true})
	retried := stepLine(types.StepResult{StepID: "s1", Tool: "web.fetch", Status: types.StepSucceeded, Retried: true, Attempt: 3})

	if !strings.Contains(carried, "沿用") || !strings.Contains(carried, "未执行") {
		t.Errorf("沿用的步骤必须点明「这次没执行」，实际：%s", carried)
	}
	if strings.Contains(deduped, "沿用") {
		t.Errorf("复用不是沿用（它跑过），不能混为一谈：%s", deduped)
	}
	if !strings.Contains(deduped, "复用") {
		t.Errorf("复用要标出来：%s", deduped)
	}
	if !strings.Contains(retried, "第 3 次尝试") {
		t.Errorf("重试要带上第几次才成：%s", retried)
	}
}

func TestResume_MissingPriorResultMustBeRefused(t *testing.T) {
	dir := t.TempDir()
	g := &types.GoalResult{
		TaskID: "t-miss",
		Goal:   "三步任务",
		Status: types.GoalFailed,
		ExecutedPlan: &types.Plan{Steps: []types.Step{
			{ID: "s1", Tool: "counter"},
			{ID: "s2", Tool: "counter", DependsOn: []string{"s1"}},
			{ID: "s3", Tool: "counter", DependsOn: []string{"s2"}},
		}},
		Steps: []types.StepResult{
			{StepID: "s1", Tool: "counter", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
			{StepID: "s3", Tool: "counter", Status: types.StepFailed, Outcome: types.OutcomeFailed},
		},
	}
	writeTask(t, dir, g)

	src, err := loadRun(dir, "t-miss")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if _, ok := src.resultOf("s2"); ok {
		t.Error("s2 本就没有记录，不该被读出来")
	}
	if _, ok := src.resultOf("s1"); !ok {
		t.Error("s1 有记录，应该能读到")
	}
}

func TestCarriedWarning_FlagsFailedCarriedSteps(t *testing.T) {
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "counter"},
		{ID: "s2", Tool: "counter", DependsOn: []string{"s1"}},
		{ID: "s3", Tool: "counter", DependsOn: []string{"s2"}},
	}}

	ok := carriedWarning(map[string]types.StepResult{
		"s1": {StepID: "s1", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
		"s2": {StepID: "s2", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
	}, plan, "s3")
	if ok != "" {
		t.Errorf("前两步都成功时不该提醒，实际：%s", ok)
	}

	warn := carriedWarning(map[string]types.StepResult{
		"s1": {StepID: "s1", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
		"s2": {StepID: "s2", Status: types.StepFailed, Outcome: types.OutcomeFailed},
	}, plan, "s3")
	for _, want := range []string{"s2", "当时的前提", "不是恢复失败", "把起点提前"} {
		if !strings.Contains(warn, want) {
			t.Errorf("提醒缺少 %q，实际：%s", want, warn)
		}
	}

	warn2 := carriedWarning(map[string]types.StepResult{
		"s1": {StepID: "s1", Status: types.StepSucceeded, Outcome: types.OutcomeFailed},
	}, plan, "s3")
	if warn2 == "" {
		t.Error("Outcome=failed 的沿用步骤也算没成功，必须提醒")
	}
}

func TestCarriedWarning_OnlyLooksBeforeTarget(t *testing.T) {
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "counter"},
		{ID: "s2", Tool: "counter", DependsOn: []string{"s1"}},
	}}
	warn := carriedWarning(map[string]types.StepResult{
		"s1": {StepID: "s1", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
		"s2": {StepID: "s2", Status: types.StepFailed, Outcome: types.OutcomeFailed},
	}, plan, "s2")
	if warn != "" {
		t.Errorf("s2 是目标步，不该被当成「沿用下来的失败」，实际：%s", warn)
	}
}

func TestRenderDiff_ShowsCarriedAndStepChanges(t *testing.T) {
	a := runView{
		Ref: "t1", Label: "任务", Status: types.GoalFailed,
		Steps: []types.StepResult{
			{StepID: "s1", Tool: "counter", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
			{StepID: "s2", Tool: "web.fetch", Status: types.StepFailed, Outcome: types.OutcomeFailed, Error: "当时超时"},
		},
	}
	a.count()
	b := runView{
		Ref: "r1", Label: "回放",
		Steps: []types.StepResult{
			{StepID: "s1", Tool: "counter", Status: types.StepSucceeded, Outcome: types.OutcomeOK, Carried: true},
			{StepID: "s2", Tool: "shell.run", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
		},
	}
	b.count()

	out := renderDiff(a, b)
	for _, want := range []string{
		"对比", "沿用", "s1", "s2",
		"换工具（web.fetch → shell.run）",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("对比缺少 %q，实际：\n%s", want, out)
		}
	}
	if !strings.Contains(out, "[沿用]") {
		t.Errorf("沿用的步骤要标出来，否则分不清哪几步是这次的：\n%s", out)
	}
}

func TestRenderDiff_ReportsStepsMissingOnEitherSide(t *testing.T) {
	a := runView{Ref: "a", Label: "任务", Steps: []types.StepResult{
		{StepID: "s1", Tool: "counter", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
		{StepID: "s2", Tool: "counter", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
	}}
	a.count()
	b := runView{Ref: "b", Label: "回放", Steps: []types.StepResult{
		{StepID: "s2", Tool: "counter", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
		{StepID: "s3", Tool: "counter", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
	}}
	b.count()

	out := renderDiff(a, b)
	if !strings.Contains(out, "s1") || !strings.Contains(out, "B 里不存在") {
		t.Errorf("A 有而 B 没有的步骤要指明，实际：\n%s", out)
	}
	if !strings.Contains(out, "s3") || !strings.Contains(out, "A 里不存在") {
		t.Errorf("B 里新增的步骤也要指明，实际：\n%s", out)
	}
}

func TestSaveReplay_GoesToReplaysNotTasks(t *testing.T) {
	dir := t.TempDir()
	rec := &types.ReplayRecord{
		ReplayID: "r-1", Kind: types.ReplayResume, SourceTaskID: "t-1", FromStep: "s2",
		CreatedAt: time.Now(), Steps: []types.StepResult{{StepID: "s2", Status: types.StepSucceeded}},
	}
	path, err := saveReplay(dir, rec)
	if err != nil {
		t.Fatalf("存档失败: %v", err)
	}
	if filepath.Base(filepath.Dir(path)) != "replays" {
		t.Errorf("回放应落在 replays/，实际 %s", path)
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks", "r-1.json")); !os.IsNotExist(err) {
		t.Error("回放记录不能出现在 tasks/ 里——那会污染质量统计的分母")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	var back types.ReplayRecord
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if back.Kind != types.ReplayResume || back.FromStep != "s2" || back.SourceTaskID != "t-1" {
		t.Errorf("往返后关键字段丢了: %+v", back)
	}
}

func TestLoadRun_AmbiguousRefIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "replays"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, "tasks", "x.json"), &types.GoalResult{TaskID: "x", Goal: "任务侧"})
	writeJSON(t, filepath.Join(dir, "replays", "x.json"), &types.ReplayRecord{ReplayID: "x", SourceTaskID: "t0"})

	if _, err := loadRun(dir, "x"); err == nil {
		t.Fatal("同名时应拒绝，而不是随便挑一个")
	} else if !strings.Contains(err.Error(), "指明") {
		t.Errorf("拒绝时要说清怎么解决（加前缀），实际：%v", err)
	}

	task, err := loadRun(dir, "task:x")
	if err != nil {
		t.Fatalf("task: 前缀应能读到: %v", err)
	}
	if task.Task == nil || task.Task.Goal != "任务侧" {
		t.Errorf("task: 应读任务记录，实际 %+v", task.Task)
	}
	rep, err := loadRun(dir, "replay:x")
	if err != nil {
		t.Fatalf("replay: 前缀应能读到: %v", err)
	}
	if rep.Replay == nil || rep.Replay.SourceTaskID != "t0" {
		t.Errorf("replay: 应读回放记录，实际 %+v", rep.Replay)
	}
}

func TestLoadRun_FallsBackToRunLog(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(types.StepResult{StepID: "s1", Tool: "counter", Status: types.StepSucceeded, Outcome: types.OutcomeOK})
	if err := os.WriteFile(filepath.Join(dir, "runs", "half.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := loadRun(dir, "half")
	if err != nil {
		t.Fatalf("应从运行日志里读出来: %v", err)
	}
	if !l.RunLogOnly || len(l.LogSteps) != 1 {
		t.Errorf("应标成半截运行并带 1 条步骤，实际 %+v", l)
	}
	if l.Plan() != nil {
		t.Error("半截运行没有落盘计划，Plan() 应为 nil（有的话会被误当成可重跑）")
	}
	out := renderLoaded(l)
	if !strings.Contains(out, "没有跑完") {
		t.Errorf("渲染要如实说明这次没跑完：\n%s", out)
	}
}

func TestLoadRun_NotFoundMentionsAllLocations(t *testing.T) {
	dir := t.TempDir()
	_, err := loadRun(dir, "ghost")
	if err == nil {
		t.Fatal("应报错")
	}
	for _, want := range []string{"tasks/", "replays/", "runs/"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("报错应提到找过 %s，实际：%v", want, err)
		}
	}
}

func writeTask(t *testing.T, dir string, g *types.GoalResult) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, "tasks", g.TaskID+".json"), g)
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
