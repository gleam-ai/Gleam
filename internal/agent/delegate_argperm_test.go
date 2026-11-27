package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/llm"
	"gleam/internal/tools/std"
	"gleam/pkg/types"
)

type countingNotifier struct {
	NopNotifier
	asked    int
	approved bool
}

func (c *countingNotifier) OnApproval(types.ApprovalRequest) types.ApprovalResponse {
	c.asked++
	return types.ApprovalResponse{Approved: c.approved}
}

func delegateFixture(t *testing.T) (*registry.Registry, *safety.Gate, *llm.MockClient) {
	t.Helper()
	client := llm.NewMock()
	a := &Agent{LLM: client}
	reg := registry.New()
	reg.MustRegister(std.NewAgentDelegate(a))
	gate := safety.New("auto", nil, nil, []string{t.TempDir()}, time.Second)
	return reg, gate, client
}

func runDelegate(t *testing.T, reg *registry.Registry, gate *safety.Gate, n Notifier, mode string) types.StepResult {
	t.Helper()
	args := map[string]any{"goal": "extract the order id"}
	if mode != "" {
		args["mode"] = mode
	}
	e := &Executor{Reg: reg, Gate: gate, Notifier: n, StepTimeout: 5 * time.Second}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "agent.delegate", Args: args}}}, "t", "auto", false)
	if len(res.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(res.Steps))
	}
	return res.Steps[0]
}

// A real-model run failed end to end on this: the plan used agent.delegate
// (mode=answer) to pull one value out of a file read, the approval was refused
// because nobody could answer it, and every dependent step was skipped.
func TestExecute_DelegateAnswerModeRunsWithoutApproval(t *testing.T) {
	reg, gate, client := delegateFixture(t)
	client.Enqueue("chat", "A-7731")
	n := &countingNotifier{approved: false}

	st := runDelegate(t, reg, gate, n, "answer")
	if st.Status != types.StepSucceeded {
		t.Fatalf("answer mode should not need a person: status=%s error=%q", st.Status, st.Error)
	}
	if n.asked != 0 {
		t.Errorf("approval was requested %d time(s) for a text-only step", n.asked)
	}
}

func TestExecute_DelegateDefaultModeIsAnswer(t *testing.T) {
	reg, gate, client := delegateFixture(t)
	client.Enqueue("chat", "A-7731")
	n := &countingNotifier{approved: false}

	if st := runDelegate(t, reg, gate, n, ""); st.Status != types.StepSucceeded {
		t.Fatalf("omitted mode means answer: status=%s error=%q", st.Status, st.Error)
	}
	if n.asked != 0 {
		t.Errorf("approval was requested %d time(s)", n.asked)
	}
}

// Task mode plans and calls tools of its own, so the approval stays.
func TestExecute_DelegateTaskModeStillAsks(t *testing.T) {
	reg, gate, _ := delegateFixture(t)
	n := &countingNotifier{approved: false}

	st := runDelegate(t, reg, gate, n, "task")
	if st.Status != types.StepFailed || st.ErrorKind != types.ErrPermission {
		t.Fatalf("task mode must be gated: status=%s kind=%q error=%q", st.Status, st.ErrorKind, st.Error)
	}
	if n.asked != 1 {
		t.Errorf("approval was requested %d time(s), want 1", n.asked)
	}
	if !strings.Contains(st.Error, types.ErrStepRejected.Error()) {
		t.Errorf("a refusal by a person reads as a rejection, got %q", st.Error)
	}
}

// The user can pin a tool to a class in the permission settings; that choice
// outranks the tool's own claim about its arguments.
func TestExecute_DelegateUserOverrideKeepsAsking(t *testing.T) {
	reg, gate, _ := delegateFixture(t)
	gate.SetToolPermission("agent.delegate", types.PermissionUserApproved)
	n := &countingNotifier{approved: false}

	st := runDelegate(t, reg, gate, n, "answer")
	if st.Status != types.StepFailed || n.asked != 1 {
		t.Fatalf("override must keep the prompt: status=%s asked=%d", st.Status, n.asked)
	}
}

// plan_first approves a plan as a whole only when it is not all read-only;
// a text-only delegate step must not turn a read-only plan into a dialog.
func TestPlanMaxRisk_FollowsArgPermission(t *testing.T) {
	reg, _, _ := delegateFixture(t)
	plan := func(mode string) types.Plan {
		args := map[string]any{"goal": "x", "mode": mode}
		return types.Plan{Steps: []types.Step{{ID: "s1", Tool: "agent.delegate", Args: args}}}
	}
	if got := planMaxRisk(reg, plan("answer")); got != "low" {
		t.Errorf("answer mode plan risk = %s, want low", got)
	}
	if got := planMaxRisk(reg, plan("task")); got != "medium" {
		t.Errorf("task mode plan risk = %s, want medium", got)
	}
}

// "No approval channel" is not the user saying no. The step fails the same
// way, but the reason must say nobody was asked.
func TestExecute_UnattendedDenialIsNotBlamedOnUser(t *testing.T) {
	reg, gate, _ := delegateFixture(t)

	st := runDelegate(t, reg, gate, nil, "task")
	if st.Status != types.StepFailed || st.ErrorKind != types.ErrPermission {
		t.Fatalf("status=%s kind=%q error=%q", st.Status, st.ErrorKind, st.Error)
	}
	if !strings.Contains(st.Error, types.ErrStepUnattended.Error()) {
		t.Errorf("want the unattended wording, got %q", st.Error)
	}
	if strings.Contains(st.Error, types.ErrStepRejected.Error()) {
		t.Errorf("must not record the user as having refused: %q", st.Error)
	}
	audit := gate.RecentAudit(1)
	if len(audit) != 1 || audit[0].Action != "unattended" {
		t.Errorf("audit action = %+v, want unattended", audit)
	}
	if got := types.ClassifyError(st.Error); got != types.ErrPermission {
		t.Errorf("classified as %q, want permission", got)
	}
}
