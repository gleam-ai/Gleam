package agent

import (
	"fmt"
	"hash/fnv"
	"time"

	"gleam/pkg/types"
)

func (a *Agent) buildResult(goal string, exec *Result, refl types.Reflection, acceptance []string, bd types.ContextBreakdown, taskID ...string) *types.GoalResult {
	res := &types.GoalResult{
		Goal:  goal,
		Score: refl.Score,
		Steps: exec.Steps,
		// What was promised and how far verification went -- makes "done"
		// checkable instead of just a score.
		Acceptance: acceptance,
		Checks:     refl.Checks,
		// The actually executed plan after parameter substitution: with it in
		// tasks/<id>.json, "what was intended at the time" can be replayed
		// offline (gleam replay) instead of leaving reviewers guessing from a
		// bare result.
		ExecutedPlan:     &exec.ExecutedPlan,
		PromptBreakdown:  &bd,
		FailureBreakdown: failureBreakdown(exec.Steps),
		Changes:          buildChanges(exec.Steps, exec.PreImages),
	}
	res.Summary = exec.ReplyText
	if res.Summary == "" {
		if exec.Succeeded == exec.Total && exec.Total > 0 {
			res.Summary = fmt.Sprintf("已完成全部 %d 个步骤。", exec.Total)
		} else {
			res.Summary = fmt.Sprintf("执行了 %d 个步骤：成功 %d，失败 %d，跳过 %d。%s",
				exec.Total, exec.Succeeded, exec.Failed, exec.Skipped, refl.Reason)
		}
	}
	if refl.Verdict == "failed" {
		res.Status = types.GoalFailed
	}
	if len(taskID) > 0 {
		res.TaskID = taskID[0]
	}
	return res
}

func statusOfExec(exec *Result) types.GoalStatus {
	switch {
	case exec.Cancelled && exec.Succeeded == 0:
		return types.GoalCancelled
	case exec.Succeeded == exec.Total && exec.Total > 0:
		return types.GoalSuccess
	case exec.Succeeded == 0:
		return types.GoalFailed
	default:
		return types.GoalPartial
	}
}

// failureBreakdown counts the attribution distribution of failed steps.
// Failure rate only answers "how much broke"; this distribution answers
// "which layer to fix first" -- wrong parameters need parameter fixes, business
// rejections need a different plan, insufficient permissions make retries
// useless; mixed together they are just noise.
func failureBreakdown(steps []types.StepResult) map[types.ErrorKind]int {
	var out map[types.ErrorKind]int
	for _, st := range steps {
		if st.Status != types.StepFailed && st.Outcome != types.OutcomeFailed {
			continue
		}
		kind := st.ErrorKind
		if kind == "" {
			kind = types.ErrUnknown
		}
		if out == nil {
			out = map[types.ErrorKind]int{}
		}
		out[kind]++
	}
	return out
}

// deriveTraceID derives a short trace ID from the task content (FNV-1a,
// truncated to 40 bits, 10 hex digits). Same input -> same ID; this is what
// makes "the same problem recurring" automatically clusterable.
func deriveTraceID(parts ...string) string {
	h := fnv.New64a()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0x1f})
	}
	return fmt.Sprintf("%010x", h.Sum64()&0xFFFFFFFFFF)
}

// completionReached decides whether this attempt counts as "done". All three
// guards are required:
//
//   - The model says done, or the score reaches the threshold -- the original
//     condition.
//   - All acceptance criteria pass -- otherwise lowering the threshold, or the
//     model simply saying done, bypasses the per-criterion check and turns
//     "not done" into "completed".
//   - Artifact verification passes -- regardless of whether acceptance
//     criteria exist, it judges the reality measured by the code. Condition 1
//     alone is not enough: when artifact verification fails the reflector is
//     squeezed into replan, but the score-at-threshold half still lets it
//     through, so it must be judged independently here.
//
// Extracted into its own function instead of staying inside the if: a missing
// clause in a long condition is invisible; split apart, each is testable.
func completionReached(refl types.Reflection, acceptance []string, exec *Result, doneThreshold int) bool {
	if refl.Verdict != "done" && refl.Score < doneThreshold {
		return false
	}
	if !acceptanceSatisfied(acceptance, refl.Checks) {
		return false
	}
	var steps []types.StepResult
	if exec != nil {
		steps = exec.Steps
	}
	return artifactsSatisfied(verifyArtifacts(steps))
}

// acceptanceSatisfied reports whether all acceptance criteria pass. Three
// cases must be distinguished; conflating them opens the "reflector dies,
// acceptance bypassed wholesale" hole:
//
//   - No acceptance criteria (len(acceptance)==0): returns true, i.e. "does not
//     interfere with the original flow". Nothing was promised, so there is
//     nothing to verify.
//   - Criteria present and verdicts present: check one by one, true only when
//     all pass.
//   - Criteria present but no verdicts (checks empty): returns false. This is
//     not "no problems" but "never verified" -- exactly the shape of a
//     reflector failure fallback. It used to be merged into the first case
//     returning true, so when the reflector failed, heuristicReflection gave
//     done/85 and this let it through, ending the task as "completed" without
//     a single promised criterion having been verified. A missing verdict must
//     go to the failure path, not count as a pass.
func acceptanceSatisfied(acceptance []string, checks []types.CheckResult) bool {
	if len(acceptance) == 0 {
		return true
	}
	if len(checks) == 0 {
		return false
	}
	for _, c := range checks {
		if !c.Passed {
			return false
		}
	}
	return true
}

func (a *Agent) cancelledResult(goal string, last *types.GoalResult) *types.GoalResult {
	if last != nil {
		last.Status = types.GoalCancelled
		last.Error = "任务已取消"
		return last
	}
	return &types.GoalResult{Goal: goal, Status: types.GoalCancelled, Error: "任务已取消"}
}

func (a *Agent) resultOf(h *taskHandle) *types.GoalResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	if h.result != nil {
		cp := *h.result
		return &cp
	}
	return &types.GoalResult{TaskID: h.id, Goal: h.goal, Status: h.status, StartedAt: h.started}
}

// elapsedMs is the duration of a finished task in whole milliseconds;
// anything under 1ms is recorded as 1ms.
//
// Why clamp to 1 instead of fractions or rounding up: duration_ms is an
// external integer field -- the frontend, the archive and replay's
// before/after comparison all read whole milliseconds, and historical
// archives are full of truncated values. Changing the type breaks those
// readers; rounding up invents sub-ms differences in replay comparisons.
// Only the "under 1ms" bucket truncates to 0, and 0 means "no duration
// recorded", which is wrong for a task that did finish. So only that bucket
// is fixed; every other value is unchanged.
func elapsedMs(d time.Duration) int64 {
	if ms := d.Milliseconds(); ms >= 1 {
		return ms
	}
	return 1
}
