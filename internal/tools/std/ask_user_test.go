package std

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeAsker struct {
	gotQuestion UserQuestion
	answer      UserAnswer
	err         error
	block       chan struct{} // when non-nil, blocks until the channel is closed
}

func (f *fakeAsker) AskUser(ctx context.Context, q UserQuestion) (UserAnswer, error) {
	f.gotQuestion = q
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return UserAnswer{}, ctx.Err()
		}
	}
	return f.answer, f.err
}

func TestAskUser_PassesQuestionAndChoices(t *testing.T) {
	f := &fakeAsker{answer: UserAnswer{Choice: "方案 B"}}
	tool := NewAskUser(f)
	out, err := tool.Execute(context.Background(), map[string]any{
		"question": "选哪个方案？",
		"choices":  []any{"方案 A", "方案 B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.gotQuestion.Question != "选哪个方案？" {
		t.Errorf("question = %q", f.gotQuestion.Question)
	}
	if len(f.gotQuestion.Choices) != 2 || f.gotQuestion.Choices[1] != "方案 B" {
		t.Errorf("choices = %v", f.gotQuestion.Choices)
	}
	m := out.(map[string]any)
	if m["choice"] != "方案 B" {
		t.Errorf("choice = %v", m["choice"])
	}
}

func TestAskUser_FreeTextAnswer(t *testing.T) {
	f := &fakeAsker{answer: UserAnswer{FreeText: "我觉得应该走中间路线"}}
	tool := NewAskUser(f)
	out, err := tool.Execute(context.Background(), map[string]any{"question": "你怎么看？"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["free_text"] != "我觉得应该走中间路线" {
		t.Errorf("free_text = %v", m["free_text"])
	}
}

func TestAskUser_RejectsEmptyQuestion(t *testing.T) {
	tool := NewAskUser(&fakeAsker{})
	if _, err := tool.Execute(context.Background(), map[string]any{"question": "  "}); err == nil {
		t.Fatal("empty question should be rejected")
	}
}

func TestAskUser_NilAskerFails(t *testing.T) {
	tool := NewAskUser(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"question": "hi"}); err == nil {
		t.Fatal("nil asker should error, not panic")
	}
}

func TestAskUser_CapsChoicesAt6(t *testing.T) {
	f := &fakeAsker{answer: UserAnswer{Choice: "x"}}
	tool := NewAskUser(f)
	many := []any{"1", "2", "3", "4", "5", "6", "7", "8", "9"}
	if _, err := tool.Execute(context.Background(), map[string]any{"question": "q", "choices": many}); err != nil {
		t.Fatal(err)
	}
	if len(f.gotQuestion.Choices) != 6 {
		t.Errorf("choices capped at 6, got %d", len(f.gotQuestion.Choices))
	}
}

func TestAskUser_AskerErrorPropagates(t *testing.T) {
	sentinel := errors.New("user closed window")
	tool := NewAskUser(&fakeAsker{err: sentinel})
	_, err := tool.Execute(context.Background(), map[string]any{"question": "q"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAskUser_ContextCancelUnblocks(t *testing.T) {
	blocker := make(chan struct{})
	f := &fakeAsker{block: blocker}
	tool := NewAskUser(f)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := tool.Execute(ctx, map[string]any{"question": "q"})
	if err == nil {
		t.Fatal("expected ctx cancel error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("ctx cancel should be prompt, took %v", elapsed)
	}
}
