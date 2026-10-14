"""Sanitize environment for shell subprocesses (avoid HISTFILE pollution)."""

from __future__ import annotations

import os
from typing import Dict, Optional

# Variables that can cause a ``bash -c`` / PTY child to run rc snippets or rewrite
# the user's real history file on exit.
_ISOLATE_KEYS = (
    "BASH_ENV",
    "ENV",
    "PROMPT_COMMAND",
    "PROMPT_DIRTRIM",
    "DEBUG",
    "HISTFILESIZE",
    "HISTCONTROL",
    "HISTIGNORE",
    "HISTTIMEFORMAT",
    "SHELLOPTS",
)


def shell_subprocess_env(overrides: Optional[Dict[str, str]] = None) -> Dict[str, str]:
    """
    Build env for ihg-spawned shells.

    Default ``HISTFILE=/dev/null`` so PTY / ``-c`` children cannot append or
    overwrite the interactive session's history (a common source of ``set +o`` /
    ``set -o`` noise on Bash).
    """
    ovr = overrides or {}
    env = {**os.environ, **ovr}
    for key in _ISOLATE_KEYS:
        env.pop(key, None)
    if "HISTFILE" in ovr:
        env["HISTFILE"] = ovr["HISTFILE"]
    else:
        env["HISTFILE"] = "/dev/null"
    env["HISTSIZE"] = "0"
    return env
