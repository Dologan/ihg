# ihg — interactive history grep

`ihg` browses your shell history in the terminal: deduplicated entries, trivial one-word commands filtered out, live substring filtering, and **Enter** to run the highlighted command with **`$SHELL -c`** (fast startup). Simple **`alias`** / fish **`abbr`** lines are read from your dotfiles and the **first command word** is expanded before execution (see below).

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
| `IHG_SHELL_FLAGS` | Extra words between shell and the command string. Default is a single **`-c`**. Example: `-lc` for a login shell, or `-x -y` for other shells. If set but empty/whitespace-only after parsing, falls back to `-c`. |
| `IHG_IGNORE` | Comma-separated extra single-word commands to hide (merged with the built-in list). |
| `IHG_NO_PTY` | If set to `1`/`true`/`yes`, skip the pseudo-terminal path (POSIX only); use if a command misbehaves under a PTY. |
| `IHG_NO_ZSH_NONOMATCH` | If set, do not prepend ``setopt nonomatch`` for **zsh** (by default ihg avoids zsh exiting early on empty globs like ``~/*.sh``). |
| `IHG_NO_PARENT_SHELL_EVAL` | If set, do not write **cd** / **pushd** / **popd** to the parent eval file; run them only under ``shell -c`` (they still cannot change your interactive cwd). |
| `IHG_NO_STARTUP_HISTORY_SYNC` | If set, skip the pre-load **zsh** ``fc -AI`` subprocess (bash never runs a startup sync subprocess). |

## Bash and Zsh history

- **Zsh:** both plain and extended (`: timestamp:duration;command`) history are supported, including multi-line entries and non-ASCII commands (zsh's metafied encoding). Extended files are detected by content; a plain file is treated as zsh when its name contains `zsh`/`zhistory`, or else when your shell is zsh. Deleting (`:d`) rewrites the file atomically while holding zsh's `$HISTFILE.LOCK`, leaving every other entry byte-for-byte unchanged. Use `setopt SHARE_HISTORY` / `INC_APPEND_HISTORY` and a valid `HISTFILE` so the file stays up to date.
- **Bash:** plain lines and timestamp pairs (`#unix_time` followed by the command line) are supported, including simple continuation lines that start with a space. By default Bash often updates `HISTFILE` only when the shell exits, so ihg cannot show commands from the **current** session until they are flushed. Use e.g. `shopt -s histappend` and `PROMPT_COMMAND='history -a'` (or `history -a` from `PROMPT_COMMAND` together with your existing hook) so new lines are appended after each command. While ihg is open, it **re-reads** the history file when its size or modification time changes, so entries appear after the next flush without restarting ihg.
- **Startup sync:** Before reading the history file, **zsh** runs ``zsh -f -c 'fc -AI'`` (with a sanitized subprocess environment). **Bash** does **not** run a startup subprocess: a child ``history -a`` cannot flush your interactive session and, with ``BASH_ENV`` / PTY startup, has been observed to append ``set +/-o`` lines into ``HISTFILE``. Use the README ``ihg()`` wrapper ``history -a`` in your interactive shell instead. Disable zsh sync with **`IHG_NO_STARTUP_HISTORY_SYNC`**. Commands run by ihg use ``HISTFILE=/dev/null`` in the child shell so PTY runs cannot rewrite your real history file.

If `HISTFILE` is missing or the path is not a regular file, `ihg` exits with an error.

## Aliases (without interactive shells)

Running **`bash -ilc '…'`** (or login shells) loads your full config but often prints motd/welcome text and is slow. ihg instead uses **`shell -c`** and, at startup, scans a few rc files for static definitions:

- **bash:** `~/.bash_aliases`, `~/.bashrc`, `~/.profile`
- **zsh:** `~/.zprofile`, `~/.zshrc`
- **fish:** `~/.config/fish/config.fish` (`abbr` lines)

Later files override earlier ones. Only lines that look like **`alias name=value`** (and simple fish **`abbr`**) are parsed; aliases created only inside conditionals, functions, or plugins are **not** seen. Each top-level segment split by **`&&`**, **`||`**, **`|`** (outside quotes/parens), **`;`**, or newline gets its leading word expanded recursively (with a depth limit), including when that word is followed by arguments. Optional leading **`VAR=value`** assignments, **`sudo`** (common flags), **`command`**, **`noglob`**, and **`nocorrect`** are skipped before expansion. A missing space before **`~/`** is tolerated when the name matches an alias (e.g. ``ll~/bin`` → ``ls -l ~/bin``). **zsh** normally errors on globs that match nothing before the command runs; ihg prepends ``setopt nonomatch`` for zsh unless **`IHG_NO_ZSH_NONOMATCH`** is set. Use **`IHG_SHELL_FLAGS`** if you need a heavier shell environment.

**Why `-l` (login) existed:** a login shell reads `*profile` files, which some setups use for PATH, locale, or tools. That is slower; the default is now a plain **`-c`** (non-login). Set e.g. **`IHG_SHELL_FLAGS=-lc`** if you rely on login-only startup.

## Usage

- **Navigate:** arrow keys (↑/↓).
- **Filter:** type text; matching is case-insensitive substring. With an **empty** filter, `:` opens colon-commands (so `:1`, `:q`, `:h`, etc. are not eaten as filter text). Once the filter is non-empty, `:` is a normal character.
- **Run:** **Enter** runs the highlighted line (alias-expanded). **stderr** shows `ihg: running: …` first (or `ihg: running with piped stdin: …` when ihg’s stdin is a pipe). On Unix, the shell runs in a **pseudo-terminal** when both stdin and stdout are TTYs so tools like `ls --color=auto` still emit color.
- **Edit:** **←** (left arrow) or **`:e`** opens the selected line in the footer editor (the list row uses a **different highlight** from copy mode when colors are available: magenta vs blue). In the editor, **←** / **→** move the cursor inside the line.
- **Copy:** **→** (right arrow) turns on **clipboard mode** (hint shows `[COPY]`). **Enter** in clipboard mode copies the **alias-expanded** command and exits (needs **pbcopy** / **wl-copy** / **xclip** / **xsel**). If the clipboard is unavailable (e.g. some SSH sessions), **stderr** still shows an error and the **full expanded line is printed on stdout** so you can copy it manually. **`:c`** copies the current selection once and quits without toggling mode. **Esc** clears the filter and turns off copy mode.
- **Help:** **`:h`** (empty filter, then `:` `h` Enter) opens a short key reference; **Esc** closes it.
- **Quit:** `:q` then **Enter** (with an empty filter, type `:` first).
- **Sort:** `:s` (A-Z) or `:S` (Z-A) on the current matches. Numeric labels `0`–`9` always refer to the **ten newest** unique (non-trivial) commands overall, not the sorted row order. **Esc** clears filter and sort.
- **Step groups:** `:g` groups by the first word of each `&&` / `|` step. **+** / **-** expand or collapse; **Space** on a **section header** toggles expand/collapse; **Enter** on a header does the same. **Enter** on a leaf runs or copies (same **←** edit / **→** copy as the flat list). **Esc** leaves group view.

## `cd`, `pushd`, and `popd`

A child process cannot change the working directory of the shell that started ihg. Plain **`SHELL -c 'cd …'`** exits successfully but leaves you in the same directory.

For a **single-line** **cd** / **pushd** / **popd** (no `&&`, `||`, `|`, or `;` on the line), ihg writes the expanded command to **`$XDG_STATE_HOME/ihg/last_eval.sh`**, or **`~/.local/state/ihg/last_eval.sh`** if `XDG_STATE_HOME` is unset. That file is **cleared when ihg starts**. Wrap **`ihg`** so your shell **sources** it after each run, for example:

**bash and zsh**

Before **`command ihg`**, the wrapper flushes your **current** shell history to **`HISTFILE`**: **zsh** runs **`builtin fc -AI`** (append + reconcile with the file—the same step ihg’s own subprocess cannot perform for your interactive session), and **bash** runs **`history -a`**. Errors from those builtins are ignored so a failed flush never blocks the UI.

```bash
ihg() {
  local _ec _f
  if [ -n "${ZSH_VERSION:-}" ]; then
    builtin fc -AI 2>/dev/null
  elif [ -n "${BASH_VERSION:-}" ]; then
    history -a 2>/dev/null
  fi
  command ihg "$@"
  _ec=$?
  _f="${XDG_STATE_HOME:-$HOME/.local/state}/ihg/last_eval.sh"
  if [[ -s "$_f" ]]; then
    # shellcheck source=/dev/null
    . "$_f"
    : >"$_f"
  fi
  return "$_ec"
}
```

**fish**

```fish
function ihg
    command ihg $argv
    set -l _ec $status
    set -l _f "$HOME/.local/state/ihg/last_eval.sh"
    if test -n "$XDG_STATE_HOME"
        set _f "$XDG_STATE_HOME/ihg/last_eval.sh"
    end
    if test -s "$_f"
        source "$_f"
        echo -n '' >"$_f"
    end
    return $_ec
end
```

Compound lines such as **`cd foo && make`** still run entirely inside **`shell -c`**, where the **cd** applies to that subprocess.

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

## Pause after Enter

There is often a short delay between choosing a command and seeing output. Typical causes:

1. **Curses teardown** — `endwin()` restores the real terminal (small cost).
2. **Shell startup** — each run starts **`shell -c`** (minimal). Heavier `IHG_SHELL_FLAGS` (e.g. login shells) add more delay.
3. **PTY setup** (Unix, TTY run) — allocating a pseudo-terminal adds a little overhead versus a bare `subprocess`.
4. **First launch** — alias tables are read from disk once when ihg starts (usually small).

## Tests

```bash
pip install -e ".[dev]"
pytest
```

## License

MIT
