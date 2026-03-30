"""Execute a selected history command under the user's shell."""

from __future__ import annotations

import os
import shlex
import subprocess
import sys
from typing import Optional


def _shell_path() -> str:
    return os.environ.get("IHG_SHELL") or os.environ.get("SHELL") or "/bin/bash"


def _shell_invocation(shell: str, command: str) -> list[str]:
    flags = os.environ.get("IHG_SHELL_FLAGS")
    if flags is not None:
        return [shell] + shlex.split(flags) + [command]
    base = os.path.basename(shell).lower()
    if base == "fish":
        return [shell, "-c", command]
    return [shell, "-lc", command]


def run_selected(
    command: str,
    stdin_data: Optional[bytes],
    *,
    piped_notice: bool = False,
) -> int:
    """
    Run ``command`` via login shell (-lc). If stdin_data is set, it is sent to
    the subprocess stdin. When piped_notice, write a one-line message to stderr.
    """
    shell = _shell_path()
    argv = _shell_invocation(shell, command)
    if piped_notice and stdin_data is not None:
        shown = command if len(command) <= 200 else command[:197] + "..."
        print(f"ihg: running with piped stdin: {shown}", file=sys.stderr)
    if stdin_data is None:
        return subprocess.run(argv).returncode
    return subprocess.run(argv, input=stdin_data).returncode
