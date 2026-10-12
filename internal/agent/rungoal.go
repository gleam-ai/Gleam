package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gleam/internal/agent/geo"
	"gleam/internal/harness/conversation"
	"gleam/internal/harness/growth"
	"gleam/internal/harness/memory"
	"gleam/internal/harness/skill"
	"gleam/internal/llm"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

var slugStrip = regexp.MustCompile(`[\s\p{P}]+`)

// RunGoal 执行一个目标（同步；调用方可放 goroutine）。req.TaskID 非空时沿用。
func (a *Agent) RunGoal(ctx context.Context, req types.GoalRequest) *types.GoalResult {
	taskID := req.TaskID
	if taskID == "" {
		taskID = types.NewID()
	}
	req.TaskID = taskID
	goal := strings.TrimSpace(req.Goal)
	mode := normalizeMode(req.Mode)

	// 重试检测（P3-4 / 站点 H15）：同一会话短时间内相似目标再次提交 = 用户重试。
	// 必须在本条用户消息写入会话**之前**判定，否则检测到的是刚写入的这条自己。
	retried := req.ConversationID != "" && a.Convos != nil && a.Convos.DetectRetry(req.ConversationID, goal)

	// 记录到持久化会话（左侧会话列表）；原始用户目标（不含系统注入的引用前缀）。
	a.appendConvo(req.ConversationID, conversation.Message{
		Role: "user", Content: goal, TaskID: taskID,
		Mode: string(normalizeTaskMode(req.TaskMode)), CreatedAt: time.Now(),
	})

	handle := &taskHandle{id: taskID, goal: goal, mode: mode, status: types.GoalRunning, started: time.Now()}
	a.mu.Lock()
	if old, ok := a.tasks[taskID]; ok && old.status == types.GoalRunning {
		a.mu.Unlock()
		return a.resultOf(old) // 同 ID 正在运行，直接引用
	}
	a.tasks[taskID] = handle
	a.mu.Unlock()
	// 建立用量计数（GEO 等任务完成后的异步调用也会计入）
	a.initUsage(taskID)

	ctx, cancel := context.WithCancel(ctx)
	handle.cancel = cancel
	defer cancel()

	// 配置快照取**启动时**的值，不是收尾时的：任务跑到一半用户在设置页改了模型或档位，
	// 后半段用的就是新配置了，而"这次任务是用什么跑的"必须以启动时为准——
	// 收尾时再取，记下的会是一个从未完整生效过的配置。
	snap := a.Cfg.Snapshot()
	// prog 收集循环内部的交付侧事实（第几轮才成、返工了几次），回来就写进结果——
	// 与下面的成长记录紧挨着，避免"记了口径"和"用了口径"隔在文件两头。
	prog := &goalProgress{}
	result := a.runGoalLoop(ctx, req, goal, mode, normalizeTaskMode(req.TaskMode), taskID, prog)
	// 跑完就处置副本：该留的留（脏的一律留，见 settleWorktree），该清的在配置允许时清。
	a.settleWorktree(taskID)
	prog.stamp(result)
	result.ConfigSnapshot = &snap
	result.TaskID = taskID
	result.Mode = mode
	result.TaskMode = normalizeTaskMode(req.TaskMode)
	result.StartedAt = handle.started
	// trace_id 由「目标 + 任务模式 + 角色 + 模型」派生，刻意不含时间戳：
	// 同一个输入配同一个模型重复出现时 ID 相同，失败聚类按 ID 分组即可——
	// "同一个问题反复出现"不需要额外索引就能看见。
	result.TraceID = deriveTraceID(goal, string(normalizeTaskMode(req.TaskMode)), req.Role, a.LLMName())
	// 这次产出用的是哪版规则集：版本指纹 + 实际生效的规则 ID。
	// 改了规则之后第一个要回答的问题是"之前那批结果是哪一版跑出来的"，
	// 回答不了就只能靠猜。对话模式不经规划器、没有规则参与，两个字段如实留空。
	if tm := normalizeTaskMode(req.TaskMode); tm != types.TaskChat {
		result.RuleSet = RuleSetVersion()
		result.Rules = ActiveRuleIDs(string(tm), a.EffectiveTier(req.Role), req.Role)
	}
	result.FinishedAt = time.Now()
	result.Usage = a.usageOf(taskID)
	result.Usage.DurationMs = elapsedMs(result.FinishedAt.Sub(handle.started))

	// 记录助手回复到同一会话；失败/取消也留痕，避免历史缺轮。
	a.appendConvo(req.ConversationID, conversation.Message{
		Role: "assistant", Content: convoReply(result), TaskID: taskID,
		Mode: string(normalizeTaskMode(req.TaskMode)), Status: string(result.Status), CreatedAt: time.Now(),
	})

	a.mu.Lock()
	handle.status = result.Status
	handle.result = result
	a.pruneTasksLocked()
	a.mu.Unlock()
	// 记录成长日志。
	// 取消单独记中断（task_aborted）：中断率与通过率混在一个分母里，
	// "用户放弃"和"做砸了"就分不开了——两者要修的东西完全不同。
	if a.Growth != nil {
		entry := growth.Entry{
			Goal: goal, Score: result.Score,
			Steps: len(result.Steps), Duration: result.FinishedAt.Sub(result.StartedAt).Seconds(),
			Tokens: result.Usage.TotalTokens(), LLMCalls: result.Usage.LLMCalls, ToolCalls: result.Usage.ToolCalls,
			RuleSet: result.RuleSet,
			// 交付侧口径（首次通过 / 返工轮数）：产物是不是一次就合格。
			// 只对工作模式记——对话模式没有"产物"可核；而工作模式即便规划屡败回落直聊，
			// 那也是一次失败的交付，必须留在分母里（摘掉最差的几个，比率就成了摆设）。
			Delivery:  normalizeTaskMode(req.TaskMode) != types.TaskChat,
			FirstPass: result.FirstPass,
			Reworks:   result.Reworks,
		}
		if result.Status == types.GoalCancelled {
			entry.Type = "task_aborted"
		} else {
			entry.Type = "task_completed"
			entry.Retried = retried
		}
		a.Growth.Record(entry)
	}
	return result
}

// goalProgress 主循环内部的一次性事实，供交付侧口径（首次通过 / 返工轮数）使用。
//
// 为什么不让调用方从 result 反推：**反推不出来**。"第几轮才成"与"返工了几次"是
// 循环内部的局部事实——重试三次才成与一次做对，最后的 status/score 完全一样，
// 只有循环自己知道中间多转了两圈。所以由循环写、由 RunGoal 读，而不是事后猜。
type goalProgress struct {
	attempts   int  // 实际走到第几轮尝试；对话模式为 0（没进过循环）
	reworks    int  // 反思判未达标后真正重新执行过的轮数
	firstRound bool // 是否在第一轮尝试就走到了完成判定
}

// stamp 把循环里的交付侧事实写回结果。
//
// 只在真正进过工作循环时写：对话模式没有"轮"的概念，两个字段由对话管线按自己的
// 核对结论给（见 runChatPath）。无条件写会把那边已经给出的结论覆盖成零值，
// 于是"核对通过的对话"会被统计成"没通过首次验收"。
func (p *goalProgress) stamp(res *types.GoalResult) {
	if p == nil || res == nil || p.attempts == 0 {
		return
	}
	res.Reworks = p.reworks
	// 首次验收通过 = 第一轮走完流程 **且最终确实算成功**。两个条件都要：
	// 完成判定通过之后 applyAcceptanceVerdict 仍可能把状态校正成"部分完成"
	// （产物核对不过、反思器没核对），那不是"一次就合格"。
	res.FirstPass = p.firstRound && res.Status == types.GoalSuccess
}

// runGoalLoop Plan-Execute-Reflect 主循环（含自动重规划）。taskMode=chat 时走直聊管线。
// prog 由调用方传入并回读，记录"第几轮才成、返工了几次"（见 goalProgress）。
func (a *Agent) runGoalLoop(ctx context.Context, req types.GoalRequest, goal, mode string, taskMode types.TaskMode, taskID string, prog *goalProgress) *types.GoalResult {
	// 结构化引用由 Web UI 注入；在规划提示前转为明确的上下文段落，
	// 同时保留原始目标文本，避免引用只停留在界面芯片层。
	refs := req.References
	if len(refs) == 0 {
		if legacy, ok := req.Context["references"].([]map[string]string); ok {
			for _, ref := range legacy {
				refs = append(refs, types.Reference{Kind: ref["kind"], Label: ref["label"], Value: ref["value"]})
			}
		}
	}
	if len(refs) > 0 {
		var b strings.Builder
		b.WriteString("目标上下文引用（仅作为用户提供的工作上下文，不要把标记本身当作任务）：\n")
		for _, ref := range refs {
			fmt.Fprintf(&b, "- [%s] %s: %s\n", ref.Kind, ref.Label, ref.Value)
		}
		goal = b.String() + "\n用户目标：" + goal
	}
	// ---------- 对话模式：直连 LLM，不经规划/执行/反思 ----------
	if taskMode == types.TaskChat {
		return a.runChatPath(ctx, goal, taskID, req.Role)
	}
	if a.Mem != nil {
		a.Mem.AddTurn("user", goal)
	}
	notify := func(phase, msg string, pct int, kind string) {
		a.Notifier.OnProgress(types.ProgressEvent{TaskID: taskID, Phase: phase, Message: msg, Progress: pct, Kind: kind})
	}

	cwd := a.workspaceOf(req)
	// ---------- 隔离：让这次任务在自己的 worktree 里跑 ----------
	// 时机是**规划之前**：规划拿到的 cwd 与执行时的文件边界是同一件事的两面，
	// 只改一边就会出现"照着主工作区规划、把文件写进 worktree"。
	if iso := a.isolateForTask(ctx, req, taskID, notify); len(iso.roots) > 0 {
		ctx = toolutil.WithRoots(ctx, iso.roots)
		cwd = iso.cwd
	}

	// 场景模板的模型档位：角色声明了档位且配置里有对应模型时换模型，否则就是主模型。
	// 规划、执行、对话都走 TierLLM（BuildPlanner 内部解析的就是它）——同一场景内换模型
	// 没有意义，只会让风格前后不一致。
	//
	// **反思是例外**：反思是验收，走 VerifyLLM 换一个不同档位的模型。
	// 原先这里复用同一个客户端，于是"独立验收"只体现在"独立调用 + 独立上下文"上，
	// 干活的和打分的仍是同一个模型——盲区相同，自评偏差没有任何东西去抵消。
	planner := a.BuildPlanner(req)
	reflector := &Reflector{
		LLM:           a.VerifyLLM(req.Role),
		DoneThreshold: a.Cfg.Agent.DoneThreshold,
		TaskID:        taskID,
	}
	tell := Narrator{Style: a.Cfg.Persona.Style}

	var feedback string
	var last *types.GoalResult
	var usedTools []string  // 本任务各轮实际用到的工具（菜单模式下重规划时强制带上 schema）
	var acceptance []string // 验收标准：一旦定下就跟着这个目标走，重规划重申与否都不丢
	planned := false
	attempts := a.Cfg.Agent.MaxReplans + 1
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		if prog != nil {
			prog.attempts = attempt
		}
		if ctx.Err() != nil {
			return a.cancelledResult(goal, last)
		}
		// ---------- 预算熔断检查点 ----------
		if stop := a.budgetCheckpoint(ctx, taskID, goal, nil); stop != nil {
			notify("budget", stop.Error, 88, "error")
			a.finalize(taskID, stop, nil, req.Role)
			return stop
		}
		// ---------- Plan ----------
		notify("plan", tell.PlanStart(), 3, "info")
		// 上下文自动压缩：短期窗口外被覆盖的旧对话汇总为滚动摘要（§9 风险应对）
		if a.Cfg.Agent.ContextCompress && a.Mem != nil {
			if a.Mem.Compress(a.CompressSummarizer(ctx, taskID)) {
				notify("plan", tell.Compressed(), 4, "info")
			}
		}
		var recent []memory.Turn
		if a.Mem != nil && a.Mem.Short != nil {
			// 带几轮由 memory 说话（水位越线时会收紧，见 CarryTurns）。
			recent = a.Mem.Short.Recent(a.Mem.CarryTurns())
		}
		var relevant []memory.Hit
		if a.Mem != nil && a.Mem.Long != nil {
			relevant = a.Mem.Relevant(goal, 3)
		}
		var summary string
		if a.Mem != nil {
			// 带省略量标注的摘要：「已自动压缩」要说清丢了多少，否则读者分不清全貌与残余
			summary = a.Mem.SummaryAnnotated()
		}
		// 计划流只报进度，不转发内容：规划响应本身就是 JSON，逐 token 转给
		// CLI/WebUI 会把原始 JSON 碎片打进进度行（2026-09-23 QA 报告 M5）。
		var planChars int
		onDelta, flush := DeltaThrottle(50, func(text string) {
			planChars += len([]rune(text))
			notify("plan", fmt.Sprintf("规划中…（已生成 %d 字）", planChars), 5, "llm")
		})
		planner.OnLLMDelta = onDelta
		// 菜单模式下把此前各轮用过的工具固定带上，避免重规划时想看却没有 schema
		planner.ForceTools = usedTools
		plan, err := planner.Plan(ctx, goal, cwd, recent, relevant, summary, feedback)
		flush()
		if err != nil {
			if ctx.Err() != nil {
				return a.cancelledResult(goal, last)
			}
			// 模型调用失败（鉴权/网络/限流）：重试无意义，立即走直聊回落给出真实错误；
			// 只有计划校验失败才值得重规划。
			if attempt == attempts || strings.Contains(err.Error(), "llm:") {
				notify("plan", "规划变更前，尝试直接回答…", 20, "warn")
				last = &types.GoalResult{Goal: goal, Status: types.GoalFailed, Error: err.Error(), Score: 0}
				break
			}
			feedback = "上一次计划无效，请修正：" + err.Error()
			notify("plan", "计划校验未通过，正在重新规划…", 5, "warn")
			continue
		}
		notify("plan", tell.PlanDone(len(plan.Steps)), 8, "info")
		planned = true
		// 验收标准只认第一次定下的那份：它是给用户的承诺，不该由没做到的那一方在重试时改写。
		// 顺带解决"重规划没重申 → 验收悄悄消失 → 又能靠一句 done 蒙混过关"。
		if len(acceptance) == 0 {
			acceptance = plan.Acceptance
		}
		plan.Acceptance = acceptance
		// 记下本轮用到的工具：下一轮规划时它们的 schema 一定在（菜单模式的漏筛兜底）
		for _, st := range plan.Steps {
			usedTools = appendUnique(usedTools, st.Tool)
		}

		// plan_first：先展示完整计划，一次批准
		preApproved := false
		if mode == string(types.ModePlanFirst) {
			resp := a.Notifier.OnApproval(types.ApprovalRequest{
				TaskID: taskID, Plan: planDescriptions(plan), WholePlan: true,
				Risk: planMaxRisk(a.Reg, plan), Reason: "plan_first 模式：执行前需要确认完整计划",
			})
			if !resp.Approved {
				// 这一行是用户会在卡片上读到的原因，不能一律写成"用户拒绝了执行计划"：
				// 取消（宿主摘掉这扇门把他叫醒）和超时自动拒绝都不是用户的拒绝。
				cause := "用户拒绝了执行计划"
				switch {
				case ctx.Err() != nil:
					cause = "任务已取消"
				case resp.Note != "":
					cause = resp.Note
				}
				res := &types.GoalResult{Goal: goal, Status: types.GoalCancelled, Error: cause, Score: 0}
				a.finalize(taskID, res, nil, req.Role)
				return res
			}
			preApproved = true
		}

		// ---------- Execute ----------
		executor := &Executor{
			Reg: a.Reg, Gate: a.Gate, Notifier: a.Notifier,
			MaxConcurrency: a.Cfg.Agent.MaxConcurrency,
			StepTimeout:    time.Duration(a.Cfg.Agent.StepTimeoutSecs) * time.Second,
			StepRetries:    a.Cfg.Agent.StepRetries,
			Dedupe:         a.Cfg.Agent.DedupeCalls,
			MaxOutputRunes: a.Cfg.Agent.MaxOutputRunes,
			DataDir:        a.Cfg.DataDir,
			OnUsage:        a.BumpUsage,
			// 运行中落盘：进程若在这次运行里退出，tasks/ 里不会有任何东西，
			// runs/<taskID>.jsonl 是唯一能回答"跑到哪一步"的凭据。
			// 正常结束时会由 DiscardRunLog 删掉（终态快照更完整，留着是噪音）。
			Sink: NewRunLog(a.Cfg.DataDir),
			Goal: goal,
			// 审核模型只在真配了辅助模型时启用：用主模型做快筛等于多花一份钱
			Reviewer: a.reviewer(),
		}
		// 深度扫描：执行前把整段计划交给主模型审一遍。否决就交用户拍板——批了继续，不批就停在这。
		// 卡在 Execute 之前是有意的：它审的正是"接下来要做什么"，一旦开跑就已经在动了。
		if a.Cfg.Safety.DeepReview {
			if ok, reason := a.deepReviewPlan(ctx, taskID, goal, &plan); !ok && !a.confirmDeepReview(taskID, reason) {
				res := &types.GoalResult{
					Goal: goal, Status: types.GoalFailed, Score: 0,
					Error: "深度扫描未通过：" + reason,
				}
				a.finalize(taskID, res, &plan, req.Role)
				return res
			}
		}
		exec := executor.Execute(ctx, plan, taskID, mode, preApproved)
		if ctx.Err() != nil && exec.Succeeded == 0 {
			res := &types.GoalResult{Goal: goal, Status: types.GoalCancelled, Score: 0, Error: "任务已取消", Steps: exec.Steps, ExecutedPlan: &exec.ExecutedPlan}
			a.finalize(taskID, res, nil, req.Role)
			return res
		}

		// ---------- 预算熔断检查点（执行后、反思前） ----------
		if stop := a.budgetCheckpoint(ctx, taskID, goal, exec.Steps); stop != nil {
			notify("budget", stop.Error, 92, "error")
			a.finalize(taskID, stop, &plan, req.Role)
			return stop
		}

		// ---------- 上下文窗口检查点（同一处：执行后、反思前） ----------
		// 放在这里是因为**接下来马上就要再发一次请求**（反思），而这一轮的执行已经
		// 把上下文撑到了最大。任务还没结束（下面还要反思、可能还要重规划），
		// 所以只有越过 96% 才动手——压完不结束任务，接着把这一轮走完。
		if compressed, tightened := a.compressMidTask(ctx, taskID); compressed || tightened {
			notify("context", a.contextActionText(compressed, tightened, true), 92, "info")
		}

		// ---------- Reflect ----------
		notify("reflect", tell.ReflectStart(), 92, "info")
		// 验收标准在规划时就定好了，这里交给反思器逐条判定（而不是让它凭感觉打分）
		reflector.Acceptance = plan.Acceptance
		reflection := reflector.Evaluate(ctx, goal, exec, attempt)
		result := a.buildResult(goal, exec, reflection, plan.Acceptance, planner.LastBreakdown)
		last = result

		// 完成判定：分数达标或模型说 done，且验收标准全部通过、产物核对也通过。
		// 抽成 completionReached 是为了让这三条守卫各自可测——它原先是一个长条件，
		// 少写一句也看不出来（见 TestCompletionReached_*）。
		if completionReached(reflection, plan.Acceptance, exec, a.Cfg.Agent.DoneThreshold) {
			if prog != nil && attempt == 1 {
				prog.firstRound = true
			}
			result.Status = statusOfExec(exec)
			if reflection.Suggestion != "" {
				result.Suggestion = reflection.Suggestion
			}
			a.finalize(taskID, result, &plan, req.Role)
			return result
		}
		// ---------- 防打转：同一动作反复出现说明在原地打转，别再耗重规划次数 ----------
		// 放在「最后一次重规划」判定之前，否则默认参数下永远不会触发（此时循环本来也要结束）。
		if fp, rounds := a.noteSteps(taskID, exec.Steps); fp != "" {
			stuckMsg := fmt.Sprintf("检测到原地打转：%s 已重复执行 %d 轮", describeFingerprint(fp), rounds)
			notify("reflect", "⚠ "+stuckMsg+"，停止自动重规划", 90, "warn")
			result.Status = statusOfExec(exec)
			if result.Status == types.GoalRunning || result.Status == types.GoalSuccess {
				result.Status = types.GoalFailed
			}
			result.Error = stuckMsg + "。继续重规划大概率还是同样结果。"
			result.Suggestion = "建议把目标说得更具体（例如指定要处理的文件或期望的输出格式），或补充缺少的信息后重试；也可以切换到「对话」模式先对齐需求。"
			a.finalize(taskID, result, &plan, req.Role)
			return result
		}
		if reflection.Verdict == "failed" || attempt == attempts {
			result.Status = statusOfExec(exec)
			if result.Status == types.GoalRunning || result.Status == types.GoalSuccess {
				if reflection.Verdict == "failed" {
					result.Status = types.GoalFailed
				}
			}
			if reflection.Suggestion != "" {
				result.Suggestion = reflection.Suggestion
			}
			a.finalize(taskID, result, &plan, req.Role)
			return result
		}

		// 继续重规划。把上次每一步的结果一并带上——原先只给一个分数和一句话，
		// 第二轮规划与第一轮几乎同样盲（文案写着"可参考"，实际没东西可参考），
		// 这正是"原地打转"频繁触发的原因之一。
		//
		// 这里才是唯一算"返工"的地方：执行过了、被判定不达标、于是重做一遍。
		// 上面那处"计划校验未通过→重规划"不算——那一步什么都没执行，谈不上重做，
		// 把它计进返工会让这个指标随"计划写得好不好"漂移，而不是随交付质量漂移。
		if prog != nil {
			prog.reworks++
		}
		feedback = fmt.Sprintf("完成度 %d/100。问题：%s\n\n上次执行的情况（针对它们改，不要原样重来）：\n%s",
			reflection.Score, reflection.Reason, buildStepDigest(exec))
		notify("reflect", tell.Replan(reflection.Score), 90, "warn")
	}
	// 规划从未产出有效计划（典型：纯聊天目标误入工作模式）→ 回落直聊，不再硬失败
	if !planned && (last == nil || last.Status == types.GoalFailed) {
		notify("plan", "多次规划未成功，切换直接回答…", 20, "warn")
		res := a.runChatPath(ctx, goal, taskID, req.Role)
		if res.Status == types.GoalSuccess {
			res.Score = 70
			res.Suggestion = "这个问题更适合「对话」模式，切换后响应更快"
		}
		// 交付物是直聊给的，不是工作循环给的 → prog 保持"没走到完成判定"的原样，
		// 于是这次交付落成 FirstPass=false。**不能把它从口径里摘出去**：
		// 规划屡败正是最差的交付，把最差的从分母里拿掉，是在把比率改成好看的样子。
		return res
	}
	return a.cancelledResult(goal, last)
}

// runChatPath 对话模式管线：记忆 + 滚动摘要注入，直连 LLM 流式回答。
// 不经规划/执行/反思；工作模式规划屡败时也回落到此路径兜底。
func (a *Agent) runChatPath(ctx context.Context, goal, taskID, role string) *types.GoalResult {
	notify := func(phase, msg string, pct int, kind string) {
		a.Notifier.OnProgress(types.ProgressEvent{TaskID: taskID, Phase: phase, Message: msg, Progress: pct, Kind: kind})
	}
	if a.Mem != nil {
		a.Mem.AddTurn("user", goal)
		if a.Cfg.Agent.ContextCompress && a.Mem.Compress(a.CompressSummarizer(ctx)) {
			notify("chat", "已自动压缩早期上下文", 15, "info")
		}
	}
	name := a.Cfg.Persona.Name
	if strings.TrimSpace(name) == "" {
		name = "Gleam"
	}
	// 稳定段在前、易变段在后（同规划器）：多轮对话里 system + 前面的消息是天然的长公共前缀，
	// 把工作目录、摘要这类每轮都可能变的内容塞在前面，会把整段前缀废掉。
	sys := fmt.Sprintf("你是 %s（微光），运行在用户本机的桌面智能体，当前对话模式。"+
		"用与用户一致的语言自然对话：回答简洁、具体、可执行；涉及本机文件或命令的操作，"+
		"提示切换到「工作」或「编程」模式提交。%s", name, styleInstruction(a.Cfg.Persona.Style))
	var volatile strings.Builder
	fmt.Fprintf(&volatile, "\n\n## 当前环境\n工作目录：%s", a.Cfg.Workspace)
	if a.Mem != nil {
		if s := a.Mem.Summary(); strings.TrimSpace(s) != "" {
			volatile.WriteString("\n\n## 早期上下文摘要\n" + types.Shorten(s, 400))
		}
	}
	sys += volatile.String()
	var msgs []llm.Message
	if a.Mem != nil && a.Mem.Short != nil {
		for _, t := range a.Mem.Short.Recent(a.Mem.CarryTurns()) {
			role := llm.RoleUser
			if t.Role == "assistant" {
				role = llm.RoleAssistant
			}
			msgs = append(msgs, llm.Message{Role: role, Content: t.Content})
		}
	}
	// 最近一轮就是本次用户消息（AddTurn 已入短期记忆），避免重复注入
	if n := len(msgs); n > 0 && strings.TrimSpace(msgs[n-1].Content) == strings.TrimSpace(goal) {
		msgs = msgs[:n-1]
	}
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: goal})

	notify("chat", "思考中…", 30, "info")
	onDelta, flush := DeltaThrottle(40, func(text string) {
		notify("chat", strings.TrimSpace(text), 55, "llm")
	})
	text, err := a.TierLLM(role).ChatStream(ctx, llm.ChatRequest{
		System:      "你是 " + name + " 的对话管线。" + llm.MarkerChat + "\n\n" + sys,
		Messages:    msgs,
		Temperature: 0.6,
		TaskID:      taskID,
	}, onDelta)
	flush()
	if err != nil {
		if ctx.Err() != nil {
			return &types.GoalResult{Goal: goal, Status: types.GoalCancelled, Error: "任务已取消"}
		}
		notify("chat", "模型调用失败: "+err.Error(), 100, "error")
		res := &types.GoalResult{Goal: goal, Status: types.GoalFailed, Score: 0, Error: err.Error()}
		a.finalize(taskID, res, nil, role)
		return res
	}
	if a.Mem != nil {
		a.Mem.AddTurn("assistant", text)
	}
	// 回答已产出，交给辅助模型独立核对一次——对话模式没有规划阶段，
	// 这是唯一能拦住"干活的自己报满分"的地方。核对不成一律不阻断对话。
	if a.shouldChatCheck(goal, text) {
		notify("chat", "核对回答是否达成目标…", 95, "info")
	}
	res := &types.GoalResult{Goal: goal, Status: types.GoalSuccess, Score: 100, Summary: text}
	criteria, checks, outcome := a.chatSelfCheck(ctx, goal, text, taskID)
	if outcome == chatCheckDone {
		applyChatCheck(res, criteria, checks)
		// 通过就不必多嘴——清单会显示在任务卡上；没全过才值得提一句
		if res.Status != types.GoalSuccess {
			notify("chat", "自检未全过："+res.Error, 100, "warn")
		}
	} else if outcome == chatCheckUnusable {
		// 核对确实跑了，但调用失败或输出读不出来，没得出判定。
		// 状态与分数保持原样——回答已经交付，分数若改成 0，前端会渲染成红色的
		// "完成度 0/100"，那是在往反方向说谎（回答可能很好，只是没核对）。
		// 但**不能让用户以为核对过了**：静默保留 100 分正是这个功能要消灭的东西。
		notify("chat", "对话自检未能完成，本次回答未经核对", 100, "warn")
	}
	notify("chat", "已回复", 100, "info")
	a.finalize(taskID, res, nil, role)
	return res
}

// finalize 收尾：写入记忆、发出主动提议与技能固化建议；随后异步压缩上下文
// （不阻塞完成事件；溢出为空时是空操作）。
func (a *Agent) finalize(taskID string, result *types.GoalResult, plan *types.Plan, role string) {
	// 先按验收结果校正状态，再走后面的留档/提议——否则"部分完成"会被当成成功记进成长统计。
	var acceptance []string
	if plan != nil {
		acceptance = plan.Acceptance
	}
	applyAcceptanceVerdict(result, acceptance)
	if result.Summary != "" && a.Mem != nil {
		a.Mem.AddTurn("assistant", result.Summary)
	}
	if a.compressionOn() {
		// 收尾路径上做，但**不阻塞交付**：摘要要花一次模型调用，任务的结论不该等它。
		// 用独立的 ctx 与超时——任务自己的 ctx 这时可能已经取消（用户点了停止、
		// 或者任务是失败收场的），而"把早期上下文汇总下来"不因为任务失败就不该做。
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		go func() {
			defer cancelC()
			if compressed, tightened := a.compressAfterTask(ctxC, taskID); compressed || tightened {
				a.notifyContextAction(compressed, tightened, false)
			}
		}()
	}
	// 长期记忆沉淀任务记录（尽力而为）
	goal := result.Goal
	if goal != "" && a.Mem != nil && a.Mem.Long != nil {
		_, err := a.Mem.RememberScoped(fmt.Sprintf("任务记录[%s] 目标：%s 结果：%s（完成度 %d）",
			result.Status, goal, types.Shorten(result.Summary, 200), result.Score), "task", a.MemoryScope(), []string{"task"})
		if err == nil {
			_ = a.Mem.Long.Flush()
		}
	}
	if result.Suggestion != "" {
		a.Notifier.OnSuggestion(taskID, result.Suggestion)
	}
	// GEO 优化建议：创作类产出成功后分析并留档（writer 角色或识别为创作意图的任务）
	if a.shouldAnalyzeGEO(role, result) {
		a.analyzeGEOAsync(taskID, result.Goal, result.Summary)
	}
	// 技能固化建议：成功且 ≥2 个非 reply 步骤
	if plan != nil && result.Status == types.GoalSuccess {
		nonReply := 0
		for _, st := range plan.Steps {
			if st.Tool != "reply" {
				nonReply++
			}
		}
		if nonReply >= 2 {
			a.Notifier.OnSuggestSkill(taskID, types.SkillDraft{
				Name:        skillSlug(goal, a.Skills),
				Description: goal,
				Steps:       plan.Steps,
			})
		}
	}
}

// minGEOContentRunes 触发 GEO 自动分析的最短产出长度：
// 太短的回复（如"好的""已完成"）没有可优化的结构，不做分析以免刷屏。
const minGEOContentRunes = 80

// shouldAnalyzeGEO 判断本次产出是否值得做 GEO 分析。
// 条件：开关打开 + 任务成功 + 产出够长 + 属于创作（writer 角色或识别为创作意图）。
func (a *Agent) shouldAnalyzeGEO(role string, result *types.GoalResult) bool {
	if a == nil || a.LLM == nil || result == nil || a.Cfg == nil {
		return false
	}
	if !a.Cfg.Agent.GEOEnabled {
		return false
	}
	if result.Status != types.GoalSuccess {
		return false
	}
	if len([]rune(strings.TrimSpace(result.Summary))) < minGEOContentRunes {
		return false
	}
	return geo.IsCreative(result.Goal, role)
}

// analyzeGEOAsync 异步分析创作产出：留档到 GEO 历史，并把建议推送给前端。
// 分析失败只静默跳过——它是对成功任务的增强，不能反过来影响任务结果。
func (a *Agent) analyzeGEOAsync(taskID, goal, content string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	go func() {
		defer cancel()
		suggestion, err := (&geo.Analyzer{LLM: a.HelperLLM(), TaskID: taskID}).Analyze(ctx, content)
		if err != nil || suggestion == nil {
			return
		}
		if a.GEO != nil {
			a.GEO.Add(geo.Record{
				TaskID:      taskID,
				Goal:        types.Shorten(goal, 140),
				Score:       suggestion.Score,
				Summary:     suggestion.Summary,
				Strengths:   suggestion.Strengths,
				Weaknesses:  suggestion.Weaknesses,
				Actionables: suggestion.Actionables,
				Source:      "auto",
			})
		}
		a.Notifier.OnSuggestion(taskID, geo.Format(suggestion))
	}()
}

// appendUnique 追加不重复的元素。
func appendUnique(list []string, v string) []string {
	for _, s := range list {
		if s == v {
			return list
		}
	}
	return append(list, v)
}

// skillSlug 从目标生成技能名（避免与现有冲突）。
func skillSlug(goal string, store *skill.Store) string {
	base := slugStrip.ReplaceAllString(strings.TrimSpace(goal), "-")
	base = strings.Trim(base, "-")
	r := []rune(base)
	if len(r) > 24 {
		base = string(r[:24])
	}
	if base == "" {
		base = "skill"
	}
	name := base
	for i := 2; ; i++ {
		if store != nil {
			if _, err := store.Get(name); err != nil {
				break // 不存在，可用
			}
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}
