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

type memoryScopeKey struct{}

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
	return a.LLM
}

func (a *Agent) taskHelperLLM(ctx context.Context) llm.Client {
	if m, ok := taskModelsFrom(ctx); ok && m.helper != nil {
		return m.helper
	}
	return a.HelperLLM()
}
