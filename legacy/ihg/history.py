"""Parse bash and zsh history files into chronological command strings."""

from __future__ import annotations

import contextlib
import fcntl
import os
import re
import socket
import stat
import subprocess
import tempfile
import time
from pathlib import Path
from typing import Iterator, List, Optional, Sequence, Tuple

from ihg.filters import build_processed
from ihg.shell_env import shell_subprocess_env


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

    **bash:** no subprocess (a child ``bash -c 'history -a'`` cannot flush the
    parent session and may append ``set +/-o`` noise from ``BASH_ENV`` / PTY
    startup into ``HISTFILE``). Use the README ``ihg()`` wrapper ``history -a``
    in your interactive shell instead.

    Set ``IHG_NO_STARTUP_HISTORY_SYNC`` to skip zsh sync too.
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
        return
    elif base == "fish":
        argv = [shell, "-c", "history save"]
    else:
        return
    env: Optional[dict[str, str]] = None
    if histfile is not None:
        env = shell_subprocess_env({"HISTFILE": str(histfile.resolve())})
    else:
        env = shell_subprocess_env()
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


# --- Encoding -----------------------------------------------------------------
#
# History files are bytes, not text. Commands are decoded as UTF-8 with
# ``surrogateescape`` so undecodable bytes survive a parse -> rewrite round trip
# unchanged (no latin-1 fallback that re-encodes the whole file).


def _decode(data: bytes) -> str:
    return data.decode("utf-8", errors="surrogateescape")


def _encode(text: str) -> bytes:
    return text.encode("utf-8", errors="surrogateescape")


def _as_bytes(data: bytes | str) -> bytes:
    return _encode(data) if isinstance(data, str) else data


def read_histfile_bytes(path: Path) -> bytes:
    return path.read_bytes()


def read_histfile_text(path: Path) -> str:
    return _decode(read_histfile_bytes(path))


# --- zsh ------------------------------------------------------------------------
#
# zsh writes each entry "metafied": bytes 0x00 and 0x83-0xa2 are stored as
# 0x83 followed by (byte ^ 0x20). A newline inside a command is stored as a
# backslash-newline, and an entry ending in backslash(-spaces) gets one extra
# trailing space so it is not mistaken for a continuation. The readers below
# mirror zsh's ``readhistline`` / ``savehistfile`` (Src/hist.c).

_ZSH_META = 0x83
_ZSH_IMETA = frozenset({0}) | frozenset(range(0x83, 0xA3))
_ZSH_HEAD = re.compile(rb": (\d+):(\d+);")


def zsh_unmetafy(data: bytes) -> bytes:
    if _ZSH_META not in data:
        return data
    out = bytearray()
    i, n = 0, len(data)
    while i < n:
        b = data[i]
        if b == _ZSH_META and i + 1 < n:
            out.append(data[i + 1] ^ 0x20)
            i += 2
        else:
            out.append(b)
            i += 1
    return bytes(out)


def zsh_metafy(data: bytes) -> bytes:
    out = bytearray()
    for b in data:
        if b in _ZSH_IMETA:
            out.append(_ZSH_META)
            out.append(b ^ 0x20)
        else:
            out.append(b)
    return bytes(out)


def _zsh_entries(data: bytes) -> List[Tuple[Optional[str], bytes]]:
    """
    Split a zsh history file into ``(command, raw_bytes)`` per logical entry.

    Files written with ``EXTENDED_HISTORY`` carry a ``: ts:elapsed;`` header on
    every entry; there, header-less lines get ``command`` None and are kept
    verbatim on rewrite. Without it, every logical line is a command.
    ``raw_bytes`` concatenate back to ``data``.
    """
    extended = _detect_zsh(data)
    lines = data.split(b"\n")
    last_has_nl = data.endswith(b"\n")
    if last_has_nl:
        lines.pop()  # split() leaves b"" after the final newline
    out: List[Tuple[Optional[str], bytes]] = []
    i, n = 0, len(lines)
    while i < n:
        start = i
        buf = lines[i]
        i += 1
        # Backslash-newline continues the entry (only if a newline followed).
        while buf.endswith(b"\\") and i < n:
            buf = buf[:-1] + b"\n" + lines[i]
            i += 1
        # Undo zsh's "trailing backslash + space" guard: drop one space.
        trimmed = buf.rstrip(b" ")
        if len(trimmed) != len(buf) and trimmed.endswith(b"\\"):
            buf = buf[:-1]
        raw = b"\n".join(lines[start:i])
        if i < n or last_has_nl:
            raw += b"\n"
        m = _ZSH_HEAD.match(buf)
        if m:
            cmd: Optional[str] = _decode(zsh_unmetafy(buf[m.end() :])).strip()
        elif not extended:
            cmd = _decode(zsh_unmetafy(buf)).strip()
        else:
            cmd = None
        out.append((cmd or None, raw))
    return out


def _zsh_encode_entry(command: str, timestamp: Optional[int]) -> bytes:
    """One zsh history entry; ``timestamp`` None writes the plain (non-extended) form."""
    body = zsh_metafy(_encode(command)).replace(b"\n", b"\\\n")
    if body.rstrip(b" ").endswith(b"\\"):
        body += b" "
    if timestamp is None:
        return body + b"\n"
    return b": %d:0;%s\n" % (timestamp, body)


def _detect_zsh(text: bytes | str) -> bool:
    """True if the content is zsh ``EXTENDED_HISTORY`` format."""
    lines = [ln for ln in _as_bytes(text).split(b"\n") if ln.strip()][:40]
    if not lines:
        return False
    hits = sum(1 for ln in lines if _ZSH_HEAD.match(ln))
    return hits >= max(1, len(lines) // 4)


def parse_zsh_history(data: bytes | str) -> List[str]:
    """Parse zsh history (plain or ``: ts:elapsed;cmd``; metafied, multi-line)."""
    return [c for c, _ in _zsh_entries(_as_bytes(data)) if c is not None]


# --- bash -----------------------------------------------------------------------


def _parse_bash_blocks(text: str) -> List[Tuple[Optional[str], str]]:
    """
    Bash history as ``(command, block_text)``. ``block_text`` is the file region
    to remove when deleting that entry (includes the ``#timestamp`` line when
    present). Blank lines are kept (``command`` None) so a rewrite only drops
    the deleted entries.
    """
    lines = text.split("\n")
    last_has_nl = text.endswith("\n")
    if last_has_nl:
        lines.pop()
    n = len(lines)
    i = 0
    pending: Optional[str] = None
    block_lines: List[str] = []
    blocks: List[Tuple[Optional[str], str]] = []

    def flush() -> None:
        nonlocal pending, block_lines
        if block_lines:
            c = pending.strip() if pending is not None else ""
            blocks.append((c or None, "".join(ln + "\n" for ln in block_lines)))
        pending = None
        block_lines = []

    while i < n:
        line = lines[i]
        stripped = line.strip()
        if not stripped:
            if pending is not None:
                block_lines.append(line)
            else:
                blocks.append((None, line + "\n"))
            i += 1
            continue

        if stripped.startswith("#") and stripped[1:].isdigit():
            flush()
            block_lines = [line]
            i += 1
            if i < n:
                block_lines.append(lines[i])
                pending = lines[i]
                i += 1
            continue

        if line.startswith(" ") and pending is not None:
            block_lines.append(line)
            pending += "\n" + line
            i += 1
            continue

        flush()
        block_lines = [line]
        pending = line
        i += 1

    flush()
    if blocks and not last_has_nl:
        c, blk = blocks[-1]
        blocks[-1] = (c, blk[:-1])
    return blocks


def parse_bash_history(text: bytes | str) -> List[str]:
    """Parse bash history with optional timestamp pairs and continuations."""
    s = _decode(text) if isinstance(text, bytes) else text
    return [c for c, _ in _parse_bash_blocks(s) if c is not None]


# --- Loading --------------------------------------------------------------------


_BASH_TIMESTAMP_LINE = re.compile(rb"^#\d+\r?$", re.MULTILINE)


def parse_history_file(data: bytes | str, *, is_zsh: Optional[bool] = None) -> List[str]:
    """Parse as zsh or bash; ``is_zsh`` None guesses from content alone."""
    raw = _as_bytes(data)
    if is_zsh is None:
        is_zsh = _detect_zsh(raw)
    if is_zsh:
        return parse_zsh_history(raw)
    return parse_bash_history(_decode(raw))


def load_commands_chronological(path: Path) -> List[str]:
    """Oldest first."""
    data = read_histfile_bytes(path)
    return parse_history_file(data, is_zsh=_histfile_is_zsh(path, data))


def load_commands_newest_first(path: Path) -> List[str]:
    cmds = load_commands_chronological(path)
    cmds.reverse()
    return cmds


def _histfile_is_zsh(path: Path, data: bytes) -> bool:
    """
    zsh ``: ts:elapsed;`` headers and bash ``#1700000000`` timestamp lines are
    unambiguous. Plain files look alike (but zsh still metafies bytes and uses
    backslash-newline continuations), so fall back to the file name, then to
    the shell ihg runs.
    """
    if _detect_zsh(data):
        return True
    if _BASH_TIMESTAMP_LINE.search(data):
        return False
    name = path.resolve().name.lower()
    if "zsh" in name or "zhistory" in name:
        return True
    if "bash" in name:
        return False
    return os.path.basename(_shell_path_for_sync()).lower() == "zsh"


def histfile_is_zsh(path: Path) -> bool:
    return _histfile_is_zsh(path, read_histfile_bytes(path))


# --- Mutation -------------------------------------------------------------------

_ZSH_LOCK_STALE_SECS = 10.0  # zsh treats older lock files as abandoned
_ZSH_LOCK_TIMEOUT_SECS = 2.0


@contextlib.contextmanager
def _zsh_histfile_lock(path: Path) -> Iterator[None]:
    """
    Hold zsh's history lock while touching the file: ``$HISTFILE.LOCK``
    (created exclusively, as zsh's ``lockhistfile``) plus an ``fcntl`` lock on
    the file itself for ``HIST_FCNTL_LOCK`` users.
    """
    target = path.resolve()
    lock = target.with_name(target.name + ".LOCK")
    deadline = time.monotonic() + _ZSH_LOCK_TIMEOUT_SECS
    while True:
        try:
            fd = os.open(str(lock), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o644)
            break
        except FileExistsError:
            try:
                if time.time() - lock.stat().st_mtime > _ZSH_LOCK_STALE_SECS:
                    lock.unlink()
                    continue
            except FileNotFoundError:
                continue
            if time.monotonic() >= deadline:
                raise OSError(f"history file is locked ({lock})") from None
            time.sleep(0.05)
    try:
        os.write(fd, f"{os.getpid()} {socket.gethostname()}\n".encode())
        os.close(fd)
        with target.open("rb+") as hf:
            try:
                fcntl.lockf(hf, fcntl.LOCK_EX)
            except OSError:
                pass  # e.g. filesystems without fcntl locks; the .LOCK file still applies
            yield
    finally:
        try:
            lock.unlink()
        except OSError:
            pass


def _atomic_write_bytes(path: Path, data: bytes) -> None:
    """
    Replace ``path``'s contents via temp file + rename, so a crash or full disk
    never leaves a truncated history. A symlinked history file stays a symlink.
    """
    target = path.resolve()
    mode = stat.S_IMODE(target.stat().st_mode)
    fd, tmp = tempfile.mkstemp(prefix=f".{target.name}.", suffix=".ihg", dir=target.parent)
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
        os.chmod(tmp, mode)
        os.replace(tmp, target)
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise


def append_histfile_line(path: Path, command: str, *, is_zsh: bool) -> None:
    """Append one command as a new history entry (does not modify existing lines)."""
    cmd = command.replace("\r\n", "\n").rstrip("\n")
    if not cmd.strip():
        return
    if is_zsh:
        with _zsh_histfile_lock(path):
            extended = path.stat().st_size == 0 or _detect_zsh(read_histfile_bytes(path))
            with path.open("ab") as f:
                f.write(_zsh_encode_entry(cmd, int(time.time()) if extended else None))
        return
    with path.open("ab+") as f:
        f.seek(0, os.SEEK_END)
        needs_nl = False
        if f.tell() > 0:
            f.seek(-1, os.SEEK_END)
            needs_nl = f.read(1) != b"\n"
        f.write((b"\n" if needs_nl else b"") + _encode(cmd) + b"\n")


def delete_matching_commands(path: Path, command: str, *, is_zsh: bool) -> int:
    """Remove all file entries whose parsed command equals ``command`` (exact string)."""
    want = command.strip()
    if not want:
        return 0
    if is_zsh:
        with _zsh_histfile_lock(path):
            entries = _zsh_entries(read_histfile_bytes(path))
            kept = [raw for c, raw in entries if c != want]
            removed = len(entries) - len(kept)
            if removed:
                _atomic_write_bytes(path, b"".join(kept))
        return removed
    blocks = _parse_bash_blocks(read_histfile_text(path))
    kept_blk = [blk for c, blk in blocks if c != want]
    removed = len(blocks) - len(kept_blk)
    if removed:
        _atomic_write_bytes(path, _encode("".join(kept_blk)))
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
        dest.write_bytes(
            b"".join(_zsh_encode_entry(cmd, ts + j) for j, cmd in enumerate(oldest_first))
        )
        return
    dest.write_bytes(b"".join(_encode(cmd) + b"\n" for cmd in oldest_first))
