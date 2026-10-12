package growth

import "testing"

func TestRecordKeepsRuleSet(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Record(Entry{Type: "task_completed", Goal: "整理表格", Score: 88, RuleSet: "abc1234567"})

	if got := l.Recent(1); len(got) != 1 || got[0].RuleSet != "abc1234567" {
		t.Fatalf("内存里的规则集丢了：%+v", got)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	all := reopened.All()
	if len(all) != 1 {
		t.Fatalf("重开后有 %d 条记录，期望 1 条", len(all))
	}
	if all[0].RuleSet != "abc1234567" {
		t.Fatalf("落盘后规则集丢了：%+v", all[0])
	}
}

func TestRecordRuleSetOmittedForChat(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Record(Entry{Type: "task_completed", Goal: "你好"})
	if got := l.Recent(1)[0].RuleSet; got != "" {
		t.Fatalf("未记规则集时应当是空串，实际 %q", got)
	}
}
