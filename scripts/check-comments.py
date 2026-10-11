#!/usr/bin/env python3
"""Enforce the repository comment policy on source files.

Policy (owner: CONTRIBUTING.md -> "Comments"):
  * Comments are written in English, including newly added ones.
  * Comments are sparse. Only the non-obvious decisions survive; anything the
    code already says gets deleted.
  * No AI-assistant residue: emojis, banner dividers, numbered walkthroughs and
    the usual filler phrases.

Only *comment* tokens are inspected. Chinese UI strings and Go error messages
are data, not comments, and must not be flagged.

The gate compares against scripts/comment-policy.json. The budget there may only
move down, so the policy is enforced by a command rather than by review mood:

    python scripts/check-comments.py
    python scripts/check-comments.py --report
    python scripts/check-comments.py --update
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

import _utf8  # noqa: F401  # emit UTF-8 to stdout: Windows defaults to cp936

ROOT = Path(__file__).resolve().parent.parent
POLICY = Path(__file__).resolve().parent / "comment-policy.json"

SCAN_DIRS = (
    "cmd",
    "internal",
    "pkg",
    "scripts",
    "desktop/src",
    "desktop/scripts",
)
SKIP_DIRS = {
    ".git",
    "node_modules",
    "dist",
    ".codebuddy",
    ".cache",
    ".runtimes",
    ".sidecar",
    "spike",
    "__pycache__",
}
EXCLUDED_FILES: frozenset[str] = frozenset()

EMOJI = re.compile(
    "["
    "\U0001f000-\U0001faff"
    "\U00002190-\U000021ff"
    "\U000025a0-\U000027bf"
    "\U0000fe0f"
    "]"
)
BANNER = re.compile(r"^\s*(//|#)\s*[-=*_~]{4,}[-=*_~\s]*$")
DIVIDER_LINE = re.compile(r"^\s*(//|#)\s*-{3,}\s+\S.*\s+-{3,}\s*$")
NUMBERED = re.compile(r"^\s*(//|#)\s*\d+[.)]\s")
FILLER = re.compile(
    r"\b(note that|keep in mind|it is important to|this is where the magic|"
    r"rest assured|under the hood|delve into|seamlessly|leverages?|robust(?:ly)?|"
    r"boilerplate|for the sake of|as you can see|simply put)\b",
    re.IGNORECASE,
)
NON_ASCII = re.compile(r"[^\x00-\x7f]")

REGEX_PRECEDERS = set("(,=:[!&|?{};+-*%~^<>")


class Scanner:
    """Extracts comments per language, skipping literals.

    Records both the 1-based line and the character span of every comment, so a
    consumer can report it (policy gate) or excise it (pruner) from one pass.
    """

    def __init__(self, lang: str, text: str) -> None:
        self.lang = lang
        self.text = text
        self.i = 0
        self.n = len(text)
        self.comments: list[tuple[int, str]] = []
        self.spans_out: list[tuple[int, int]] = []

    def line_of(self, pos: int) -> int:
        return self.text.count("\n", 0, pos) + 1

    def run(self) -> list[tuple[int, str]]:
        while self.i < self.n:
            c = self.text[self.i]
            if c == "\n":
                self.i += 1
            elif c.isspace():
                self.i += 1
            elif self.starts_comment():
                pass
            elif self.starts_regex():
                pass
            elif self.lang == "py" and self.text.startswith(
                ('"""', "'''"), self.i
            ):
                self.i = self.skip_triple(self.text[self.i : self.i + 3])
            elif c in "\"'`":
                self.skip_string()
            elif c == "/" and self.peek(1) == "*":
                self.skip_block_comment()
            else:
                self.i += 1
        return self.comments

    def spans(self) -> list[tuple[int, int]]:
        if not self.comments:
            self.run()
        return self.spans_out

    def peek(self, offset: int) -> str:
        j = self.i + offset
        return self.text[j] if j < self.n else ""

    def prev(self) -> str:
        j = self.i - 1
        while j >= 0 and self.text[j].isspace():
            j -= 1
        return self.text[j] if j >= 0 else ""

    def starts_regex(self) -> bool:
        if self.lang not in ("js", "ts") or self.text[self.i] != "/":
            return False
        if self.peek(1) in ("/", "*"):
            return False
        if self.prev() not in REGEX_PRECEDERS and self.prev() != "":
            return False
        j = self.i + 1
        in_class = False
        while j < self.n and self.text[j] not in "\n":
            ch = self.text[j]
            if ch == "\\":
                j += 2
                continue
            if ch == "[":
                in_class = True
            elif ch == "]":
                in_class = False
            elif ch == "/" and not in_class:
                self.i = j + 1
                return True
            j += 1
        return False

    def starts_comment(self) -> bool:
        if self.lang == "py":
            if self.text[self.i] == "#":
                end = self.text.find("\n", self.i)
                end = self.n if end < 0 else end
                self.comments.append(
                    (self.line_of(self.i), self.text[self.i : end])
                )
                self.spans_out.append((self.i, end))
                self.i = end
                return True
            return False
        if self.text.startswith("//", self.i):
            end = self.text.find("\n", self.i)
            end = self.n if end < 0 else end
            self.comments.append(
                (self.line_of(self.i), self.text[self.i : end])
            )
            self.spans_out.append((self.i, end))
            self.i = end
            return True
        return False

    def skip_block_comment(self) -> None:
        end = self.text.find("*/", self.i + 2)
        end = self.n if end < 0 else end + 2
        for offset, chunk in enumerate(self.text[self.i : end].split("\n")):
            self.comments.append((self.line_of(self.i) + offset, chunk))
        self.spans_out.append((self.i, end))
        self.i = end

    def skip_string(self) -> None:
        quote = self.text[self.i]
        escapes = quote != "`"
        self.i += 1
        while self.i < self.n:
            c = self.text[self.i]
            if escapes and c == "\\":
                self.i += 2
                continue
            if c == quote:
                self.i += 1
                return
            if quote != "`" and c == "\n":
                return
            self.i += 1

    def skip_triple(self, delim: str) -> int:
        end = self.text.find(delim, self.i + 3)
        return self.n if end < 0 else end + 3


EXT_LANG = {
    ".go": "go",
    ".js": "js",
    ".mjs": "js",
    ".cjs": "js",
    ".ts": "ts",
    ".py": "py",
}


def iter_source_files() -> list[tuple[Path, str]]:
    out: list[tuple[Path, str]] = []
    for rel in SCAN_DIRS:
        base = ROOT / rel
        if not base.exists():
            continue
        for path in base.rglob("*"):
            if not path.is_file():
                continue
            if any(part in SKIP_DIRS for part in path.parts):
                continue
            ext = path.suffix.lower()
            lang = EXT_LANG.get(ext)
            if lang is None:
                continue
            out.append((path, lang))
    return sorted(out)


def violations_for(path: Path, lang: str, max_ratio: float) -> list[str]:
    text = path.read_text(encoding="utf-8", errors="replace")
    comments = Scanner(lang, text).run()
    rel = path.relative_to(ROOT).as_posix()
    if rel in EXCLUDED_FILES:
        return []
    problems: list[str] = []
    for lineno, raw in comments:
        body = raw.strip()
        if NON_ASCII.search(body):
            problems.append(f"{rel}:{lineno} non-ascii")
        if EMOJI.search(body):
            problems.append(f"{rel}:{lineno} emoji")
        if BANNER.match(body) or DIVIDER_LINE.match(body):
            problems.append(f"{rel}:{lineno} banner")
        if NUMBERED.match(body):
            problems.append(f"{rel}:{lineno} numbered-list")
        if FILLER.search(body):
            problems.append(f"{rel}:{lineno} filler-phrase")

    lines = text.splitlines()
    total = max(len(lines), 1)
    comment_lines = len({lineno for lineno, _ in comments})
    ratio = comment_lines / total
    if lines and ratio > max_ratio:
        problems.append(f"{rel}:1 dense {ratio:.0%}>{max_ratio:.0%}")
    return problems


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument(
        "--report", action="store_true", help="print every violation"
    )
    ap.add_argument("--update", action="store_true", help="rewrite the budget")
    args = ap.parse_args()

    policy = json.loads(POLICY.read_text(encoding="utf-8"))
    max_ratio = float(policy.get("comment_ratio_max", 0.3))

    problems: list[str] = []
    for path, lang in iter_source_files():
        problems.extend(violations_for(path, lang, max_ratio))
    problems.sort()

    budget = int(policy.get("max_violations", 0))
    if args.update:
        POLICY.write_text(
            json.dumps(
                {
                    "max_violations": len(problems),
                    "comment_ratio_max": max_ratio,
                    "note": policy.get("note", ""),
                },
                indent=2,
            )
            + "\n",
            encoding="utf-8",
        )
        print(f"[check-comments] budget set to {len(problems)}")
        return 0

    if args.report:
        for p in problems:
            print(p)
        print(f"[check-comments] {len(problems)} violation(s), budget {budget}")
        return 0

    if len(problems) > budget:
        print(
            f"[check-comments] FAIL: {len(problems)} violation(s) > budget {budget}"
        )
        for p in problems[:40]:
            print("  " + p)
        return 1
    print(f"[check-comments] OK: {len(problems)} violation(s), budget {budget}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
