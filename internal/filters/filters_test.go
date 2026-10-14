package filters

import (
	"testing"
)

func TestDefaultIgnore(t *testing.T) {
	expected := map[string]bool{
		"ls": true, "ll": true, "la": true, "l": true, "pwd": true,
		"cd": true, "clear": true, "cls": true, "exit": true, "logout": true,
		"fg": true, "bg": true, "jobs": true, "history": true, "fc": true,
		"dirs": true, "pushd": true, "popd": true, "hash": true, "type": true,
		"which": true, "whoami": true, "date": true, "uptime": true, "echo": true,
	}
	for k := range expected {
		if !DefaultIgnore[k] {
			t.Errorf("DefaultIgnore missing %q", k)
		}
	}
	for k := range DefaultIgnore {
		if !expected[k] {
			t.Errorf("DefaultIgnore has unexpected %q", k)
		}
	}
}

func TestIgnoreWordsWithoutExtra(t *testing.T) {
	ig := IgnoreWords("")
	if !ig["ls"] {
		t.Error("IgnoreWords should include defaults")
	}
	if !ig["pwd"] {
		t.Error("IgnoreWords should include pwd")
	}
}

func TestIgnoreWordsWithExtra(t *testing.T) {
	ig := IgnoreWords("foo,bar")
	if !ig["foo"] {
		t.Error("IgnoreWords should include foo")
	}
	if !ig["bar"] {
		t.Error("IgnoreWords should include bar")
	}
	if !ig["ls"] {
		t.Error("IgnoreWords should still include defaults")
	}
}

func TestIgnoreWordsWithSpacesAndCase(t *testing.T) {
	ig := IgnoreWords(" FOO , bar ")
	if !ig["foo"] {
		t.Error("IgnoreWords should lowercase and trim FOO to foo")
	}
	if !ig["bar"] {
		t.Error("IgnoreWords should trim and include bar")
	}
}

func TestIgnoreWordsWithEmpty(t *testing.T) {
	ig := IgnoreWords(",,")
	if len(ig) != len(DefaultIgnore) {
		t.Error("IgnoreWords should skip empty parts")
	}
}

func TestDedupeNewestFirst(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "no dupes",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with dupes",
			input:    []string{"a", "b", "a", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "first occurrence wins",
			input:    []string{"ls", "pwd", "ls"},
			expected: []string{"ls", "pwd"},
		},
		{
			name:     "empty",
			input:    []string{},
			expected: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DedupeNewestFirst(tc.input)
			if len(result) != len(tc.expected) {
				t.Errorf("got %v, want %v", result, tc.expected)
				return
			}
			for i, v := range result {
				if v != tc.expected[i] {
					t.Errorf("got %v, want %v", result, tc.expected)
					return
				}
			}
		})
	}
}

func TestIsTrivialSingleToken(t *testing.T) {
	ig := map[string]bool{"ls": true, "pwd": true, "cd": true}

	tests := []struct {
		name     string
		cmd      string
		ignore   map[string]bool
		expected bool
	}{
		{
			name:     "single trivial word",
			cmd:      "ls",
			ignore:   ig,
			expected: true,
		},
		{
			name:     "single trivial word with spaces",
			cmd:      "  ls  ",
			ignore:   ig,
			expected: true,
		},
		{
			name:     "single trivial word quoted",
			cmd:      "'ls'",
			ignore:   ig,
			expected: true,
		},
		{
			name:     "single non-trivial word",
			cmd:      "unknown",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "with arguments",
			cmd:      "ls -la",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "with path",
			cmd:      "/bin/ls",
			ignore:   ig,
			expected: true,
		},
		{
			name:     "with path and args",
			cmd:      "/bin/ls -la",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "empty string",
			cmd:      "",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "whitespace only",
			cmd:      "   ",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "trailing newline is trimmed first",
			cmd:      "ls\n",
			ignore:   ig,
			expected: true,
		},
		{
			name:     "embedded newline",
			cmd:      "ls\nls",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "with pipe",
			cmd:      "ls | grep",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "with semicolon",
			cmd:      "ls;pwd",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "with &&",
			cmd:      "ls && pwd",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "with ||",
			cmd:      "ls || pwd",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "with redirect >",
			cmd:      "ls > file",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "with redirect <",
			cmd:      "ls < file",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "unbalanced single quote",
			cmd:      "'ls",
			ignore:   ig,
			expected: false,
		},
		{
			name:     "unbalanced double quote",
			cmd:      `"ls`,
			ignore:   ig,
			expected: false,
		},
		{
			name:     "uppercase normalized to lowercase",
			cmd:      "LS",
			ignore:   ig,
			expected: true,
		},
		{
			name:     "uppercase not in ignore after normalization",
			cmd:      "UNKNOWN",
			ignore:   map[string]bool{"ls": true},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsTrivialSingleToken(tc.cmd, tc.ignore)
			if result != tc.expected {
				t.Errorf("got %v, want %v", result, tc.expected)
			}
		})
	}
}

func TestBuildProcessed(t *testing.T) {
	ig := map[string]bool{"ls": true, "pwd": true}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "basic dedup and filter",
			input:    []string{"dup", "uniq", "dup", "ls", "git status"},
			expected: []string{"dup", "uniq", "git status"},
		},
		{
			name:     "all trivial",
			input:    []string{"ls", "pwd", "ls"},
			expected: []string{},
		},
		{
			name:     "no trivial",
			input:    []string{"git status", "npm test"},
			expected: []string{"git status", "npm test"},
		},
		{
			name:     "empty input",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "preserves order",
			input:    []string{"git commit", "ls", "git log", "ls", "npm test"},
			expected: []string{"git commit", "git log", "npm test"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := BuildProcessed(tc.input, ig)
			if len(result) != len(tc.expected) {
				t.Errorf("got %v, want %v", result, tc.expected)
				return
			}
			for i, v := range result {
				if v != tc.expected[i] {
					t.Errorf("got %v, want %v", result, tc.expected)
					return
				}
			}
		})
	}
}

func TestShellWordSplitter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		valid    bool
	}{
		{
			name:     "single word",
			input:    "ls",
			expected: []string{"ls"},
			valid:    true,
		},
		{
			name:     "multiple words",
			input:    "ls -la",
			expected: []string{"ls", "-la"},
			valid:    true,
		},
		{
			name:     "quoted single word",
			input:    "'ls'",
			expected: []string{"ls"},
			valid:    true,
		},
		{
			name:     "double quoted word",
			input:    `"ls"`,
			expected: []string{"ls"},
			valid:    true,
		},
		{
			name:     "backslash escape",
			input:    `ls\ file`,
			expected: []string{"ls file"},
			valid:    true,
		},
		{
			name:     "double quote with escape",
			input:    `"ls \"test\""`,
			expected: []string{`ls "test"`},
			valid:    true,
		},
		{
			name:     "unbalanced single quote",
			input:    "'ls",
			expected: nil,
			valid:    false,
		},
		{
			name:     "unbalanced double quote",
			input:    `"ls`,
			expected: nil,
			valid:    false,
		},
		{
			name:     "mixed quotes",
			input:    `'ls' "pwd"`,
			expected: []string{"ls", "pwd"},
			valid:    true,
		},
		{
			name:     "word with spaces between quotes",
			input:    `ls   "my file"`,
			expected: []string{"ls", "my file"},
			valid:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := splitShellWords(tc.input)
			if !tc.valid {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if result == nil {
				t.Errorf("expected %v, got nil", tc.expected)
				return
			}
			if len(result) != len(tc.expected) {
				t.Errorf("got %v, want %v", result, tc.expected)
				return
			}
			for i, v := range result {
				if v != tc.expected[i] {
					t.Errorf("got %v, want %v", result, tc.expected)
					return
				}
			}
		})
	}
}
