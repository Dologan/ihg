package groups

import (
	"testing"
)

func TestStepFirstWords(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		expected []string
	}{
		{
			name:     "single command",
			cmd:      "echo ok",
			expected: []string{"echo"},
		},
		{
			name:     "pipe",
			cmd:      "ls | grep x",
			expected: []string{"ls", "grep"},
		},
		{
			name:     "and",
			cmd:      "git pull && npm test",
			expected: []string{"git", "npm"},
		},
		{
			name:     "multiple pipes",
			cmd:      "cat file | grep pattern | wc -l",
			expected: []string{"cat", "grep", "wc"},
		},
		{
			name:     "mixed && and |",
			cmd:      "git pull && npm test | grep passed",
			expected: []string{"git", "npm", "grep"},
		},
		{
			name:     "empty string",
			cmd:      "",
			expected: []string{},
		},
		{
			name:     "whitespace only",
			cmd:      "   ",
			expected: []string{},
		},
		{
			name:     "empty parts between operators",
			cmd:      "ls || pwd",
			expected: []string{"ls", "pwd"},
		},
		{
			name:     "extra whitespace around operators",
			cmd:      "ls    |    grep    x",
			expected: []string{"ls", "grep"},
		},
		{
			name:     "duplicate steps",
			cmd:      "ls && ls",
			expected: []string{"ls"},
		},
		{
			name:     "path with command",
			cmd:      "/bin/ls | grep",
			expected: []string{"/bin/ls", "grep"},
		},
		{
			name:     "command with args",
			cmd:      "ls -la | grep x",
			expected: []string{"ls", "grep"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := StepFirstWords(tc.cmd)
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

func TestBuild(t *testing.T) {
	tests := []struct {
		name  string
		cmds  []string
		limit int
		check func(t *testing.T, groups []Group)
	}{
		{
			name:  "basic grouping",
			cmds:  []string{"ls | grep x", "other"},
			limit: 10,
			check: func(t *testing.T, groups []Group) {
				// Should have groups for "ls", "grep", "other"
				if len(groups) != 3 {
					t.Errorf("expected 3 groups, got %d", len(groups))
				}
				// Check ls group
				found := false
				for _, g := range groups {
					if g.Key == "ls" {
						if len(g.Members) != 1 || g.Members[0] != "ls | grep x" {
							t.Errorf("ls group has wrong members: %v", g.Members)
						}
						found = true
						break
					}
				}
				if !found {
					t.Error("ls group not found")
				}
				// Check grep group
				found = false
				for _, g := range groups {
					if g.Key == "grep" {
						if len(g.Members) != 1 || g.Members[0] != "ls | grep x" {
							t.Errorf("grep group has wrong members: %v", g.Members)
						}
						found = true
						break
					}
				}
				if !found {
					t.Error("grep group not found")
				}
			},
		},
		{
			name:  "command under multiple keys",
			cmds:  []string{"ls | grep foo"},
			limit: 10,
			check: func(t *testing.T, groups []Group) {
				cmd := "ls | grep foo"
				for _, g := range groups {
					if g.Key == "ls" || g.Key == "grep" {
						found := false
						for _, m := range g.Members {
							if m == cmd {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("command %q not in group %q", cmd, g.Key)
						}
					}
				}
			},
		},
		{
			name:  "limit parameter",
			cmds:  []string{"a", "b", "c", "d"},
			limit: 2,
			check: func(t *testing.T, groups []Group) {
				// Only first 2 commands should be processed
				if len(groups) != 2 {
					t.Errorf("expected 2 groups for limit=2, got %d", len(groups))
				}
			},
		},
		{
			name:  "zero limit",
			cmds:  []string{"a", "b"},
			limit: 0,
			check: func(t *testing.T, groups []Group) {
				if len(groups) != 0 {
					t.Errorf("expected 0 groups for limit=0, got %d", len(groups))
				}
			},
		},
		{
			name:  "negative limit",
			cmds:  []string{"a", "b"},
			limit: -1,
			check: func(t *testing.T, groups []Group) {
				if len(groups) != 0 {
					t.Errorf("expected 0 groups for limit=-1, got %d", len(groups))
				}
			},
		},
		{
			name:  "empty commands",
			cmds:  []string{},
			limit: 10,
			check: func(t *testing.T, groups []Group) {
				if len(groups) != 0 {
					t.Errorf("expected 0 groups for empty commands, got %d", len(groups))
				}
			},
		},
		{
			name:  "limit exceeds commands",
			cmds:  []string{"a", "b"},
			limit: 100,
			check: func(t *testing.T, groups []Group) {
				if len(groups) != 2 {
					t.Errorf("expected 2 groups, got %d", len(groups))
				}
			},
		},
		{
			name:  "case-folded sorting",
			cmds:  []string{"Zebra cmd", "apple cmd", "Banana cmd"},
			limit: 10,
			check: func(t *testing.T, groups []Group) {
				// Should be sorted by case-folded key: apple, Banana, Zebra
				if len(groups) != 3 {
					t.Errorf("expected 3 groups, got %d", len(groups))
					return
				}
				keys := []string{groups[0].Key, groups[1].Key, groups[2].Key}
				expected := []string{"apple", "Banana", "Zebra"}
				for i, k := range keys {
					if k != expected[i] {
						t.Errorf("sorting issue: got %v, want %v", keys, expected)
						break
					}
				}
			},
		},
		{
			name:  "duplicate commands",
			cmds:  []string{"ls | grep x", "ls | grep x"},
			limit: 10,
			check: func(t *testing.T, groups []Group) {
				// ls and grep groups should each have 2 members
				for _, g := range groups {
					if g.Key == "ls" || g.Key == "grep" {
						if len(g.Members) != 2 {
							t.Errorf("expected 2 members in %q group, got %d", g.Key, len(g.Members))
						}
					}
				}
			},
		},
		{
			name:  "no operators",
			cmds:  []string{"git status", "npm test"},
			limit: 10,
			check: func(t *testing.T, groups []Group) {
				// Should have 2 groups: git and npm
				if len(groups) != 2 {
					t.Errorf("expected 2 groups, got %d", len(groups))
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Build(tc.cmds, tc.limit)
			tc.check(t, result)
		})
	}
}

func TestBuildMembersOrder(t *testing.T) {
	// Members should appear in input order
	cmds := []string{"cmd1 | step2", "cmd2", "cmd1"}
	groups := Build(cmds, 10)

	// Find the "cmd1" group
	for _, g := range groups {
		if g.Key == "cmd1" {
			// Should have both "cmd1 | step2" and "cmd1"
			if len(g.Members) != 2 {
				t.Errorf("expected 2 members, got %d", len(g.Members))
				return
			}
			if g.Members[0] != "cmd1 | step2" || g.Members[1] != "cmd1" {
				t.Errorf("members order wrong: %v", g.Members)
				return
			}
			return
		}
	}
	t.Error("cmd1 group not found")
}
