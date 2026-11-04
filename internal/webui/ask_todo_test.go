package webui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gleam/internal/agent"
	"gleam/internal/config"
	"gleam/internal/tools/std"
)

// newTestAgent builds a minimal usable Agent: NewServer needs DataDir
// (feedback storage).
func newTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	return &agent.Agent{Cfg: &config.Config{DataDir: t.TempDir()}}
}

// Server.AskUser should broadcast a question event and return once the user
// answers.
func TestServerAskUser_RoundTrip(t *testing.T) {
	a := newTestAgent(t)
	s := NewServer(a)

	// Simulate the frontend: listen on the SSE channel and answer as soon as
	// a question arrives.
	ch := s.subscribe()
	defer s.unsubscribe(ch)

	ctx := agent.WithTaskIDForTest(context.Background(), "task-1")
	done := make(chan std.UserAnswer, 1)
	errCh := make(chan error, 1)
	go func() {
		ans, err := s.AskUser(ctx, std.UserQuestion{Question: "选 A 还是 B？", Choices: []string{"A", "B"}})
		if err != nil {
			errCh <- err
			return
		}
		done <- ans
	}()

	// Wait until the question event arrives.
	var qID string
	timeout := time.After(2 * time.Second)
	for qID == "" {
		select {
		case ev := <-ch:
			if ev.Type == "question" {
				// Extract the id from the broadcast.
				var payload map[string]any
				if err := jsonUnmarshal(ev.Data, &payload); err == nil {
					qID, _ = payload["id"].(string)
				}
			}
		case <-timeout:
			t.Fatal("did not receive question event")
		}
	}

	if err := s.AnswerQuestion(qID, questionAnswer{Choice: "A"}); err != nil {
		t.Fatalf("AnswerQuestion: %v", err)
	}

	select {
	case ans := <-done:
		if ans.Choice != "A" {
			t.Errorf("answer choice = %q, want A", ans.Choice)
		}
	case err := <-errCh:
		t.Fatalf("AskUser: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("AskUser did not return")
	}
}

func TestServerAskUser_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("5min timeout; skip in short mode")
	}
	// Do not actually wait 5 minutes; only the ctx cancellation path is
	// exercised here.
	a := newTestAgent(t)
	s := NewServer(a)
	ctx, cancel := context.WithTimeout(agent.WithTaskIDForTest(context.Background(), "t"), 50*time.Millisecond)
	defer cancel()
	_, err := s.AskUser(ctx, std.UserQuestion{Question: "?"})
	if err == nil {
		t.Fatal("expected ctx cancel")
	}
}

func TestServerPublishTodos_BroadcastsAndStores(t *testing.T) {
	a := newTestAgent(t)
	s := NewServer(a)
	ch := s.subscribe()
	defer s.unsubscribe(ch)

	ctx := agent.WithTaskIDForTest(context.Background(), "task-x")
	items := []std.TodoItem{
		{Text: "读文件", Status: "done"},
		{Text: "写代码", Status: "in_progress"},
	}
	if err := s.PublishTodos(ctx, items); err != nil {
		t.Fatal(err)
	}
	// The broadcast should include a todos event.
	gotEvent := false
	timeout := time.After(time.Second)
	for !gotEvent {
		select {
		case ev := <-ch:
			if ev.Type == "todos" {
				gotEvent = true
			}
		case <-timeout:
			t.Fatal("no todos event")
		}
	}
	// Todos() should return the stored snapshot.
	got := s.Todos("task-x")
	if len(got) != 2 || got[0].Status != "done" || got[1].Status != "in_progress" {
		t.Errorf("Todos = %+v", got)
	}
}

// Tiny helper to avoid repeating json.Unmarshal throughout the tests.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
