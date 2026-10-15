package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func newTestSource(t *testing.T, body string) *source {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".zsh_history")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"IHG_HISTFILE": path, "IHG_SHELL": "zsh"}
	s, err := newSource(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSourceLoadTimesAreNewestPerCommand(t *testing.T) {
	s := newTestSource(t, ": 100:0;make test\n: 200:0;git push\n: 300:0;make test\n: 400:0;ls\n: 500:0;ihg make\n")
	cmds, times, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	// Newest first; "ls" is trivial and "ihg make" is ihg itself.
	if want := []string{"make test", "git push"}; !slices.Equal(cmds, want) {
		t.Errorf("cmds = %q, want %q", cmds, want)
	}
	if want := []int64{300, 200}; !slices.Equal(times, want) {
		t.Errorf("times = %v, want %v", times, want)
	}
}

func TestSourceReloadOnlyOnChange(t *testing.T) {
	s := newTestSource(t, ": 1:0;git status\n")
	if _, _, err := s.load(); err != nil {
		t.Fatal(err)
	}
	if _, _, changed, err := s.reload(); changed || err != nil {
		t.Fatalf("unchanged file: changed=%v err=%v", changed, err)
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(": 2:0;docker ps\n")
	f.Close()
	cmds, _, changed, err := s.reload()
	if !changed || err != nil || !slices.Equal(cmds, []string{"docker ps", "git status"}) {
		t.Fatalf("after append: changed=%v err=%v cmds=%q", changed, err, cmds)
	}

	// ihg's own delete must not look like an outside change.
	if n, err := s.delete("docker ps"); n != 1 || err != nil {
		t.Fatalf("delete: n=%d err=%v", n, err)
	}
	if _, _, changed, _ := s.reload(); changed {
		t.Error("reload reported a change after our own delete")
	}
}

func TestSourceBackup(t *testing.T) {
	s := newTestSource(t, ": 1:0;git status\n: 2:0;ls\n: 3:0;make\n: 4:0;git status\n")
	if _, _, err := s.load(); err != nil {
		t.Fatal(err)
	}

	path, err := s.backup("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, s.path+".ihg-clean.") {
		t.Errorf("default backup path = %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Oldest first, deduplicated, trivial "ls" dropped, zsh format kept.
	body := string(data)
	if !strings.Contains(body, ";make\n") || strings.Count(body, "git status") != 1 || strings.Contains(body, ";ls\n") ||
		strings.Index(body, "make") > strings.Index(body, "git status") {
		t.Errorf("backup contents = %q", body)
	}

	dest := filepath.Join(t.TempDir(), "sub", "clean")
	if got, err := s.backup(dest); err != nil || got != dest {
		t.Errorf("backup(dest) = %q, %v", got, err)
	}
}
