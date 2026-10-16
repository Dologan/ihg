# ihg — interactive history grep

> **Rewrite in progress.** ihg is being rewritten in Go as a single static
> binary plus a shell integration (`eval "$(ihg init zsh)"`) that runs the
> chosen command in your own shell, so aliases, functions, `cd`, `source` and
> `export` behave natively. The working Python version, and its full
> documentation, live in [`legacy/`](legacy/README.md) until the Go version
> reaches feature parity.

## Using the Python version meanwhile

```bash
pipx install ./legacy      # or: uv tool install ./legacy
```

See [legacy/README.md](legacy/README.md) for keys, environment variables and the
shell wrapper.

## Trying the Go version

```bash
make build && export PATH="$PWD/bin:$PATH" && eval "$(ihg init zsh)"   # or bash
ihg            # or: ihg git, or IHG_BIND_CTRL_R=1 before the eval for Ctrl-R
```

The `eval` line is what makes Enter *run* the command in your shell. It only lasts
for that shell session, so add it (and the `PATH` line, if needed) to `~/.zshrc` or
`~/.bashrc` on every machine you use ihg on. Without it `ihg` can only print the
chosen command, and says so; `type ihg` should answer "ihg is a function".

Options (defaults can go in `IHG_DEFAULT_OPTS`; command-line flags win):

| Flag | Meaning |
|------|---------|
| `--height N` / `--height N%` | Lines to use, inline below the prompt (default `40%`). `100%` is full screen. |
| `--layout default\|reverse` | Prompt at the bottom with the best match above it (default), or prompt on top. |
| `--mouse` / `--no-mouse` | Wheel moves the selection; in full screen, click selects and double-click runs. While ihg is open, select text with Shift (or Option on macOS) held. |
| `--match exact\|fuzzy\|regex` | How the filter text matches (default `exact`: plain words are substrings; `'word` is fuzzy). `fuzzy` makes plain words fuzzy (`'word` is exact); `regex` treats the whole filter as one Go (RE2) regular expression with smart case. `^prefix`, `suffix$`, `!not` and `a \| b` work in exact and fuzzy. |
| `--edit window\|shell` | Tab edits the command inside ihg (default), or with `shell` puts it on the shell's command line instead. |

Inside the picker, type `:` on an empty filter for commands: `:f` toggles fuzzy
matching, `:p` regex matching, `:+N` / `:-N` grow or shrink the inline height by
N lines (default 1), `:?` or `:h` shows all keys and commands. In the group view
(`:g`) Right toggles copy mode, and `d` / Delete removes a command or, on a
group header, every command in the group (after a y/N prompt).

For example, full screen with the mouse, like the Python version's whole-screen view:

```bash
export IHG_DEFAULT_OPTS="--height 100% --mouse"
```

## Developing the Go version

Requires Go 1.26 or newer.

```bash
make            # gofmt check, go vet, go test
make build      # static binary at bin/ihg
make legacy-test
```

Layout:

| Path | Purpose |
|------|---------|
| `cmd/ihg/` | entry point: flags, `init` subcommand, wiring |
| `internal/history/` | read/parse/rewrite bash and zsh history (byte-exact) |
| `internal/filters/`, `internal/groups/` | dedupe, trivial-command filter, step groups |
| `internal/match/` | fzf-style extended matching with highlight positions |
| `internal/clipboard/`, `internal/result/` | OSC 52 copy; result handed to the shell |
| `internal/shellinit/` | the zsh/bash/fish integration scripts |
| `internal/ui/` | the terminal UI |
| `e2e/` | tests that drive real zsh and bash in a pseudo-terminal |

## License

MIT
