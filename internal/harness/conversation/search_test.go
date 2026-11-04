package conversation

import (
	"strings"
	"testing"
	"time"
)

func newSearchTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustConvo(t *testing.T, s *Store, spaceID string, msgs ...Message) *Conversation {
	t.Helper()
	c, err := s.Create(spaceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		m.CreatedAt = time.Now()
		if _, err := s.Append(c.ID, m); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestStoreSearch_TermAndRanking(t *testing.T) {
	s := newSearchTestStore(t)

	// Conversation A hits twice: both messages mention deployment.
	a := mustConvo(t, s, "",
		Message{Role: "user", Content: "帮我写一个部署脚本"},
		Message{Role: "assistant", Content: "部署脚本已写好，放在 scripts/deploy.sh"},
	)
	// Conversation B matches only via its title.
	b := mustConvo(t, s, "", Message{Role: "user", Content: "今天天气怎么样"})
	if _, err := s.Append(b.ID, Message{Role: "assistant", Content: "晴天"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(b.ID, "部署脚本的讨论"); err != nil {
		t.Fatal(err)
	}
	// Conversation C is entirely unrelated.
	mustConvo(t, s, "", Message{Role: "user", Content: "晚饭吃什么"})

	hits, err := s.Search("部署", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2: %+v", len(hits), hits)
	}
	// A has more hits, so it must rank first.
	if hits[0].ID != a.ID {
		t.Errorf("first hit = %s, want %s（按命中次数降序）", hits[0].ID, a.ID)
	}
	if hits[0].Count < 2 {
		t.Errorf("count = %d, want >= 2", hits[0].Count)
	}
	if len(hits[0].MatchedMessages) != 2 {
		t.Errorf("matched messages = %v, want [0 1]", hits[0].MatchedMessages)
	}
	// B matched via title only: still listed, but with no message excerpts.
	var hitB *SearchHit
	for i := range hits {
		if hits[i].ID == b.ID {
			hitB = &hits[i]
		}
	}
	if hitB == nil {
		t.Fatalf("标题命中的会话 %s 未出现", b.ID)
	}
	if len(hitB.MatchedMessages) != 0 {
		t.Errorf("B 不应有消息命中: %v", hitB.MatchedMessages)
	}
}

func TestStoreSearch_MultiTermAND(t *testing.T) {
	s := newSearchTestStore(t)
	mustConvo(t, s, "", Message{Role: "user", Content: "把 redis 缓存加上过期时间"})
	mustConvo(t, s, "", Message{Role: "user", Content: "redis 持久化用 RDB 还是 AOF"})

	hits, err := s.Search("redis 过期", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("AND 语义应只命中 1 个会话，got %d", len(hits))
	}
}

func TestStoreSearch_ExcerptWindow(t *testing.T) {
	s := newSearchTestStore(t)
	long := strings.Repeat("背景介绍。", 40) + "关键结论是缓存击穿要用互斥锁。" + strings.Repeat("后续说明。", 40)
	mustConvo(t, s, "", Message{Role: "assistant", Content: long})

	hits, err := s.Search("互斥锁", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || len(hits[0].Excerpts) == 0 {
		t.Fatalf("hits = %+v", hits)
	}
	ex := hits[0].Excerpts[0]
	if !strings.Contains(ex, "互斥锁") {
		t.Errorf("片段缺少命中词: %q", ex)
	}
	if len([]rune(ex)) > 2*excerptRadius+40 { // window 120 + the "message#N (role):" prefix and ellipsis
		t.Errorf("片段过长: %d runes", len([]rune(ex)))
	}
}

func TestStoreSearch_EmptyQueryAndLimit(t *testing.T) {
	s := newSearchTestStore(t)
	if _, err := s.Search("  ", 0); err == nil {
		t.Fatal("空检索词应报错")
	}
	for i := 0; i < 5; i++ {
		mustConvo(t, s, "", Message{Role: "user", Content: "缓存刷新"})
	}
	hits, err := s.Search("缓存", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("limit=2 应只返回 2 条，got %d", len(hits))
	}
}

func TestStoreSearch_CaseInsensitive(t *testing.T) {
	s := newSearchTestStore(t)
	mustConvo(t, s, "", Message{Role: "user", Content: "用 GORM 连 PostgreSQL"})

	hits, err := s.Search("gorm postgresql", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("大小写不敏感匹配失败，got %d hits", len(hits))
	}
}
