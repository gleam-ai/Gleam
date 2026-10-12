#!/usr/bin/env python3
# coding: utf-8
"""静态 CSS 必须是浏览器能解析的结构：注释配平 + 花括号配平。

**为什么要有**：CSS 解析器对残缺结构的处理是**静默丢弃**，不报错、不中断加载。
真实事故：`03-align.css` 里一条规则的注释开头被吃掉了一行（`/* 会话态…` 没了），
剩下半截说明文字被当成**选择器**解析，那条 `#view-goals.thread-mode :is(...)`
连同它后面的规则一起消失——页面照常渲染，只是「候补目标卡在会话态藏不住了」，
而代码里那条规则明明在、grep 也查得到。括号总数配平也拦不住：丢的是注释不是括号。

这类问题**只能机械拦**：CSS 没有语法错误可循，浏览器不报错，review 时肉眼最难发现
（文件 2000 行，谁会注意到少了一行 `/*`）。判据是纯文本的，所以配一条 exit non-zero
的命令（AGENTS.md 铁律 3）。

**判据两条**：
1. `/*` 与 `*/` 数量相等（注释成对）；
2. 花括号 `{` 与 `}` 数量相等（块成对）。
两条都只做计数，不试图解析 CSS——它们要拦的是"结构残缺"，不是"写得对不对"。

**自我失效的防线 + 负例自带**：`--self-test` 现造三份坏文本（注释少一半 / 括号多一个 /
正常文件），断言前两份被判红、第三份放行。判据不跑负例就不知道自己是不是在永远点头。

用法：python scripts/check-css-structure.py [--self-test] [仓库根目录]
退出码：0 = 全部 CSS 结构完整（`--self-test` 时：三份样例结论正确）；1 = 否则。
"""

import io
import os
import sys

import _utf8  # noqa: F401  # emit UTF-8 to stdout: Windows defaults to cp936

STATIC_DIR = os.path.join("internal", "webui", "static")


def css_files(root):
    d = os.path.join(root, STATIC_DIR)
    if not os.path.isdir(d):
        return None
    return sorted(f for f in os.listdir(d) if f.endswith(".css"))


def problems(text):
    """返回这一份 CSS 的结构问题列表（空 = 完好）。"""
    out = []
    opens = text.count("/*")
    closes = text.count("*/")
    if opens != closes:
        out.append("注释不成对：/* %d 个 vs */ %d 个" % (opens, closes))
    lb = text.count("{")
    rb = text.count("}")
    if lb != rb:
        out.append("花括号不成对：{ %d 个 vs } %d 个" % (lb, rb))
    return out


def check(root):
    files = css_files(root)
    if files is None:
        print("[css] 找不到静态目录：%s" % os.path.join(root, STATIC_DIR))
        return 1
    bad = 0
    for name in files:
        with io.open(
            os.path.join(root, STATIC_DIR, name), encoding="utf-8"
        ) as f:
            text = f.read()
        issues = problems(text)
        if issues:
            bad += 1
            for msg in issues:
                print("[css] %s: %s" % (name, msg))
    if bad:
        print(
            "[css] %d/%d 份 CSS 结构残缺——浏览器会**静默丢弃**残缺处之后的规则，"
            "页面照常渲染但那些样式凭空消失。查注释开头与括号。"
            % (bad, len(files))
        )
        return 1
    print("[css] %d 份 CSS 结构完整（注释与花括号均成对）" % len(files))
    return 0


def self_test():
    good = "/* 注释 */\n.a {\n  color: red;\n}\n"
    no_comment_end = "/* 注释开着头\n.a {\n  color: red;\n}\n"
    extra_brace = ".a {\n  color: red;\n}\n}\n"
    cases = [
        ("正常文件应放行", good, True),
        ("注释不成对应判红", no_comment_end, False),
        ("花括号多一个应判红", extra_brace, False),
    ]
    rc = 0
    for name, text, should_pass in cases:
        passed = not problems(text)
        if passed != should_pass:
            print(
                "[css 负例] %s：结论不对（%s），判据失效"
                % (name, "放行" if passed else "判红")
            )
            rc = 1
        else:
            print("[css 负例] 拦住/放行正确：%s" % name)
    return rc


def main(argv):
    args = [a for a in argv[1:] if not a.startswith("-")]
    root = args[0] if args else os.getcwd()
    if "--self-test" in argv:
        rc = self_test()
        if rc:
            return rc
        print("")
    return check(root)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
