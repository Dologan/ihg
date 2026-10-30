# CLAUDE.md

Orientation for agents working on **ihg**: an interactive, fzf-like browser for bash/zsh
shell history, written in Go (`cmd/ihg`, `internal/*`). It replaced a Python implementation
(removed in Phase 5; the last commit that has it is the git tag `python-final`, useful as a
behavioural reference: `git show python-final:legacy/ihg/tui.py`).

## The one idea to understand first

**The `ihg` binary never runs your command. The user's own shell does.**

```
ihg  (shell function from `eval "$(ihg init zsh|bash)"`)
  1. flush this shell's history to $HISTFILE   (zsh: fc -AI, bash: history -a)
  2. IHG_HISTFILE=$HISTFILE IHG_SHELL=<sh> IHG_OUT=<tmpfile> command ihg "$@"
  3. read $IHG_OUT:  line 1 = action (run|edit|none), rest = the command, verbatim
  4. run:  record in history (print -s / history -s), then `eval "$cmd"` in this shell
     edit: only with --edit shell (zsh print -z, bash read -e -i)
```

This is why aliases, functions, `cd`, `source`, `export` and `conda activate` just work, and
why there is no alias parser, no PTY and no environment sanitising (the Python version
needed all three). Keep it that way: do not make the binary exec the command.

- The picker draws on `/dev/tty` (so piped stdin works) and writes its answer to the file in
  `$IHG_OUT`. With `IHG_OUT` unset it prints the command to stdout (for `ihg | pbcopy`); on a
  terminal it then also tells the user the shell integration is not loaded.
- Without the `eval` line in the user's rc file, Enter only *prints* the command. This
  looked like a bug once ("commands get pasted, not run"). Check `type ihg` first.

## Layout

| Path | Role |
|------|------|
| `cmd/ihg/` | flags, `init`, `IHG_DEFAULT_OPTS`, history *source* (`source.go`: load/reload/delete/backup callbacks for the UI), result delivery |
| `internal/history/` | **byte-exact** bash/zsh history parse, append, delete, backup, locking. The riskiest code: it rewrites the user's real history file |
| `internal/filters/`, `internal/groups/` | dedupe + trivial one-word filter; step grouping for `:g` |
| `internal/match/` | fzf algorithm (`github.com/junegunn/fzf/src/algo`, pinned) + our own query parser; modes exact (default), fuzzy, regex |
| `internal/ui/` | Bubble Tea v2 model: `model.go` (Update, modes, quit), `commands.go` (colon commands, delete confirm, copy), `groups.go`, `lineedit.go` (shared line editor for filter + edit mode), `view.go`, `help.go`, `ui.go` (Config, Height, Run) |
| `internal/clipboard/`, `internal/result/` | OSC 52 + tool fallback; the `$IHG_OUT` file protocol |
| `internal/shellinit/` | `ihg.zsh`, `ihg.bash` (go:embed). fish is **not** supported |
| `e2e/` | real zsh/bash in a pty driving the real binary |

## Commands

```bash
make            # gofmt check + go vet + go test  (run before every commit)
make build      # static binary -> bin/ihg
go test ./e2e/  # ~30 s; needs zsh and bash; uses the machine's shells
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/ihg-linux ./cmd/ihg   # Linux build
```

Go **1.26+** is required (Bubble Tea v2's dependencies set it; do not lower the `go` line).
No cgo. `bin/`, `build/`, `dist/`, `*.egg-info` are ignored leftovers; never commit them.

## Pitfalls (each one cost us a bug or a debugging session)

### History files (internal/history)
- History is **bytes**. Never decode it as text and write it back. The old Python version fell
  back to latin-1 and `:d` silently corrupted every non-ASCII line. Go strings hold raw bytes, so
  keep commands as `string`/`[]byte` end to end.
- zsh "metafies" bytes `0x00` and `0x83-0xA2` (stored as `0x83` then `b^0x20`), writes a newline
  inside a command as backslash-newline (**not** `\n`), and appends a guard space to a command
  ending in a backslash. A literal `\n` in a command must stay a literal backslash-n.
- A zsh file may be *extended* (`: ts:elapsed;cmd` per entry) or *plain*; the user's real file
  was plain. Detection order: extended headers, bash `#ts` lines, file name, then `$SHELL`.
- Parsing returns entries whose `Raw` bytes concatenate back to the input exactly; delete must
  preserve every other byte. Rewrites are atomic (temp + rename, keeps mode and symlinks) and
  hold zsh's `$HISTFILE.LOCK` plus an fcntl lock. Tests compare bytes, not strings.
- zsh 5.5.1 (common on RHEL) does not write the trailing-backslash guard space and merges such a
  command with the next entry when reading its own file. ihg follows newer zsh; the real-zsh
  oracle test probes the installed zsh and skips those cases there.
- **Never write to, or print command text from, the developer's real `~/.zsh_history` or
  `~/.bash_history`.** Copy it to a temp dir to experiment; print counts, not contents.
- bash only appends `HISTFILE` on `history -a`/exit. The wrapper flushes first, which makes the
  just-typed `ihg foo` line the newest entry; `isSelfInvocation` in `cmd/ihg/main.go` filters
  lines starting with `ihg` so a query never matches its own invocation.

### Shell integration (internal/shellinit)
- zsh: the helper uses `emulate -L zsh` but the user's command is `eval`ed *outside* it, under
  the user's own options. Do not wrap the eval in `emulate`.
- bash 3.2 (macOS `/bin/bash`) is supported for the function only. The Ctrl-R widget needs
  bash 4+ (`bind -x` cannot run the line, so it uses a two-key macro that rebinds a helper key
  to `accept-line` or `redraw-current-line`).
- Use `history -s -- "$cmd"` / `print -rs -- "$cmd"` so commands starting with `-` are safe.

### TUI (internal/ui)
- Bubble Tea **v2**: import `charm.land/bubbletea/v2` and `charm.land/lipgloss/v2` (not
  `github.com/charmbracelet/...`). `View()` returns `tea.View`; keys are `tea.KeyPressMsg`;
  alt-screen and mouse mode are View fields.
- Inline mode: the frame is exactly N lines, N = `frameLines()`. **Keep the frame height
  constant** across modes (status text goes on the info line, never in an extra line). The
  final frame after quit must be empty so the inline UI erases itself.
- **Anything that arrives after ihg exits is typed into the user's shell.** The mouse double-click
  once leaked `[<0;10;38m` into the prompt because we quit on the second *press* and its
  *release* arrived later. Quit on the release. Remember this for any new event-driven exit.
- History text is displayed through `sanitize` (control characters would otherwise reach the
  terminal). The mapping must stay **1:1 per rune** so match positions remain valid.
- A lone ESC becomes Esc after ~50 ms; ESC + key in one read is Alt+key. In iTerm2, Alt shortcuts
  need Settings, Profiles, Keys, **Left Option key = Esc+**.
- Mouse clicks only map to rows in full screen (the frame's screen row is unknown inline). Mouse
  is opt-in (`--mouse`) because it steals terminal selection and scrollback.
- `help.go`'s `helpLines` documents every key. Update it whenever you change a binding, and keep
  the README option table in step.
- Unit tests cannot see a real terminal. Colours, scrolling at the bottom of the screen,
  iTerm2/tmux behaviour and OSC 52 acceptance need a real terminal (see Testing).

### Matching (internal/match)
- fzf's `src/algo` is not a supported public API: it is pinned (`go.mod`) and may change. Only
  its algorithm is reused; the query parser is ours. Default mode is **exact substring** (user
  decision); `:f` toggles fuzzy, `:p` regex (Go RE2: no lookaround or backreferences).
- `MatchAll` runs with `withPos=false` over the whole list (about 7-14 ms per 100k lines);
  compute highlight positions only for visible rows via `Matcher.Match`. Regex is slower
  (45-60 ms per 100k).

### Clipboard
- Copy goes through OSC 52 first (works over SSH if the local terminal allows it; iTerm2 needs
  "Applications in terminal may access clipboard", tmux needs `set-clipboard on`), then
  pbcopy/wl-copy/xclip/xsel. Never capture tool stdout/stderr with pipes: xclip/wl-copy fork and
  hold the pipe, which hangs. "No tool available" is normal over SSH and not an error.

## Testing

- **Unit tests** are table-driven, next to the code. The UI is tested by driving `Update()` with
  synthetic messages and asserting on `View()` with ANSI stripped.
- **Real-shell oracle tests** (`internal/history`) have zsh write history and ihg parse it, then
  ihg write and zsh read it, for plain and extended files; they skip if zsh is missing.
- **e2e** (`e2e/`) runs real shells in a pty against the real binary (`IHG_TEST_PICK="run:cmd"`
  bypasses the UI to test the wrapper alone). Hard-won rules:
  - Assert on **computed** output (`echo X-$((6*7))` then wait for `X-42`), otherwise the
    terminal's echo of what you typed satisfies the assertion.
  - Keys are sent after a short settle delay: zle/readline discard input queued before the
    prompt is ready.
  - bash writes the test session's own commands into `$HISTFILE`, shifting recency indexes. Use a
    fixed `IHG_HISTFILE` (see `features_test.go`).
  - Tests set `IHG_TEST_NO_CLIPBOARD_TOOLS=1` so they never overwrite the real clipboard.
  - `IHG_E2E_BIN=<path>` uses a prebuilt binary, so the suite can run on a host without Go:
    `GOOS=linux go test -c -o e2e.test ./e2e`, copy both, run there.
- **Seeing the screen**: pty tests only see bytes. For layout, cursor position and scrolling,
  drive a real `tmux` session (`send-keys`, `capture-pane -p`, `display -p '#{cursor_x}'`).
  This found issues unit tests could not (resize near the bottom, mouse leak, edit flow).
- A new behaviour bug deserves a regression test that **fails without the fix**; revert the fix
  once to prove it.
- Deploying a build to another machine: copy to a temp name and `mv -f` over the old binary
  (a running ihg makes a direct overwrite fail with "text file busy").

## Behaviour that was decided deliberately (do not "fix")

- Enter runs; **Tab / Left-at-filter-start / `:e` edit inside the ihg window** by default.
  `--edit shell` puts the command on the shell's command line instead (it did not work reliably
  for one user over SSH on bash, and could not be reproduced).
- Fuzzy matching is off by default. Copy mode is Right-arrow at the end of the filter.
- Defaults live in `IHG_DEFAULT_OPTS`; command-line flags come after it and win.
- Group view (`:g`) draws top-down (`▸` collapsed, `▾` expanded); `d`/Delete/`:d` delete a
  command or a whole group after a `[y/N]` prompt.
- `ihg` lines are hidden from the list; one-word commands such as `ls`/`cd` are hidden
  (`IHG_IGNORE` adds more).

## Status and next steps

Done: Phases 0-5 (scaffolding, core libraries, shell integration, picker UI, colon commands,
delete/backup, copy, groups, preview, sort, live reload, mouse, match modes, in-window edit, parity
check against the Python version, README rewrite, removal of `legacy/`).

Parity facts worth knowing: on a real 406-command zsh history and a synthetic bash file, the Go
and Python pipelines (parse, dedupe, trivial filter, newest first) produced byte-identical lists.
Deliberate differences from Python: commands run in the user's shell (no alias parsing, no PTY,
no `ihg: running:` line), exact matching now splits on spaces into ANDed terms, and copy mode no
longer prints the command when no clipboard works (OSC 52 success cannot be detected).

Phase 6 is done: `.github/workflows/ci.yml` runs `make check` (gofmt, vet, unit and e2e tests with
real zsh and bash) on ubuntu and macOS plus a cross-compile of all four release targets;
`.github/workflows/release.yml` runs goreleaser (`.goreleaser.yaml`: static linux/darwin x
amd64/arm64 tarballs with README and LICENSE, plus `checksums.txt`) when a `v*` tag is pushed.
Cut a release with `git tag v0.1.0 && git push origin v0.1.0` (use `TZ=UTC` for annotated tags).
Validate release changes locally with `goreleaser check` and a snapshot build; pass a config copy
whose `dist:` points at a scratch dir, because `--clean` deletes `dist/`.

Open:
1. No Homebrew tap yet (goreleaser `homebrew_casks` needs a tap repo and a token); unsigned macOS
   binaries downloaded in a browser carry the quarantine attribute (`xattr -d com.apple.quarantine`).
2. The e2e tests depend on timing (fixed settle delay); if CI shows flakiness, raise `settle` in
   `e2e/shell_test.go` before anything else.
3. Known follow-ups: deleting a whole group rewrites the history file once per command (slow on
   NFS; add a batch delete); unverified in real terminals: Ctrl-/ key, mouse clicks with the
   preview open, ZWJ emoji widths.

## Identity and publishing

The project is published under the pseudonymous identity **Dologan <github@dologan.com>**; the
repo-local git config is set to it. Keep it that way: never commit with a different name or
email, never put real names, usernames, employer names, hostnames or filesystem paths (such as a
home directory) in code, docs, tests or commit messages, and check `git log --format='%an <%ae>'`
before pushing. The history was rewritten once to remove an earlier identity; do not re-add
anything from the pre-rewrite history. Commit with `TZ=UTC git commit ...`: the machine's local
timezone would otherwise put its offset (for example `+0100` in summer time) into every commit and
hint at a region. Push with the SSH key that authenticates as Dologan
(`ssh -T git@github.com` should say "Hi Dologan!").

## Conventions

- Run `make` before committing. Keep `gofmt` clean and `go vet` clean for darwin and linux.
- Commit messages: a short subject, then a body explaining *why*, not just what.
- Match the surrounding style: small per-mode key handlers, value-receiver `Model`, comments only
  where the zsh/bash format or a terminal quirk needs explaining.
- Prefer extending the existing helpers (`lineEdit`, `groups`, `source`) over adding parallel ones.
