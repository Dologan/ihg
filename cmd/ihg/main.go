// Command ihg is an interactive shell-history browser.
//
// The binary only reads history and shows the picker; the shell integration
// printed by `ihg init <shell>` runs the chosen command in the user's own shell.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Dologan/ihg/internal/filters"
	"github.com/Dologan/ihg/internal/history"
	"github.com/Dologan/ihg/internal/result"
	"github.com/Dologan/ihg/internal/shellinit"
	"github.com/Dologan/ihg/internal/ui"
)

// version is overridden at build time: go build -ldflags "-X main.version=v1.2.3".
var version = "dev"

// exitCancelled is returned when the user cancels without the shell wrapper,
// matching fzf so scripts can tell "nothing chosen" from success.
const exitCancelled = 130

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run is main without os.Exit or global state, so tests can call it directly.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-V":
			fmt.Fprintf(stdout, "ihg %s\n", version)
			return 0
		case "--help", "-h":
			fmt.Fprint(stdout, usage)
			return 0
		case "init":
			return runInit(args[1:], stdout, stderr)
		}
	}

	// IHG_DEFAULT_OPTS holds default flags (like fzf's FZF_DEFAULT_OPTS);
	// flags on the command line come later and so win.
	opts, err := parseOptions(append(strings.Fields(getenv("IHG_DEFAULT_OPTS")), args...))
	if err != nil {
		fmt.Fprintf(stderr, "ihg: %v\n%s", err, usage)
		return 2
	}

	// Test hook: IHG_TEST_PICK="run:cmd" or "edit:cmd" answers without the UI,
	// so the e2e tests can exercise the shell integration on its own.
	if pick := getenv("IHG_TEST_PICK"); pick != "" {
		action, cmd, _ := strings.Cut(pick, ":")
		return deliver(getenv("IHG_OUT"), result.Action(action), cmd, stdout, stderr)
	}

	cmds, err := loadCommands(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "ihg: %v\n", err)
		return 2
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintf(stderr, "ihg: needs a terminal: %v\n", err)
		return 2
	}
	defer tty.Close()

	res, err := ui.Run(ui.Config{
		Commands: cmds,
		Query:    opts.query,
		Height:   opts.height,
		Layout:   opts.layout,
		Mouse:    opts.mouse,
	}, tty)
	if err != nil {
		fmt.Fprintf(stderr, "ihg: %v\n", err)
		return 1
	}
	return deliver(getenv("IHG_OUT"), res.Action, res.Command, stdout, stderr)
}

type options struct {
	query  string
	height ui.Height
	layout ui.Layout
	mouse  bool
}

func parseOptions(args []string) (options, error) {
	fs := flag.NewFlagSet("ihg", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors are reported by the caller with usage
	height := fs.String("height", "40%", "")
	layout := fs.String("layout", "default", "")
	var mouse bool // --mouse / --no-mouse; the last one given wins
	fs.BoolFunc("mouse", "", func(string) error { mouse = true; return nil })
	fs.BoolFunc("no-mouse", "", func(string) error { mouse = false; return nil })
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	o := options{query: strings.Join(fs.Args(), " "), mouse: mouse}
	var err error
	if o.height, err = ui.ParseHeight(*height); err != nil {
		return options{}, fmt.Errorf("--height: %w", err)
	}
	switch *layout {
	case "default":
		o.layout = ui.LayoutDefault
	case "reverse":
		o.layout = ui.LayoutReverse
	default:
		return options{}, fmt.Errorf("--layout: want default or reverse, got %q", *layout)
	}
	return o, nil
}

// loadCommands reads the history file and returns its commands newest first,
// deduplicated and without trivial one-word commands.
func loadCommands(getenv func(string) string) ([]string, error) {
	path := firstNonEmpty(getenv("IHG_HISTFILE"), getenv("HISTFILE"))
	if path == "" {
		return nil, errors.New(`no history file: set HISTFILE (the "ihg init" shell integration passes it automatically)`)
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		path = filepath.Join(getenv("HOME"), rest)
	}
	cmds, _, err := history.Load(path, firstNonEmpty(getenv("IHG_SHELL"), getenv("SHELL")))
	if err != nil {
		return nil, err
	}
	slices.Reverse(cmds) // Load returns oldest first
	cmds = slices.DeleteFunc(cmds, isSelfInvocation)
	return filters.BuildProcessed(cmds, filters.IgnoreWords(getenv("IHG_IGNORE"))), nil
}

// isSelfInvocation reports whether cmd runs ihg itself. The shell wrapper
// flushes history before the picker opens, so the `ihg query` line just typed
// would otherwise be the newest entry and match its own query.
func isSelfInvocation(cmd string) bool {
	fields := strings.Fields(cmd)
	return len(fields) > 0 && fields[0] == "ihg"
}

// deliver hands the chosen command to the shell wrapper via the file named by
// IHG_OUT, or prints it to stdout when ihg runs without the wrapper.
func deliver(outPath string, action result.Action, cmd string, stdout, stderr io.Writer) int {
	if outPath == "" {
		if action == result.None {
			return exitCancelled
		}
		fmt.Fprintln(stdout, cmd)
		return 0
	}
	if err := result.Write(outPath, action, cmd); err != nil {
		fmt.Fprintf(stderr, "ihg: %v\n", err)
		return 1
	}
	return 0
}

func runInit(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintf(stderr, "usage: ihg init %s\n", strings.Join(shellinit.Shells(), "|"))
		return 2
	}
	script, err := shellinit.Script(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "ihg: %v\n", err)
		return 2
	}
	fmt.Fprint(stdout, script)
	return 0
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

const usage = `usage: ihg [--height N|N%] [--layout default|reverse] [--[no-]mouse] [--] [query]
       ihg init zsh|bash

  --height N|N%   lines to use (default 40%); 100% is full screen
  --layout L      default (prompt at bottom) or reverse (prompt on top)
  --mouse         wheel scrolls; in full screen, click selects, double-click runs

Defaults can be set in IHG_DEFAULT_OPTS, e.g.
  export IHG_DEFAULT_OPTS="--height 100% --mouse"

Interactive, deduplicated shell history. Set up the shell integration with
  eval "$(ihg init zsh)"     # or bash
Enter runs the selected command in your shell, Tab puts it on the command
line for editing, Esc cancels. See README for more.
`
