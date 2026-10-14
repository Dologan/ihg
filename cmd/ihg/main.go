// Command ihg is an interactive shell-history browser.
//
// The binary only reads history and shows the picker; the shell integration
// printed by `ihg init <shell>` runs the chosen command in the user's own shell.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is overridden at build time: go build -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without os.Exit, so tests can call it directly.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-V":
			fmt.Fprintf(stdout, "ihg %s\n", version)
			return 0
		case "--help", "-h":
			fmt.Fprint(stdout, usage)
			return 0
		}
	}
	fmt.Fprintln(stderr, "ihg: the Go rewrite is in progress; use legacy/ for now (see README)")
	return 2
}

const usage = `usage: ihg [query]
       ihg init zsh|bash|fish

Interactive, deduplicated shell history. See README for setup and keys.
`
