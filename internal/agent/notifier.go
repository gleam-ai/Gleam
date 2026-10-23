package agent

import (
	"context"

	"gleam/pkg/types"
)

type Notifier interface {
	OnProgress(ev types.ProgressEvent)
	OnApproval(req types.ApprovalRequest) types.ApprovalResponse
	OnSuggestion(taskID, text string)
	OnSuggestSkill(taskID string, draft types.SkillDraft)
	OnTaskDone(ev types.TaskDoneEvent)
}

// askApproval waits for the answer but abandons the wait when the task is
// cancelled. A notifier may block for the full approval timeout; a stop press
// or a shutting-down host must not be stuck behind a dialog nobody will answer.
func askApproval(ctx context.Context, n Notifier, req types.ApprovalRequest) types.ApprovalResponse {
	if n == nil {
		return types.ApprovalResponse{}
	}
	if ctx == nil {
		return n.OnApproval(req)
	}
	ch := make(chan types.ApprovalResponse, 1)
	go func() { ch <- n.OnApproval(req) }()
	select {
	case resp := <-ch:
		return resp
	case <-ctx.Done():
		return types.ApprovalResponse{Note: "任务已取消"}
	}
}

type ToolNotifier interface {
	OnTool(ev types.ToolEvent)
}

func toolSink(n Notifier) (ToolNotifier, bool) {
	tn, ok := n.(ToolNotifier)
	return tn, ok
}

type NopNotifier struct{}

func (NopNotifier) OnProgress(types.ProgressEvent) {}
func (NopNotifier) OnApproval(types.ApprovalRequest) types.ApprovalResponse {
	return types.ApprovalResponse{}
}
func (NopNotifier) OnSuggestion(string, string)             {}
func (NopNotifier) OnSuggestSkill(string, types.SkillDraft) {}
func (NopNotifier) OnTaskDone(types.TaskDoneEvent)          {}

type fanout []Notifier

func (f fanout) OnProgress(ev types.ProgressEvent) {
	for _, n := range f {
		n.OnProgress(ev)
	}
}
func (f fanout) OnApproval(req types.ApprovalRequest) types.ApprovalResponse {
	for _, n := range f {
		return n.OnApproval(req)
	}
	return types.ApprovalResponse{}
}
func (f fanout) OnSuggestion(taskID, text string) {
	for _, n := range f {
		n.OnSuggestion(taskID, text)
	}
}
func (f fanout) OnSuggestSkill(taskID string, draft types.SkillDraft) {
	for _, n := range f {
		n.OnSuggestSkill(taskID, draft)
	}
}
func (f fanout) OnTaskDone(ev types.TaskDoneEvent) {
	for _, n := range f {
		n.OnTaskDone(ev)
	}
}

func (f fanout) OnTool(ev types.ToolEvent) {
	for _, n := range f {
		if tn, ok := toolSink(n); ok {
			tn.OnTool(ev)
		}
	}
}
