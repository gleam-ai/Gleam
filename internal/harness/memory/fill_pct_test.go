package memory

import "testing"

func TestManager_StatsFillPct(t *testing.T) {
	m, err := Open(t.TempDir(), 4, 64, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Stats().FillPct; got != 0 {
		t.Errorf("空窗口的百分比 = %d，应为 0（一打开就显示 25%% 是虚警）", got)
	}

	for i := 0; i < 3; i++ {
		m.AddTurn("user", "第"+string(rune('0'+i))+"轮")
	}
	if got := m.Stats().FillPct; got != 75 {
		t.Errorf("3/4 轮的百分比 = %d，应为 75", got)
	}

	m.AddTurn("user", "把窗口挤满的一轮")
	m.AddTurn("user", "溢出的一轮")
	st := m.Stats()
	if st.FillPct != 100 {
		t.Errorf("满窗的百分比 = %d，应为 100", st.FillPct)
	}
	if st.Overflow == 0 {
		t.Error("满窗后应留下待压缩溢出，否则「满了→去压缩」这条线在源头就断了")
	}
}

func TestFillPct_ZeroCap(t *testing.T) {
	for _, c := range []struct{ turns, cap, want int }{
		{0, 0, 0}, {5, 0, 0},
		{0, 4, 0}, {1, 4, 25}, {2, 4, 50}, {4, 4, 100},
		{6, 4, 100},
	} {
		if got := fillPct(c.turns, c.cap); got != c.want {
			t.Errorf("fillPct(%d, %d) = %d，应为 %d", c.turns, c.cap, got, c.want)
		}
	}
}
