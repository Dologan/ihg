package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Dologan/ihg/internal/result"
)

func TestRunVersionAndHelp(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
	}{
		{"version long", []string{"--version"}, 0, "ihg dev"},
		{"version short", []string{"-V"}, 0, "ihg dev"},
		{"help", []string{"--help"}, 0, "usage: ihg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(tt.args, func(string) string { return "" }, &out, &errOut)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("stdout = %q, want it to contain %q", out.String(), tt.wantOut)
			}
		})
	}
}

func TestParseOptions(t *testing.T) {
	tests := []struct {
		args      []string
		wantQuery string
		wantErr   bool
	}{
		{nil, "", false},
		{[]string{"git", "push"}, "git push", false},
		{[]string{"--height", "15", "docker"}, "docker", false},
		{[]string{"--layout=reverse", "--", "-v"}, "-v", false},
		{[]string{"--layout=sideways"}, "", true},
		{[]string{"--height=abc"}, "", true},
		{[]string{"--bogus"}, "", true},
	}
	for _, tt := range tests {
		o, err := parseOptions(tt.args)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseOptions(%q) err = %v, wantErr %v", tt.args, err, tt.wantErr)
			continue
		}
		if err == nil && o.query != tt.wantQuery {
			t.Errorf("parseOptions(%q) query = %q, want %q", tt.args, o.query, tt.wantQuery)
		}
	}
}

func TestDeliverWithoutWrapper(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := deliver("", result.Run, "git status", &out, &errOut); code != 0 || out.String() != "git status\n" {
		t.Errorf("run: code %d, stdout %q", code, out.String())
	}
	out.Reset()
	if code := deliver("", result.None, "", &out, &errOut); code != exitCancelled || out.Len() != 0 {
		t.Errorf("cancel: code %d, stdout %q", code, out.String())
	}
}

func TestLoadCommandsNeedsHistfile(t *testing.T) {
	_, err := loadCommands(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "HISTFILE") {
		t.Errorf("err = %v, want a HISTFILE hint", err)
	}
}

func TestLoadCommandsHidesSelfInvocations(t *testing.T) {
	hist := filepath.Join(t.TempDir(), "hist")
	if err := os.WriteFile(hist, []byte("git status\nihg git\nihg\nihgrep foo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"IHG_HISTFILE": hist, "IHG_SHELL": "bash"}
	got, err := loadCommands(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ihgrep foo", "git status"}
	if !slices.Equal(got, want) {
		t.Errorf("loadCommands = %q, want %q", got, want)
	}
}
