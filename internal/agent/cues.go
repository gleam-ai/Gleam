package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gleam/pkg/types"
)

var cueSignals = []string{"repeat_failure", "half_done", "tool_failure"}

func cueSignalText(signal string, en bool) string {
	switch signal {
	case "repeat_failure":
		return bi{"同一个目标反复失败", "The same goal failed repeatedly"}.pick(en)
	case "half_done":
		return bi{"同一个目标每次只做一半", "The same goal stalls halfway every time"}.pick(en)
	case "tool_failure":
		return bi{"同一类失败反复出现", "One failure kind keeps recurring"}.pick(en)
	}
	return signal
}

const (
	cueRepeatMin = 2
	cueHalfMin   = 2
	cueKindMin   = 3
	cueKindTasks = 2
	cueTaskScan  = 200
	cueLimit     = 3
	cueGoalCut   = 40
	cueEvidMax   = 3
)

type CueRow struct {
	ID         string   `json:"id"`
	Signal     string   `json:"signal"`
	SignalText string   `json:"signal_text"`
	Title      string   `json:"title"`
	Goal       string   `json:"goal"`
	Why        string   `json:"why"`
	Evidence   []string `json:"evidence"`
	Count      int      `json:"count"`
	TraceID    string   `json:"trace_id,omitempty"`
	LastSeen   string   `json:"last_seen,omitempty"`
}

type CueLedger struct {
	Rows       []CueRow           `json:"rows"`
	Suppressed []CueSuppressedRow `json:"suppressed"`
	Limit      int                `json:"limit"`
	Overflow   int                `json:"overflow"`
	Truncated  string             `json:"truncated,omitempty"`
	Scanned    int                `json:"scanned"`
	Skipped    int                `json:"skipped"`
	Coverage   string             `json:"coverage"`
	Note       string             `json:"note,omitempty"`
}

type CueSuppressedRow struct {
	CueSuppression
	Title      string `json:"title"`
	Goal       string `json:"goal"`
	SignalText string `json:"signal_text"`
	ReasonText string `json:"reason_text"`
}

type traceGroup struct {
	traceID  string
	goal     string
	failed   []types.GoalResult
	partial  []types.GoalResult
	everWon  bool
	lastSeen time.Time
}

func (a *Agent) CueView(en bool) CueLedger {
	results, skipped, err := ListTaskResults(a.Cfg.DataDir, cueTaskScan)
	led := CueLedger{
		Limit:    cueLimit,
		Scanned:  len(results),
		Skipped:  skipped,
		Coverage: cueCoverage(len(results), skipped, en),
	}
	if err != nil {
		led.Note = fmt.Sprintf(bi{
			"任务归档读不动，这一页什么都没算出来：%v",
			"Task archives are unreadable, so this page computed nothing: %v",
		}.pick(en), err)
		return led
	}
	kept := loadCueStore(a.Cfg.DataDir)
	all := deriveCues(results, en)
	led.Suppressed = kept.list(en, all)
	rows := make([]CueRow, 0, len(all))
	for _, r := range all {
		if !kept.muted(r.ID) {
			rows = append(rows, r)
		}
	}
	if len(rows) > cueLimit {
		led.Overflow = len(rows) - cueLimit
		led.Truncated = fmt.Sprintf(bi{
			"另有 %d 条够格但没挤进这一屏：名额按严重程度排，前面的占满了。",
			"%d more qualified items did not fit on this screen: slots go by severity and the top ones filled them.",
		}.pick(en), led.Overflow)
		rows = rows[:cueLimit]
	}
	led.Rows = rows
	led.Note = kept.note(en)
	return led
}

func deriveCues(results []*types.GoalResult, en bool) []CueRow {
	var rows []CueRow
	groups := groupByTrace(results)
	kinds := tallyFailureKinds(results)
	for _, signal := range cueSignals {
		switch signal {
		case "repeat_failure":
			rows = append(rows, repeatFailureRows(groups, en)...)
		case "half_done":
			rows = append(rows, halfDoneRows(groups, en)...)
		case "tool_failure":
			rows = append(rows, toolFailureRows(kinds, en)...)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

func repeatFailureRows(groups map[string]*traceGroup, en bool) []CueRow {
	var rows []CueRow
	for _, g := range groups {
		if len(g.failed) < cueRepeatMin {
			continue
		}
		rows = append(rows, CueRow{
			ID:         "repeat_failure:" + g.traceID,
			Signal:     "repeat_failure",
			SignalText: cueSignalText("repeat_failure", en),
			Title: fmt.Sprintf(bi{
				"「%s」已经失败 %d 次",
				"\"%s\" has failed %d times",
			}.pick(en), cutRunes(g.goal, cueGoalCut), len(g.failed)),
			Goal: fmt.Sprintf(bi{
				"查清「%s」为什么反复失败：列出每一次的失败类别与最后一步的错误，给出一个改走的方案（把目标拆小 / 换任务模式 / 补上缺失的引用），不要照原样再跑一遍",
				"Find out why \"%s\" keeps failing: list every failure kind and the last step's error, then propose a different approach (split the goal smaller / switch task mode / supply the missing reference). Do not simply rerun it as-is",
			}.pick(en), cutRunes(g.goal, cueGoalCut)),
			Why: bi{
				"同一个输入配同一个模型，trace_id 才会相同——它反复出现说明卡住的是做法，不是运气。",
				"The trace_id only matches when the same input meets the same model — its recurrence means the approach is stuck, not the luck.",
			}.pick(en),
			Evidence: g.evidence(en),
			Count:    len(g.failed),
			TraceID:  g.traceID,
			LastSeen: stamp(g.lastSeen, en),
		})
	}
	return rows
}

func halfDoneRows(groups map[string]*traceGroup, en bool) []CueRow {
	var rows []CueRow
	for _, g := range groups {
		if len(g.partial) < cueHalfMin {
			continue
		}
		if g.everWon {
			continue
		}
		if len(g.failed) >= cueRepeatMin {
			continue
		}
		rows = append(rows, CueRow{
			ID:         "half_done:" + g.traceID,
			Signal:     "half_done",
			SignalText: cueSignalText("half_done", en),
			Title: fmt.Sprintf(bi{
				"「%s」连着 %d 次停在半路",
				"\"%s\" stalled halfway %d times in a row",
			}.pick(en), cutRunes(g.goal, cueGoalCut), len(g.partial)),
			Goal: fmt.Sprintf(bi{
				"给「%s」定几条能核对到底的验收标准，再找出每次都停在同一处的原因",
				"Define acceptance criteria for \"%s\" that can be checked to the end, then find out why every run stalls at the same place",
			}.pick(en), cutRunes(g.goal, cueGoalCut)),
			Why: bi{
				"每次都停在同一个地方，通常是承诺本身含糊——不是再试一次就能过。",
				"Stalling at the same spot every time usually means the promise itself is vague — one more retry will not get it through.",
			}.pick(en),
			Evidence: g.evidence(en),
			Count:    len(g.partial),
			TraceID:  g.traceID,
			LastSeen: stamp(g.lastSeen, en),
		})
	}
	return rows
}

func toolFailureRows(kinds map[types.ErrorKind]*kindTally, en bool) []CueRow {
	var rows []CueRow
	for _, k := range kinds {
		if k.steps < cueKindMin || len(k.tasks) < cueKindTasks {
			continue
		}
		human := cueKindText(k.kind, en)
		var title, goal string
		if en {
			title = fmt.Sprintf("%s appeared %d times across %d recent tasks", human, k.steps, len(k.tasks))
			goal = fmt.Sprintf("Investigate why \"%s\" failures appeared %d times across %d recent tasks: reproduce one first, then decide whether the tool, the permissions or the arguments must change. Do not rely on rerun luck",
				human, k.steps, len(k.tasks))
		} else {
			title = fmt.Sprintf("%s 在最近 %d 个任务里出现了 %d 次", human, len(k.tasks), k.steps)
			goal = fmt.Sprintf("排查「%s」这类失败在最近 %d 个任务里出现 %d 次的原因：先复现一次，再判断该改的是工具、权限还是参数；不要靠重跑碰运气",
				human, len(k.tasks), k.steps)
		}
		rows = append(rows, CueRow{
			ID:         "tool_failure:" + string(k.kind),
			Signal:     "tool_failure",
			SignalText: cueSignalText("tool_failure", en),
			Title:      title,
			Goal:       goal,
			Why: bi{
				"归因分布回答的是「该先修哪一层」；同类失败跨任务反复出现时，重跑只是把同一堵墙再撞一遍。",
				"The attribution spread answers \"which layer to fix first\"; when one failure kind recurs across tasks, rerunning just hits the same wall again.",
			}.pick(en),
			Evidence: k.evidence(en),
			Count:    k.steps,
			LastSeen: stamp(k.last, en),
		})
	}
	return rows
}

type kindTally struct {
	kind   types.ErrorKind
	steps  int
	tasks  map[string]bool
	sample string
	last   time.Time
}

func (k *kindTally) evidence(en bool) []string {
	out := []string{fmt.Sprintf(bi{"最后一次：%s", "last attempt: %s"}.pick(en), stamp(k.last, en))}
	if k.sample != "" {
		out = append(out, fmt.Sprintf(bi{"现场：%s", "sample: %s"}.pick(en), cutRunes(k.sample, 80)))
	}
	out = append(out, fmt.Sprintf(bi{
		"涉及 %d 个任务、累计 %d 步",
		"%d task(s) involved, %d steps in total",
	}.pick(en), len(k.tasks), k.steps))
	return out
}

func groupByTrace(results []*types.GoalResult) map[string]*traceGroup {
	groups := map[string]*traceGroup{}
	for _, g := range results {
		if g == nil || g.TraceID == "" {
			continue
		}
		tg := groups[g.TraceID]
		if tg == nil {
			tg = &traceGroup{traceID: g.TraceID, goal: g.Goal}
			groups[g.TraceID] = tg
		}
		if tg.goal == "" {
			tg.goal = g.Goal
		}
		if g.FinishedAt.After(tg.lastSeen) {
			tg.lastSeen = g.FinishedAt
		}
		switch g.Status {
		case types.GoalSuccess:
			tg.everWon = true
		case types.GoalFailed, types.GoalCancelled:
			tg.failed = append(tg.failed, *g)
		case types.GoalPartial:
			tg.partial = append(tg.partial, *g)
		default:
		}
	}
	return groups
}

func tallyFailureKinds(results []*types.GoalResult) map[types.ErrorKind]*kindTally {
	kinds := map[types.ErrorKind]*kindTally{}
	for _, g := range results {
		if g == nil {
			continue
		}
		for k, n := range g.FailureBreakdown {
			t := kinds[k]
			if t == nil {
				t = &kindTally{kind: k, tasks: map[string]bool{}}
				kinds[k] = t
			}
			t.steps += n
			t.tasks[g.TaskID] = true
			if g.FinishedAt.After(t.last) {
				t.last = g.FinishedAt
			}
		}
		if t := strings.TrimSpace(g.Error); t != "" && len(g.FailureBreakdown) == 0 {
			k := types.ClassifyError(t)
			t2 := kinds[k]
			if t2 == nil {
				t2 = &kindTally{kind: k, tasks: map[string]bool{}}
				kinds[k] = t2
			}
			t2.steps++
			t2.tasks[g.TaskID] = true
			if t2.sample == "" {
				t2.sample = t
			}
			if g.FinishedAt.After(t2.last) {
				t2.last = g.FinishedAt
			}
		}
		for i := range g.Steps {
			st := g.Steps[i]
			if st.ErrorKind == "" {
				continue
			}
			t := kinds[st.ErrorKind]
			if t == nil {
				t = &kindTally{kind: st.ErrorKind, tasks: map[string]bool{}}
				kinds[st.ErrorKind] = t
			}
			if t.sample == "" && st.Error != "" {
				t.sample = fmt.Sprintf("%s：%s", st.Tool, st.Error)
			}
		}
	}
	return kinds
}

func cueKindText(k types.ErrorKind, en bool) string {
	switch k {
	case types.ErrParam:
		return bi{"参数错（改参数可重试）", "bad parameters (retry after fixing them)"}.pick(en)
	case types.ErrFormat:
		return bi{"格式错（改格式可重试）", "bad format (retry after fixing it)"}.pick(en)
	case types.ErrBusiness:
		return bi{"业务拒绝（换方案，重试无用）", "rejected by a business rule (change the approach; retrying is useless)"}.pick(en)
	case types.ErrPermission:
		return bi{"权限不足（重试无用）", "insufficient permission (retrying is useless)"}.pick(en)
	case types.ErrNotFound:
		return bi{"资源不存在（换目标）", "resource not found (change the target)"}.pick(en)
	case types.ErrTimeout:
		return bi{"超时或被限流（退避重试）", "timeout or rate-limited (retry with backoff)"}.pick(en)
	case types.ErrUpstream:
		return bi{"上游服务异常（有限重试）", "upstream service error (limited retries)"}.pick(en)
	case types.ErrInternal:
		return bi{"工具自身缺陷（要改代码）", "the tool itself is faulty (needs a code change)"}.pick(en)
	case types.ErrUnknown:
		return bi{"归不了类的失败", "unclassified failure"}.pick(en)
	}
	return string(k)
}

func cueCoverage(scanned, skipped int, en bool) string {
	var s string
	if en {
		s = fmt.Sprintf("This page draws cues only from task archives still kept on this machine: %d read", scanned)
		if skipped > 0 {
			s += fmt.Sprintf(", %d more unreadable and skipped", skipped)
		}
		s += fmt.Sprintf("; older archives are trimmed by task count, and unseen is not the same as never happened. At most %d cards.", cueLimit)
		return s
	}
	s = fmt.Sprintf("这一页只从本机还留着的任务归档里想线索：读了 %d 条", scanned)
	if skipped > 0 {
		s += fmt.Sprintf("，另有 %d 条读不动已跳过", skipped)
	}
	s += fmt.Sprintf("；更早的归档已按任务数裁剪，看不见不等于没发生。最多列 %d 张卡。", cueLimit)
	return s
}

func stamp(t time.Time, en bool) string {
	if t.IsZero() {
		return bi{"时间未记录", "no timestamp recorded"}.pick(en)
	}
	return t.Format("2006-01-02 15:04")
}

func cutRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (g *traceGroup) evidence(en bool) []string {
	runs := append([]types.GoalResult{}, g.failed...)
	runs = append(runs, g.partial...)
	sort.Slice(runs, func(i, j int) bool { return runs[i].FinishedAt.After(runs[j].FinishedAt) })
	var out []string
	for i, r := range runs {
		if i >= cueEvidMax {
			out = append(out, fmt.Sprintf(bi{
				"…另有 %d 次同类记录",
				"…%d more records of the same kind",
			}.pick(en), len(runs)-cueEvidMax))
			break
		}
		status := bi{"失败", "failed"}.pick(en)
		if r.Status == types.GoalPartial {
			status = fmt.Sprintf(bi{
				"只做了一半（完成度 %d）",
				"halfway (score %d)",
			}.pick(en), r.Score)
		}
		line := fmt.Sprintf("%s · %s", stamp(r.FinishedAt, en), status)
		if k := dominantKind(r.FailureBreakdown); k != "" {
			line += " · " + cueKindText(k, en)
		}
		if strings.TrimSpace(r.Error) != "" {
			line += bi{"：", ": "}.pick(en) + cutRunes(r.Error, 60)
		}
		out = append(out, line)
	}
	if g.traceID != "" {
		out = append(out, fmt.Sprintf(bi{"同一 trace_id：%s", "same trace_id: %s"}.pick(en), g.traceID))
	}
	return out
}

func dominantKind(m map[types.ErrorKind]int) types.ErrorKind {
	var best types.ErrorKind
	bestN := 0
	for k, n := range m {
		if n > bestN {
			best, bestN = k, n
		}
	}
	return best
}
