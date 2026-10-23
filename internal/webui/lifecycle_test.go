package webui

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/conversation"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

type scheduledBlockingClient struct {
	entered  chan struct{}
	observed chan llm.ChatRequest
}

func (c *scheduledBlockingClient) Name() string { return "synthetic" }
func (c *scheduledBlockingClient) Chat(ctx context.Context, r llm.ChatRequest) (string, error) {
	if r.TaskID == "scheduled-A" {
		close(c.entered)
		<-ctx.Done()
		return "", ctx.Err()
	}
	c.observed <- r
	return "web answer", nil
}
func (c *scheduledBlockingClient) ChatStream(ctx context.Context, r llm.ChatRequest, _ func(string)) (string, error) {
	return c.Chat(ctx, r)
}

func TestWebSubmitDuringScheduledTask(t *testing.T) {
	f := newFixture(t, nil)
	a, srv := f.agent, f.srv
	a.Cfg.Agent.ContextCompress = false
	a.Cfg.Agent.GEOEnabled = false
	a.Cfg.Agent.ChatAcceptance = false
	c, err := a.Convos.Create()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Convos.Append(c.ID, conversation.Message{Role: "user", Content: "B_ONLY_PERSISTED_CONTEXT"}); err != nil {
		t.Fatal(err)
	}
	client := &scheduledBlockingClient{entered: make(chan struct{}), observed: make(chan llm.ChatRequest, 4)}
	a.LLM = client
	events := srv.subscribe()
	defer srv.unsubscribe(events)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *types.GoalResult, 1)
	go func() { done <- a.RunGoal(ctx, types.GoalRequest{Goal: "PRIVATE_SCHEDULE_A", TaskID: "scheduled-A"}) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("scheduled task did not exit")
		}
	})
	select {
	case <-client.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled task did not start")
	}
	body, _ := json.Marshal(map[string]any{"goal": "B new question", "task_mode": "chat", "conversation_id": c.ID})
	submit := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.handleGoalSubmit(w, httptest.NewRequest("POST", "/api/goals", strings.NewReader(string(body))))
		return w
	}
	if w := submit(); w.Code != 409 {
		t.Errorf("busy submit HTTP=%d, want 409", w.Code)
	}
	select {
	case r := <-client.observed:
		t.Errorf("busy submit called model with %v", r.Messages)
	default:
	}
	cancel()
	select {
	case res := <-done:
		if res.Status != types.GoalCancelled {
			t.Fatalf("cancel status=%s", res.Status)
		}
		done <- res // cleanup can join even after a failed assertion below
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	if w := submit(); w.Code != 200 {
		t.Fatalf("idle submit HTTP=%d: %s", w.Code, w.Body.String())
	}
	select {
	case got := <-client.observed:
		var joined string
		for _, m := range got.Messages {
			joined += m.Content + "\n"
		}
		if strings.Contains(joined, "PRIVATE_SCHEDULE_A") || !strings.Contains(joined, "B_ONLY_PERSISTED_CONTEXT") {
			t.Errorf("wrong conversation: %s", joined)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("web request absent")
	}
	for {
		select {
		case ev := <-events:
			if ev.Type == "completed" {
				return
			}
		case <-time.After(3 * time.Second):
			t.Fatal("web completion absent")
		}
	}
}
