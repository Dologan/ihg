package clipboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestOSC52Basic(t *testing.T) {
	tests := []struct {
		name string
		text string
		tmux bool
		want string
	}{
		{
			name: "simple text without tmux",
			text: "hi",
			tmux: false,
			want: "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hi")) + "\x07",
		},
		{
			name: "non-ASCII text without tmux",
			text: "こんにちは",
			tmux: false,
			want: "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("こんにちは")) + "\x07",
		},
		{
			name: "simple text with tmux",
			text: "hi",
			tmux: true,
			want: "\x1bPtmux;\x1b\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hi")) + "\x07\x1b\\",
		},
		{
			name: "text containing ESC with tmux",
			text: "test\x1bdata",
			tmux: true,
			// The inner sequence contains \x1b which should be doubled
			want: "\x1bPtmux;\x1b\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("test\x1bdata")) + "\x07\x1b\\",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OSC52(tt.text, tt.tmux)
			if string(got) != tt.want {
				t.Errorf("OSC52(%q, %v) = %q, want %q", tt.text, tt.tmux, string(got), tt.want)
			}
		})
	}
}

func TestOSC52Format(t *testing.T) {
	// Test the exact format of OSC52 without tmux
	text := "hello"
	got := OSC52(text, false)
	gotStr := string(got)

	// Should start with ESC]52;c;
	if !strings.HasPrefix(gotStr, "\x1b]52;c;") {
		t.Errorf("OSC52 should start with ESC]52;c;, got %q", gotStr)
	}

	// Should end with BEL
	if !strings.HasSuffix(gotStr, "\x07") {
		t.Errorf("OSC52 should end with BEL (\\x07), got %q", gotStr)
	}

	// Extract and verify base64 content
	parts := strings.Split(gotStr, ";")
	if len(parts) < 3 {
		t.Errorf("OSC52 format incorrect: %q", gotStr)
	}
	encoded := strings.TrimPrefix(parts[2], "\x1b]52;c;")
	encoded = strings.TrimSuffix(encoded, "\x07")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Errorf("Failed to decode base64: %v", err)
	}
	if string(decoded) != text {
		t.Errorf("Decoded text %q != original %q", string(decoded), text)
	}
}

func TestOSC52TmuxWrapping(t *testing.T) {
	text := "test"
	got := OSC52(text, true)
	gotStr := string(got)

	// Should start with tmux escape
	if !strings.HasPrefix(gotStr, "\x1bPtmux;") {
		t.Errorf("Tmux OSC52 should start with \\x1bPtmux;, got %q", gotStr)
	}

	// Should end with tmux terminator
	if !strings.HasSuffix(gotStr, "\x1b\\") {
		t.Errorf("Tmux OSC52 should end with \\x1b\\\\, got %q", gotStr)
	}

	// Verify inner sequence with doubled ESC
	inner := strings.TrimPrefix(gotStr, "\x1bPtmux;")
	inner = strings.TrimSuffix(inner, "\x1b\\")
	if strings.Contains(inner, "\x1b]") {
		// The ESC in ]52 should be part of the doubled escape
		parts := strings.Split(inner, "\x1b\x1b]")
		if len(parts) < 2 {
			t.Errorf("Expected doubled ESC in tmux sequence, got %q", inner)
		}
	}
}

func TestCopyWithToolsOrder(t *testing.T) {
	tests := []struct {
		name          string
		goos          string
		wayland       bool
		display       bool
		lookupResults map[string]bool // tool name -> found
		expectedOrder []string        // tools actually attempted (found and run)
	}{
		{
			name:          "darwin only",
			goos:          "darwin",
			wayland:       false,
			display:       false,
			lookupResults: map[string]bool{"pbcopy": true},
			expectedOrder: []string{"pbcopy"},
		},
		{
			name:          "darwin with wayland, pbcopy not found",
			goos:          "darwin",
			wayland:       true,
			display:       false,
			lookupResults: map[string]bool{"pbcopy": false, "wl-copy": true},
			expectedOrder: []string{"wl-copy"},
		},
		{
			name:          "linux with wayland",
			goos:          "linux",
			wayland:       true,
			display:       false,
			lookupResults: map[string]bool{"wl-copy": true},
			expectedOrder: []string{"wl-copy"},
		},
		{
			name:          "linux with display only, xclip fails",
			goos:          "linux",
			wayland:       false,
			display:       true,
			lookupResults: map[string]bool{"xclip": true, "xsel": true},
			expectedOrder: []string{"xclip", "xsel"},
		},
		{
			name:          "all tools available",
			goos:          "darwin",
			wayland:       true,
			display:       true,
			lookupResults: map[string]bool{"pbcopy": true, "wl-copy": true, "xclip": true, "xsel": true},
			expectedOrder: []string{"pbcopy", "wl-copy", "xclip", "xsel"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attemptedOrder []string
			env := Env{
				GOOS:    tt.goos,
				Wayland: tt.wayland,
				Display: tt.display,
				LookPath: func(name string) (string, error) {
					if tt.lookupResults[name] {
						return name, nil
					}
					return "", fmt.Errorf("not found")
				},
				Run: func(ctx context.Context, name string, args []string, stdin string) error {
					attemptedOrder = append(attemptedOrder, name)
					// All tools fail
					return fmt.Errorf("tool failed")
				},
			}

			_, err := CopyWithTools(context.Background(), "test", env)
			if err == nil {
				t.Errorf("Expected error when all tools fail")
			}

			if !slicesEqual(attemptedOrder, tt.expectedOrder) {
				t.Errorf("Tool attempt order: got %v, want %v", attemptedOrder, tt.expectedOrder)
			}
		})
	}
}

func TestCopyWithToolsSuccess(t *testing.T) {
	tests := []struct {
		name           string
		goos           string
		wayland        bool
		display        bool
		successToolIdx int // which tool in the attempted order should succeed
		expectTool     string
	}{
		{
			name:           "pbcopy success",
			goos:           "darwin",
			wayland:        false,
			display:        false,
			successToolIdx: 0, // pbcopy
			expectTool:     "pbcopy",
		},
		{
			name:           "wl-copy success",
			goos:           "linux",
			wayland:        true,
			display:        false,
			successToolIdx: 0, // wl-copy
			expectTool:     "wl-copy",
		},
		{
			name:           "xclip success",
			goos:           "linux",
			wayland:        false,
			display:        true,
			successToolIdx: 0, // xclip is first when no wayland
			expectTool:     "xclip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attemptCount := 0
			env := Env{
				GOOS:    tt.goos,
				Wayland: tt.wayland,
				Display: tt.display,
				LookPath: func(name string) (string, error) {
					return name, nil // all tools found
				},
				Run: func(ctx context.Context, name string, args []string, stdin string) error {
					// Succeed on the indexed tool
					if attemptCount == tt.successToolIdx {
						attemptCount++
						return nil
					}
					attemptCount++
					return fmt.Errorf("tool failed")
				},
			}

			tool, err := CopyWithTools(context.Background(), "test", env)
			if tool != tt.expectTool {
				t.Errorf("Expected %q to succeed, got %q, err=%v", tt.expectTool, tool, err)
			}
			if err != nil {
				t.Errorf("Expected no error, got %v", err)
			}
		})
	}
}

func TestCopyWithToolsTimeout(t *testing.T) {
	env := Env{
		GOOS: "darwin",
		LookPath: func(name string) (string, error) {
			return name, nil
		},
		Run: func(ctx context.Context, name string, args []string, stdin string) error {
			// Simulate timeout by checking context
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Otherwise simulate a hang that should be caught
			<-ctx.Done()
			return ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Immediately cancel

	tool, err := CopyWithTools(ctx, "test", env)
	if tool != "" {
		t.Errorf("Expected empty tool on timeout, got %q", tool)
	}
	if err == nil {
		t.Errorf("Expected error on timeout")
	}
}

func TestCopyWithToolsNotFound(t *testing.T) {
	env := Env{
		GOOS:    "linux",
		Wayland: false,
		Display: false,
		LookPath: func(name string) (string, error) {
			return "", fmt.Errorf("not found")
		},
		Run: func(ctx context.Context, name string, args []string, stdin string) error {
			return nil
		},
	}

	tool, err := CopyWithTools(context.Background(), "test", env)
	if tool != "" {
		t.Errorf("Expected empty tool when no tools found, got %q", tool)
	}
	if err == nil {
		t.Errorf("Expected error when no tools found")
	}
	if !strings.Contains(err.Error(), "no clipboard tool found") {
		t.Errorf("Expected 'no clipboard tool found' in error, got %v", err)
	}
}

func TestCopy(t *testing.T) {
	tests := []struct {
		name            string
		ttyNil          bool
		toolSucceeds    bool
		expectedMethods []string
		expectError     bool
	}{
		{
			name:            "OSC52 only (tty nil, tool fails)",
			ttyNil:          true,
			toolSucceeds:    false,
			expectedMethods: nil,
			expectError:     true,
		},
		{
			name:            "tool only (tty nil, tool succeeds)",
			ttyNil:          true,
			toolSucceeds:    true,
			expectedMethods: []string{"pbcopy"},
			expectError:     false,
		},
		{
			name:            "both OSC52 and tool",
			ttyNil:          false,
			toolSucceeds:    true,
			expectedMethods: []string{"osc52", "pbcopy"},
			expectError:     false,
		},
		{
			name:            "OSC52 only (tty present, tool fails)",
			ttyNil:          false,
			toolSucceeds:    false,
			expectedMethods: []string{"osc52"},
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tty io.Writer
			if !tt.ttyNil {
				tty = &bytes.Buffer{}
			}

			env := Env{
				GOOS: "darwin",
				LookPath: func(name string) (string, error) {
					return name, nil
				},
				Run: func(ctx context.Context, name string, args []string, stdin string) error {
					if tt.toolSucceeds {
						return nil
					}
					return fmt.Errorf("tool failed")
				},
			}

			methods, err := Copy(context.Background(), tty, "test", env, false)

			if (err != nil) != tt.expectError {
				t.Errorf("Copy error: got %v, expectError=%v", err, tt.expectError)
			}

			if !slicesEqual(methods, tt.expectedMethods) {
				t.Errorf("Copy methods: got %v, want %v", methods, tt.expectedMethods)
			}

			if tty != nil && !tt.ttyNil {
				buf := tty.(*bytes.Buffer)
				if buf.Len() == 0 {
					t.Errorf("Expected OSC52 written to tty")
				}
			}
		})
	}
}

func TestCopyOSC52WithTmux(t *testing.T) {
	buf := &bytes.Buffer{}
	env := Env{
		GOOS: "linux",
		LookPath: func(name string) (string, error) {
			return "", fmt.Errorf("not found")
		},
	}

	methods, err := Copy(context.Background(), buf, "test", env, true)

	// Should have written tmux-wrapped OSC52
	if err != nil {
		t.Errorf("Expected no error when OSC52 succeeds, got %v", err)
	}

	if !contains(methods, "osc52") {
		t.Errorf("Expected 'osc52' in methods, got %v", methods)
	}

	// Verify tmux wrapping
	osc := buf.String()
	if !strings.HasPrefix(osc, "\x1bPtmux;") {
		t.Errorf("Expected tmux wrapper in OSC52, got %q", osc)
	}
}

// Helper functions
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}
