package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gleam/pkg/types"
)

func step(id string, st types.StepStatus) types.StepResult {
	return types.StepResult{StepID: id, Tool: "counter", Status: st, Outcome: types.OutcomeOK}
}

func TestRunLog_AppendAndReadBack(t *testing.T) {
	dir := t.TempDir()
	l := NewRunLog(dir)
	for _, id := range []string{"s1", "s2", "s3"} {
		l.RecordStep("t-1", step(id, types.StepSucceeded))
	}
	got, err := ReadRunLog(dir, "t-1")
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("应读回 3 条，实际 %d", len(got))
	}
	for i, id := range []string{"s1", "s2", "s3"} {
		if got[i].StepID != id {
			t.Errorf("第 %d 条 = %q，应为 %q（顺序不能变）", i, got[i].StepID, id)
		}
	}
}

func TestRunLog_AppendDoesNotRewriteExisting(t *testing.T) {
	dir := t.TempDir()
	l := NewRunLog(dir)
	l.RecordStep("t-1", step("s1", types.StepSucceeded))
	l.RecordStep("t-1", step("s2", types.StepSucceeded))

	path := RunLogPath(dir, "t-1")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	l.RecordStep("t-1", step("s3", types.StepSucceeded))
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !strings.HasPrefix(string(after), string(before)) {
		t.Error("写入新记录后，原有内容被改动了——这必须是纯追加（崩溃时前面的记录还要靠它保住）")
	}
	if n := strings.Count(strings.TrimSpace(string(after)), "\n") + 1; n != 3 {
		t.Errorf("应为 3 行（每行一条 JSON），实际 %d 行", n)
	}
}

func TestRunLog_SkipsBrokenLine(t *testing.T) {
	dir := t.TempDir()
	l := NewRunLog(dir)
	l.RecordStep("t-1", step("s1", types.StepSucceeded))
	l.RecordStep("t-1", step("s2", types.StepSucceeded))

	f, err := os.OpenFile(RunLogPath(dir, "t-1"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	if _, err := f.WriteString(`{"step_id":"s3","tool":"coun`); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	f.Close()

	got, err := ReadRunLog(dir, "t-1")
	if err != nil {
		t.Fatalf("半行不该让整份读失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应读回前 2 条完整记录，实际 %d", len(got))
	}
	if got[1].StepID != "s2" {
		t.Errorf("最后一条 = %q", got[1].StepID)
	}
}

func TestRunLog_UnsafeNameIsNotWritten(t *testing.T) {
	dir := t.TempDir()
	l := NewRunLog(dir)
	for _, bad := range []string{"a/b", "a\\b", "..", "a b", "a:b", "", "a*b"} {
		l.RecordStep(bad, step("s1", types.StepSucceeded))
	}
	if RunLogPath(dir, "a/b") != "" {
		t.Error("不安全的 taskID 不该有日志路径")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "runs"))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("读取目录失败: %v", err)
	}
	if len(entries) != 0 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("不安全的名字一个都不该落盘，实际落了: %v", names)
	}
}

func TestRunLog_DistinctTaskIDsDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	l := NewRunLog(dir)
	l.RecordStep("t-a", step("s1", types.StepSucceeded))
	l.RecordStep("t-b", step("s1", types.StepSucceeded))
	l.RecordStep("t-b", step("s2", types.StepSucceeded))

	a, err := ReadRunLog(dir, "t-a")
	if err != nil {
		t.Fatalf("读 t-a 失败: %v", err)
	}
	b, err := ReadRunLog(dir, "t-b")
	if err != nil {
		t.Fatalf("读 t-b 失败: %v", err)
	}
	if len(a) != 1 || len(b) != 2 {
		t.Errorf("两次运行必须各自成文件：t-a %d 条、t-b %d 条", len(a), len(b))
	}
}

func TestRunLog_Discard(t *testing.T) {
	dir := t.TempDir()
	l := NewRunLog(dir)
	l.RecordStep("t-1", step("s1", types.StepSucceeded))
	if _, err := os.Stat(RunLogPath(dir, "t-1")); err != nil {
		t.Fatalf("日志应存在: %v", err)
	}
	DiscardRunLog(dir, "t-1")
	if _, err := os.Stat(RunLogPath(dir, "t-1")); !os.IsNotExist(err) {
		t.Error("Discard 之后日志应被删除")
	}
	DiscardRunLog(dir, "t-1")
	DiscardRunLog(dir, "a/b")
	if got, err := ReadRunLog(dir, "t-1"); err != nil || len(got) != 0 {
		t.Errorf("删掉之后应读不到内容：%v %d", err, len(got))
	}
}

func TestRunLog_MissingFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadRunLog(dir, "never-ran")
	if err != nil {
		t.Errorf("没有日志不该报错（读不到就是读不到）: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("应为空，实际 %d 条", len(got))
	}
}

func TestRunLog_OverlongNameIsNotWritten(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("a", 129)
	if RunLogPath(dir, long) != "" {
		t.Error("超长 taskID 不该有日志路径")
	}
	if RunLogPath(dir, strings.Repeat("a", 128)) == "" {
		t.Error("128 字符仍在允许范围内")
	}
}

func TestRunLog_ImplementsStepSink(t *testing.T) {
	var _ StepSink = (*RunLog)(nil)
}

func TestRunLog_NilReceiver(t *testing.T) {
	var l *RunLog
	l.RecordStep("t-1", step("s1", types.StepSucceeded))
}
