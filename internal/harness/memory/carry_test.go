package memory

import "testing"

func TestCarryTurnsDefault(t *testing.T) {
	m := &Manager{}
	if got := m.CarryTurns(); got != CarryFull {
		t.Errorf("零值应当读成 %d，实际 %d", CarryFull, got)
	}
}

func TestSetCarryTurnsClampsAndReportsChange(t *testing.T) {
	m := &Manager{}
	if !m.SetCarryTurns(CarryTight) {
		t.Error("从正常态收到收紧态应当报告变化")
	}
	if got := m.CarryTurns(); got != CarryTight {
		t.Errorf("应当是 %d，实际 %d", CarryTight, got)
	}
	if m.SetCarryTurns(CarryTight) {
		t.Error("已经是这个值了，不该报告变化（水位附近每轮都会调用它）")
	}
	if !m.SetCarryTurns(CarryFull) {
		t.Error("松开也应当报告变化")
	}
	if got := m.CarryTurns(); got != CarryFull {
		t.Errorf("应当回到 %d，实际 %d", CarryFull, got)
	}

	m.SetCarryTurns(0)
	if got := m.CarryTurns(); got != CarryTight {
		t.Errorf("0 应当被钳到 %d，实际 %d", CarryTight, got)
	}
	m.SetCarryTurns(999)
	if got := m.CarryTurns(); got != CarryFull {
		t.Errorf("超大值应当被钳到 %d，实际 %d", CarryFull, got)
	}
}

func TestResetConversationRelaxesCarry(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, 20, 128, 100)
	if err != nil {
		t.Fatal(err)
	}
	m.SetCarryTurns(CarryTight)
	m.ResetConversation()
	if got := m.CarryTurns(); got != CarryFull {
		t.Errorf("新对话应当回到 %d，实际 %d", CarryFull, got)
	}
}
