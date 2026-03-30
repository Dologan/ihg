"""Parse bash and zsh history files into chronological command strings."""

from __future__ import annotations

import os
import re
from pathlib import Path
from typing import List

_ZSH_LINE = re.compile(r"^: (\d+):(\d+);(.*)$")


def histfile_path() -> Path | None:
    p = os.environ.get("IHG_HISTFILE") or os.environ.get("HISTFILE")
    if not p:
        return None
    path = Path(p).expanduser()
    if path.is_file():
        return path
    return None


def read_histfile_text(path: Path) -> str:
    data = path.read_bytes()
    for enc in ("utf-8", "utf-8-sig", "latin-1"):
        try:
            return data.decode(enc)
        except UnicodeDecodeError:
            continue
    return data.decode("utf-8", errors="replace")


def _detect_zsh(text: str) -> bool:
    lines = [ln for ln in text.splitlines() if ln.strip()][:40]
    if not lines:
        return False
    hits = sum(1 for ln in lines if _ZSH_LINE.match(ln))
    return hits >= max(1, len(lines) // 4)


def parse_zsh_history(text: str) -> List[str]:
    """Parse zsh extended history (one entry per line: ': ts:elapsed;cmd')."""
    out: List[str] = []
    for raw in text.splitlines():
        m = _ZSH_LINE.match(raw)
        if not m:
            continue
        cmd = m.group(3)
        cmd = cmd.replace("\\n", "\n")
        cmd = cmd.strip()
        if cmd:
            out.append(cmd)
    return out


def parse_bash_history(text: str) -> List[str]:
    """Parse bash history with optional timestamp pairs and continuations."""
    lines = text.splitlines()
    out: List[str] = []
    i = 0
    pending: str | None = None

    def flush():
        nonlocal pending
        if pending is not None:
            c = pending.strip()
            if c:
                out.append(c)
            pending = None

    while i < len(lines):
        line = lines[i]
        stripped = line.strip()
        if not stripped:
            i += 1
            continue

        if stripped.startswith("#") and stripped[1:].isdigit():
            flush()
            i += 1
            if i < len(lines):
                cmd_line = lines[i]
                if cmd_line.startswith(" ") and pending is not None:
                    pending = (pending or "") + "\n" + cmd_line
                else:
                    pending = cmd_line.rstrip("\n")
                i += 1
            continue

        if line.startswith(" ") and pending is not None:
            pending += "\n" + line.rstrip("\n")
            i += 1
            continue

        flush()
        pending = line.rstrip("\n")
        i += 1

    flush()
    return out


def parse_history_file(text: str) -> List[str]:
    if _detect_zsh(text):
        return parse_zsh_history(text)
    return parse_bash_history(text)


def load_commands_chronological(path: Path) -> List[str]:
    """Oldest first."""
    text = read_histfile_text(path)
    return parse_history_file(text)


def load_commands_newest_first(path: Path) -> List[str]:
    cmds = load_commands_chronological(path)
    cmds.reverse()
    return cmds
