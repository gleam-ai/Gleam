#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""厂商目录里写给用户看的字符串，必须能在 i18n.js 里翻成英文：缺一条，英文界面就漏中文。

**为什么要有**：「英文语言下不要有其他语言」靠两层拼起来——后端拼的句子（连接台账、
候补目标）由 Go 里的 `bi` 两份现算，运行时判据（TestConnections_EnglishLedgerHasNoCJK 等）
盯着零 CJK；而厂商目录（`internal/llm/providers.go` 的 `Name:` / `Label:`）走的是另一条路：
**原文渲染进下拉框，前端按整句精确匹配查 `I18N_EN` 词条翻译**。这条路没有运行时判据——
Go 测试不认识 JS 字典，界面测试不存在；新接一家厂商忘了加词条，英文界面就多一行中文，
而且没有任何测试会红。这正是本仓库栽过的那一类（AGENTS.md 铁律 3：每条可机械判断的规则
都要有 exit non-zero 的命令）。

**判据两条**：
1. providers.go 里每个含 CJK 的 `Name:` / `Label:` **字符串字面量**，必须原样出现在
   i18n.js `I18N_EN` 的键里。前端查表按「空白压成单格后的整句」匹配，所以词条键必须
   与 Go 字面量一字不差。
2. `Name:` / `Label:` 必须是字符串字面量。传变量、拼字符串的写法查表无从对起，
   当场拦：宁可要求写死，也不要留一个"看着有其实翻不动"的显示名。

**为什么只判单向**：出网台账那种双向对账在这里不成立——`I18N_EN` 里**本来就合法地**住着
大量只属于前端自己的词条（index.html / app.js 的静态文案）。反向要求"每个词条都要有 Go
出处"会把字典清空。这一闸只回答一个问题：Go 递给界面的每一句，字典接得住吗。

**为什么读源码前先剥注释**：providers.go 的文档注释里到处是中文（它在解释收录规则）。
不剥注释，一句注释就凭空多出一个"要翻的显示名"，判据第一天就开始说谎。
剥注释的实现住在 `scripts/_gocomment.py`（与 check-egress-owner.py / check-cue-owner.py
共用同一个字符状态机），这里不另写一份。

**怎么改才通过**：往 providers.go 加厂商 / 套餐时，在 i18n.js 的 `I18N_EN` 里加一条
**键与 Go 字面量完全相同**的词条。英文说法不需要逐字对译，但键必须逐字相同——
查表按键，不按意思。

**自我失效的防线**：providers.go 一个 `Name:`/`Label:` 都扫不到、i18n.js 里解析不到
`I18N_EN` 或一个键都提取不到，都直接失败——宁可红着，也不给"已通过"的错觉。

**负例控制自带**：`--self-test` 现造三份坏文本（词条缺失 / 显示名传变量 / 字典解析不出）
断言会被判红，再拿两份**长得像**的干扰文本（CJK 只在注释里 / 不含 CJK 的显示名）断言不许误报。

用法：python scripts/check-i18n-catalog.py [--self-test] [仓库根目录]
退出码：0 = 每一句都接得住；1 = 否则。
"""

import io
import os
import re
import sys

import _gocomment
import _utf8  # noqa: F401  # Windows 下 stdout 默认按 GBK 写，中文会变乱码

PROVIDERS_FILE = os.path.join("internal", "llm", "providers.go")
I18N_FILE = os.path.join("internal", "webui", "static", "i18n.js")


# 与 Go 侧运行时判据（connections_test.go 的 hasCJK）同一组区段：全角标点也算 CJK——
# 「（）：」落在这些区段里，英文词条必须连标点一起换成 ASCII，否则照样漏。
def has_cjk(s):
    return any(
        0x3000 <= c <= 0x303F
        or 0x4E00 <= c <= 0x9FFF
        or 0xF900 <= c <= 0xFAFF
        or 0xFF00 <= c <= 0xFFEF
        for c in map(ord, s)
    )


# `Name: "…"` / `Label: "…"`：字段名 + 冒号 + 一个 Go 字符串字面量（或不是字面量）。
FIELD_RE = re.compile(
    r'\b(Name|Label)\s*:\s*(?:"((?:[^"\\]|\\.)*)"|([^"\s][^,\n]*))'
)


def catalog_strings(src):
    """剥注释后的 providers.go 里，所有 Name/Label 的取值。

    返回 (literals, non_literals)：字面量列表与"传了变量"的行文，供两条判据分别用。
    """
    literals, non_literals = [], []
    for m in FIELD_RE.finditer(src):
        if m.group(2) is not None:
            literals.append(m.group(2))
        else:
            line_no = src.count("\n", 0, m.start()) + 1
            non_literals.append((line_no, m.group(1), m.group(3).strip()))
    return literals, non_literals


def i18n_keys(src):
    """从 i18n.js 里提取 I18N_EN 的全部键。

    键有两种写法：带引号（键里含标点/空格时 prettier 会加引号）与裸标识符。
    词条按键行首两格缩进识别——值折行时缩进是四格，天然分得开。
    """
    m = re.search(r"const I18N_EN\s*=\s*\{", src)
    if not m:
        return None
    body = src[m.end() : src.find("\n};", m.end())]
    keys = set()
    for line in body.splitlines():
        km = re.match(
            r'^  (?:"((?:[^"\\]|\\.)*)"|\'((?:[^\'\\]|\\.)*)\'|([^\'"\s].*?)):\s*(?:$|[/\s]|\S)',
            line,
        )
        if km:
            key = next(g for g in km.groups() if g is not None)
            keys.add(key)
    return keys


def check(root):
    problems = []

    ppath = os.path.join(root, PROVIDERS_FILE)
    with io.open(ppath, "r", encoding="utf-8") as f:
        src = _gocomment.strip(f.read())
    literals, non_literals = catalog_strings(src)
    if not literals and not non_literals:
        problems.append(
            "providers.go 里一个 Name:/Label: 都扫不到：字段改名了？判据失效。"
        )
        return problems

    for line_no, field, expr in non_literals:
        problems.append(
            "providers.go:%d  %s 不是字符串字面量（%s）：查表按整句匹配，"
            "变量值无从对起——显示名必须写死。" % (line_no, field, expr)
        )

    ipath = os.path.join(root, I18N_FILE)
    with io.open(ipath, "r", encoding="utf-8") as f:
        keys = i18n_keys(f.read())
    if not keys:
        problems.append(
            "i18n.js 里解析不到 I18N_EN 或一个键都提取不到：字典结构变了？判据失效。"
        )
        return problems

    for lit in literals:
        if not has_cjk(lit):
            continue
        if lit not in keys:
            problems.append(
                'providers.go 的显示名 "%s" 在 i18n.js 的 I18N_EN 里没有词条：'
                "英文界面会原样显示这句中文。" % lit
            )
    return problems


# ---------- 负例控制：判据自己也要被证明会红 ----------


def self_test():
    ok = True

    def run(providers, i18n):
        return check_text(providers, i18n)

    good_i18n = "const I18N_EN = {\n  '按量 Token（OpenAI 兼容）': 'Pay-as-you-go (OpenAI-compatible)',\n};\n"

    # 坏例 1：词条缺失。
    bad = 'p := ProviderPreset{ID: "x", Name: "未收录 厂商"}'
    ps = run(bad, good_i18n)
    ok &= expect(bool(ps), "词条缺失会被判红", ps)

    # 坏例 2：显示名传变量。
    bad_var = "Name: providerName,"
    ps = run(bad_var, good_i18n)
    ok &= expect(bool(ps), "传变量会被判红", ps)

    # 坏例 3：字典解析不出。
    ps = run('Name: "某厂商"', "const I18N_EN = broken")
    ok &= expect(bool(ps), "字典解析不出会被判红", ps)

    # 干扰 1：CJK 只在注释里——剥注释后不该要求词条。
    decoy = '// 这句中文在注释里，解释收录规则\nName: "PlainName"'
    ps = run(decoy, good_i18n)
    ok &= expect(not ps, "注释里的中文不误报", ps)

    # 干扰 2：不含 CJK 的显示名本来就不归这道闸管。
    ps = run('Label: "Coding Plan (OpenAI-compatible)"', good_i18n)
    ok &= expect(not ps, "非 CJK 显示名不误报", ps)

    print("self-test: " + ("ok" if ok else "FAILED"))
    return ok


def check_text(providers_text, i18n_text):
    """self-test 用的内存版 check：不碰仓库文件。"""
    src = _gocomment.strip(providers_text)
    literals, non_literals = catalog_strings(src)
    problems = []
    for _, field, expr in non_literals:
        problems.append("%s 不是字符串字面量：%s" % (field, expr))
    keys = i18n_keys(i18n_text)
    if not keys:
        return problems + ["I18N_EN 解析不出"]
    for lit in literals:
        if has_cjk(lit) and lit not in keys:
            problems.append('缺词条："%s"' % lit)
    return problems


def expect(cond, what, problems):
    if cond:
        print("  ok  " + what)
        return True
    print("  FAIL  " + what + " -> " + repr(problems))
    return False


def main():
    argv = [a for a in sys.argv[1:]]
    do_self = "--self-test" in argv
    args = [a for a in argv if not a.startswith("--")]
    root = args[0] if args else "."
    if do_self:
        if not self_test():
            return 1
    problems = check(root)
    if problems:
        for p in problems:
            print(p)
        print(
            "i18n 覆盖判据：红。见 scripts/check-i18n-catalog.py 头部的「怎么改才通过」。"
        )
        return 1
    if not do_self:
        print("i18n 覆盖判据：绿——厂商目录的每一句显示文案，字典里都有英文。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
