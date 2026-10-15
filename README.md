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
