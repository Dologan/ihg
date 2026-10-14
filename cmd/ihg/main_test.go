package main

import (
	"bytes"
	"strings"
	"testing"
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
			code := run(tt.args, &out, &errOut)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("stdout = %q, want it to contain %q", out.String(), tt.wantOut)
			}
		})
	}
}
