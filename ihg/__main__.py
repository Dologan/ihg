"""CLI entry for ihg."""

from __future__ import annotations

import sys

from ihg.filters import build_processed
from ihg.history import histfile_is_zsh, histfile_path, load_commands_newest_first
from ihg.run import preload_aliases
from ihg.tui import run_interactive


def main() -> int:
    if len(sys.argv) > 1 and sys.argv[1] in ("-h", "--help"):
        print(
            "usage: ihg [filter_substring]\n"
            "\n"
            "Interactive, deduplicated shell history (HISTFILE / IHG_HISTFILE).\n"
            "Optional argument sets the initial filter. See README for key bindings.",
            file=sys.stdout,
        )
        return 0

    path = histfile_path()
    if path is None:
        print(
            "ihg: set HISTFILE or IHG_HISTFILE to your shell history file",
            file=sys.stderr,
        )
        return 2

    cmds_nf = load_commands_newest_first(path)
    base, _ = build_processed(cmds_nf)
    is_zsh = histfile_is_zsh(path)
    initial_filter = sys.argv[1] if len(sys.argv) > 1 else ""

    stdin_data = None
    if not sys.stdin.isatty():
        stdin_data = sys.stdin.buffer.read()

    if not sys.stdout.isatty():
        print("ihg: stdout must be a terminal (curses UI)", file=sys.stderr)
        return 2

    preload_aliases()

    try:
        return run_interactive(base, initial_filter, stdin_data, path, is_zsh)
    except KeyboardInterrupt:
        return 130


if __name__ == "__main__":
    raise SystemExit(main())
