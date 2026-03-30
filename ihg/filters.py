"""Deduplicate history and drop trivial single-token commands."""

from __future__ import annotations

import os
import shlex
from typing import Iterable, List, Sequence, Tuple

_DEFAULT_IGNORE = frozenset(
    {
        "ls",
        "ll",
        "la",
        "l",
        "pwd",
        "cd",
        "clear",
        "cls",
        "exit",
        "logout",
        "fg",
        "bg",
        "jobs",
        "history",
        "fc",
        "dirs",
        "pushd",
        "popd",
        "hash",
        "type",
        "which",
        "whoami",
        "date",
        "uptime",
        "echo",
    }
)


def ignore_words() -> frozenset[str]:
    extra = os.environ.get("IHG_IGNORE", "")
    if not extra.strip():
        return _DEFAULT_IGNORE
    parts = {p.strip().lower() for p in extra.split(",") if p.strip()}
    return _DEFAULT_IGNORE | parts


def is_trivial_single_token(cmd: str, ignore: frozenset[str]) -> bool:
    s = cmd.strip()
    if not s or "\n" in s:
        return False
    if "|" in s or ";" in s or "&&" in s or "||" in s:
        return False
    if ">" in s or "<" in s:
        return False
    try:
        parts = shlex.split(s, posix=True)
    except ValueError:
        return False
    if len(parts) != 1:
        return False
    word = parts[0]
    base = word.split("/")[-1].lower()
    return base in ignore


def dedupe_newest_first(commands_newest_first: Sequence[str]) -> List[str]:
    seen: set[str] = set()
    out: List[str] = []
    for c in commands_newest_first:
        if c in seen:
            continue
        seen.add(c)
        out.append(c)
    return out


def filter_trivial(commands: Iterable[str], ignore: frozenset[str] | None = None) -> List[str]:
    ig = ignore or ignore_words()
    return [c for c in commands if not is_trivial_single_token(c, ig)]


def build_processed(commands_newest_first: Sequence[str]) -> Tuple[List[str], List[int]]:
    """
    Dedupe (newest wins), drop trivial single-token commands.

    Returns (commands, recency_rank) where recency_rank[i] == i: 0 is newest.
    """
    deduped = dedupe_newest_first(commands_newest_first)
    cleaned = filter_trivial(deduped)
    recency = list(range(len(cleaned)))
    return cleaned, recency


def apply_text_filter(
    commands: Sequence[str],
    recency_indices: Sequence[int],
    needle: str,
) -> Tuple[List[str], List[int]]:
    if not needle:
        return list(commands), list(recency_indices)
    n = needle.casefold()
    fc: List[str] = []
    fr: List[int] = []
    for c, r in zip(commands, recency_indices):
        if n in c.casefold():
            fc.append(c)
            fr.append(r)
    return fc, fr
