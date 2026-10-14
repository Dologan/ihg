"""Load static alias/abbr definitions from rc files and expand alias uses in commands.

Interactive shells were avoided for execution (lag, motd). Most ``alias name=…``
lines live in dotfiles; we parse those without running a shell. Dynamic aliases
and ``alias`` defined only after conditionals are not covered.

Expansion applies to each ``&&`` / ``||`` / ``|`` / ``;`` / newline-separated
segment at top level (outside quotes and parentheses), after optional environment
assignments and a leading ``sudo`` (with common flags).
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


_ENV_ASSIGN_RE = re.compile(
    r"^([a-zA-Z_][a-zA-Z0-9_]*=(?:\"[^\"]*\"|'[^']*'|\S+)\s+)+"
)

_WRAPPER_RE = re.compile(
    r"^(command|builtin|noglob|nocorrect)\s+",
    re.I,
)


def _strip_sudo_prefix(s: str) -> tuple[str, str]:
    """Split ``sudo … `` (with a few common flags) from the start of *s*."""
    if len(s) < 4 or s[:4].lower() != "sudo":
        return "", s
    n = len(s)
    i = 4
    while i < n and s[i].isspace():
        i += 1
    while i < n and s[i] == "-":
        j = i + 1
        while j < n and not s[j].isspace():
            j += 1
        flag = s[i:j]
        i = j
        while i < n and s[i].isspace():
            i += 1
        if flag in ("-E", "-H", "-i", "-P", "-n", "-s"):
            continue
        if len(flag) > 2 and flag[1] in "uUgG" and flag[2] != "-":
            continue
        if flag in ("-u", "-g", "-U", "-D", "-p"):
            while i < n and not s[i].isspace():
                i += 1
            while i < n and s[i].isspace():
                i += 1
            continue
        if flag.startswith("--preserve-env") or flag.startswith("--login"):
            continue
        if flag.startswith("--user") or flag.startswith("--group"):
            if "=" in flag:
                continue
            while i < n and not s[i].isspace():
                i += 1
            while i < n and s[i].isspace():
                i += 1
            continue
        if flag.startswith("--chdir") and "=" not in flag:
            while i < n and not s[i].isspace():
                i += 1
            while i < n and s[i].isspace():
                i += 1
            continue
        break
    prefix = s[:i]
    rest = s[i:].lstrip()
    return prefix, rest


def _strip_env_and_wrappers(s: str) -> tuple[str, str]:
    """Leading ``VAR=value`` assignments, ``sudo``, ``command``, etc."""
    prefix = ""
    rest = s
    while True:
        m = _ENV_ASSIGN_RE.match(rest)
        if m:
            prefix += m.group(0)
            rest = rest[m.end() :]
            continue
        sp, sr = _strip_sudo_prefix(rest)
        if sp:
            prefix += sp
            rest = sr
            continue
        m2 = _WRAPPER_RE.match(rest)
        if m2:
            prefix += m2.group(0)
            rest = rest[m2.end() :]
            continue
        break
    return prefix, rest


def _split_top_level_segments(text: str) -> list[str]:
    """Split into [seg0, sep0, seg1, sep1, …]; *sep* is &&, ||, |, ;, or newline."""
    chunks: list[str] = []
    n = len(text)
    start = 0
    i = 0
    depth = 0
    in_sq = False
    in_dq = False
    esc = False
    while i < n:
        c = text[i]
        if esc:
            esc = False
            i += 1
            continue
        if in_sq:
            if c == "'":
                in_sq = False
            i += 1
            continue
        if in_dq:
            if c == "\\":
                esc = True
            elif c == '"':
                in_dq = False
            i += 1
            continue
        if c == "'":
            in_sq = True
            i += 1
            continue
        if c == '"':
            in_dq = True
            i += 1
            continue
        if c == "(":
            depth += 1
            i += 1
            continue
        if c == ")":
            if depth > 0:
                depth -= 1
            i += 1
            continue
        if depth == 0:
            two = text[i : i + 2]
            if two == "||" or two == "&&":
                chunks.append(text[start:i])
                chunks.append(two)
                i += 2
                start = i
                continue
            if c in ";|\n":
                chunks.append(text[start:i])
                chunks.append(c)
                i += 1
                start = i
                continue
        i += 1
    chunks.append(text[start:])
    return chunks


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


def _split_aliasable_head(cmd: str, aliases: Dict[str, str]) -> tuple[str, str]:
    """
    First token split, plus ``ll~/path`` when ``ll`` is an alias (no space before ``~``).
    """
    first, rest = _split_first_token(cmd)
    if first in aliases:
        return first, rest
    m = re.match(r"^([a-zA-Z_][a-zA-Z0-9_]*)(~/.+)$", first)
    if m and m.group(1) in aliases:
        tail = m.group(2)
        if rest:
            tail = f"{tail} {rest}"
        return m.group(1), tail
    return first, rest


def _expand_leading_alias_chain(s: str, aliases: Dict[str, str], max_depth: int) -> str:
    """Expand the first word of *s* while it matches *aliases* (recursive)."""
    depth = 0
    while depth < max_depth and s:
        first, rest = _split_aliasable_head(s, aliases)
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


def _expand_one_segment(segment: str, aliases: Dict[str, str], max_depth: int) -> str:
    """Expand aliases for one simple-command span (may include args)."""
    if not segment.strip():
        return segment
    stripped_right = segment.rstrip(" \t")
    trailing = segment[len(stripped_right) :]
    stripped_left = stripped_right.lstrip(" \t")
    leading = stripped_right[: len(stripped_right) - len(stripped_left)]
    if not stripped_left:
        return segment
    pre, rest = _strip_env_and_wrappers(stripped_left)
    expanded = _expand_leading_alias_chain(rest, aliases, max_depth)
    return leading + pre + expanded + trailing


def expand_command_aliases(cmd: str, aliases: Dict[str, str], max_depth: int = 24) -> str:
    """Expand alias uses with arguments, pipelines, and common prefixes."""
    if not aliases or not cmd:
        return cmd
    parts = _split_top_level_segments(cmd)
    out: list[str] = []
    for idx, piece in enumerate(parts):
        if idx % 2 == 0:
            out.append(_expand_one_segment(piece, aliases, max_depth))
        else:
            out.append(piece)
    return "".join(out)
