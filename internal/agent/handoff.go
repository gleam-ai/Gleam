package agent

import (
	"strings"

	"gleam/pkg/types"
)

// handoffSuggestion is the short hint on the result card.
const handoffSuggestion = "把内容粘贴到对话里，或完成浏览器验证后重发目标；已停止自动重规划，避免继续空转。"

// humanHandoff returns a user-facing message when only a human can clear the
// blocker (risk-control page, login wall). Empty means the agent can still
// handle it. Without this, a challenge page was classified as ErrUnknown:
// the reflector kept replanning with different tools, so the fingerprint
// breaker never fired and the user got a step counter instead of a next step.
func humanHandoff(exec *Result) string {
	if exec == nil || strings.TrimSpace(exec.ReplyText) != "" {
		return ""
	}
	var reason string
	for _, st := range exec.Steps {
		if st.ErrorKind != types.ErrNeedsHuman {
			continue
		}
		if st.Status != types.StepFailed && st.Outcome != types.OutcomeFailed {
			continue
		}
		if text := strings.TrimSpace(st.Error); text != "" {
			reason = text
			break
		}
	}
	if reason == "" {
		return ""
	}
	return "这一步需要你帮忙，我已停下，没有继续自动重试：\n" +
		types.Shorten(reason, 260) +
		"\n\n请把需要的正文粘贴到对话里，或在浏览器打开链接完成验证后再发一次；也可以换一个能直接访问的来源。收到内容后我接着完成任务。"
}
