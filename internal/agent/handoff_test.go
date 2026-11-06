package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gleam/internal/llm"
	"gleam/pkg/types"
)

const challengePageErr = "目标站点返回了微信访问验证页，没有拿到正文。这是对方的风控策略，不是网络故障，" +
	"重试同一 URL 不会成功。如果内容确实必要，改用 computer_use 用 open_app 打开浏览器，" +
	"请用户完成验证或登录，再用 screenshot 读取页面；否则换一个可访问的来源，或请用户直接粘贴正文"

func blockedStep() types.StepResult {
	return types.StepResult{
		StepID:    "s1",
		Tool:      "web.fetch",
		Status:    types.StepFailed,
		Outcome:   types.OutcomeFailed,
		Error:     challengePageErr,
		ErrorKind: types.ClassifyError(challengePageErr),
	}
}

func TestHumanHandoff_OnlyWhenUserGotNoAnswer(t *testing.T) {
	cases := []struct {
		name string
		exec *Result
		want bool
	}{
		{
			name: "blocked fetch, dependent reply skipped",
			exec: &Result{Steps: []types.StepResult{
				blockedStep(),
				{StepID: "s2", Tool: "memory.search", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
				{StepID: "s3", Tool: "reply", Status: types.StepSkipped, Error: "依赖步骤未成功: s1"},
			}, Failed: 1, Succeeded: 1, Skipped: 1, Total: 3},
			want: true,
		},
		{
			name: "a reply already reached the user",
			exec: &Result{Steps: []types.StepResult{blockedStep()}, ReplyText: "这是分析结果"},
			want: false,
		},
		{
			name: "ordinary failure stays retryable",
			exec: &Result{Steps: []types.StepResult{{
				StepID: "s1", Tool: "web.fetch", Status: types.StepFailed,
				Error: "dial tcp: connection reset", ErrorKind: types.ErrUpstream,
			}}},
			want: false,
		},
		{name: "nil exec", exec: nil, want: false},
	}
	for _, tc := range cases {
		got := humanHandoff(tc.exec)
		if (got != "") != tc.want {
			t.Errorf("%s: humanHandoff = %q, want present=%v", tc.name, got, tc.want)
		}
		if tc.want && !strings.Contains(got, "请把需要的正文粘贴") {
			t.Errorf("%s: handoff did not tell the user the next step: %q", tc.name, got)
		}
	}
}

// A challenge page used to be an ErrUnknown failure: the reflector kept
// asking for another attempt, each replan used different tools so the
// fingerprint breaker never fired, and the user got a step counter after the
// whole budget was gone. One blocked round must now end with a handoff.
func TestRunGoal_ChallengePageStopsOnFirstRound(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[` +
			`{"id":"s1","description":"抓取推文正文","tool":"blocked"},` +
			`{"id":"s2","description":"分析岗位并回复","tool":"reply","args":{"text":"分析"},"depends_on":["s1"]}` +
			`]}`),
	})
	f.a.Reg.MustRegister(&funcTool{
		name: "blocked", perm: types.PermissionReadOnly,
		fn: func(context.Context, map[string]any) (any, error) {
			return nil, errors.New(challengePageErr)
		},
	})
	f.a.Cfg.Agent.MaxReplans = 5
	f.a.Cfg.Agent.StepRetries = 0

	res := f.a.RunGoal(context.Background(), types.GoalRequest{
		Goal: "分析这篇推文里适合我的岗位", Mode: "auto",
	})

	if res.Status != types.GoalFailed {
		t.Errorf("status = %s, want failed", res.Status)
	}
	if !strings.Contains(res.Summary, "请把需要的正文粘贴") {
		t.Errorf("用户没拿到可执行的下一步：%q", res.Summary)
	}
	if strings.Contains(res.Summary, "执行了") {
		t.Errorf("仍在用步骤计数当回复：%q", res.Summary)
	}
	if res.Suggestion != handoffSuggestion {
		t.Errorf("suggestion = %q", res.Suggestion)
	}
	// finalize() may kick off an async context-compression call; count by kind
	// so the assertion is about replanning, not about background bookkeeping.
	plans, reflections := 0, 0
	for _, call := range f.llm.Calls {
		switch llm.KindOf(call.System) {
		case "plan":
			plans++
		case "reflect":
			reflections++
		}
	}
	if plans != 1 || reflections != 0 {
		t.Errorf("规划 %d 次、反思 %d 次；应只有 1 次规划且不得反思/重规划", plans, reflections)
	}
	if len(res.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(res.Steps))
	}
	if res.Steps[0].ErrorKind != types.ErrNeedsHuman {
		t.Errorf("风控错误被归类为 %q，应为 needs_human", res.Steps[0].ErrorKind)
	}
	if res.Steps[1].Status != types.StepSkipped {
		t.Errorf("依赖步骤状态 = %s，应为 skipped", res.Steps[1].Status)
	}
}
