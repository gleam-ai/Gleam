#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Windows-specific helpers for subprocess and stdout encoding."""

import os
import sys
import tempfile


def reconfigure_stdout_utf8():
    """Reconfigure stdout to UTF-8 to avoid cp936 issues in Git Bash on Windows."""
    if sys.platform == "win32":
        try:
            import io

            codecs = __import__("codecs")
            stream = codecs.getincrementalencoder("utf-8")(errors="replace")
            # Only reconfigure if needed; this is defensive
            if hasattr(sys.stdout, "reconfigure"):
                sys.stdout.reconfigure(encoding="utf-8", errors="replace")
        except Exception:
            # Best-effort; fallback is safe because encode will handle it
            pass


def get_safe_temp_dir():
    """Return a reliable temp dir for Windows (no %TEMP% expansion issues)."""
    return tempfile.gettempdir()


def escape_path_for_shell(path):
    """Escape a path with spaces for PowerShell-like shell. Double-quote only when needed."""
    if " " in path:
        return f'"{path}"'
    return path


def build_command_parts(base_cmd, args):
    """Build a command list suitable for subprocess.Popen on Windows (no shell=True)."""
    parts = [base_cmd] + list(args)
    return parts


if __name__ == "__main__":
    print("windows_helper module loaded", file=sys.stderr)
    print(f"temp_dir={get_safe_temp_dir()}", file=sys.stderr)
