package agent

import (
	"context"

	"gleam/internal/llm"
)

// taskModels pins the clients a goal started with. Settings saves swap the
// Agent's live clients; a running task must not mix two providers or tiers
// across planning, reflection and compression.
type taskModels struct {
	tier   llm.Client
	verify llm.Client
	helper llm.Client
}

type taskModelsKey struct{}

type taskIDKey struct{}

func withTaskID(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, taskIDKey{}, taskID)
}

func taskIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	taskID, _ := ctx.Value(taskIDKey{}).(string)
	return taskID
}

// TaskIDFromContext is exported for outer packages such as webui: it pulls the
// current task id out of the tool execution chain. The taskIDKey itself stays
// unexported so it cannot be misused or written from outside.
func TaskIDFromContext(ctx context.Context) string { return taskIDFrom(ctx) }

// WithTaskIDForTest is exported for tests in outer packages: it fakes the
// "tool running inside a task" context. Production code always goes through
// withTaskID (injected by the runGoal chain) and must not use this entry.
func WithTaskIDForTest(ctx context.Context, taskID string) context.Context {
	return withTaskID(ctx, taskID)
}

type memoryScopeKey struct{}

type delegateDepthKey struct{}

// MaxDelegateDepth caps nested agent.delegate calls: depth 0 is the top-level
// goal, a depth-1 sub-agent may delegate once more, and anything deeper is
// rejected. Without this cap, a confused model can recurse delegate into itself
// and burn tokens until the run budget is exhausted.
const MaxDelegateDepth = 2

func withDelegateDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, delegateDepthKey{}, depth)
}

func delegateDepthFrom(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	depth, _ := ctx.Value(delegateDepthKey{}).(int)
	return depth
}

func withMemoryScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, memoryScopeKey{}, scope)
}

func memoryScopeFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	scope, ok := ctx.Value(memoryScopeKey{}).(string)
	return scope, ok
}

func withTaskModels(ctx context.Context, m taskModels) context.Context {
	return context.WithValue(ctx, taskModelsKey{}, m)
}

func taskModelsFrom(ctx context.Context) (taskModels, bool) {
	if ctx == nil {
		return taskModels{}, false
	}
	m, ok := ctx.Value(taskModelsKey{}).(taskModels)
	return m, ok
}

func (a *Agent) snapshotTaskModels(role string) taskModels {
	return taskModels{
		tier:   a.TierLLM(role),
		verify: a.VerifyLLM(role),
		helper: a.HelperLLM(),
	}
}

func (a *Agent) taskTierLLM(ctx context.Context, role string) llm.Client {
	if m, ok := taskModelsFrom(ctx); ok && m.tier != nil {
		return m.tier
	}
	return a.TierLLM(role)
}

// TaskLLM serves prompt.run and any other model-calling tool: inside a goal it
// stays on the client pinned at start, outside one it follows live settings.
func (a *Agent) TaskLLM(ctx context.Context) llm.Client {
	if m, ok := taskModelsFrom(ctx); ok && m.tier != nil {
		return m.tier
	}
	return a.PrimaryLLM()
}

func (a *Agent) taskHelperLLM(ctx context.Context) llm.Client {
	if m, ok := taskModelsFrom(ctx); ok && m.helper != nil {
		return m.helper
	}
	return a.HelperLLM()
}
