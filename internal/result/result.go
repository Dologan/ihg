// Package result handles writing and reading the chosen command from a temporary file.
package result

import (
	"bytes"
	"fmt"
	"os"
)

// Action represents the action to be taken with the command.
type Action string

const (
	// Run executes the command immediately.
	Run Action = "run"
	// Edit opens the command in an editor.
	Edit Action = "edit"
	// None represents no action (cancel).
	None Action = "none"
)

// Write writes the action and command to a file at the given path.
// The file is created or truncated with mode 0600.
// The format is: "<action>\n<command>" with no trailing newline added.
func Write(path string, a Action, command string) error {
	content := string(a) + "\n" + command
	return os.WriteFile(path, []byte(content), 0600)
}

// Read reads the action and command from a file at the given path.
// Returns an error if the action is not recognized.
func Read(path string) (Action, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("failed to read result file: %w", err)
	}

	// Split on first newline
	parts := bytes.SplitN(data, []byte("\n"), 2)
	if len(parts) < 1 {
		return "", "", fmt.Errorf("empty result file")
	}

	actionStr := string(parts[0])
	var action Action
	switch actionStr {
	case "run":
		action = Run
	case "edit":
		action = Edit
	case "none":
		action = None
	default:
		return "", "", fmt.Errorf("unknown action: %q", actionStr)
	}

	var command string
	if len(parts) > 1 {
		command = string(parts[1])
	}

	return action, command, nil
}
