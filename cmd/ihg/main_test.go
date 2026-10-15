package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Dologan/ihg/internal/result"
	"github.com/Dologan/ihg/internal/ui"
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

func TestNewSourceNeedsHistfile(t *testing.T) {
	_, err := newSource(func(string) string { return "" })
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
	src, err := newSource(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := src.load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ihgrep foo", "git status"}
	if !slices.Equal(got, want) {
		t.Errorf("loadCommands = %q, want %q", got, want)
	}
}

func TestOptionsMouseAndDefaults(t *testing.T) {
	tests := []struct {
		args       []string
		wantMouse  bool
		wantHeight ui.Height
	}{
		{nil, false, ui.Height{Percent: 40}},
		{[]string{"--mouse"}, true, ui.Height{Percent: 40}},
		// IHG_DEFAULT_OPTS comes first, so command-line flags override it.
		{[]string{"--height", "100%", "--mouse", "--no-mouse"}, false, ui.Height{Percent: 100}},
		{[]string{"--no-mouse", "--mouse", "--height=10"}, true, ui.Height{Lines: 10}},
	}
	for _, tt := range tests {
		o, err := parseOptions(tt.args)
		if err != nil {
			t.Fatalf("parseOptions(%q): %v", tt.args, err)
		}
		if o.mouse != tt.wantMouse || o.height != tt.wantHeight {
			t.Errorf("parseOptions(%q) = mouse %v height %+v, want %v %+v",
				tt.args, o.mouse, o.height, tt.wantMouse, tt.wantHeight)
		}
	}
}
