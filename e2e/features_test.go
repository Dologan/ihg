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

func fixedHistory(t *testing.T) string {
	t.Helper()
	fixed := filepath.Join(t.TempDir(), "fixed_hist")
	if err := os.WriteFile(fixed, []byte(uiHistory), 0o600); err != nil {
		t.Fatal(err)
	}
	return "IHG_HISTFILE=" + fixed
}

// Tab edits the command inside the picker window; the edited text then runs.
func TestPickerEditInWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: skipped with -short")
	}
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			s := startWithHistory(t, sh, fixedHistory(t))
			s.send("ihg\r")
			s.expect("docker ps")
			s.send("TAB")
			s.send("\t")
			s.expect("edit>")
			s.send("; echo ED-$((2+3))")
			s.send("\r")
			s.expect("TAB-25")
			s.expect("ED-5")
			s.expect(prompt)
		})
	}
}

// Exact matching is the default; :f and :p switch to fuzzy and regex; :? is help.
func TestPickerMatchModesAndHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: skipped with -short")
	}
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			s := startWithHistory(t, sh, fixedHistory(t))
			s.send("ihg\r")
			s.expect("docker ps")

			// "ehoUI" is a fuzzy-only match of "echo UI-...": nothing in exact
			// mode, so Enter does nothing (a run would end the picker here).
			s.send("ehoUI")
			s.expect("0/") // the screen redraw only touches the changed cells
			s.send("\r")
			s.send("\x15")
			s.send(":f\r") // fuzzy: now it matches and Enter runs it
			s.expect("match: fuzzy")
			s.send("ehoUI")
			s.send("\r")
			s.expect("UI-42")
			s.expect(prompt)

			// :p switches to regex; ^e.{3} UI- is not a substring of anything.
			s.send("ihg\r")
			s.expect("docker ps")
			s.send(":p\r")
			s.expect("match: regex")
			s.send(`^e.{3} UI-`)
			s.send("\r")
			s.expect("UI-42")
			s.expect(prompt)

			// :? is help; q closes it; :+3 grows the inline frame (16 -> 19 lines).
			s.send("ihg\r")
			s.expect("docker ps")
			s.send(":?\r")
			s.expect("quick help")
			s.send("q")
			s.send(":+3\r")
			s.expect("height: 19 lines")
			s.send("\x1b")
			s.expect(prompt)
			s.line(`echo "RC=$((300+$?))"`, "RC=300")
		})
	}
}
