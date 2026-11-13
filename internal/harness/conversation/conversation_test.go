package conversation

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestCreateAppendList(t *testing.T) {
	s := newStore(t)
	c, err := s.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.ID == "" || c.Title != "新对话" {
		t.Fatalf("新会话默认值异常: %+v", c)
	}
	if _, err := s.Append(c.ID, Message{Role: "user", Content: "帮我整理下载目录的文件", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("Append user: %v", err)
	}
	if _, err := s.Append(c.ID, Message{Role: "assistant", Content: "好的，已整理完成", Status: "success", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("Append assistant: %v", err)
	}
	got, err := s.Get(c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("消息数=%d, 期望 2", len(got.Messages))
	}
	if got.Title != "帮我整理下载目录的文件" {
		t.Fatalf("首条用户消息未生成标题: %q", got.Title)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List: n=%d err=%v", len(list), err)
	}
	if list[0].Preview == "" || list[0].Count != 2 {
		t.Fatalf("摘要异常: %+v", list[0])
	}
}

func TestTitleTruncate(t *testing.T) {
	s := newStore(t)
	c, _ := s.Create()
	long := "这是一个非常非常非常非常非常非常非常非常非常非常非常非常长的第一句话用于测试截断"
	if _, err := s.Append(c.ID, Message{Role: "user", Content: long, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(c.ID)
	if r := []rune(got.Title); len(r) != maxTitleRunes {
		t.Fatalf("标题长度=%d, 期望 %d（含省略号）", len(r), maxTitleRunes)
	}
}

func TestTruncateEmojiAndUnicode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		wantEnd  string // expected suffix
	}{
		{"emoji_pair", "Hello 👨‍👩‍👧‍👦 world", 10, "…"},
		{"chinese_char", "你好世界 🌍", 4, "…"},
		{"exact_fit", "abc 定", 3, ""},      // exactly 3 runes, no ellipsis
		{"one_rune", "a", 1, ""},           // single rune within limit
		{"empty", "", 5, ""},               // empty string
		{"only_ellipsis", "😀", 1, "…"},     // emoji replaced by ellipsis
		{"multi_emoji", "🎉🎊🎈test", 5, "…"}, // multiple emoji
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxRunes)
			title := got

			// over the limit must end with the ellipsis
			if len([]rune(tt.input)) > tt.maxRunes {
				if !strings.HasSuffix(title, "…") {
					t.Errorf("截断后应以省略号结尾：%q", title)
				}
			}

			// total length including ellipsis stays within maxRunes
			if len([]rune(title)) > tt.maxRunes {
				t.Errorf("标题长度=%d, 超过上限 %d: %q", len([]rune(title)), tt.maxRunes, title)
			}

			// no split surrogate pairs
			r := []rune(title)
			for i := 0; i < len(r)-1; i++ {
				if r[i] >= 0xD800 && r[i] <= 0xDBFF {
					// a high surrogate must be followed by a low one
					if i+1 >= len(r) || r[i+1] < 0xDC00 || r[i+1] > 0xDFFF {
						t.Errorf("代理对被切断：%v", r)
					}
				}
			}
		})
	}
}

func TestRenameDelete(t *testing.T) {
	s := newStore(t)
	c, _ := s.Create()
	renamed, err := s.Rename(c.ID, "  我的周报任务\n第二行 ")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if renamed.Title != "我的周报任务 第二行" {
		t.Fatalf("重命名清洗异常: %q", renamed.Title)
	}
	if err := s.Delete(c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(c.ID); err == nil {
		t.Fatal("删除后仍可读取")
	}
	if err := s.Delete(c.ID); err != nil {
		t.Fatalf("重复删除应幂等: %v", err)
	}
}

func TestListOrdering(t *testing.T) {
	s := newStore(t)
	base := time.Now()
	for i := 0; i < 3; i++ {
		c, _ := s.Create()
		_, _ = s.Append(c.ID, Message{Role: "user", Content: "对话" + string(rune('A'+i)), CreatedAt: base.Add(time.Duration(i) * time.Minute)})
	}
	list, err := s.List()
	if err != nil || len(list) != 3 {
		t.Fatalf("List n=%d err=%v", len(list), err)
	}
	if list[0].Title != "对话C" {
		t.Fatalf("列表未按最近更新倒序: 首项=%q", list[0].Title)
	}
}

func TestRejectTraversalID(t *testing.T) {
	s := newStore(t)
	if _, err := s.Get(filepath.FromSlash("../x")); err == nil {
		t.Fatal("期望拒绝目录穿越 ID")
	}
	if err := s.Delete("ab/cd"); err == nil {
		t.Fatal("期望拒绝带分隔符 ID")
	}
}
