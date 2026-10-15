// Package clipboard copies text to the system clipboard using OSC 52 and/or command-line tools.
package clipboard

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// OSC52 returns the OSC 52 escape sequence for copying text to the clipboard.
// The sequence format is: ESC ] 52 ; c ; <base64 text> BEL
// When tmux is true, wraps the sequence for tmux passthrough by escaping BEL as "\x1b\\"
// and doubling all ESC bytes within the sequence.
func OSC52(text string, tmux bool) []byte {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	if !tmux {
		return []byte("\x1b]52;c;" + encoded + "\x07")
	}

	// For tmux: wrap in "\x1bPtmux;" ... "\x1b\\" and double all ESC bytes
	inner := "\x1b]52;c;" + encoded + "\x07"
	doubled := strings.ReplaceAll(inner, "\x1b", "\x1b\x1b")
	return []byte("\x1bPtmux;" + doubled + "\x1b\\")
}

// Env encapsulates the environment for clipboard operations.
type Env struct {
	// GOOS is the operating system ("darwin", "linux", etc.)
	GOOS string
	// Wayland is true if WAYLAND_DISPLAY is set
	Wayland bool
	// Display is true if DISPLAY is set
	Display bool
	// LookPath searches for an executable in PATH (like exec.LookPath)
	LookPath func(string) (string, error)
	// Run executes a command with the given context, name, args, and stdin.
	// Stdout and Stderr should be left nil by the caller.
	Run func(ctx context.Context, name string, args []string, stdin string) error
}

// DefaultEnv returns an Env configured from the current runtime environment.
func DefaultEnv() Env {
	return Env{
		GOOS:     runtime.GOOS,
		Wayland:  os.Getenv("WAYLAND_DISPLAY") != "",
		Display:  os.Getenv("DISPLAY") != "",
		LookPath: exec.LookPath,
		Run: func(ctx context.Context, name string, args []string, stdin string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdin = strings.NewReader(stdin)
			// Leave Stdout and Stderr as nil
			return cmd.Run()
		},
	}
}

// ErrNoTool means no clipboard tool is available here (normal over SSH, where
// OSC 52 is the only way to reach the local clipboard).
var ErrNoTool = errors.New("no clipboard tool found")

// toolTimeout bounds each clipboard tool run.
const toolTimeout = 5 * time.Second

type tool struct {
	name string
	args []string
}

// tools returns the clipboard tools to try for env, in order.
func tools(env Env) []tool {
	var ts []tool
	if env.GOOS == "darwin" {
		ts = append(ts, tool{"pbcopy", nil})
	}
	if env.Wayland {
		ts = append(ts, tool{"wl-copy", nil})
	}
	if env.Display {
		ts = append(ts,
			tool{"xclip", []string{"-selection", "clipboard"}},
			tool{"xsel", []string{"--clipboard", "--input"}})
	}
	return ts
}

// CopyWithTools copies text with the first clipboard tool that works: pbcopy
// on macOS, then wl-copy under Wayland, then xclip and xsel under X11. It
// returns the name of the tool that succeeded, or an error listing what was
// tried. Each attempt is limited to toolTimeout.
func CopyWithTools(ctx context.Context, text string, env Env) (string, error) {
	var tried []string
	for _, t := range tools(env) {
		if _, err := env.LookPath(t.name); err != nil {
			continue
		}
		tried = append(tried, t.name)
		tctx, cancel := context.WithTimeout(ctx, toolTimeout)
		err := env.Run(tctx, t.name, t.args, text)
		cancel()
		if err == nil {
			return t.name, nil
		}
	}
	if len(tried) == 0 {
		return "", ErrNoTool
	}
	return "", fmt.Errorf("clipboard tools failed: %s", strings.Join(tried, ", "))
}

// Copy attempts to copy text to the clipboard using both OSC 52 and command-line tools.
// It always writes OSC 52 to tty (if tty is non-nil) and also tries CopyWithTools.
// The returned methods slice lists the successful methods: "osc52" and/or a tool name.
// err is non-nil only if both OSC 52 write and CopyWithTools failed.
// Note: OSC 52 success cannot be confirmed as terminals silently ignore it.
func Copy(ctx context.Context, tty io.Writer, text string, env Env, inTmux bool) (methods []string, err error) {
	if tty != nil {
		osc := OSC52(text, inTmux)
		if _, writeErr := tty.Write(osc); writeErr == nil {
			methods = append(methods, "osc52")
		}
	}

	tool, toolErr := CopyWithTools(ctx, text, env)
	if toolErr == nil {
		methods = append(methods, tool)
	}

	if len(methods) == 0 {
		return nil, toolErr
	}
	return methods, nil
}
