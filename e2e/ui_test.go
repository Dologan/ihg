package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// uiHistory is the history file the picker shows in these tests. Each command
// prints a computed value so its output cannot be confused with echoed input.
const uiHistory = "git status\necho UI-$((6*7))\necho TAB-$((5*5))\ndocker ps\n"

func startWithHistory(t *testing.T, sh shellKind, extraEnv ...string) *session {
	t.Helper()
	hist := filepath.Join(t.TempDir(), "hist")
	if err := os.WriteFile(hist, []byte(uiHistory), 0o600); err != nil {
		t.Fatal(err)
	}
	s := startShell(t, sh.argv, append([]string{"HISTFILE=" + hist}, extraEnv...)...)
	s.line(sh.init, prompt)
	return s
}

func TestPicker(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: skipped with -short")
	}
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			s := startWithHistory(t, sh)

			// Enter runs the best match for the typed query in this shell.
			s.send("ihg\r")
			s.expect("docker ps") // the list is on screen
			s.send("echo UI")
			s.send("\r")
			s.expect("UI-42")
			s.expect(prompt)

			// A query given as an argument pre-filters the list.
			s.send("ihg TAB\r")
			s.expect("-$((5*5))") // "TAB" itself is wrapped in highlight codes
			s.send("\r")
			s.expect("TAB-25")
			s.expect(prompt)

			// Esc on an empty query cancels: nothing runs, status 0.
			s.send("ihg\r")
			s.expect("docker ps")
			s.send("\x1b")
			s.expect(prompt)
			s.line(`echo "RC=$((300+$?))"`, "RC=300")

			// Tab puts the command on the command line for editing.
			if sh.name == "bash3" {
				return // bash 3.2 cannot pre-fill the line
			}
			s.send("ihg\r")
			s.expect("docker ps")
			s.send("TAB")
			s.send("\t")
			s.expect(`-$((5*5))`)
			s.send("\r")
			s.expect("TAB-25")
			s.expect(prompt)
		})
	}
}

func TestPickerWidget(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: skipped with -short")
	}
	for _, sh := range shells(t) {
		if !sh.widgets {
			continue
		}
		t.Run(sh.name, func(t *testing.T) {
			s := startWithHistory(t, sh, "IHG_BIND_CTRL_R=1")

			// The text already on the command line seeds the query.
			s.send("echo UI")
			s.send("\x12")
			s.expect("1/") // the picker's match counter
			s.send("\r")
			s.expect("UI-42")
			s.expect(prompt)
		})
	}
}
