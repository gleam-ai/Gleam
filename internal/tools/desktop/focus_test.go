package desktop

import "testing"

func TestFocusWindow_RejectsBadKeyword(t *testing.T) {
	if _, err := FocusWindow("  "); err == nil {
		t.Error("空关键词应报错")
	}
	if _, err := FocusWindow("a\nb"); err == nil {
		t.Error("含换行的关键词应报错")
	}
}
