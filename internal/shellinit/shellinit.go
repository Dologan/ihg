// Package shellinit holds the shell integration scripts printed by `ihg init`.
//
// The scripts define an `ihg` function (and an optional Ctrl-R widget) that
// runs the binary, reads the result it writes to $IHG_OUT, and then runs or
// edits the chosen command in the user's own shell.
package shellinit

import (
	_ "embed"
	"fmt"
	"sort"
)

//go:embed ihg.zsh
var zsh string

//go:embed ihg.bash
var bash string

var scripts = map[string]string{"zsh": zsh, "bash": bash}

// Script returns the integration script for shell ("zsh" or "bash").
func Script(shell string) (string, error) {
	s, ok := scripts[shell]
	if !ok {
		return "", fmt.Errorf("unsupported shell %q (supported: %v)", shell, Shells())
	}
	return s, nil
}

// Shells lists the supported shell names, sorted.
func Shells() []string {
	names := make([]string, 0, len(scripts))
	for name := range scripts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
