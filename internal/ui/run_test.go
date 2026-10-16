//go:build !windows

package ui

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/Dologan/ihg/internal/result"
)

// runPTY runs the picker on a pty, feeds input (each element sent after a
// short pause) and returns the result plus everything written to the terminal.
func runPTY(t *testing.T, cfg Config, input ...string) (Result, string) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer master.Close()
	if err := pty.Setsize(master, &pty.Winsize{Rows: 20, Cols: 60}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	var mu sync.Mutex
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			out.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	type ret struct {
		r   Result
		err error
	}
	done := make(chan ret, 1)
	go func() {
		r, err := Run(cfg, slave)
		done <- ret{r, err}
	}()

	time.Sleep(300 * time.Millisecond)
	for _, s := range input {
		_, _ = io.WriteString(master, s)
		time.Sleep(200 * time.Millisecond)
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Run: %v", r.err)
		}
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		slave.Close()
		return r.r, out.String()
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not finish")
	}
	return Result{}, ""
}

func TestRunPTYEnter(t *testing.T) {
	r, out := runPTY(t, Config{Commands: sample, Height: Height{Percent: 40}}, "mak", "\r")
	if r != (Result{Action: result.Run, Command: "make test"}) {
		t.Errorf("result = %+v", r)
	}
	last := bytes.LastIndex([]byte(out), []byte("make test"))
	if last < 0 {
		t.Fatalf("list was never drawn: %q", out)
	}
	if tail := out[last:]; !bytes.Contains([]byte(tail), []byte("\x1b[J")) && !bytes.Contains([]byte(tail), []byte("\x1b[0J")) {
		t.Errorf("no erase-below after the last frame: %q", tail)
	}
}

func TestRunPTYBareEscCancels(t *testing.T) {
	r, _ := runPTY(t, Config{Commands: sample, Height: Height{Percent: 100}}, "\x1b")
	if r.Action != result.None {
		t.Errorf("result = %+v", r)
	}
}

func TestRunPTYAltBIsNotEsc(t *testing.T) {
	// ESC b arrives together: must be alt+b (word move), not cancel + "b".
	r, out := runPTY(t, Config{Commands: sample, Query: "foo bar"}, "\x1bbX", "\x03")
	if r.Action != result.None {
		t.Errorf("result = %+v", r)
	}
	if i := bytes.LastIndex([]byte(out), []byte("> foo bar")); i < 0 || !bytes.Contains([]byte(out[i:]), []byte("X")) {
		t.Errorf("alt+b did not move back a word before inserting X: %q", out)
	}
}

func TestRunPTYTabEdits(t *testing.T) {
	r, _ := runPTY(t, Config{Commands: sample, EditShell: true}, "\t")
	if r != (Result{Action: result.Edit, Command: "git status"}) {
		t.Errorf("result = %+v", r)
	}
}
