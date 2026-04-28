"""Parse bash and zsh history files into chronological command strings."""

from __future__ import annotations

import os
import re
import subprocess
import time
from pathlib import Path
from typing import List, Optional, Sequence, Tuple

from ihg.filters import build_processed

_ZSH_LINE = re.compile(r"^: (\d+):(\d+);(.*)$")


def histfile_path() -> Path | None:
    p = os.environ.get("IHG_HISTFILE") or os.environ.get("HISTFILE")
    if not p:
        return None
    path = Path(p).expanduser()
    if path.is_file():
        return path
    return None


def _shell_path_for_sync() -> str:
    return os.environ.get("IHG_SHELL") or os.environ.get("SHELL") or "/bin/bash"


def _shell_invokes_bash_history(shell: str) -> bool:
    """True if ``shell`` is bash or ``sh`` resolving to bash (e.g. macOS /bin/sh)."""
    base = os.path.basename(shell).lower()
    if base == "bash":
        return True
    if base != "sh":
        return False
    try:
        return os.path.basename(os.path.realpath(shell)).lower() == "bash"
    except OSError:
        return False


def sync_shell_history_to_disk_best_effort(histfile: Optional[Path] = None) -> None:
    """
    Ask ``$SHELL`` (or ``IHG_SHELL``) to flush in-memory history to the history
    file before ihg reads it.

    **zsh:** runs ``zsh -f -c 'fc -AI'`` (``fc`` **-A** / **-I** per zsh: append
    and reconcile with ``HISTFILE``). Uses ``-f`` so startup stays fast;
    ``HISTFILE`` should be **exported** in the environment (not only set
    inside ``.zshrc``).

    **bash** (including ``sh`` when it resolves to bash): runs
    ``bash --noprofile --norc -c 'history -a'``, which appends the subshell’s
    session lines to ``HISTFILE`` (same builtin as in an interactive bash).

    If ``histfile`` is passed (ihg’s resolved path), it is set as ``HISTFILE`` in
    the subprocess environment so **bash** / **zsh** target the same file even
    when only ``IHG_HISTFILE`` was set in the parent.

    This cannot flush another process's history unless that shell has already
    written to the file or ``HISTFILE`` is shared; it is a best-effort nudge.

    Set ``IHG_NO_STARTUP_HISTORY_SYNC`` to skip.
    """
    if os.environ.get("IHG_NO_STARTUP_HISTORY_SYNC", "").strip() in (
        "1",
        "true",
        "yes",
    ):
        return
    shell = _shell_path_for_sync().strip()
    if not shell:
        return
    base = os.path.basename(shell).lower()
    if base == "zsh":
        argv = [shell, "-f", "-c", "fc -AI"]
    elif _shell_invokes_bash_history(shell):
        bash_exe = os.path.realpath(shell) if base == "sh" else shell
        argv = [bash_exe, "--noprofile", "--norc", "-c", "history -a"]
    elif base == "fish":
        argv = [shell, "-c", "history save"]
    else:
        return
    env: Optional[dict[str, str]] = None
    if histfile is not None:
        env = {**os.environ, "HISTFILE": str(histfile.resolve())}
    try:
        subprocess.run(
            argv,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=5,
            check=False,
            env=env,
        )
    except (OSError, subprocess.TimeoutExpired):
        pass


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


def histfile_is_zsh(path: Path) -> bool:
    return _detect_zsh(read_histfile_text(path))


def _zsh_cmd_from_match(m: re.Match[str]) -> str:
    cmd = m.group(3).replace("\\n", "\n").strip()
    return cmd


def append_histfile_line(path: Path, command: str, *, is_zsh: bool) -> None:
    """Append one command as a new history entry (does not modify existing lines)."""
    cmd = command.replace("\r\n", "\n").rstrip("\n")
    if not cmd.strip():
        return
    enc = "utf-8"
    if is_zsh:
        esc = cmd.replace("\\", "\\\\").replace("\n", "\\n")
        line = f": {int(time.time())}:0;{esc}\n"
        with path.open("a", encoding=enc) as f:
            f.write(line)
        return
    blob = path.read_bytes() if path.is_file() else b""
    with path.open("a", encoding=enc) as f:
        if blob and not blob.endswith(b"\n"):
            f.write("\n")
        f.write(cmd)
        if not cmd.endswith("\n"):
            f.write("\n")


def _parse_bash_blocks(text: str) -> List[Tuple[str, str]]:
    """
    Bash history as (canonical_cmd, block_text). ``canonical_cmd`` matches
    ``parse_bash_history`` output; ``block_text`` is the file region to remove
    when deleting that entry (includes ``#timestamp`` line when present).
    """
    lines = text.splitlines()
    n = len(lines)
    i = 0
    pending: str | None = None
    block_lines: List[str] = []
    blocks: List[Tuple[str, str]] = []

    def flush() -> None:
        nonlocal pending, block_lines
        if pending is not None:
            c = pending.strip()
            if c:
                raw = "\n".join(block_lines)
                blocks.append((c, raw + ("\n" if not raw.endswith("\n") else "")))
        pending = None
        block_lines = []

    while i < n:
        line = lines[i]
        stripped = line.strip()
        if not stripped:
            i += 1
            continue

        if stripped.startswith("#") and stripped[1:].isdigit():
            flush()
            block_lines = [line]
            i += 1
            if i < n:
                cmd_line = lines[i]
                block_lines.append(cmd_line)
                if cmd_line.startswith(" ") and pending is not None:
                    pending = (pending or "") + "\n" + cmd_line
                else:
                    pending = cmd_line.rstrip("\n")
                i += 1
            continue

        if line.startswith(" ") and pending is not None:
            block_lines.append(line)
            pending += "\n" + line.rstrip("\n")
            i += 1
            continue

        flush()
        block_lines = [line]
        pending = line.rstrip("\n")
        i += 1

    flush()
    return blocks


def delete_matching_commands(path: Path, command: str, *, is_zsh: bool) -> int:
    """Remove all file entries whose parsed command equals ``command`` (exact string)."""
    want = command.strip()
    if not want:
        return 0
    text = read_histfile_text(path)
    if is_zsh:
        kept: List[str] = []
        removed = 0
        for raw in text.splitlines(keepends=True):
            m = _ZSH_LINE.match(raw.rstrip("\n"))
            if m and _zsh_cmd_from_match(m) == want:
                removed += 1
                continue
            kept.append(raw)
        if removed:
            path.write_text("".join(kept), encoding="utf-8")
        return removed
    blocks = _parse_bash_blocks(text)
    kept_blk = [blk for c, blk in blocks if c.strip() != want]
    removed = len(blocks) - len(kept_blk)
    if removed:
        path.write_text("".join(kept_blk), encoding="utf-8")
    return removed


def write_clean_histfile_backup(
    commands_newest_first: Sequence[str],
    dest: Path,
    *,
    is_zsh: bool,
) -> None:
    """
    Write deduplicated, non-trivial commands (same rules as the TUI) to ``dest``.
    Order is oldest-first for readability.
    """
    cleaned, _ = build_processed(commands_newest_first)
    oldest_first = list(reversed(cleaned))
    dest.parent.mkdir(parents=True, exist_ok=True)
    if is_zsh:
        ts = int(time.time())
        lines = []
        for j, cmd in enumerate(oldest_first):
            esc = cmd.replace("\\", "\\\\").replace("\n", "\\n")
            lines.append(f": {ts + j}:0;{esc}\n")
        dest.write_text("".join(lines), encoding="utf-8")
        return
    body = "\n".join(oldest_first)
    if body and not body.endswith("\n"):
        body += "\n"
    dest.write_text(body, encoding="utf-8")
