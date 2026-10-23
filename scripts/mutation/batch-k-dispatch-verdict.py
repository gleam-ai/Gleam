#!/usr/bin/env python3
"""批次 K「派发前重算裁决」的变异清单。

这一批盯的失效全是**静默**的：门控、审核模型、审计台账三处都会在参数还是
`$ref:` 占位符时就下结论，然后一切照常跑完——没有报错，只有一句读起来像查过了的话。

- 门控对**非绝对路径**的口径是"按工作区内放过"（`safety.pathTrusted`），而
  `$ref:s1.output.path` 正是一个非绝对路径字符串。真值可能指向信任范围外。
- 审计于是写下"操作路径均在信任路径内"——它在替一次没做过的检查作证。
- 审核模型快筛看到的也是占位符：花钱筛一个假值，真值一次都没被筛过。
- 审批卡片上如果是占位符，人批的是一个永远不会被执行的路径。

现在的实现只有**一次**裁决，且发生在 `$ref:` 替换之后；旧实现"先按占位符裁决、
再按真值重算"的中间态已经不存在。所以这里改的是：裁决仍看占位符、裁决结果不执行、
人工批准后又补记自动放行、真值没进快筛、审批卡片退回占位符——五条都仍然静默。

运行：python scripts/mutation/batch-k-dispatch-verdict.py
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import _utf8  # noqa: F401  # emit UTF-8 to stdout: Windows defaults to cp936
from _harness import run_batch  # noqa: E402

AGENT = ("agent", "go test ./internal/agent/ -count=1 -timeout 90s")

MUTATIONS = [
    {
        "name": "verdict_not_recomputed",
        "file": "internal/agent/executor.go",
        "edits": [
            (
                "\tdec := e.adjudicate(ctx, tool, args, preApproved && !hasRefs(step.Args), true)\n",
                "\tdec := e.adjudicate(ctx, tool, step.Args, preApproved && !hasRefs(step.Args), true)\n",
            )
        ],
        "targets": [AGENT],
    },
    {
        "name": "recomputed_but_not_enforced",
        "file": "internal/agent/executor.go",
        "edits": [
            (
                "\tif dec.NeedApproval {\n",
                "\tif false && dec.NeedApproval {\n",
            )
        ],
        "targets": [AGENT],
    },
    {
        "name": "human_approved_also_recorded_as_auto",
        "file": "internal/agent/executor.go",
        "edits": [
            (
                '\t} else if dec.Risk != "low" && e.Gate != nil {\n',
                '\t}\n\tif dec.Risk != "low" && e.Gate != nil {\n',
            )
        ],
        "targets": [AGENT],
    },
    {
        "name": "real_args_never_reviewed",
        "file": "internal/agent/executor.go",
        "edits": [
            (
                "\tdec := e.adjudicate(ctx, tool, args, preApproved && !hasRefs(step.Args), true)\n",
                "\tdec := e.adjudicate(ctx, tool, args, preApproved && !hasRefs(step.Args), !hasRefs(step.Args))\n",
            )
        ],
        "targets": [AGENT],
    },
    {
        "name": "approval_card_shows_placeholder",
        "file": "internal/agent/executor.go",
        "edits": [
            (
                "\t\tresolved := step\n\t\tresolved.Args = args\n",
                "\t\tresolved := step\n",
            )
        ],
        "targets": [AGENT],
    },
]

if __name__ == "__main__":
    spec = sys.argv[1] if len(sys.argv) > 1 else ""
    sys.exit(
        run_batch("批次 K · 派发前重算裁决 变异验证", MUTATIONS, spec=spec)
    )
