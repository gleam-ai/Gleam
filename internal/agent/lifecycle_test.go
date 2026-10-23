package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gleam/internal/harness/conversation"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

// lifecycleClient lets tests stop at a model boundary without network or sleeps.
type lifecycleClient struct {
	chat func(context.Context, llm.ChatRequest) (string, error)
}

func (c *lifecycleClient) Name() string { return "lifecycle-test" }
func (c *lifecycleClient) Chat(ctx context.Context, r llm.ChatRequest) (string, error) {
	return c.chat(ctx, r)
}
func (c *lifecycleClient) ChatStream(ctx context.Context, r llm.ChatRequest, _ func(string)) (string, error) {
	return c.Chat(ctx, r)
}

func lifecycleAgent(t *testing.T) *Agent {
	t.Helper()
	a := newFixture(t, nil).a
	a.Cfg.Agent.ContextCompress = false
	a.Cfg.Agent.GEOEnabled = false
	a.Cfg.Agent.ChatAcceptance = false
	a.Cfg.Agent.MaxReplans = 0
	var err error
	a.Convos, err = conversation.Open(a.Cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestGoalAdmissionAndCancellationKeepConversationsSeparate(t *testing.T) {
	a := lifecycleAgent(t)
	c, err := a.NewConversation()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Convos.Append(c.ID, conversation.Message{Role: "user", Content: "B_ONLY_HISTORY"}); err != nil {
		t.Fatal(err)
	}
	entered, cancelled, release := make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	a.LLM = &lifecycleClient{chat: func(ctx context.Context, r llm.ChatRequest) (string, error) {
		entered <- struct{}{}
		<-ctx.Done()
		close(cancelled)
		<-release // cancellation must not release admission before this returns
		return "", ctx.Err()
	}}
	const contenders = 12
	type admission struct {
		done <-chan *types.GoalResult
		err  error
	}
	results := make(chan admission, contenders)
	start := make(chan struct{})
	for i := 0; i < contenders; i++ {
		go func(i int) {
			<-start
			done, err := a.StartGoal(ctx, types.GoalRequest{Goal: "PRIVATE_A", TaskID: fmt.Sprintf("contender-%d", i), TaskMode: types.TaskChat})
			results <- admission{done, err}
		}(i)
	}
	close(start)
	var winner <-chan *types.GoalResult
	for i := 0; i < contenders; i++ {
		select {
		case r := <-results:
			if r.err == nil {
				if winner != nil {
					t.Fatal("more than one admitted goal")
				}
				winner = r.done
			} else if !errors.Is(r.err, ErrAgentBusy) {
				t.Fatal(r.err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("admission blocked on a model request")
		}
	}
	if winner == nil || a.RunningTasks() != 1 {
		t.Fatal("expected one reserved/running goal")
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("model not entered")
	}
	if _, err := a.LoadConversationContext(c.ID); !errors.Is(err, ErrAgentBusy) {
		t.Fatalf("activation err=%v", err)
	}
	if _, err := a.NewConversation(); err != nil {
		t.Fatal(err)
	}
	a.ResetConversation()
	if turns := a.Mem.Short.Recent(0); len(turns) != 1 || turns[0].Content != "PRIVATE_A" {
		t.Fatalf("busy activation changed memory: %v", turns)
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("model did not observe cancel")
	}
	req := types.GoalRequest{Goal: "B next question", ConversationID: c.ID, TaskMode: types.TaskChat}
	if r := a.RunGoal(context.Background(), req); r.Status != types.GoalFailed || !strings.Contains(r.Error, ErrAgentBusy.Error()) {
		t.Fatalf("cancel pending admitted a second goal: %+v", r)
	}
	unblock()
	select {
	case r := <-winner:
		if r.Status != types.GoalCancelled {
			t.Fatalf("status=%s", r.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not exit")
	}
	a.LLM = &lifecycleClient{chat: func(_ context.Context, r llm.ChatRequest) (string, error) {
		var text string
		for _, m := range r.Messages {
			text += m.Content
		}
		if strings.Contains(text, "PRIVATE_A") || !strings.Contains(text, "B_ONLY_HISTORY") {
			t.Errorf("wrong conversation: %s", text)
		}
		return "B answer", nil
	}}
	if r := a.RunGoal(context.Background(), req); r.Status != types.GoalSuccess {
		t.Fatalf("next goal: %+v", r)
	}
	stored, err := a.Convos.Get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Messages) != 3 {
		t.Fatalf("rejected submission was persisted: %v", stored.Messages)
	}
}

func TestPlannerRespectsProjectScope(t *testing.T) {
	for _, scoped := range []bool{true, false} {
		t.Run(map[bool]string{true: "project", false: "global"}[scoped], func(t *testing.T) {
			a := lifecycleAgent(t)
			a.Cfg.Memory.ProjectScope = scoped
			marker := "project deployment PRIVATE_PROJECT_A_MARKER"
			if _, err := a.Mem.RememberScoped(marker, "user", filepath.Join(a.Cfg.DataDir, "project-A"), nil); err != nil {
				t.Fatal(err)
			}
			var included bool
			a.LLM = &lifecycleClient{chat: func(_ context.Context, r llm.ChatRequest) (string, error) {
				if llm.KindOf(r.System) == "plan" {
					included = strings.Contains(r.System, marker)
					return `{"steps":[{"id":"s1","tool":"reply","args":{"text":"ok"}}]}`, nil
				}
				return `{"score":90,"verdict":"done","reason":"ok"}`, nil
			}}
			hits := a.MemorySearch("project deployment", 3)
			a.RunGoal(context.Background(), types.GoalRequest{Goal: "project deployment", TaskID: "scope-task"})
			if included != (len(hits) > 0) || included == scoped {
				t.Fatalf("project_scope=%v: UI hits=%d, planner includes foreign memory=%v", scoped, len(hits), included)
			}
		})
	}
}
