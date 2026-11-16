package eval

import (
	"fmt"
	"io"

	"gleam/internal/agent"
	"gleam/pkg/types"
)

// AutoApproveNotifier answers every approval with "yes" so an unattended eval
// run is not stalled by the high-risk confirmation the Gate always raises. It
// is an explicit opt-in (--approve-high) for isolated eval workspaces only: the
// Gate itself is unchanged, interactive goal/webui still ask a human, and each
// auto-approval is logged and audited like a manual one.
type AutoApproveNotifier struct {
	agent.NopNotifier
	W io.Writer
}

func (n AutoApproveNotifier) OnApproval(req types.ApprovalRequest) types.ApprovalResponse {
	if n.W != nil {
		fmt.Fprintf(n.W, "[gleam eval] 自动批准（--approve-high，风险 %s）：%s\n", req.Risk, req.Reason)
	}
	return types.ApprovalResponse{Approved: true, Note: "eval --approve-high 自动批准"}
}

func (n AutoApproveNotifier) OnTool(ev types.ToolEvent) {
	if n.W == nil || ev.Phase != types.ToolEnd {
		return
	}
	icon := "✓"
	switch ev.Status {
	case types.ToolFailed:
		icon = "✗"
	case types.ToolSkipped:
		icon = "⏭"
	}
	line := fmt.Sprintf("[gleam eval] %s %s", icon, types.Shorten(ev.Title, 80))
	if ev.Error != "" {
		line += fmt.Sprintf("（%s）", types.Shorten(ev.Error, 80))
	}
	fmt.Fprintln(n.W, line)
}
