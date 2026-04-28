"""Execute a selected history command under the user's shell."""

from __future__ import annotations

import os
import shlex
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Dict, Optional

from ihg.shell_aliases import expand_command_aliases, load_aliases_for_shell

_SHELL_ALIASES: Optional[Dict[str, str]] = None


def _shell_path() -> str:
    return os.environ.get("IHG_SHELL") or os.environ.get("SHELL") or "/bin/bash"


def expand_command_for_shell(command: str) -> str:
    """Expand leading aliases the same way :func:`run_selected` does before execution."""
    return expand_command_aliases(command, _aliases())


def preload_aliases() -> None:
    """Load alias map from rc files once (call before the TUI for no first-run lag)."""
    global _SHELL_ALIASES
    _SHELL_ALIASES = load_aliases_for_shell(_shell_path())


def _aliases() -> Dict[str, str]:
    global _SHELL_ALIASES
    if _SHELL_ALIASES is None:
        preload_aliases()
    if _SHELL_ALIASES is None:
        return {}
    return _SHELL_ALIASES


def _zsh_nonomatch_prefix(shell: str, command: str) -> str:
    """
    zsh aborts with 'no matches found' before running the command when a glob
    matches nothing (unlike bash). Prepend setopt so ``ls -l ~/*.sh``-style lines work.
    """
    if os.path.basename(shell).lower() != "zsh":
        return command
    if os.environ.get("IHG_NO_ZSH_NONOMATCH", "").strip() in ("1", "true", "yes"):
        return command
    return f"setopt nonomatch 2>/dev/null; {command}"


_COMPOUND_MARKERS = ("&&", "||", "|", ";", "\n")


def _parent_shell_eval_path() -> Path:
    base = os.environ.get("XDG_STATE_HOME", "").strip()
    if base:
        root = Path(base) / "ihg"
    else:
        root = Path.home() / ".local" / "state" / "ihg"
    return root / "last_eval.sh"


def clear_parent_shell_eval_file() -> None:
    """Truncate the pending parent-shell eval file (call once per ihg session)."""
    p = _parent_shell_eval_path()
    try:
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text("", encoding="utf-8")
    except OSError:
        pass


def _is_simple_parent_shell_command(cmd: str) -> bool:
    """
    True if ``cmd`` is a single directory-stack builtin line (no &&/||/|; chains).

    Such builtins only affect the ``shell -c`` subprocess, not the terminal that
    launched ihg; we enqueue them for the user's shell wrapper instead.
    """
    s = cmd.strip()
    if not s or any(m in s for m in _COMPOUND_MARKERS):
        return False
    parts = s.split()
    if not parts:
        return False
    if parts[0] in ("cd", "pushd", "popd"):
        return True
    if parts[0] == "builtin" and len(parts) > 1 and parts[1] == "cd":
        return True
    return False


def _enqueue_parent_shell_eval(cmd: str) -> bool:
    """Write ``cmd`` as a single line for the parent shell to source. Returns False on I/O error."""
    p = _parent_shell_eval_path()
    try:
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(cmd.rstrip("\n") + "\n", encoding="utf-8")
    except OSError:
        return False
    return True


def _shell_invocation(shell: str, command: str) -> list[str]:
    flags = os.environ.get("IHG_SHELL_FLAGS")
    if flags is not None:
        parts = shlex.split(flags)
        if parts:
            return [shell] + parts + [command]
    return [shell, "-c", command]


def _announce_command(
    command: str,
    *,
    stdin_data: Optional[bytes],
    piped_notice: bool,
) -> None:
    shown = command if len(command) <= 200 else command[:197] + "..."
    if stdin_data is not None and piped_notice:
        print(f"ihg: running with piped stdin: {shown}", file=sys.stderr)
    else:
        print(f"ihg: running: {shown}", file=sys.stderr)
    sys.stderr.flush()


def _ensure_term_geometry_env() -> None:
    """Help tools that use COLUMNS/LINES; safe no-op if not a tty."""
    if sys.stdout is None or not sys.stdout.isatty():
        return
    try:
        sz = shutil.get_terminal_size()
        os.environ.setdefault("COLUMNS", str(sz.columns))
        os.environ.setdefault("LINES", str(sz.lines))
    except OSError:
        pass


def _run_via_pty(argv: list[str]) -> int:
    import pty

    status = pty.spawn(argv)
    return os.waitstatus_to_exitcode(status)


def run_selected(
    command: str,
    stdin_data: Optional[bytes],
    *,
    piped_notice: bool = False,
) -> int:
    """
    Run ``command`` via ``SHELL -c`` (override words with IHG_SHELL_FLAGS).

    Leading tokens are expanded using static ``alias`` / fish ``abbr`` lines
    parsed from your rc files (see :mod:`ihg.shell_aliases`).

    On POSIX, when stdin/stdout are TTYs and stdin is not piped into ihg, runs
    the shell under a pseudo-terminal so programs that gate ANSI (e.g. ``ls
    --color=auto``) still see a terminal.

    For **zsh**, ``setopt nonomatch`` is prepended so empty globs (e.g. ``~/*.sh``)
    do not abort before ``ls`` runs (override with ``IHG_NO_ZSH_NONOMATCH``).

    Simple **cd** / **pushd** / **popd** lines (no ``&&``/``||``/``|``/``;``) cannot
    change the caller's working directory via ``-c``; they are written to
    ``$XDG_STATE_HOME/ihg/last_eval.sh`` (or ``~/.local/state/ihg/last_eval.sh``)
    for a shell wrapper to ``source`` (see README). Set ``IHG_NO_PARENT_SHELL_EVAL``
    to force the old subprocess-only behavior.

    If stdin_data is set, it is sent to the subprocess stdin; the announcement
    line notes piped stdin when ``piped_notice`` is true.
    """
    shell = _shell_path()
    to_run = expand_command_aliases(command, _aliases())
    exec_cmd = _zsh_nonomatch_prefix(shell, to_run)
    argv = _shell_invocation(shell, exec_cmd)
    _announce_command(to_run, stdin_data=stdin_data, piped_notice=piped_notice)

    _ensure_term_geometry_env()

    if (
        os.environ.get("IHG_NO_PARENT_SHELL_EVAL", "").strip() not in ("1", "true", "yes")
        and _is_simple_parent_shell_command(to_run)
        and _enqueue_parent_shell_eval(to_run)
    ):
        return 0

    if stdin_data is not None:
        return subprocess.run(argv, input=stdin_data).returncode

    stdin_tty = sys.stdin is not None and sys.stdin.isatty()
    stdout_tty = sys.stdout is not None and sys.stdout.isatty()
    use_pty = (
        sys.platform != "win32"
        and stdin_tty
        and stdout_tty
        and os.environ.get("IHG_NO_PTY", "").strip() not in ("1", "true", "yes")
    )
    if use_pty:
        try:
            return _run_via_pty(argv)
        except OSError:
            pass

    return subprocess.run(
        argv,
        stdin=sys.stdin,
        stdout=sys.stdout,
        stderr=sys.stderr,
    ).returncode
