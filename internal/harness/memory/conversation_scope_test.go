package memory

import "testing"

func TestSwitchConversationKeepsSummaryCarryAndCursor(t *testing.T) {
	m, err := Open(t.TempDir(), 4, 128, 100)
	if err != nil {
		t.Fatal(err)
	}
	m.SwitchConversation("convo-a")
	m.AddTurn("user", "alpha first")
	m.AddTurn("assistant", "alpha answer")
	m.AddTurn("user", "alpha second")
	m.AddTurn("assistant", "alpha answer 2")
	m.AddTurn("user", "alpha third")
	m.AddTurn("assistant", "alpha answer 3")
	if !m.Compress(func(string) (string, error) { return "summary-marker", nil }) {
		t.Fatal("前提不成立：应当有溢出轮可汇总")
	}
	m.AdvanceCursor(6)
	m.SetCarryTurns(CarryTight)

	// Re-opening the bound conversation is a no-op, not a reset.
	m.SwitchConversation("convo-a")
	if got := m.Stats(); got.Summary != "summary-marker" {
		t.Fatalf("re-open cleared summary: %q", got.Summary)
	}
	if got := m.CarryTurns(); got != CarryTight {
		t.Fatalf("re-open reset carry: %d", got)
	}
	if got := m.Cursor(); got != 6 {
		t.Fatalf("re-open reset cursor: %d", got)
	}

	m.SwitchConversation("convo-b")
	if got := m.Stats(); got.Summary != "" {
		t.Fatalf("new conversation inherited summary: %q", got.Summary)
	}
	if got := m.CarryTurns(); got != CarryFull {
		t.Fatalf("new conversation inherited carry: %d", got)
	}
	if got := m.Cursor(); got != 0 {
		t.Fatalf("new conversation inherited cursor: %d", got)
	}

	m.SwitchConversation("convo-a")
	if got := m.Stats(); got.Summary != "summary-marker" {
		t.Fatalf("switching back lost summary: %q", got.Summary)
	}
	if got := m.CarryTurns(); got != CarryTight {
		t.Fatalf("switching back lost carry: %d", got)
	}
	if got := m.Cursor(); got != 6 {
		t.Fatalf("switching back lost cursor: %d", got)
	}
}

func TestConversationContextSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, 4, 128, 100)
	if err != nil {
		t.Fatal(err)
	}
	m.SwitchConversation("convo-a")
	for i := 0; i < 6; i++ {
		m.AddTurn("user", "turn")
	}
	if !m.Compress(func(string) (string, error) { return "persisted-summary", nil }) {
		t.Fatal("前提不成立：应当有溢出轮可汇总")
	}
	m.AdvanceCursor(6)
	m.SetCarryTurns(CarryTight)
	m.SaveContext()

	m2, err := Open(dir, 4, 128, 100)
	if err != nil {
		t.Fatal(err)
	}
	m2.SwitchConversation("convo-a")
	if got := m2.Stats().Summary; got != "persisted-summary" {
		t.Fatalf("summary after reopen = %q", got)
	}
	if got := m2.CarryTurns(); got != CarryTight {
		t.Fatalf("carry after reopen = %d", got)
	}
	if got := m2.Cursor(); got != 6 {
		t.Fatalf("cursor after reopen = %d", got)
	}
}
