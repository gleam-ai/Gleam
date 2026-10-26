package memory

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManager_OverflowCapturedWhenRingFull(t *testing.T) {
	m, err := Open(t.TempDir(), 3, 64, 100)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		m.AddTurn("user", fmt.Sprintf("早期对话 %d", i))
	}
	if got := m.Short.Len(); got != 3 {
		t.Fatalf("短期记忆 = %d，应为容量 3", got)
	}
	if len(m.overflow) != 2 {
		t.Fatalf("溢出区 = %d，应为 2", len(m.overflow))
	}
	if m.overflow[0].Content != "早期对话 0" || m.overflow[1].Content != "早期对话 1" {
		t.Errorf("溢出顺序错误: %+v", m.overflow)
	}
}

func TestManager_CompressUsesLLMSummary(t *testing.T) {
	m, _ := Open(t.TempDir(), 2, 64, 100)
	m.AddTurn("user", "第一轮")
	m.AddTurn("assistant", "第二轮")
	m.AddTurn("user", "触发溢出的第三轮")

	compressed := m.Compress(func(overflow string) (string, error) {
		if !strings.Contains(overflow, "第一轮") {
			t.Errorf("溢出内容缺失: %q", overflow)
		}
		return "用户讨论了第一轮内容（LLM 摘要）", nil
	})
	if !compressed {
		t.Fatal("应发生压缩")
	}
	if !strings.Contains(m.Summary(), "LLM 摘要") {
		t.Errorf("摘要 = %q", m.Summary())
	}
	if m.Compress(nil) {
		t.Error("空溢出区不应再次压缩")
	}
}

func TestManager_CompressFallsBackToExtractive(t *testing.T) {
	m, _ := Open(t.TempDir(), 2, 64, 100)
	m.AddTurn("user", "请记住项目代号是 Gleam，使用 Go 语言自研")
	m.AddTurn("assistant", "好的")
	m.AddTurn("user", "另外我喜欢中文回复")

	ok := m.Compress(func(string) (string, error) { return "", errors.New("LLM 不可用") })
	if !ok {
		t.Fatal("LLM 失败时也应完成压缩")
	}
	if s := m.Summary(); !strings.Contains(s, "Gleam") {
		t.Errorf("抽取式摘要缺失关键内容: %q", s)
	}
}

func TestManager_CompressNilSummarizer(t *testing.T) {
	m, _ := Open(t.TempDir(), 2, 64, 100)
	m.AddTurn("user", "甲")
	m.AddTurn("assistant", "乙")
	m.AddTurn("user", "丙")
	if !m.Compress(nil) {
		t.Fatal("nil 摘要器应走本地兜底")
	}
	if strings.TrimSpace(m.Summary()) == "" {
		t.Error("摘要不应为空")
	}
}

func TestManager_CompressStatePersistedAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	m, _ := Open(dir, 2, 64, 100)
	m.AddTurn("user", "早期内容")
	m.AddTurn("assistant", "x")
	m.AddTurn("user", "溢出内容")
	m.Compress(func(string) (string, error) { return "持久化摘要内容", nil })

	m2, err := Open(dir, 2, 64, 100)
	if err != nil {
		t.Fatal(err)
	}
	if s := m2.Summary(); !strings.Contains(s, "持久化摘要内容") {
		t.Errorf("重开后摘要丢失: %q", s)
	}
	if len(m2.overflow) != 0 {
		t.Errorf("重开后溢出区应为空: %+v", m2.overflow)
	}
}

func TestShortTerm_Oldest(t *testing.T) {
	s := NewShortTerm(3)
	if _, ok := s.Oldest(); ok {
		t.Error("空缓冲不应有最旧轮")
	}
	s.Add("user", "a")
	s.Add("user", "b")
	s.Add("user", "c")
	s.Add("user", "d")
	if old, ok := s.Oldest(); !ok || old.Content != "b" {
		t.Errorf("最旧 = %+v ok=%v，应为 b", old, ok)
	}
}

func TestManager_StatsAndClearSummary(t *testing.T) {
	m, _ := Open(t.TempDir(), 3, 64, 100)
	for i := 0; i < 5; i++ {
		m.AddTurn("user", "历史对话内容")
	}
	st := m.Stats()
	if st.ShortTurns != 3 || st.ShortCap != 3 || st.Overflow != 2 {
		t.Fatalf("stats = %+v", st)
	}
	m.Compress(func(string) (string, error) { return "统计测试摘要", nil })

	st2 := m.Stats()
	if st2.Summary == "" || st2.Overflow != 0 {
		t.Fatalf("压缩后 stats = %+v", st2)
	}

	m.ClearSummary()
	st3 := m.Stats()
	if st3.Summary != "" || st3.Overflow != 0 {
		t.Errorf("清空后 stats = %+v", st3)
	}
	m2, _ := Open(m.dir, 3, 64, 100)
	if s := m2.Summary(); s != "" {
		t.Errorf("清空后重开仍有摘要: %q", s)
	}
}

func TestAutoCompressionReproSwitchRecovery(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("model_failure=%v", fail), func(t *testing.T) {
			dir := t.TempDir()
			m, err := Open(dir, 2, 64, 100)
			if err != nil {
				t.Fatal(err)
			}
			m.SwitchConversation("convo-a")
			m.AddTurn("user", "A_EARLY_MARKER")
			m.AddTurn("assistant", "recent answer")
			m.AddTurn("user", "recent question")
			m.AdvanceCursor(3)
			entered, release := make(chan struct{}), make(chan struct{})
			done := make(chan bool, 1)
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			go func() {
				done <- m.Compress(func(text string) (string, error) {
					close(entered)
					<-release
					if fail {
						return "", errors.New("offline")
					}
					return text, nil
				})
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("summarizer not entered")
			}
			if turns, runes := m.Omission(); turns != 0 || runes != 0 {
				t.Errorf("in-flight batch counted as omitted: %d/%d", turns, runes)
			}
			if s := m.SummaryAnnotated(); !strings.Contains(s, "A_EARLY_MARKER") || strings.Contains(s, "已自动压缩") {
				t.Errorf("in-flight prompt fallback missing or misleading: %q", s)
			}
			reopened, err := Open(dir, 2, 64, 100)
			if err != nil {
				t.Fatal(err)
			}
			reopened.SwitchConversation("convo-a")
			if s := reopened.SummaryAnnotated(); !strings.Contains(s, "A_EARLY_MARKER") {
				t.Errorf("in-flight original was not persisted: %q", s)
			}
			m.SwitchConversation("convo-b")
			m.AddTurn("user", "B_ONLY_MARKER")
			unblock()
			select {
			case compressed := <-done:
				if compressed {
					t.Error("stale batch reported committed compression")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("compression did not finish")
			}
			if s := m.SummaryAnnotated(); strings.Contains(s, "A_EARLY_MARKER") {
				t.Fatalf("stale A summary contaminated B: %q", s)
			}
			m.SwitchConversation("convo-a")
			// A failed or invalidated batch must remain recoverable by its owner.
			available := m.Summary() + renderTurns(m.overflow) + renderTurns(m.Short.Recent(0))
			if !strings.Contains(available, "A_EARLY_MARKER") {
				t.Errorf("A lost in-flight turn: summary=%q overflow=%d short=%d cursor=%d",
					m.Summary(), m.Stats().Overflow, m.Short.Len(), m.Cursor())
			}
			if turns, runes := m.Omission(); turns != 0 || runes != 0 {
				t.Errorf("invalidated batch counted as omitted after restore: %d/%d", turns, runes)
			}
			if !m.Compress(nil) {
				t.Fatal("restored original should remain compressible")
			}
			if turns, _ := m.Omission(); turns != 1 {
				t.Errorf("restored batch should be counted exactly once: %d", turns)
			}
		})
	}
}

func TestCompressionPendingSurvivesOverflowCap(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, 2, 64, 100)
	if err != nil {
		t.Fatal(err)
	}
	m.AddTurn("user", "IN_FLIGHT_MARKER")
	m.AddTurn("assistant", "recent answer")
	m.AddTurn("user", "recent question")
	if !m.Compress(func(string) (string, error) {
		for i := 0; i < overflowCap+10; i++ {
			m.AddTurn("user", strings.Repeat("new history", 100))
		}
		if s := m.SummaryAnnotated(); !strings.Contains(s, "IN_FLIGHT_MARKER") || len([]rune(s)) > 800 {
			t.Errorf("in-flight fallback missing or unbounded: %q", s)
		}
		if m.Compress(nil) {
			t.Error("another compression started before the pending batch committed")
		}
		m.SaveContext()
		reopened, err := Open(dir, 2, 64, 100)
		if err != nil {
			t.Fatal(err)
		}
		if got := reopened.Stats(); got.Overflow != overflowCap+1 || got.SavedTokens != 0 || got.Summary != "" {
			t.Errorf("pending and queued originals not persisted: %+v", got)
		}
		if got := reopened.overflow[0].Content; got != "IN_FLIGHT_MARKER" {
			t.Errorf("pending original was evicted: %q", got)
		}
		return "committed in-flight summary", nil
	}) {
		t.Fatal("pending batch should commit")
	}
	if got := m.Stats().Overflow; got != overflowCap {
		t.Errorf("commit consumed newly queued turns: overflow=%d", got)
	}
	if turns, _ := m.Omission(); turns != 1 {
		t.Errorf("commit counted uncompressed turns: %d", turns)
	}
}

func TestCompressionGenerationGuards(t *testing.T) {
	m, err := Open(t.TempDir(), 2, 64, 100)
	if err != nil {
		t.Fatal(err)
	}
	gen := m.Generation()
	m.SwitchConversation("new-conversation")
	for i := 0; i < 3; i++ {
		m.AddTurn("user", "new conversation")
	}
	called := false
	if m.CompressForGeneration(gen, func(string) (string, error) {
		called = true
		return "stale worker summary", nil
	}) || called {
		t.Error("stale worker started summarizing the new generation")
	}
	if m.SetCarryTurnsForGeneration(gen, CarryTight) || m.CarryTurns() != CarryFull {
		t.Error("stale action tightened the new generation")
	}
	if _, current := m.CarryTurnsForGeneration(gen); current {
		t.Error("stale generation returned a notification snapshot")
	}
	gen = m.Generation()
	if !m.SetCarryTurnsForGeneration(gen, CarryTight) {
		t.Error("current generation should allow tightening")
	}
	if carry, current := m.CarryTurnsForGeneration(gen); !current || carry != CarryTight {
		t.Errorf("notification snapshot=%d/%v want=%d/true", carry, current, CarryTight)
	}
	if !m.CompressForGeneration(gen, nil) {
		t.Error("current generation should remain compressible")
	}
}
