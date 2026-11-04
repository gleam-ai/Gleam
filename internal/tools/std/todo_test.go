package std

import (
	"context"
	"testing"
)

type fakeTodoSink struct {
	got []TodoItem
	err error
}

func (f *fakeTodoSink) PublishTodos(ctx context.Context, items []TodoItem) error {
	f.got = append([]TodoItem(nil), items...)
	return f.err
}

func TestTodoWrite_HappyPath(t *testing.T) {
	sink := &fakeTodoSink{}
	tool := NewTodoWrite(sink)
	out, err := tool.Execute(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"text": "读配置", "status": "done"},
			map[string]any{"text": "改代码", "status": "in_progress"},
			map[string]any{"text": "跑测试", "status": "pending"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.got) != 3 {
		t.Fatalf("sink got %d items, want 3", len(sink.got))
	}
	if sink.got[0].Status != "done" || sink.got[2].Status != "pending" {
		t.Errorf("status not preserved: %+v", sink.got)
	}
	m := out.(map[string]any)
	if m["count"] != 3 || m["done"] != 1 {
		t.Errorf("count/done wrong: %v", m)
	}
}

func TestTodoWrite_RejectsEmptyItems(t *testing.T) {
	tool := NewTodoWrite(&fakeTodoSink{})
	if _, err := tool.Execute(context.Background(), map[string]any{"items": []any{}}); err == nil {
		t.Fatal("empty items should be rejected")
	}
}

func TestTodoWrite_RejectsBadStatus(t *testing.T) {
	tool := NewTodoWrite(&fakeTodoSink{})
	_, err := tool.Execute(context.Background(), map[string]any{
		"items": []any{map[string]any{"text": "x", "status": "doing"}},
	})
	if err == nil {
		t.Fatal("invalid status should be rejected")
	}
}

func TestTodoWrite_RejectsEmptyText(t *testing.T) {
	tool := NewTodoWrite(&fakeTodoSink{})
	_, err := tool.Execute(context.Background(), map[string]any{
		"items": []any{map[string]any{"text": "  ", "status": "pending"}},
	})
	if err == nil {
		t.Fatal("empty text should be rejected")
	}
}

func TestTodoWrite_CapsAt20Items(t *testing.T) {
	sink := &fakeTodoSink{}
	tool := NewTodoWrite(sink)
	items := make([]any, 25)
	for i := range items {
		items[i] = map[string]any{"text": "task", "status": "pending"}
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"items": items}); err != nil {
		t.Fatal(err)
	}
	if len(sink.got) != 20 {
		t.Errorf("items capped at 20, got %d", len(sink.got))
	}
}

func TestTodoWrite_NilSinkFails(t *testing.T) {
	tool := NewTodoWrite(nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"items": []any{map[string]any{"text": "x", "status": "pending"}},
	})
	if err == nil {
		t.Fatal("nil sink should error")
	}
}
