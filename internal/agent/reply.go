package agent

import (
	"context"
	"fmt"
	"strings"

	"gleam/internal/llm"
)

// ReplyComposer rewrites a plan's reply step from the evidence the earlier
// steps actually returned.
//
// The planner writes args.text before any step has run, so a reply that
// interpolates "{ref:s1}" is a draft wrapped around raw output. Echoing that
// draft is what the reply tool does by itself, which is why "list the top-level
// directories" used to end with a JSON blob followed by an instruction to the
// executor, a 25/100 score and a loop.
type ReplyComposer interface {
	Compose(ctx context.Context, goal, draft string) (string, error)
}

const composeSystem = "你是 Gleam 的收尾助手：前面的步骤已经执行完，现在要依据它们返回的真实结果，写出面向用户的最终回答。" + llm.MarkerCompose + `
要求：
- 只依据草稿里给出的真实结果作答；结果里没有的不要编造，也不要写成已经做过。
- 用用户能直接阅读的话重写，不要原样粘贴工具输出的 JSON 或原始数据，不要输出给执行者的指令（例如「请按…逐行列出」）。
- 保留草稿中与结果一致的结构与要点；结果不足以回答时，直说缺什么、已经做了什么。
- 只输出最终回答本身，不要加标题、前言或解释。`

// composeReply is the executor-side hook. The draft is returned untouched
// whenever there is no composer, nothing to rewrite or the model call fails,
// so a composing failure can never turn a succeeding step into a failing one.
func (e *Executor) composeReply(ctx context.Context, args map[string]any) map[string]any {
	if e.Composer == nil || ctx.Err() != nil {
		return args
	}
	draft, _ := args["text"].(string)
	if strings.TrimSpace(draft) == "" {
		return args
	}
	composed, err := e.Composer.Compose(ctx, e.Goal, draft)
	// Trim here so the contract does not depend on the implementation: a
	// reply of whitespace must never replace a draft that has content.
	composed = strings.TrimSpace(composed)
	if err != nil || composed == "" {
		return args
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	out["text"] = composed
	return out
}

type llmReplyComposer struct {
	client llm.Client
	taskID string
}

// NewReplyComposer returns nil when there is no client, which keeps the
// executor on the echo-only behaviour. The offline mock is skipped on purpose:
// scripted runs and eval baselines count model calls and queue replies per
// task kind, and a mock cannot write a better answer than the draft anyway.
func NewReplyComposer(client llm.Client, taskID string) ReplyComposer {
	if client == nil || llm.IsMockClient(client) {
		return nil
	}
	return &llmReplyComposer{client: client, taskID: taskID}
}

func (c *llmReplyComposer) Compose(ctx context.Context, goal, draft string) (string, error) {
	text, err := c.client.Chat(ctx, llm.ChatRequest{
		System: composeSystem,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: fmt.Sprintf("任务目标：%s\n\n答复草稿（步骤的真实结果已插入其中）：\n%s", goal, draft),
		}},
		Temperature: 0.3,
		TaskID:      c.taskID,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}
