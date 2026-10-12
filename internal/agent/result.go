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
		// 承诺了什么、验到哪一步——让"完成"可核对，而不只是一个分数
		Acceptance: acceptance,
		Checks:     refl.Checks,
		// 参数替换后的实际执行计划：tasks/<id>.json 有了它，"当时打算怎么做"
		// 才能离线回放（gleam replay），而不是只留一个结果让复盘的人猜。
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

// failureBreakdown 统计失败步骤的归因分布。
// 失败率只回答"坏了多少"，这份分布回答"该先修哪一层"——
// 参数错要改参数、业务拒绝要换方案、权限不足重试无用，混在一起就是噪音。
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

// deriveTraceID 由任务内容派生 trace 短 ID（FNV-1a，截断到 40 位、10 位十六进制）。
// 输入相同 → ID 相同，这是"同一问题反复出现可自动聚类"的根据。
func deriveTraceID(parts ...string) string {
	h := fnv.New64a()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0x1f})
	}
	return fmt.Sprintf("%010x", h.Sum64()&0xFFFFFFFFFF)
}

// completionReached 判定这次尝试是否够格算"完成"。三条守卫缺一不可：
//
//  1. 模型说 done，或分数达到阈值——这是原来就有的那条；
//  2. 验收标准全部通过——否则把阈值调低、或模型自己说一句 done，
//     就能绕过逐条判定，让"没做到"变成"已完成"；
//  3. 产物核对通过——**不看有没有验收标准**，它判的是代码实测到的现实。
//     只靠第 1 条不够：产物核对不过时反思器已被压成 replan，但分数达标的
//     那半边仍然会放行，所以必须在这里独立判一次。
//
// 抽成独立函数而不是留在 if 里：长条件少写一句看不出来，拆开后每条都能单测。
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

// acceptanceSatisfied 验收标准是否全部通过。三种情况必须分清，混在一起就会出现
// 「反思器一挂、验收被整体绕过」那个洞：
//
//   - 没有验收标准（len(acceptance)==0）：返回 true，即"不干预原有流程"。
//     没承诺过什么，就没什么可核对的。
//   - 有标准、也有判定：逐条看，全过才 true。
//   - **有标准、却没有判定**（checks 为空）：返回 **false**。
//     这不是"没问题"，而是"没核对过"——反思器故障回退时正是这个形态。
//     原先它与第一种情况合并返回 true，于是反思器一失败，
//     heuristicReflection 给出 done/85，这里再放行，任务就以"已完成"收尾，
//     而它承诺的验收标准一条都没被核对过。判定缺失应当进失败处理，不能算通过。
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

// elapsedMs 已结束任务的耗时（整数毫秒），不足 1ms 记为 1ms。
//
// 为什么钳到 1 而不是改成小数或向上取整：duration_ms 是对外的整数字段——前端、归档、
// replay 的前后对比都按整数毫秒读，历史归档里也全是截断值。改类型会破坏这些读者；
// 全量向上取整会让每一条新记录都比同口径的旧记录多出不到 1ms，replay 对比里凭空冒出差异。
// 只有「不足 1ms」这一档会被截成 0，而 0 在这里的意思是「没记录耗时」，对一个确实跑完的
// 任务是错的说法。所以只修这一档：其余数值与以前逐位相同。
func elapsedMs(d time.Duration) int64 {
	if ms := d.Milliseconds(); ms >= 1 {
		return ms
	}
	return 1
}
