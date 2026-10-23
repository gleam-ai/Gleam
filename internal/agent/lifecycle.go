package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gleam/pkg/types"
)

// ErrAgentBusy means another goal owns the shared conversation memory. Callers
// must fail explicitly (HTTP 409 for Web), never queue or run with stale context.
var ErrAgentBusy = errors.New("还有目标在运行，等它结束或先停止它再提交")

type preparedGoal struct {
	ctx    context.Context
	req    types.GoalRequest
	handle *taskHandle
}

// StartGoal reserves the single task slot and loads its conversation before
// returning. Cancellation does not release that slot until the worker exits.
func (a *Agent) StartGoal(ctx context.Context, req types.GoalRequest) (<-chan *types.GoalResult, error) {
	p, err := a.prepareGoal(ctx, req)
	if err != nil {
		return nil, err
	}
	done := make(chan *types.GoalResult, 1)
	go func() {
		done <- a.runPreparedGoal(p)
		close(done)
	}()
	return done, nil
}

// prepareGoal is the sole admission owner for Web, scheduler and CLI. The lock
// covers local context loading and reservation, never a model request. It calls
// only locked conversation helpers, not the public methods that take a.mu.
func (a *Agent) prepareGoal(ctx context.Context, req types.GoalRequest) (*preparedGoal, error) {
	if req.TaskID == "" {
		req.TaskID = types.NewID()
	}
	if err := ValidateGoalRequest(req); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.idleLocked(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.ConversationID != "" {
		if _, err := a.loadConversationContextLocked(req.ConversationID); err != nil {
			return nil, err
		}
	} else if a.Convos != nil && a.Mem != nil {
		// Anonymous CLI/scheduled goals must not inherit the selected Web tab.
		a.Mem.DetachConversation()
	}
	ctx, cancel := context.WithCancel(ctx)
	h := &taskHandle{id: req.TaskID, goal: strings.TrimSpace(req.Goal), mode: normalizeMode(req.Mode), status: types.GoalRunning, started: time.Now(), cancel: cancel}
	if a.tasks == nil {
		a.tasks = make(map[string]*taskHandle)
	}
	a.tasks[req.TaskID] = h
	return &preparedGoal{ctx: ctx, req: req, handle: h}, nil
}

// idleLocked requires a.mu; keeping the check and every memory mutation under
// this same lock prevents activation/submission TOCTOU races.
func (a *Agent) idleLocked() error {
	for _, h := range a.tasks {
		if h.status == types.GoalRunning {
			return fmt.Errorf("%w（%s）", ErrAgentBusy, types.Shorten(h.goal, 40))
		}
	}
	return nil
}
