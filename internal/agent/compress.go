package agent

import (
	"context"

	"gleam/internal/llm"
)

func (a *Agent) CompressSummarizer(ctx context.Context, taskID ...string) func(string) (string, error) {
	var owner string
	if len(taskID) > 0 {
		owner = taskID[0]
	}
	return func(overflow string) (string, error) {
		sys := "你是 Gleam 的上下文压缩器。" + llm.MarkerCompress + "\n\n" +
			"接下来是一条包含早期对话记录的消息。请用不超过 200 字的中文摘要，保留对后续任务仍有价值的信息：用户偏好、项目背景、已做的决定、遗留问题。只输出摘要正文。"
		req := llm.ChatRequest{System: sys, Messages: []llm.Message{{Role: llm.RoleUser, Content: overflow}}, Temperature: 0.2, TaskID: owner}
		return a.HelperLLM().Chat(ctx, req)
	}
}
