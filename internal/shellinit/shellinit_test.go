package shellinit

import (
	"os/exec"
	"strings"
	"testing"
)

func TestScript(t *testing.T) {
	for _, sh := range Shells() {
		s, err := Script(sh)
		if err != nil || !strings.Contains(s, "ihg()") {
			t.Errorf("Script(%q) = %d bytes, err %v; want an ihg function", sh, len(s), err)
		}
	}
	if _, err := Script("fish"); err == nil {
		t.Error("Script(fish) succeeded; want an unsupported-shell error")
	}
}

// TestScriptsParse checks each script is syntactically valid for its shell.
func TestScriptsParse(t *testing.T) {
	for _, sh := range Shells() {
		path, err := exec.LookPath(sh)
		if err != nil {
			t.Logf("skip %s: not installed", sh)
			continue
		}
		s, _ := Script(sh)
		out, err := exec.Command(path, "-n", "-c", s).CombinedOutput()
		if err != nil {
			t.Errorf("%s -n rejected the script: %v\n%s", sh, err, out)
		}
	}
}
