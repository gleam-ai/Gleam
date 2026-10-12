package growth

import (
	"math"
	"path/filepath"
	"testing"
)

func TestStats_FirstPassAndReworks(t *testing.T) {
	l, err := Open(filepath.Join(t.TempDir(), "growth.json"))
	if err != nil {
		t.Fatal(err)
	}
	l.Record(Entry{Type: "task_completed", Goal: "甲", Score: 90, Delivery: true, FirstPass: true})
	l.Record(Entry{Type: "task_completed", Goal: "乙", Score: 88, Delivery: true, FirstPass: true})
	l.Record(Entry{Type: "task_completed", Goal: "丙（返工两轮才成）", Score: 80, Delivery: true, Reworks: 2})
	l.Record(Entry{Type: "task_completed", Goal: "闲聊两句", Score: 100})
	l.Record(Entry{Type: "task_aborted", Goal: "丁，用户不等了", Reworks: 1})

	st := l.Stats()
	if st.TotalTasks != 4 {
		t.Errorf("TotalTasks = %d，应为 4", st.TotalTasks)
	}
	if st.FirstPassBase != 3 {
		t.Errorf("FirstPassBase = %d，应为 3（只算记过口径的完成任务，对话任务不进分母）", st.FirstPassBase)
	}
	if st.FirstPassCount != 2 {
		t.Errorf("FirstPassCount = %d，应为 2", st.FirstPassCount)
	}
	if want := 2.0 / 3.0; math.Abs(st.FirstPassRate-want) > 1e-9 {
		t.Errorf("FirstPassRate = %.4f，应为 %.4f（2/3）", st.FirstPassRate, want)
	}
	if st.ReworksTotal != 3 {
		t.Errorf("ReworksTotal = %d，应为 3（含中断任务中断前已发生的返工）", st.ReworksTotal)
	}
}

func TestStats_FirstPassRateNoBase(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "growth.json"))
	l.Record(Entry{Type: "task_completed", Goal: "闲聊", Score: 100})
	l.Record(Entry{Type: "task_completed", Goal: "老条目，没记过口径", Score: 70})

	st := l.Stats()
	if st.FirstPassBase != 0 {
		t.Errorf("FirstPassBase = %d，应为 0", st.FirstPassBase)
	}
	if st.FirstPassRate != 0 {
		t.Errorf("FirstPassRate = %.4f，无分母时应为 0（不是 NaN）", st.FirstPassRate)
	}
	if st.ReworksTotal != 0 {
		t.Errorf("ReworksTotal = %d，应为 0", st.ReworksTotal)
	}
}

func TestStats_FirstPassEmpty(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "growth.json"))
	st := l.Stats()
	if st.FirstPassRate != 0 || st.FirstPassBase != 0 || st.ReworksTotal != 0 {
		t.Errorf("空日志的交付口径应为零值: %+v", st)
	}
}
