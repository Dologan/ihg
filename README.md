# ihg — interactive history grep

`ihg` browses your shell history in the terminal: deduplicated entries, trivial one-word commands filtered out, live substring filtering, and **Enter** to run the highlighted command under your login shell (`bash -lc` / `zsh -lc` by default).

## Requirements

- Python 3.9+
- Standard library only at runtime (`curses` is included with CPython on macOS and most Linux distributions; some minimal Linux images need `python3-curses` / your distro’s equivalent package).

## Install

From the repository root:

```bash
pip install -e .
# or
pipx install .
```

Run (if another `ihg` exists on your `PATH`, prefer `python3 -m ihg`):

```bash
ihg
ihg docker          # start with filter text "docker"
```

Without installation (from repo root):

```bash
PYTHONPATH=. python3 -m ihg
```

Add to your shell config:

```bash
alias ihg='PYTHONPATH=/path/to/ihg python3 -m ihg'
# or after pipx:
alias ihg='pipx run --spec /path/to/ihg ihg'
```

## Environment

| Variable | Meaning |
|----------|---------|
| `HISTFILE` | Path to your history file (bash/zsh). |
| `IHG_HISTFILE` | Overrides `HISTFILE` if set. |
| `SHELL` | Shell used to run the selected command. |
| `IHG_SHELL` | Overrides `SHELL`. |
| `IHG_SHELL_FLAGS` | Extra words between shell and the command (default: `-lc`). Example: `-fc` for zsh. |
| `IHG_IGNORE` | Comma-separated extra single-word commands to hide (merged with the built-in list). |

## Bash and Zsh history

- **Zsh:** extended history lines (`: timestamp:duration;command`) are detected automatically. Use `setopt SHARE_HISTORY` / `INC_APPEND_HISTORY` and a valid `HISTFILE` so the file stays up to date.
- **Bash:** plain lines and timestamp pairs (`#unix_time` followed by the command line) are supported, including simple continuation lines that start with a space.

If `HISTFILE` is missing or the path is not a regular file, `ihg` exits with an error.

## Usage

- **Navigate:** arrow keys or `j` / `k`.
- **Filter:** type text; matching is case-insensitive substring. With an **empty** filter, `:` opens colon-commands (so `:1`, `:q`, etc. are not eaten as filter text). Once the filter is non-empty, `:` is a normal character.
- **Run:** **Enter** on a line runs it.
- **Quit:** `:q` then **Enter** (with an empty filter, type `:` first).
- **Sort:** `:s` (A-Z) or `:S` (Z-A) on the current matches. Numeric labels `0`–`9` always refer to the **ten newest** unique (non-trivial) commands overall, not the sorted row order. **Esc** clears filter and sort.
- **Similarity groups:** `:g` builds groups from the current matches (capped for speed). **+** / **-** or **Right** / **Left** expand or collapse; **Enter** on a leaf runs the command, on a header toggles expansion. **Esc** leaves group view.

## Piped stdin

If stdin is not a TTY (e.g. `cat file | ihg`), the whole stream is read before the UI opens. When you run a command, that data is passed as stdin to the subprocess. A single status line is printed on **stderr**, for example:

```text
ihg: running with piped stdin: grep -i foo
```

Very large stdin is read fully into memory; for huge inputs, consider writing to a file first.

## Limitations

- The UI needs a real terminal on **stdout**; piping `ihg` to a file will fail with an error.
- Windows is not a target (no `curses` in the same form).
- Running arbitrary history lines is inherently risky; review before pressing **Enter**.

## Tests

```bash
pip install -e ".[dev]"
pytest
```

## License

MIT
