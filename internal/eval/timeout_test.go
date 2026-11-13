package eval

import (
	"context"
	"testing"
	"time"

	"gleam/internal/agent"
	"gleam/internal/config"
	"gleam/internal/harness/registry"
	"gleam/internal/llm"
	"gleam/internal/tools/std"
)

// deadlineLLM records the context deadline seen by the planning call so a test
// can prove the per-case timeout came from Runner.Timeout, not the hardcoded
// default. Both probe values stay below the planner's own 90s cap, so the
// deadline reflects the Runner setting without being masked.
type deadlineLLM struct {
	planLLM
	got time.Duration
	set bool
}

func (m *deadlineLLM) Chat(ctx context.Context, req llm.ChatRequest) (string, error) {
	if !m.set && llm.KindOf(req.System) == "plan" {
		if dl, ok := ctx.Deadline(); ok {
			m.got, m.set = time.Until(dl), true
		}
	}
	return m.planLLM.Chat(ctx, req)
}

func TestRunnerTimeoutControlsCaseDeadline(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{"custom", 30 * time.Second, 30 * time.Second},
		{"default", 0, DefaultTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &deadlineLLM{}
			cfg := config.Default()
			cfg.LLM.Provider = "mock"
			reg := registry.New()
			reg.MustRegister(std.NewReply())
			a := agent.New(cfg, stub, reg, nil, nil, nil, nil, nil)
			r := &Runner{Agent: a, Depth: DepthPlan, CWD: t.TempDir(), Timeout: tc.timeout}
			r.Run(context.Background(), []Case{{ID: "t1", Goal: "随便做点什么"}})

			if !stub.set {
				t.Fatal("规划调用未捕获到截止时间")
			}
			if stub.got > tc.want || stub.got < tc.want-2*time.Second {
				t.Errorf("用例截止时间应≈%v，实际 %v", tc.want, stub.got)
			}
		})
	}
}
