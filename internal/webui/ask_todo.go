package webui

import (
	"context"
	"fmt"
	"time"

	"gleam/internal/agent"
	"gleam/internal/tools/std"
	"gleam/pkg/types"
)

// AskUser implements std.QuestionAsker: it broadcasts the question to the
// frontend and waits for the user to submit an answer. Times out after 5
// minutes without a response so a task cannot hang forever.
func (s *Server) AskUser(ctx context.Context, q std.UserQuestion) (std.UserAnswer, error) {
	taskID := taskIDFromContext(ctx)
	w := &questionWaiter{
		ID:        "q-" + types.NewID(),
		TaskID:    taskID,
		Question:  q.Question,
		Choices:   q.Choices,
		CreatedAt: time.Now(),
		ch:        make(chan questionAnswer, 1),
	}
	s.mu.Lock()
	s.questions[w.ID] = w
	if t, ok := s.tasks[taskID]; ok {
		t.appendEvent(newEvent("question", questionView(w)))
	}
	s.mu.Unlock()
	s.broadcast(newEvent("question", questionView(w)))

	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		if _, ok := s.takeQuestion(w.ID); ok {
			s.broadcast(newEvent("question_resolved", map[string]any{"id": w.ID}))
		}
		return std.UserAnswer{}, ctx.Err()
	case ans := <-w.ch:
		return std.UserAnswer{Choice: ans.Choice, FreeText: ans.FreeText}, nil
	case <-timer.C:
		if _, ok := s.takeQuestion(w.ID); ok {
			s.broadcast(newEvent("question_resolved", map[string]any{"id": w.ID}))
			return std.UserAnswer{}, fmt.Errorf("提问超时（5 分钟无回应）")
		}
		// An answer won the race and is already buffered; the timeout must
		// not swallow it.
		ans := <-w.ch
		return std.UserAnswer{Choice: ans.Choice, FreeText: ans.FreeText}, nil
	}
}

// takeQuestion removes a pending question under the lock. Whoever removes it
// owns the outcome: the answer sender or the timeout, never both.
func (s *Server) takeQuestion(id string) (*questionWaiter, bool) {
	s.mu.Lock()
	w, ok := s.questions[id]
	if ok {
		delete(s.questions, id)
	}
	s.mu.Unlock()
	return w, ok
}

// AnswerQuestion is called by the HTTP handler to deliver the user's answer
// to the tool that is waiting for it.
func (s *Server) AnswerQuestion(id string, ans questionAnswer) error {
	w, ok := s.takeQuestion(id)
	if !ok {
		return fmt.Errorf("提问 %s 不存在或已结束", id)
	}
	w.ch <- ans
	s.broadcast(newEvent("question_resolved", map[string]any{"id": id}))
	return nil
}

func questionView(w *questionWaiter) map[string]any {
	return map[string]any{
		"id":         w.ID,
		"task_id":    w.TaskID,
		"question":   w.Question,
		"choices":    w.Choices,
		"created_at": w.CreatedAt,
	}
}

// PendingQuestions backs subscribe replay: it lets a newly opened page see
// questions still waiting for an answer.
func (s *Server) PendingQuestions() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.questions))
	for _, w := range s.questions {
		out = append(out, questionView(w))
	}
	return out
}

// PublishTodos implements std.TodoSink: it broadcasts a todo snapshot.
func (s *Server) PublishTodos(ctx context.Context, items []std.TodoItem) error {
	taskID := taskIDFromContext(ctx)
	views := make([]todoItemView, 0, len(items))
	for _, it := range items {
		views = append(views, todoItemView{Text: it.Text, Status: it.Status})
	}
	s.mu.Lock()
	s.todos[taskID] = views
	if t, ok := s.tasks[taskID]; ok {
		t.appendEvent(newEvent("todos", map[string]any{"task_id": taskID, "items": views}))
	}
	s.mu.Unlock()
	s.broadcast(newEvent("todos", map[string]any{"task_id": taskID, "items": views}))
	return nil
}

// Todos returns the latest todo snapshot for a task.
func (s *Server) Todos(taskID string) []todoItemView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]todoItemView(nil), s.todos[taskID]...)
}

// taskIDFromContext forwards straight to the agent package: maintaining a
// separate key in webui could cause write/read mismatches.
func taskIDFromContext(ctx context.Context) string {
	return agent.TaskIDFromContext(ctx)
}
