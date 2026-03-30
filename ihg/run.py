"""Execute a selected history command under the user's shell."""

from __future__ import annotations

import os
import shlex
import shutil
import subprocess
import sys
from typing import Dict, Optional

from ihg.shell_aliases import expand_command_aliases, load_aliases_for_shell

_SHELL_ALIASES: Optional[Dict[str, str]] = None


def _shell_path() -> str:
    return os.environ.get("IHG_SHELL") or os.environ.get("SHELL") or "/bin/bash"


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

    If stdin_data is set, it is sent to the subprocess stdin; the announcement
    line notes piped stdin when ``piped_notice`` is true.
    """
    shell = _shell_path()
    to_run = expand_command_aliases(command, _aliases())
    argv = _shell_invocation(shell, to_run)
    _announce_command(to_run, stdin_data=stdin_data, piped_notice=piped_notice)

    _ensure_term_geometry_env()

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
