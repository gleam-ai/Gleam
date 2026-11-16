package eval

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"gleam/internal/agent"
	"gleam/internal/config"
	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/llm"
	"gleam/internal/tools/shell"
	"gleam/internal/tools/std"
	"gleam/pkg/types"
)

func TestAutoApproveNotifierApprovesHighRisk(t *testing.T) {
	var buf bytes.Buffer
	n := AutoApproveNotifier{W: &buf}
	resp := n.OnApproval(types.ApprovalRequest{Risk: "high", Reason: "高风险操作，必须人工确认"})
	if !resp.Approved {
		t.Fatalf("--approve-high 应批准高风险审批，却返回 %+v", resp)
	}
	if resp.Note == "" {
		t.Fatal("自动批准应留下 Note，供审计区分人工与自动")
	}
	if !strings.Contains(buf.String(), "自动批准") {
		t.Fatalf("自动批准应打印可见日志，实际 %q", buf.String())
	}
}

func TestAutoApproveNotifierLogsToolEnd(t *testing.T) {
	var buf bytes.Buffer
	n := AutoApproveNotifier{W: &buf}
	n.OnTool(types.ToolEvent{Phase: types.ToolEnd, Status: types.ToolDone, Title: "shell.exec"})
	if !strings.Contains(buf.String(), "shell.exec") {
		t.Fatalf("工具结束应打印标题，实际 %q", buf.String())
	}
}

type approveStubLLM struct{}

func (approveStubLLM) Chat(_ context.Context, req llm.ChatRequest) (string, error) {
	if llm.KindOf(req.System) == "plan" {
		return `{"steps":[{"id":"s1","tool":"shell.exec","args":{"command":"echo gleam-approve-ok"}}],"estimated_time":"short"}`, nil
	}
	return `{"score":90,"verdict":"done","reason":"ok"}`, nil
}

func (m approveStubLLM) ChatStream(ctx context.Context, req llm.ChatRequest, _ func(string)) (string, error) {
	return m.Chat(ctx, req)
}

func (approveStubLLM) Name() string { return "approve-stub" }

func runHighRiskCase(t *testing.T, notifier agent.Notifier) []types.StepResult {
	t.Helper()
	cfg := config.Default()
	cfg.LLM.Provider = "mock"
	cfg.DataDir = t.TempDir()
	ws := t.TempDir()
	reg := registry.New()
	reg.MustRegister(shell.NewWithRoots(5*time.Second, ws))
	reg.MustRegister(std.NewReply())
	gate := safety.New("auto", nil, nil, []string{ws}, time.Second)
	a := agent.New(cfg, approveStubLLM{}, reg, nil, gate, nil, nil, notifier)
	r := &Runner{Agent: a, Depth: DepthFull, CWD: ws, Timeout: 15 * time.Second}
	rep := r.Run(context.Background(), []Case{{ID: "a1", Goal: "跑一个命令"}})
	if len(rep.Cases) == 0 || rep.Cases[0].TaskID == "" {
		t.Fatal("full 深度应归档任务并带回 TaskID")
	}
	g, err := agent.ReadTaskResult(cfg.DataDir, rep.Cases[0].TaskID)
	if err != nil || g == nil {
		t.Fatalf("读取任务归档失败: %v", err)
	}
	return g.Steps
}

func TestHighRiskDeniedWithoutOptIn(t *testing.T) {
	steps := runHighRiskCase(t, agent.NopNotifier{})
	for _, st := range steps {
		if st.Tool == "shell.exec" && st.Status == types.StepSucceeded {
			t.Fatal("未 opt-in 时高风险 shell.exec 不该成功（默认必须人工确认）")
		}
	}
	var denied bool
	for _, st := range steps {
		if st.Tool == "shell.exec" && st.ErrorKind == types.ErrPermission {
			denied = true
		}
	}
	if !denied {
		t.Fatalf("未 opt-in 时高风险 shell.exec 应被审批拒绝，实际步骤 %+v", steps)
	}
}

func TestHighRiskApprovedWithOptIn(t *testing.T) {
	steps := runHighRiskCase(t, AutoApproveNotifier{W: io.Discard})
	for _, st := range steps {
		if st.Tool == "shell.exec" && st.Status == types.StepSucceeded {
			return
		}
	}
	t.Fatalf("--approve-high 后高风险 shell.exec 应执行成功，实际步骤 %+v", steps)
}
