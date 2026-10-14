package result

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWrite(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "result")

	err := Write(path, Run, "echo hello")
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify file mode
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("Expected file mode 0600, got %o", perm)
	}

	// Verify content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	expected := "run\necho hello"
	if string(data) != expected {
		t.Errorf("Expected content %q, got %q", expected, string(data))
	}
}

func TestWriteMultilineCommand(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "result")

	cmd := "echo hello\necho world"
	err := Write(path, Edit, cmd)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	expected := "edit\necho hello\necho world"
	if string(data) != expected {
		t.Errorf("Expected content %q, got %q", expected, string(data))
	}
}

func TestWriteEmptyCommand(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "result")

	err := Write(path, None, "")
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	expected := "none\n"
	if string(data) != expected {
		t.Errorf("Expected content %q, got %q", expected, string(data))
	}
}

func TestWriteNonUTF8Bytes(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "result")

	// Command with invalid UTF-8 bytes
	cmd := "test\x80\x81\x82"
	err := Write(path, Run, cmd)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	// Verify the non-UTF-8 bytes are preserved
	expected := "run\ntest\x80\x81\x82"
	if string(data) != expected {
		t.Errorf("Non-UTF-8 bytes not preserved: got %v, want %v", []byte(data), []byte(expected))
	}
}

func TestRead(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantA   Action
		wantCmd string
		wantErr bool
	}{
		{
			name:    "run action",
			content: "run\necho hello",
			wantA:   Run,
			wantCmd: "echo hello",
		},
		{
			name:    "edit action",
			content: "edit\necho world",
			wantA:   Edit,
			wantCmd: "echo world",
		},
		{
			name:    "none action",
			content: "none\n",
			wantA:   None,
			wantCmd: "",
		},
		{
			name:    "run with multiline command",
			content: "run\necho hello\necho world",
			wantA:   Run,
			wantCmd: "echo hello\necho world",
		},
		{
			name:    "unknown action",
			content: "invalid\necho hello",
			wantErr: true,
		},
		{
			name:    "empty file",
			content: "",
			wantErr: true,
		},
		{
			name:    "just newline",
			content: "\n",
			wantA:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			path := filepath.Join(tempDir, "result")

			err := os.WriteFile(path, []byte(tt.content), 0600)
			if err != nil {
				t.Fatalf("WriteFile failed: %v", err)
			}

			a, cmd, err := Read(path)

			if (err != nil) != tt.wantErr {
				t.Errorf("Read error: got %v, wantErr=%v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if a != tt.wantA {
					t.Errorf("Action: got %q, want %q", a, tt.wantA)
				}
				if cmd != tt.wantCmd {
					t.Errorf("Command: got %q, want %q", cmd, tt.wantCmd)
				}
			}
		})
	}
}

func TestReadNonUTF8Bytes(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "result")

	// Write file with invalid UTF-8 bytes
	content := []byte("run\ntest\x80\x81\x82")
	err := os.WriteFile(path, content, 0600)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	a, cmd, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if a != Run {
		t.Errorf("Action: got %q, want %q", a, Run)
	}

	expectedCmd := "test\x80\x81\x82"
	if cmd != expectedCmd {
		t.Errorf("Command with non-UTF-8 bytes not preserved: got %v, want %v", []byte(cmd), []byte(expectedCmd))
	}
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		action  Action
		command string
	}{
		{
			name:    "run simple",
			action:  Run,
			command: "echo hello",
		},
		{
			name:    "edit multiline",
			action:  Edit,
			command: "echo line1\necho line2\necho line3",
		},
		{
			name:    "none empty",
			action:  None,
			command: "",
		},
		{
			name:    "run with special chars",
			action:  Run,
			command: "echo 'hello world' && ls -la | grep test",
		},
		{
			name:    "command with newlines and special chars",
			action:  Edit,
			command: "for i in {1..5}; do\n  echo $i\ndone",
		},
		{
			name:    "non-UTF-8 bytes",
			action:  Run,
			command: "test\x80\x81\x82\xff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			path := filepath.Join(tempDir, "result")

			// Write
			err := Write(path, tt.action, tt.command)
			if err != nil {
				t.Fatalf("Write failed: %v", err)
			}

			// Read back
			a, cmd, err := Read(path)
			if err != nil {
				t.Fatalf("Read failed: %v", err)
			}

			// Verify round-trip
			if a != tt.action {
				t.Errorf("Action round-trip failed: got %q, want %q", a, tt.action)
			}
			if cmd != tt.command {
				t.Errorf("Command round-trip failed: got %v, want %v", []byte(cmd), []byte(tt.command))
			}
		})
	}
}

func TestWriteTruncatesExisting(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "result")

	// Write initial content
	err := Write(path, Run, "echo hello world")
	if err != nil {
		t.Fatalf("First write failed: %v", err)
	}

	// Write shorter content
	err = Write(path, Edit, "x")
	if err != nil {
		t.Fatalf("Second write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	expected := "edit\nx"
	if string(data) != expected {
		t.Errorf("File not truncated properly: got %q, want %q", string(data), expected)
	}
}

func TestFileNotFound(t *testing.T) {
	_, _, err := Read("/nonexistent/path/to/file")
	if err == nil {
		t.Errorf("Expected error for nonexistent file")
	}
}

func TestActionConstants(t *testing.T) {
	if Run != "run" {
		t.Errorf("Run constant incorrect: got %q", Run)
	}
	if Edit != "edit" {
		t.Errorf("Edit constant incorrect: got %q", Edit)
	}
	if None != "none" {
		t.Errorf("None constant incorrect: got %q", None)
	}
}
