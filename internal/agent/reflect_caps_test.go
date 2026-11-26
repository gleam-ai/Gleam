package agent

import (
	"strings"
	"testing"

	"gleam/pkg/types"
)

func capSteps(tools ...string) []types.StepResult {
	out := make([]types.StepResult, len(tools))
	for i, t := range tools {
		out[i] = types.StepResult{StepID: "s", Tool: t, Status: types.StepSucceeded}
	}
	return out
}

func sum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}

// A small plan is not squeezed: the reply gets its full budget and tools get
// theirs.
func TestReflectCaps_SmallPlanUnsqueezed(t *testing.T) {
	got := reflectCaps(capSteps("file.list", "file.read", "reply"))
	want := []int{reflectToolRunes, reflectToolRunes, reflectReplyRunes}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("caps = %v, want %v", got, want)
		}
	}
}

// Many tool steps are squeezed to their floor first; the reply is untouched.
func TestReflectCaps_SqueezesToolsBeforeReply(t *testing.T) {
	tools := make([]string, 10)
	for i := range tools {
		tools[i] = "shell.exec"
	}
	got := reflectCaps(capSteps(append(tools, "reply")...))
	if got[10] != reflectReplyRunes {
		t.Errorf("reply cap = %d, want %d", got[10], reflectReplyRunes)
	}
	for _, c := range got[:10] {
		if c < reflectToolMinRunes {
			t.Errorf("tool cap %d below floor %d", c, reflectToolMinRunes)
		}
	}
	if sum(got) != reflectDigestRunes {
		t.Errorf("total = %d, want %d", sum(got), reflectDigestRunes)
	}
}

// With more replies than the budget holds, everything scales down and the
// total still respects the budget.
func TestReflectCaps_ScalesWhenRepliesAloneExceedBudget(t *testing.T) {
	got := reflectCaps(capSteps("reply", "reply", "reply"))
	if sum(got) > reflectDigestRunes {
		t.Errorf("total = %d, want at most %d", sum(got), reflectDigestRunes)
	}
	for _, c := range got {
		if c <= 0 {
			t.Errorf("cap %d must stay positive", c)
		}
	}
}

func TestBuildDigest_ShowsTheJudgeTheWholeReply(t *testing.T) {
	body := strings.Repeat("答", 2000)
	exec := &Result{Steps: []types.StepResult{
		{StepID: "s1", Tool: "file.list", Status: types.StepSucceeded, Output: strings.Repeat("x", 2000)},
		{StepID: "s2", Tool: "reply", Status: types.StepSucceeded, Output: map[string]any{"text": body}},
	}}
	digest := buildDigest("目标", exec, 1, nil, nil)
	if !strings.Contains(digest, body) {
		t.Errorf("the reply must reach the judge in full (digest is %d runes)", len([]rune(digest)))
	}
	if !strings.Contains(digest, strings.Repeat("x", reflectToolRunes)+"…") {
		t.Error("tool output must be cut at its cap and marked as truncated")
	}
	if strings.Contains(digest, strings.Repeat("x", reflectToolRunes+1)) {
		t.Error("tool output beyond its cap must not be in the digest")
	}
}
