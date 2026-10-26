package webui

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"gleam/internal/agent"
	"gleam/internal/harness/feedback"
	"gleam/pkg/types"
)

type sseEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func newEvent(typ string, payload any) sseEvent {
	b, _ := json.Marshal(payload)
	return sseEvent{Type: typ, Data: b}
}

type Server struct {
	Agent *agent.Agent

	ShowWindowFunc func()

	BindAddr string

	token string

	feedback *feedback.Store

	DisableSelfUpdate bool
	updateMu          sync.Mutex
	desktopPinMu      sync.Mutex
	desktopPin        *releaseInfo
	desktopPinAt      time.Time

	mu        sync.Mutex
	tasks     map[string]*taskInfo
	approvals map[string]*approvalWaiter
	clients   map[chan sseEvent]struct{}
}

type taskInfo struct {
	ID             string             `json:"task_id"`
	Goal           string             `json:"goal"`
	Mode           string             `json:"mode"`
	TaskMode       string             `json:"task_mode,omitempty"`
	ConversationID string             `json:"conversation_id,omitempty"`
	Status         types.GoalStatus   `json:"status"`
	Result         *types.GoalResult  `json:"result,omitempty"`
	Events         []sseEvent         `json:"events,omitempty"`
	ProgressSeq    uint64             `json:"progress_seq"`
	ChatText       string             `json:"chat_text"`
	Started        time.Time          `json:"started_at"`
	Cancel         context.CancelFunc `json:"-"`
}

func (t *taskInfo) snapshot() *taskInfo {
	copy := *t
	copy.Cancel = nil
	copy.Events = append([]sseEvent(nil), t.Events...)
	return &copy
}

type approvalWaiter struct {
	ID        string                `json:"id"`
	Req       types.ApprovalRequest `json:"request"`
	ch        chan types.ApprovalResponse
	CreatedAt time.Time `json:"created_at"`
}

func NewServer(a *agent.Agent) *Server {
	s := &Server{
		Agent:     a,
		token:     tokenOrNew(),
		feedback:  feedback.NewStore(a.DataDir()),
		tasks:     map[string]*taskInfo{},
		approvals: map[string]*approvalWaiter{},
		clients:   map[chan sseEvent]struct{}{},
	}
	a.SetNotifier(s)
	return s
}

type progressEvent struct {
	types.ProgressEvent
	Seq uint64 `json:"seq"`
}

func (s *Server) OnProgress(ev types.ProgressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := progressEvent{ProgressEvent: ev}
	if t, ok := s.tasks[ev.TaskID]; ok {
		t.ProgressSeq++
		payload.Seq = t.ProgressSeq
		if ev.Phase == "chat" && ev.Kind == "llm" {
			t.ChatText += ev.Message
		}
		t.appendEvent(newEvent("progress", payload))
	}
	// Sequence assignment, snapshot text and delivery must share one ordering.
	s.broadcastLocked(newEvent("progress", payload))
}

func (s *Server) OnTool(ev types.ToolEvent) {
	s.mu.Lock()
	if t, ok := s.tasks[ev.TaskID]; ok {
		t.appendEvent(newEvent("tool", ev))
	}
	s.mu.Unlock()
	s.broadcast(newEvent("tool", ev))
}

func (s *Server) OnApproval(req types.ApprovalRequest) types.ApprovalResponse {
	return s.onApprovalContext(context.Background(), req)
}

func (s *Server) onApprovalContext(ctx context.Context, req types.ApprovalRequest) types.ApprovalResponse {
	cancelled := types.ApprovalResponse{Approved: false, Note: "请求已取消，自动拒绝"}
	if ctx.Err() != nil {
		return cancelled
	}
	w := &approvalWaiter{
		ID:        "apr-" + types.NewID(),
		Req:       req,
		ch:        make(chan types.ApprovalResponse, 1),
		CreatedAt: time.Now(),
	}
	s.mu.Lock()
	s.approvals[w.ID] = w
	if t, ok := s.tasks[req.TaskID]; ok {
		t.appendEvent(newEvent("approval", w.view()))
	}
	s.mu.Unlock()
	s.broadcast(newEvent("approval", w.view()))

	timer := time.NewTimer(s.Agent.ApprovalTimeout())
	defer timer.Stop()
	select {
	case <-ctx.Done():
		s.resolve(w.ID, cancelled)
		return cancelled
	case resp := <-w.ch:
		if ctx.Err() != nil {
			return cancelled
		}
		return resp
	case <-timer.C:
		s.resolve(w.ID, types.ApprovalResponse{Approved: false, Note: "审批超时，自动拒绝"})
		return types.ApprovalResponse{Approved: false, Note: "审批超时，自动拒绝"}
	}
}

func (s *Server) OnSuggestion(taskID, text string) {
	payload := map[string]any{"task_id": taskID, "text": text}
	s.mu.Lock()
	if t, ok := s.tasks[taskID]; ok {
		t.appendEvent(newEvent("suggestion", payload))
	}
	s.mu.Unlock()
	s.broadcast(newEvent("suggestion", payload))
}

func (s *Server) OnSuggestSkill(taskID string, draft types.SkillDraft) {
	payload := map[string]any{"task_id": taskID, "skill": draft}
	s.mu.Lock()
	if t, ok := s.tasks[taskID]; ok {
		t.appendEvent(newEvent("suggest_skill", payload))
	}
	s.mu.Unlock()
	s.broadcast(newEvent("suggest_skill", payload))
}

func (s *Server) OnTaskDone(ev types.TaskDoneEvent) {
	payload := map[string]any{"task_id": ev.TaskID, "origin": ev.Origin, "title": ev.Title(), "line": ev.Line(), "result": ev.Result}
	s.mu.Lock()
	t, ok := s.tasks[ev.TaskID]
	if !ok {
		started := ev.Result.StartedAt
		if started.IsZero() {
			started = time.Now()
		}
		t = &taskInfo{ID: ev.TaskID, Goal: ev.Result.Goal, Status: ev.Result.Status, Started: started}
		res := ev.Result
		t.Result = &res
		s.tasks[ev.TaskID] = t
	}
	t.appendEvent(newEvent("task_done", payload))
	s.pruneTasksLocked(ev.TaskID)
	s.mu.Unlock()
	s.broadcast(newEvent("task_done", payload))
}

func (s *Server) subscribe() chan sseEvent {
	ch := make(chan sseEvent, 256)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	pending := make([]sseEvent, 0, len(s.approvals))
	for _, w := range s.approvals {
		pending = append(pending, newEvent("approval", w.view()))
	}
	s.mu.Unlock()
	for _, ev := range pending {
		select {
		case ch <- ev:
		default:
		}
	}
	return ch
}

func (s *Server) unsubscribe(ch chan sseEvent) {
	s.mu.Lock()
	delete(s.clients, ch)
	s.mu.Unlock()
}

func (s *Server) broadcast(ev sseEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcastLocked(ev)
}

func (s *Server) broadcastLocked(ev sseEvent) {
	// Slow tabs resync from snapshots; delivery never waits on a client.
	for ch := range s.clients {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (s *Server) resolve(id string, resp types.ApprovalResponse) bool {
	s.mu.Lock()
	w, ok := s.approvals[id]
	if ok {
		delete(s.approvals, id)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	// Other tabs must retire their copy of the card; without this they keep
	// offering a decision the server has already taken.
	s.broadcast(newEvent("approval_resolved", map[string]any{"id": id}))
	select {
	case w.ch <- resp:
	default:
	}
	return true
}

func (s *Server) dropTaskApprovals(taskID, note string) {
	s.mu.Lock()
	var stale []*approvalWaiter
	for id, w := range s.approvals {
		if w.Req.TaskID == taskID {
			stale = append(stale, w)
			delete(s.approvals, id)
		}
	}
	s.mu.Unlock()
	for _, w := range stale {
		s.broadcast(newEvent("approval_resolved", map[string]any{"id": w.ID}))
		resp := types.ApprovalResponse{Approved: false, Note: note}
		select {
		case w.ch <- resp:
		default:
		}
	}
}

func (w *approvalWaiter) view() map[string]any {
	view := map[string]any{
		"id":         w.ID,
		"task_id":    w.Req.TaskID,
		"plan":       w.Req.Plan,
		"risk":       w.Req.Risk,
		"reason":     w.Req.Reason,
		"step_id":    w.Req.StepID,
		"whole_plan": w.Req.WholePlan,
		"created_at": w.CreatedAt,
	}
	if w.Req.RequestID != "" {
		view["request_id"] = w.Req.RequestID
	}
	return view
}

func (t *taskInfo) appendEvent(ev sseEvent) {
	if len(t.Events) >= 300 {
		t.Events = t.Events[1:]
	}
	t.Events = append(t.Events, ev)
}

func (s *Server) PendingApprovals() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.approvals)
}
