// Command ihg is an interactive shell-history browser.
//
// The binary only reads history and shows the picker; the shell integration
// printed by `ihg init <shell>` runs the chosen command in the user's own shell.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Dologan/ihg/internal/result"
	"github.com/Dologan/ihg/internal/shellinit"
)

// version is overridden at build time: go build -ldflags "-X main.version=v1.2.3".
var version = "dev"

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
		case "--":
			args = args[1:]
		}
	}
	query := strings.Join(args, " ")
	_ = query // used by the UI (Phase 3)

	// Test hook: IHG_TEST_PICK="run:cmd" or "edit:cmd" answers without the UI,
	// so the e2e tests can exercise the shell integration on its own.
	if pick := getenv("IHG_TEST_PICK"); pick != "" {
		action, cmd, _ := strings.Cut(pick, ":")
		return deliver(getenv("IHG_OUT"), result.Action(action), cmd, stdout, stderr)
	}

	fmt.Fprintln(stderr, "ihg: the Go rewrite is in progress; use legacy/ for now (see README)")
	return 2
}

// deliver hands the chosen command to the shell wrapper via the file named by
// IHG_OUT, or prints it to stdout when ihg runs without the wrapper.
func deliver(outPath string, action result.Action, cmd string, stdout, stderr io.Writer) int {
	if outPath == "" {
		if action != result.None {
			fmt.Fprintln(stdout, cmd)
		}
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

const usage = `usage: ihg [--] [query]
       ihg init zsh|bash

Interactive, deduplicated shell history. Set up the shell integration with
  eval "$(ihg init zsh)"     # or bash
See README for keys and options.
`
