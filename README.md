# ihg: interactive history grep

`ihg` browses your shell history in the terminal: deduplicated, trivial one-word commands
hidden, live filtering with highlighting, and **Enter runs the highlighted command in your own
shell**, so aliases, functions, `cd`, `source`, `export` and `conda activate` all behave exactly
as if you had typed the line. It is a single static binary for macOS and Linux (bash and zsh).

## Install

You need Go 1.26 or newer to build it. From a clone of this repository:

```bash
go install ./cmd/ihg        # installs to $(go env GOPATH)/bin; make sure that is on your PATH
# or
make build                  # writes bin/ihg; copy it somewhere on your PATH
```

For a Linux machine without Go, cross-compile and copy the binary over:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ihg-linux ./cmd/ihg    # arm64: GOARCH=arm64
scp ihg-linux host:bin/.ihg.new && ssh host 'chmod +x ~/bin/.ihg.new && mv -f ~/bin/.ihg.new ~/bin/ihg'
```

## Set up your shell (required)

Add one line to `~/.zshrc` or `~/.bashrc` on every machine where you use ihg:

```bash
eval "$(ihg init zsh)"      # or: eval "$(ihg init bash)"
```

That defines an `ihg` function that opens the picker and then runs the result in your shell.
Without it `ihg` can only *print* the chosen command (useful for `ihg | pbcopy`, but nothing runs),
and it tells you so. `type ihg` should say "ihg is a function" (bash) or "ihg is a shell function"
(zsh). With the wrapper loaded, use `command ihg | pbcopy` to get the plain printing behaviour.

Optional, set **before** the `eval` line:

| Variable | Effect |
|----------|--------|
| `IHG_BIND_CTRL_R=1` | Bind Ctrl-R to the picker. The text already on the command line becomes the initial filter (in zsh only the part left of the cursor). (bash needs 4.0 or newer; macOS's `/bin/bash` 3.2 only gets the `ihg` function.) |
| `IHG_DEFAULT_OPTS="--height 100% --mouse"` | Default flags (see Options). Flags typed on the command line win. |

History: the wrapper flushes the current session's history first (zsh `fc -AI`, bash
`history -a`), so recent commands show up. To see commands from your *other* terminals too, make
every session write as it goes (zsh `setopt INC_APPEND_HISTORY` or `SHARE_HISTORY`; bash
`shopt -s histappend` and `PROMPT_COMMAND='history -a'`). While ihg is open it checks the
history file once a second and reloads it when it changed, so such commands appear without
restarting it.

fish is not supported.

## Using it

```bash
ihg               # open the picker
ihg docker        # start with the filter "docker"
cat notes | ihg   # the piped text is handed to the command you pick, as its stdin
```

| Key | Action |
|-----|--------|
| type | filter (see Matching) |
| Up / Down, Ctrl-P / Ctrl-N, Ctrl-K / Ctrl-J, PgUp / PgDn | move the selection (mouse wheel with `--mouse`) |
| **Enter** | run the selected command in your shell |
| **Tab**, or Left at the start of the filter, or `:e` | edit the command inside ihg, then Enter to run it |
| Right at the end of the filter | copy mode (`[COPY]`): Enter copies instead of running; Esc leaves it |
| Ctrl-R | cycle sort: relevance, recency, A-Z |
| `?` (empty filter) or Ctrl-/ | preview: the full, multi-line command and, when your history records timestamps, when it last ran |
| F1, `:h`, `:?` | help |
| Esc | clear the filter; on an empty filter, cancel |
| Ctrl-C, Ctrl-G, Ctrl-Q, Ctrl-D (empty filter) | cancel |
| Left/Right, Ctrl-B/F, Home/End or Ctrl-A/E, Alt-B/F (or Alt-Left/Right), Ctrl-W or Alt-Backspace, Ctrl-U, Backspace, Delete (Ctrl-D) | edit the filter text (Alt keys need your terminal to send Option as Esc+; in iTerm2: Profiles, Keys, Left Option key = Esc+) |

Numbers `0`-`9` in the left margin mark the ten most recent commands; `:3` runs number 3.

Editing happens in a line at the bottom (`edit>`) with the same keys as the filter plus Ctrl-K
(delete to the end). Enter runs the text (or copies it in copy mode; an empty line is
refused), Esc goes back to the list without running. Newlines in a multi-line command show
as `↵` and are kept. With `--edit shell`, Tab instead puts the command on your shell's command
line: in zsh it appears at the next prompt; in bash 4+ it opens a readline prompt and runs on
Enter; bash 3.2 can only queue it in history for the Up arrow.

Commands whose first word is `ihg` never appear in the list or in backups.

### Colon commands

Type `:` on an empty filter, then Enter:

| Command | Does |
|---------|------|
| `:q` | quit |
| `:r` | reset the filter, sort, matching mode and copy mode, and leave the group view |
| `:0` ... `:9` | run the command with that recency number (copies it in copy mode) |
| `:e` | edit the selected command |
| `:c` | copy the selected command once and quit |
| `:d` | delete the selected command from your history file, after a `y/N` question; every entry with exactly that text goes, not just the one shown |
| `:b [PATH]` | write a cleaned backup (deduplicated, trivial commands removed, oldest first); default `HISTFILE.ihg-clean.TIMESTAMP`. Timestamps are not kept: zsh backups get new consecutive ones, bash backups none. Written with mode 0600; `~/` is expanded and missing folders are created |
| `:s`, `:S` | sort A-Z, Z-A |
| `:g` | group view (below) |
| `:f`, `:p` | toggle fuzzy or regex matching |
| `:+N`, `:-N` | grow or shrink the inline height by N lines (default 1, minimum 3 lines; not in full screen) |
| `:h`, `:?` | help |

### Group view (`:g`)

Commands grouped by the programs in their pipelines (`a && b | c` appears under `a`, `b` and `c`;
steps split on `&&` and `|`, not `;`). It covers the first 500 commands matching the current filter.
Space or Enter on a header opens or closes it; `+` opens, `-` closes (also from inside a group);
Enter on a command runs it; Tab edits it; Right toggles copy mode. `d` or Delete removes the
selected command, or, on a header, every command in the group, after a `y/N` question. Because a
pipeline belongs to several groups, deleting a group deletes those commands completely. Esc goes
back to the search (if copy mode is on, the first Esc only leaves copy mode).

### Matching

The default is **exact**: space-separated terms are all required (any order), as plain substrings,
ignoring case unless the filter contains a capital letter. The syntax is fzf's:

| Term | Matches |
|------|---------|
| `word` | substring (in fuzzy mode: fuzzy match) |
| `'word` | the other kind: fuzzy in exact mode, exact in fuzzy mode |
| `^word`, `word$`, `^word$` | starts with, ends with, equals |
| `!word` | does not contain (also `!^word`, `!word$`) |
| `a \| b` | either term |

`:f` switches to **fuzzy** matching, `:p` to **regex** (the whole filter is one Go regular
expression: no lookahead or backreferences; smart case as above). The mode shows in the info
line, and `--match` sets the default.

## Options

Flags go before the optional initial filter (use `--` first if the filter starts with `-`);
defaults can live in `IHG_DEFAULT_OPTS`, split on spaces (no quoting). `ihg --version` and
`ihg --help` must be the first argument.

| Flag | Meaning |
|------|---------|
| `--height N` / `--height N%` | Lines to use, drawn inline below the prompt (default `40%`). `100%` is full screen. |
| `--layout default\|reverse` | Prompt at the bottom with the best match above it (default), or prompt on top. |
| `--mouse` / `--no-mouse` | Wheel moves the selection; in full screen, click selects and double-click runs. While ihg is open, select text with Shift (Option on macOS) held. |
| `--match exact\|fuzzy\|regex` | Default matching mode. |
| `--edit window\|shell` | Edit inside ihg (default), or put the command on the shell's command line. |

| Variable | Meaning |
|----------|---------|
| `IHG_IGNORE` | Comma-separated extra one-word commands to hide (added to `ls`, `cd`, `pwd`, `clear`, ...). |
| `IHG_HISTFILE` | History file. The wrapper passes your shell's `HISTFILE` (bash falls back to `~/.bash_history`; zsh needs `HISTFILE` set, as it normally is). |
| `IHG_SHELL` | Set by the wrapper; selects the history format when it cannot be told from the file. |
| `IHG_DEFAULT_OPTS`, `IHG_BIND_CTRL_R` | See above. |

## Copying, locally and over SSH

Copy mode and `:c` send the command to your terminal clipboard with OSC 52, which also reaches
your local clipboard through SSH, and also try the first working local tool: `pbcopy` on macOS,
`wl-copy` under Wayland (`WAYLAND_DISPLAY` set), then `xclip` or `xsel` under X11 (`DISPLAY`
set). ihg cannot tell whether the terminal accepted the OSC 52 request:

- iTerm2: Settings, General, Selection, "Applications in terminal may access clipboard".
- tmux: `set -g set-clipboard on`.
- Terminal.app ignores OSC 52. Without a clipboard tool on the machine, nothing is copied there.

## History formats

bash (plain or with `#timestamp` lines; continuation lines are recognised when they start with
a space) and zsh (plain or `EXTENDED_HISTORY`, including multi-line commands and non-ASCII text)
are read byte-exactly.
Deleting (`:d`) rewrites the file atomically while holding zsh's `HISTFILE.LOCK` and leaves every
other entry unchanged. That locking also covers shared NFS home directories and several shells
writing at once; bash itself has no history lock, so a bash write that lands during a delete can
be lost.

## Coming from the Python version

The Python implementation is gone; the history parsing it perfected was ported and is checked
against real zsh. What changed:

- Commands now run in **your** shell instead of a child `$SHELL -c`. ihg no longer parses alias
  files, needs no pseudo-terminal, and `cd`, `source`, `export` just work. Chosen commands are
  added to your history by the shell itself.
- The wrapper (`eval "$(ihg init ...)"`) is required, and replaces the old `ihg()` function from
  the README.
- Filtering takes several terms: `git commit` now means *git* AND *commit* (any order), where it
  used to mean the exact phrase. For the exact phrase, escape the space: `git\ commit`.
- Retired variables: `IHG_SHELL_FLAGS`, `IHG_NO_PTY`, `IHG_NO_ZSH_NONOMATCH`,
  `IHG_NO_PARENT_SHELL_EVAL`, `IHG_NO_STARTUP_HISTORY_SYNC`. Still honoured: `HISTFILE`,
  `IHG_HISTFILE`, `IHG_IGNORE`.
- Editing (now inside the picker), `:b`, `:d`, `:g` and copy mode remain. ihg no longer prints
  `ihg: running: ...` before a command, because the command is simply run in your shell. Copy
  mode no longer prints the command when no clipboard works, since OSC 52 success cannot be
  detected; see Copying above.
- Uninstall the old one with `pipx uninstall ihg` or `pip uninstall ihg` if you installed it.

## Development

Layout, design rationale and the pitfalls to know about are in [CLAUDE.md](CLAUDE.md).

```bash
make            # gofmt check, go vet, go test
make build      # static binary at bin/ihg
go test ./e2e/  # real zsh and bash in a pseudo-terminal (needs both installed)
```

## License

MIT
