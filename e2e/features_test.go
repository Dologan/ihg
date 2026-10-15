package e2e

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// uiHistory, newest first after loading: "docker ps" (0), "echo TAB-..." (1),
// "echo UI-..." (2), "git status" (3).

func TestPickerFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: skipped with -short")
	}
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			// The picker reads a fixed file (IHG_HISTFILE) so the session's own
			// lines, which bash flushes to $HISTFILE, cannot shift recency indexes.
			fixed := filepath.Join(t.TempDir(), "fixed_hist")
			if err := os.WriteFile(fixed, []byte(uiHistory), 0o600); err != nil {
				t.Fatal(err)
			}
			s := startWithHistory(t, sh, "IHG_HISTFILE="+fixed)

			// Copy mode: Right at the end of the query, then Enter, sends the
			// command to the terminal clipboard (OSC 52) and runs nothing.
			s.send("ihg\r")
			s.expect("docker ps")
			s.send("echo UI")
			s.expect("1/")
			s.send("\x1b[C") // Right
			s.send("\r")
			s.expect("\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("echo UI-$((6*7))")))
			s.expect(prompt)
			s.line(`echo "AFTER=$((1+2))"`, "AFTER=3") // and "UI-42" never ran before it

			// :2 runs the command with recency index 2, whatever is selected.
			s.send("ihg\r")
			s.expect("docker ps")
			s.send(":2\r")
			s.expect("UI-42")
			s.expect(prompt)

			// :d deletes the selected (newest) command after confirmation.
			s.send("ihg\r")
			s.expect("docker ps")
			s.send(":d\r")
			s.expect("[y/N]")
			s.send("y")
			s.expect("deleted")
			s.send("\x1b")
			s.expect(prompt)
			s.line(`echo "N=$(grep -cx 'docker ps' "$IHG_HISTFILE")"`, "N=0")
		})
	}
}
