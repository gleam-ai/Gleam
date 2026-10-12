package agent

import (
	"strconv"
	"strings"
	"testing"
)

func TestDiffText_AddDelAndLineNumbers(t *testing.T) {
	before := "第一行\n第二行\n第三行\n第四行\n"
	after := "第一行\n第二行改成了别的\n第三行\n第四行\n"

	d := DiffText(before, after)
	if d.Truncated {
		t.Fatalf("四处改动的小对比不该降级：%q", d.Note)
	}
	if d.Deleted != 1 || d.Added != 1 {
		t.Errorf("应是一删一增，实得 del=%d add=%d（%+v）", d.Deleted, d.Added, d.Lines)
	}
	var del, add DiffLine
	for _, l := range d.Lines {
		if l.Kind == DiffDel {
			del = l
		}
		if l.Kind == DiffAdd {
			add = l
		}
	}
	if del.Text != "第二行" || del.No != 2 {
		t.Errorf("删除行不对：%+v", del)
	}
	if add.Text != "第二行改成了别的" || add.No != 2 {
		t.Errorf("新增行不对：%+v", add)
	}
	if got := countDiffLines(d.Lines, DiffAdd); got != d.Added {
		t.Errorf("Added=%d 与 add 行数 %d 不符", d.Added, got)
	}
	if got := countDiffLines(d.Lines, DiffDel); got != d.Deleted {
		t.Errorf("Deleted=%d 与 del 行数 %d 不符", d.Deleted, got)
	}
}

func TestDiffText_KeepsContextAroundChange(t *testing.T) {
	before := numbered("", 20)
	after := strings.Replace(before, "第8行", "第8行（改了）", 1)

	d := DiffText(before, after)
	a, b := splitLines(before), splitLines(after)
	var ctx int
	for _, l := range d.Lines {
		if l.Kind != DiffCtx {
			continue
		}
		ctx++
		if l.No < 1 || l.No > len(a) || a[l.No-1] != l.Text {
			t.Errorf("上下文行 %d 在原文件里不是这个内容：%+v", l.No, l)
		}
		if l.No <= len(b) && b[l.No-1] != l.Text {
			t.Errorf("上下文行 %d 在新文件里已经变了，不该标成上下文：%+v", l.No, l)
		}
	}
	if ctx == 0 {
		t.Fatalf("应给出上下文行，实得 %+v", d.Lines)
	}
	if ctx == len(d.Lines) {
		t.Error("全是上下文、一行增删都没有，说明改动被漏掉了")
	}
}

func TestDiffText_CRLFSameContentIsNoChange(t *testing.T) {
	lf := "甲\n乙\n丙\n"
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")

	d := DiffText(lf, crlf)
	if d.Added != 0 || d.Deleted != 0 || len(d.Lines) != 0 {
		t.Errorf("只有行尾不同时不该有任何结果，实得 %+v", d.Lines)
	}
}

func TestDiffText_TrailingNewlineNotABlankLine(t *testing.T) {
	d := DiffText("一\n二\n", "一\n二")
	if d.Added != 0 || d.Deleted != 0 {
		t.Errorf("只差一个结尾换行不该算改动：%+v", d.Lines)
	}
}

func TestDiffText_EmptySides(t *testing.T) {
	d := DiffText("", "新文件第一行\n第二行\n")
	if d.Added != 2 || d.Deleted != 0 {
		t.Errorf("空 -> 两行应是两增，实得 %+v", d)
	}
	for _, l := range d.Lines {
		if l.Kind != DiffAdd {
			t.Errorf("不该出现非 add 行：%+v", l)
		}
	}

	d = DiffText("一\n二\n", "")
	if d.Deleted != 2 || d.Added != 0 {
		t.Errorf("两行 -> 空应是两删，实得 %+v", d)
	}

	d = DiffText("", "")
	if len(d.Lines) != 0 || d.Truncated {
		t.Errorf("两侧皆空应为空结果，实得 %+v", d)
	}
}

func TestDiffText_OnlyTailsOff(t *testing.T) {
	n := diffMaxLines/2 + 10
	before := numbered("旧", n)
	after := numbered("新", n)

	d := DiffText(before, after)
	if !d.Truncated {
		t.Fatalf("结果超过 %d 行应说明被截断", diffMaxLines)
	}
	if !strings.Contains(d.Note, strconv.Itoa(diffMaxLines)) {
		t.Errorf("说明里要给出保留了多少行，实得 %q", d.Note)
	}
	if len(d.Lines) > diffMaxLines+1 {
		t.Errorf("返回行数应受 %d 约束，实得 %d", diffMaxLines, len(d.Lines))
	}
	last := d.Lines[len(d.Lines)-1]
	if !strings.Contains(last.Text, "省略") {
		t.Errorf("末尾应有一句省略说明，实得 %+v", last)
	}
	for _, kind := range []string{DiffAdd, DiffDel} {
		prev := 0
		for _, l := range d.Lines {
			if l.Kind != kind {
				continue
			}
			if l.No <= prev {
				t.Fatalf("%s 行号不连续（%d 出现在 %d 之后），中间被掐过", kind, l.No, prev)
			}
			prev = l.No
		}
	}
	if d.Added != n || d.Deleted != n {
		t.Errorf("截断不该改计数，实得 add=%d del=%d，期望各 %d", d.Added, d.Deleted, n)
	}
}

func TestDiffText_HugeChangeDegradesToCounts(t *testing.T) {
	before := numbered("", diffMaxSideLines+1)
	after := strings.Repeat("完全不同的内容\n", diffMaxSideLines+1)

	d := DiffText(before, after)
	if !d.Truncated {
		t.Fatal("超过单侧行数上限应降级")
	}
	if len(d.Lines) != 0 {
		t.Errorf("降级后不该给出逐行结果（那是半截的），实得 %d 行", len(d.Lines))
	}
	if d.Note == "" || !strings.Contains(d.Note, "行数") {
		t.Errorf("降级要说明只剩增删计数，实得 %q", d.Note)
	}
	if d.Added == 0 || d.Deleted == 0 {
		t.Errorf("降级后计数仍要在，实得 add=%d del=%d", d.Added, d.Deleted)
	}
}

func TestDiffText_CellBudgetBlocksWorstShape(t *testing.T) {
	before := numbered("a", 1000)
	after := numbered("b", 1000)
	d := DiffText(before, after)
	if !d.Truncated {
		t.Fatalf("乘积超过 %d 应降级，实得 %d 行结果", diffCellBudget, len(d.Lines))
	}
}

func TestSplitLines_Shape(t *testing.T) {
	if got := splitLines(""); got != nil {
		t.Errorf("空串应切出 nil（不是长度 1 的空行），实得 %#v", got)
	}
	if got := splitLines("一\n二"); len(got) != 2 || got[1] != "二" {
		t.Errorf("无结尾换行也要有第二行，实得 %#v", got)
	}
	if got := splitLines("一\n"); len(got) != 1 {
		t.Errorf("结尾换行不该多出一个空行，实得 %#v", got)
	}
	if got := splitLines("一\r\n二\r\n"); len(got) != 2 || got[1] != "二" {
		t.Errorf("CRLF 要去掉 \\r，实得 %#v", got)
	}
}

func numbered(prefix string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("第" + prefix + strconv.Itoa(i) + "行\n")
	}
	return b.String()
}

func countDiffLines(lines []DiffLine, kind string) int {
	c := 0
	for _, l := range lines {
		if l.Kind == kind {
			c++
		}
	}
	return c
}
