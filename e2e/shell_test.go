// Package e2e drives real zsh and bash in a pseudo-terminal to check the
// shell integration from `ihg init`: that the chosen command runs in the
// user's own shell (cd, aliases, export stick), lands in history, can be
// edited first, receives piped stdin, and works from the Ctrl-R widget.
//
// The picker UI is bypassed with IHG_TEST_PICK="<action>:<command>".
// Expected output always uses a computed value (e.g. $((40+2)) -> 42) so the
// terminal echoing the typed line can never satisfy an assertion by itself.
package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

const prompt = "IHGPS1> "

const settle = 150 * time.Millisecond

var ihgBin string // built once in TestMain

func TestMain(m *testing.M) {
	// IHG_E2E_BIN points at a prebuilt ihg, for running the compiled test
	// binary on a host without Go (e.g. GOOS=linux go test -c, then scp).
	if bin := os.Getenv("IHG_E2E_BIN"); bin != "" {
		ihgBin = bin
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "ihg-e2e-bin")
	if err != nil {
		panic(err)
	}
	ihgBin = filepath.Join(dir, "ihg")
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(thisFile))
	build := exec.Command("go", "build", "-o", ihgBin, "./cmd/ihg")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		panic("building ihg: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// session is one interactive shell running in a pty.
type session struct {
	t    *testing.T
	f    *os.File
	cmd  *exec.Cmd
	mu   sync.Mutex
	out  bytes.Buffer
	mark int // output offset where the next expectation starts searching
}

func startShell(t *testing.T, argv []string, extraEnv ...string) *session {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = home
	cmd.Env = append([]string{
		"HOME=" + home,
		"PATH=" + filepath.Dir(ihgBin) + ":/usr/bin:/bin",
		"TERM=xterm",
		"PS1=" + prompt,
		"TMPDIR=" + home,
		"IHG_TEST_NO_CLIPBOARD_TOOLS=1", // never overwrite the real clipboard
	}, extraEnv...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 200})
	if err != nil {
		t.Fatalf("starting %v: %v", argv, err)
	}
	s := &session{t: t, f: f, cmd: cmd}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := f.Read(buf)
			s.mu.Lock()
			s.out.Write(buf[:n])
			s.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		f.Close()
		cmd.Process.Kill()
		cmd.Wait()
	})
	s.expect(prompt)
	return s
}

// expect waits until want appears in the output after the current mark,
// then moves the mark past it.
func (s *session) expect(want string) {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		out := s.out.String()
		s.mu.Unlock()
		if i := strings.Index(out[s.mark:], want); i >= 0 {
			s.mark += i + len(want)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.mu.Lock()
	tail := s.out.String()[s.mark:]
	s.mu.Unlock()
	s.t.Fatalf("timed out waiting for %q; output since last match:\n%s", want, tail)
}

// line types a command line, waits for want in its output, then for the prompt.
func (s *session) line(text, want string) {
	s.t.Helper()
	s.send(text + "\r")
	s.expect(want)
	if want != prompt {
		s.expect(prompt)
	}
}

func (s *session) send(keys string) {
	s.t.Helper()
	// zle and readline switch the tty to raw mode after printing the prompt,
	// discarding input that is already queued; give them a moment first.
	time.Sleep(settle)
	if _, err := s.f.Write([]byte(keys)); err != nil {
		s.t.Fatalf("writing to pty: %v", err)
	}
}

type shellKind struct {
	name    string
	argv    []string
	init    string // eval line that loads the integration
	widgets bool   // supports the Ctrl-R widget
	histCmd string // prints history entries one per line, no numbers
}

func shells(t *testing.T) []shellKind {
	var out []shellKind
	if zsh, err := exec.LookPath("zsh"); err == nil {
		out = append(out, shellKind{
			name: "zsh", argv: []string{zsh, "-f", "-i"}, init: `eval "$(ihg init zsh)"`,
			widgets: true, histCmd: "fc -ln 1",
		})
	}
	seen := map[string]bool{}
	for _, cand := range []string{"/bin/bash", "/opt/homebrew/bin/bash", "/usr/local/bin/bash", "/usr/bin/bash"} {
		real, err := filepath.EvalSymlinks(cand)
		if err != nil || seen[real] {
			continue
		}
		seen[real] = true
		v, err := exec.Command(real, "-c", "echo ${BASH_VERSINFO[0]}").Output()
		if err != nil {
			continue
		}
		major, _ := strconv.Atoi(strings.TrimSpace(string(v)))
		out = append(out, shellKind{
			name: "bash" + strconv.Itoa(major), argv: []string{real, "--norc", "--noprofile", "-i"},
			init: `eval "$(ihg init bash)"`, widgets: major >= 4,
			histCmd: `HISTTIMEFORMAT= history | sed 's/^ *[0-9]* *//'`,
		})
	}
	if len(out) == 0 {
		t.Skip("neither zsh nor bash is installed")
	}
	return out
}

func TestFunction(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: skipped with -short")
	}
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			s := startShell(t, sh.argv, "HISTFILE="+filepath.Join(t.TempDir(), "hist"))
			s.line(sh.init, prompt)
			dir := t.TempDir()
			real, _ := filepath.EvalSymlinks(dir)

			// cd changes the interactive shell's own directory.
			s.line(`IHG_TEST_PICK='run:cd `+dir+`' ihg`, prompt)
			s.line(`echo "CWD=$(pwd -P)"`, "CWD="+real)

			// Aliases defined in the shell are honoured (no alias re-parsing).
			s.line(`alias ihghi='echo ALIAS-$((40+2))'`, prompt)
			s.line(`IHG_TEST_PICK='run:ihghi' ihg`, "ALIAS-42")

			// export persists in the shell.
			s.line(`IHG_TEST_PICK='run:export IHGFOO=bar$((1+1))' ihg`, prompt)
			s.line(`echo "FOO=$IHGFOO"`, "FOO=bar2")

			// The command is recorded in history exactly once, as itself.
			s.line(`echo "HIST=$(`+sh.histCmd+` | grep -cxF 'export IHGFOO=bar$((1+1))')"`, "HIST=1")

			// Piped stdin reaches the chosen command.
			s.line(`printf 'a\nb\n' | IHG_TEST_PICK='run:sed s/^/PIPED-/' ihg`, "PIPED-b")

			// The exit status of the chosen command is returned.
			s.line(`IHG_TEST_PICK='run:(exit 7)' ihg; echo "RC=$((100+$?))"`, "RC=107")

			// "none" (user cancelled) does nothing and succeeds.
			s.line(`IHG_TEST_PICK='none:' ihg; echo "RC=$((200+$?))"`, "RC=200")

			// Edit: zsh puts it on the next prompt; bash >= 4 pre-fills read -e.
			if sh.name == "bash3" {
				return
			}
			s.send(`IHG_TEST_PICK='edit:echo EDITED-$((2+3))' ihg` + "\r")
			s.expect(`echo EDITED-$((2+3))`) // the pre-filled editable line
			s.send("\r")
			s.expect("EDITED-5")
		})
	}
}

func TestWidget(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: skipped with -short")
	}
	for _, sh := range shells(t) {
		if !sh.widgets {
			continue
		}
		t.Run(sh.name, func(t *testing.T) {
			s := startShell(t, sh.argv, "HISTFILE="+filepath.Join(t.TempDir(), "hist"), "IHG_BIND_CTRL_R=1")
			s.line(sh.init, prompt)

			// Run: Ctrl-R picks and the shell executes it via accept-line.
			s.line(`export IHG_TEST_PICK='run:echo WIDGET-$((3+4))'`, prompt)
			s.send("\x12")
			s.expect("WIDGET-7")
			s.expect(prompt)

			// Edit: Ctrl-R only fills the line; Enter then runs it.
			s.line(`export IHG_TEST_PICK='edit:echo WEDIT-$((1+1))'`, prompt)
			s.send("\x12")
			s.expect(`echo WEDIT-$((1+1))`)
			s.send("\r")
			s.expect("WEDIT-2")
			s.expect(prompt)

			// The widget-run command went through normal history recording.
			s.line(`unset IHG_TEST_PICK; echo "HIST=$(`+sh.histCmd+` | grep -cxF 'echo WIDGET-$((3+4))')"`, "HIST=1")
		})
	}
}
