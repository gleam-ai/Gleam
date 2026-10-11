#!/usr/bin/env python3
"""批次 A「审计四用法」的变异清单。

对应 AI Coding 工程演变对照与升级清单.md 的 P0-1。这一批新增的东西有一个共同点：
**每一处失效都是静默的**——沿用被当成执行、沿用数没印、运行日志漏了沿用步、
缺记录时顺手跑一遍。它们不会报错，只会让复盘的人得出一个错误但看起来合理的结论。
所以变异验证在这里不是走形式：它回答的是"这些错有没有可能悄悄溜过去"。

运行：python scripts/mutation/batch-a-audit.py
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import _utf8  # noqa: F401  # emit UTF-8 to stdout: Windows defaults to cp936
from _harness import run_batch  # noqa: E402

AGENT = ("agent", "go test ./internal/agent/ -count=1 -timeout 90s")
CMD = ("cmd", "go test ./cmd/gleam/ -count=1 -timeout 90s")
SMOKE = ("smoke", "bash scripts/smoke-replay.sh")

MUTATIONS = [
    {
        "name": "carried_not_marked",
        "file": "internal/agent/executor.go",
        "edits": [("\t\tprev.Carried = true\n", "\t\tprev.Carried = false\n")],
        "targets": [AGENT],
    },
    {
        "name": "carried_count_not_summed",
        "file": "internal/agent/executor.go",
        "edits": [("\t\t\tres.Carried++\n", "\t\t\t_ = res.Carried\n")],
        "targets": [AGENT, CMD, SMOKE],
    },
    {
        "name": "carried_does_not_release_dependents",
        "file": "internal/agent/executor.go",
        "edits": [
            (
                "\t\tstate.results[i] = prev\n\t\tclose(state.finished[i])\n",
                "\t\tstate.results[i] = prev\n",
            )
        ],
        "targets": [AGENT],
    },
    {
        "name": "carried_steps_not_recorded",
        "file": "internal/agent/executor.go",
        "edits": [
            (
                "\t\te.record(taskID, state.results[i])\n\t\tcarried++",
                "\t\tcarried++",
            )
        ],
        "targets": [AGENT],
    },
    {
        "name": "carried_step_also_executed",
        "file": "internal/agent/executor.go",
        "edits": [
            (
                "\t\tif _, ok := e.Carried[plan.Steps[i].ID]; ok {\n",
                "\t\tif false {\n",
            ),
            (
                "\t\tstate.results[i] = prev\n\t\tclose(state.finished[i])\n",
                "\t\tstate.results[i] = prev\n",
            ),
        ],
        "targets": [AGENT, SMOKE],
    },
    {
        "name": "carried_double_close_panics",
        "file": "internal/agent/executor.go",
        "edits": [
            (
                "\t\tif _, ok := e.Carried[plan.Steps[i].ID]; ok {\n\t\t\tcontinue\n\t\t}\n",
                "\t\tif _, ok := e.Carried[plan.Steps[i].ID]; ok {\n\t\t}\n",
            )
        ],
        "targets": [AGENT],
    },
    {
        "name": "runlog_write_is_noop",
        "file": "internal/agent/runlog.go",
        "edits": [("\t_, _ = f.Write(append(b, '\\n'))\n", "\t_ = b\n")],
        "targets": [AGENT],
    },
    {
        "name": "runlog_sanitizes_unsafe_name",
        "file": "internal/agent/runlog.go",
        "edits": [
            (
                "\t\tcase r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':\n\t\tdefault:\n\t\t\treturn \"\"\n",
                "\t\tcase r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':\n\t\tdefault:\n\t\t\t_ = r\n",
            )
        ],
        "targets": [AGENT],
    },
    {
        "name": "runlog_missing_file_is_error",
        "file": "internal/agent/runlog.go",
        "edits": [
            (
                "\t\tif os.IsNotExist(err) {\n\t\t\treturn nil, nil\n\t\t}\n",
                "\t\tif false {\n\t\t\treturn nil, nil\n\t\t}\n",
            )
        ],
        "targets": [AGENT],
    },
    {
        "name": "runlog_broken_line_fails_whole",
        "file": "internal/agent/runlog.go",
        "edits": [
            (
                "\t\tvar r types.StepResult\n\t\tif err := json.Unmarshal([]byte(line), &r); err != nil {\n\t\t\tcontinue\n\t\t}\n",
                "\t\tvar r types.StepResult\n\t\tif err := json.Unmarshal([]byte(line), &r); err != nil {\n\t\t\treturn nil, err\n\t\t}\n",
            )
        ],
        "targets": [AGENT],
    },
    {
        "name": "discard_runlog_noop",
        "file": "internal/agent/runlog.go",
        "edits": [("\t_ = os.Remove(path)\n", "\t_ = path\n")],
        "targets": [AGENT, SMOKE],
    },
    {
        "name": "countsteps_status_only",
        "file": "cmd/gleam/replay.go",
        "edits": [
            (
                "\t\tcase r.Status == types.StepSucceeded && r.Outcome == types.OutcomeFailed:\n\t\t\tfailed++\n",
                "\t\tcase false:\n\t\t\tfailed++\n",
            )
        ],
        "targets": [CMD, SMOKE],
    },
    {
        "name": "countsteps_drops_carried",
        "file": "cmd/gleam/replay.go",
        "edits": [
            (
                "\t\tif r.Carried {\n\t\t\tcarried++\n\t\t}\n",
                "\t\tif false {\n\t\t\tcarried++\n\t\t}\n",
            )
        ],
        "targets": [CMD],
    },
    {
        "name": "stepline_hides_carried",
        "file": "cmd/gleam/replay.go",
        "edits": [('\t\tline += " 沿用（未执行）"\n', "")],
        "targets": [CMD, SMOKE],
    },
    {
        "name": "diff_drops_carried_line",
        "file": "cmd/gleam/replay.go",
        "edits": [
            (
                '\t\tfmt.Fprintf(&out, "  沿用：  %d 步 → %d 步（沿用=这次没有执行，结果取自上一次）\\n", a.Carried, b.Carried)\n',
                "",
            )
        ],
        "targets": [SMOKE],
    },
    {
        "name": "carried_warning_removed",
        "file": "cmd/gleam/replay.go",
        "edits": [
            (
                '\treturn fmt.Sprintf("提醒：沿用下来的步骤 %s 当时没有成功，依赖它们的步骤这次仍会被跳过——"+\n'
                '\t\t"这是当时的前提，不是恢复失败。要改这个前提，请把起点提前到那一步。", strings.Join(bad, "、"))\n',
                '\treturn ""\n',
            )
        ],
        "targets": [CMD],
    },
    {
        "name": "save_replay_to_tasks",
        "file": "cmd/gleam/replay.go",
        "edits": [
            (
                '\treturn filepath.Join(dataDir, "replays", id+".json")\n',
                '\treturn filepath.Join(dataDir, "tasks", id+".json")\n',
            )
        ],
        "targets": [CMD, SMOKE],
    },
    {
        "name": "loadrun_ambiguous_picks_task",
        "file": "cmd/gleam/replay.go",
        "edits": [
            (
                "\tif taskOK && replayOK {\n",
                "\tif false && taskOK && replayOK {\n",
            )
        ],
        "targets": [CMD],
    },
    {
        "name": "resume_missing_result_runs_anyway",
        "file": "cmd/gleam/replay.go",
        "edits": [
            (
                "\t\t\tr, ok := src.resultOf(st.ID)\n\t\t\tif !ok {\n",
                "\t\t\tr, ok := src.resultOf(st.ID)\n\t\t\tif false && !ok {\n",
            )
        ],
        "targets": [SMOKE],
    },
    {
        "name": "replay_sink_not_wired",
        "file": "cmd/gleam/replay.go",
        "edits": [
            ("\t\te.Sink = agent.NewRunLog(dataDir)\n", "\t\t_ = dataDir\n")
        ],
        "targets": [CMD],
    },
]

if __name__ == "__main__":
    spec = sys.argv[1] if len(sys.argv) > 1 else ""
    sys.exit(run_batch("批次 A · 审计四用法 变异验证", MUTATIONS, spec=spec))
