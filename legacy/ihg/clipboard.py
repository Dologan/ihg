"""Copy text to the system clipboard (best-effort, no extra dependencies)."""

from __future__ import annotations

import os
import shutil
import subprocess
import sys


def copy_text(text: str) -> bool:
    """
    Copy UTF-8 text to the clipboard.

    Tries, in order: **pbcopy** (macOS), **wl-copy** (Wayland), **xclip**, **xsel**.
    Returns True on success.
    """
    if text == "":
        return True
    data = text.encode("utf-8", errors="surrogateescape")

    if sys.platform == "darwin":
        try:
            r = subprocess.run(
                ["pbcopy"],
                input=data,
                capture_output=True,
                timeout=30,
            )
            return r.returncode == 0
        except (OSError, subprocess.TimeoutExpired):
            return False

    if os.environ.get("WAYLAND_DISPLAY") and shutil.which("wl-copy"):
        try:
            r = subprocess.run(
                ["wl-copy"],
                input=data,
                capture_output=True,
                timeout=30,
            )
            return r.returncode == 0
        except (OSError, subprocess.TimeoutExpired):
            pass

    if shutil.which("xclip"):
        try:
            r = subprocess.run(
                ["xclip", "-selection", "clipboard"],
                input=data,
                capture_output=True,
                timeout=30,
            )
            return r.returncode == 0
        except (OSError, subprocess.TimeoutExpired):
            pass

    if shutil.which("xsel"):
        try:
            r = subprocess.run(
                ["xsel", "--clipboard", "--input"],
                input=data,
                capture_output=True,
                timeout=30,
            )
            return r.returncode == 0
        except (OSError, subprocess.TimeoutExpired):
            pass

    return False
