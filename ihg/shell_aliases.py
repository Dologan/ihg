"""Load static alias/abbr definitions from rc files and expand the first command word.

Interactive shells were avoided for execution (lag, motd). Most ``alias name=…``
lines live in dotfiles; we parse those without running a shell. Dynamic aliases
and ``alias`` defined only after conditionals are not covered.
"""

from __future__ import annotations

import os
import re
from pathlib import Path
from typing import Dict, List


def _strip_trailing_comment(line: str) -> str:
    in_sq = in_dq = False
    i = 0
    while i < len(line):
        c = line[i]
        if not in_dq and c == "'" and not in_sq:
            in_sq = not in_sq
        elif not in_sq and c == '"' and not in_dq:
            in_dq = not in_dq
        elif not in_sq and not in_dq and c == "#":
            return line[:i].rstrip()
        i += 1
    return line.rstrip()


def _parse_bash_zsh_alias_lines(text: str, into: Dict[str, str]) -> None:
    """Merge ``alias name=value`` from a chunk of shell source (later wins)."""
    for raw_line in text.splitlines():
        line = _strip_trailing_comment(raw_line)
        stripped = line.strip()
        m_alias = re.match(r"^alias\s+", stripped)
        if not m_alias:
            continue
        rest = stripped[m_alias.end() :].lstrip()
        if re.match(r"-g\s+", rest):
            continue
        m_eq = re.match(r"^([a-zA-Z_][a-zA-Z0-9_]*)\s*=(.*)$", rest, re.DOTALL)
        if not m_eq:
            continue
        name, value = m_eq.group(1), m_eq.group(2).strip()
        if not name:
            continue
        if (value.startswith("'") and value.endswith("'")) or (
            value.startswith('"') and value.endswith('"')
        ):
            value = value[1:-1]
        into[name] = value


def _parse_fish_abbr_lines(text: str, into: Dict[str, str]) -> None:
    for raw_line in text.splitlines():
        line = _strip_trailing_comment(raw_line)
        stripped = line.strip()
        if not stripped.startswith("abbr "):
            continue
        toks = stripped.split()
        if len(toks) < 3 or toks[0] != "abbr":
            continue
        if toks[1] in ("-s", "--show", "-e", "--erase", "-l", "--list"):
            continue
        i = 1
        while i < len(toks) and toks[i].startswith("-"):
            i += 1
        if i + 1 >= len(toks):
            continue
        name = toks[i]
        value = " ".join(toks[i + 1 :])
        into[name] = value.strip("'\"")


def _rc_paths_for_shell(shell: str) -> List[Path]:
    home = Path.home()
    base = os.path.basename(shell).lower()
    if base == "fish":
        return [home / ".config/fish/config.fish"]
    if base == "zsh":
        return [home / ".zprofile", home / ".zshrc"]
    return [home / ".bash_aliases", home / ".bashrc", home / ".profile"]


def load_aliases_for_shell(shell: str) -> Dict[str, str]:
    """Static aliases from rc files; missing files are skipped."""
    merged: Dict[str, str] = {}
    for path in _rc_paths_for_shell(shell):
        try:
            if not path.is_file():
                continue
            text = path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        base = os.path.basename(shell).lower()
        if base == "fish":
            _parse_fish_abbr_lines(text, merged)
        else:
            _parse_bash_zsh_alias_lines(text, merged)
    return merged


def _split_first_token(cmd: str) -> tuple[str, str]:
    """First shell word (respecting '…' or \"…\") and the remainder."""
    s = cmd.lstrip()
    if not s:
        return "", ""
    if s[0] == "'":
        j = s.find("'", 1)
        if j == -1:
            return s, ""
        return s[: j + 1], s[j + 1 :].lstrip()
    if s[0] == '"':
        i = 1
        while i < len(s):
            if s[i] == "\\":
                i += 2
                continue
            if s[i] == '"':
                return s[: i + 1], s[i + 1 :].lstrip()
            i += 1
        return s, ""
    end = 0
    while end < len(s):
        c = s[end]
        if c.isspace() or c in "|&;<>()":
            break
        end += 1
    return s[:end], s[end:].lstrip()


def expand_command_aliases(cmd: str, aliases: Dict[str, str], max_depth: int = 24) -> str:
    """Expand leading alias recursively (first token only)."""
    s = cmd.strip()
    depth = 0
    while depth < max_depth and s:
        first, rest = _split_first_token(s)
        if not first or first[0] in "'\"":
            break
        if first not in aliases:
            break
        repl = aliases[first].strip()
        if rest:
            sep = "" if rest[0] in "|&;<>)" else " "
            s = f"{repl}{sep}{rest}".strip()
        else:
            s = repl
        depth += 1
    return s
