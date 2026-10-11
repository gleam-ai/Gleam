#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""app.js 的启动序列必须是最后一段：它同步读到的顶层 let/const 不能落在它后面。

**为什么要有**：JS 的顶层 `let`/`const` 在赋值语句执行之前是**暂时性死区**，读它就是
`ReferenceError`——不是 undefined，不是"先空着回头再填"。而一个 4400 行的单文件前端里，
启动 IIFE 写在中间、后面的节还照常声明 `const roleSelect = ...`，看起来天经地义，
实际是 `loadRoles()` 在首屏抛错、角色下拉永远停在 HTML 里的写死项。
更糟的是这类错误**不打断渲染**：SSE 照常连、卡片照常出，只有那几块静默是空的。

**为什么不靠 code review 拦住**：`init()` 调到的函数有七个，每个函数体第一行是不是
`await` 决定了它会不会撞死区——这条链没人会在 review 时逐个数。判据本身是机械的
（启动段之后不许再有顶层语句），所以配一条 exit non-zero 的命令（AGENTS.md 铁律 3）。

**怎么改才通过**：新代码加在启动段**之前**。确实要在启动前跑的副作用，
写进 `init()` 里，别在文件尾巴上挂一条裸语句。

**第二条判据：顶层前向引用**。启动段在最后只保证「启动序列」不撞死区；
文件**中部**的顶层语句照样能在声明之前读一个 `const`——真实事故：`app.js`
第 632 行的顶层语句读 `VoiceInput`，而它的 `const` 声明在第 8649 行，TDZ
`ReferenceError` 让脚本从那一行起中断，其后全部监听与 `init()` 都不再执行。
这条判据扫描列 0 的顶层语句，发现它引用了**行号更大**的顶层 `const`/`let`
就判红。函数体内的引用都带缩进，天然排除；`function` 声明会提升，不在此列。

**自我失效的防线**：找不到启动段标记就失败——宁可红着，也不给"已通过"的错觉。

**负例控制自带**：`--self-test` 现造三份坏文本（尾巴上挂一条顶层语句 / 标记丢了 /
顶层前向引用），断言本脚本会把它们判红。判据不跑负例就不知道自己是不是在永远点头。

用法：python scripts/check-app-startup.py [--self-test] [仓库根目录]
退出码：0 = 启动段是最后一段且无顶层前向引用（`--self-test` 时：三份负例都被拦住）；1 = 否则。
"""

import io
import os
import re
import sys

import _utf8  # noqa: F401  # emit UTF-8 to stdout: Windows defaults to cp936

APP_JS = os.path.join("internal", "webui", "static", "app.js")
STARTUP_HEAD = "(async function init()"
STARTUP_TAIL = "})();"

COMMENT = ("//", "/*", "*")

TOP_DECL = re.compile(r"^(?:const|let)\s+([A-Za-z_$][\w$]*)\s*=")


def is_top_level_code(line: str) -> bool:
    s = line.strip()
    if not s or s.startswith(COMMENT):
        return False
    return line[0] not in (" ", "\t", "}", ")")


def forward_refs(lines):
    """找出「顶层语句引用了行号更大的顶层 const/let」——TDZ ReferenceError 的机械判据。

    返回 [(引用行 0 基, 声明行 0 基, 名字, 引用行文本), ...]，一行只报第一个命中的名字。
    """
    decls = {}
    tops = []
    for i, line in enumerate(lines):
        if not is_top_level_code(line):
            continue
        tops.append((i, line))
        m = TOP_DECL.match(line)
        if m and m.group(1) not in decls:
            name = m.group(1)
            decls[name] = (i, re.compile(r"\b%s\b" % re.escape(name)))
    bad = []
    for i, line in tops:
        for name, (d, pat) in decls.items():
            if d > i and pat.search(line):
                bad.append((i, d, name, line.strip()))
                break
    return bad


def judge(lines, label, say):
    """判定一份文本。返回退出码；说明经 `say(文本)` 送出（self-test 不进最终输出）。"""
    heads = [i for i, line in enumerate(lines) if line.startswith(STARTUP_HEAD)]
    if not heads:
        say(
            "%s 里找不到启动段入口 %s：判据失效，请先修本脚本。"
            % (label, STARTUP_HEAD)
        )
        return 1
    if len(heads) > 1:
        say("%s：启动段入口出现多次，判据不知道哪一段才是启动" % label)
        return 1
    start = heads[0]

    end = None
    for j in range(start + 1, len(lines)):
        if lines[j].startswith(STARTUP_TAIL):
            end = j
            break
    if end is None:
        say(
            "%s：启动段没有 %s 收尾，本脚本无法判断「后面」从哪一行算起。"
            % (label, STARTUP_TAIL)
        )
        return 1

    bad = [
        i + 1 for i in range(end + 1, len(lines)) if is_top_level_code(lines[i])
    ]
    if bad:
        say(
            "启动序列必须是 app.js 的最后一段，否则它同步读到的顶层 let/const 还在暂时性死区里："
        )
        for n in bad[:20]:
            say("  %s:%d  %s" % (label, n, lines[n - 1].strip()[:72]))
        if len(bad) > 20:
            say("  …另有 %d 处" % (len(bad) - 20))
        say("  把这些移到启动段之前（或挪进 init() 里）。")
        return 1

    fwd = forward_refs(lines)
    if fwd:
        say(
            "顶层语句在声明之前引用了 const/let（暂时性死区，脚本会从该行起中断）："
        )
        for i, d, name, text in fwd[:20]:
            say(
                "  %s:%d  %s  ← %s 在第 %d 行才声明"
                % (label, i + 1, text[:60], name, d + 1)
            )
        if len(fwd) > 20:
            say("  …另有 %d 处" % (len(fwd) - 20))
        say("  把引用移到声明之后（或挪进函数体 / init() 里）。")
        return 1
    say(
        "  启动段是 %s 最后一段（第 %d–%d 行），其后无顶层语句、无顶层前向引用"
        % (label, start + 1, end + 1)
    )
    return 0


def self_test(good_lines) -> int:
    """拿真文件现造两份坏文本，断言判据会把它们拦住。"""
    marker = next(
        (i for i, l in enumerate(good_lines) if l.startswith(STARTUP_HEAD)),
        None,
    )
    if marker is None:
        print("self-test：真文件里找不到启动段入口，负例无从构造")
        return 1
    idx = next(
        (
            j
            for j in range(marker + 1, len(good_lines))
            if good_lines[j].startswith(STARTUP_TAIL)
        ),
        None,
    )
    if idx is None:
        print("self-test：启动段之后找不到 %s，负例无从构造" % STARTUP_TAIL)
        return 1
    cases = [
        (
            "尾巴上挂一条顶层语句",
            good_lines[: idx + 1]
            + ["", 'const lateThing = document.getElementById("x");'],
        ),
        (
            "启动段入口被删了",
            [l for i, l in enumerate(good_lines) if i != marker],
        ),
        (
            "顶层语句引用了行号更大的 const（TDZ）",
            ["const fwdRefUser = fwdRefTarget || null;"]
            + good_lines[:marker]
            + ["const fwdRefTarget = 1;"]
            + good_lines[marker:],
        ),
    ]
    bad = 0
    for name, lines in cases:
        sink = []
        rc = judge(lines, "负例·" + name, sink.append)
        if rc == 0:
            print("负例没被拦住：%s —— 判据在永远点头，本脚本要修" % name)
            bad += 1
        else:
            print("  拦住：%s（%s）" % (name, sink[0][:60]))
    return 1 if bad else 0


def main() -> int:
    argv = [a for a in sys.argv[1:] if a != "--self-test"]
    selftest = "--self-test" in sys.argv[1:]
    root = argv[0] if argv else "."
    path = os.path.join(root, APP_JS)
    try:
        with io.open(path, "rb") as f:
            text = f.read().decode("utf-8", "replace")
    except OSError as e:
        print("读不到 %s：%s" % (APP_JS.replace("\\", "/"), e))
        return 1

    lines = text.splitlines()
    label = (
        APP_JS.replace("\\", "/")
        if root == "."
        else os.path.join(root, APP_JS).replace("\\", "/")
    )
    rc = judge(lines, label, print)
    if rc != 0 or not selftest:
        return rc
    return self_test(lines)


if __name__ == "__main__":
    sys.exit(main())
