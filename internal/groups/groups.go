package groups

import (
	"regexp"
	"sort"
	"strings"
)

// Group represents a grouping of commands by a step-first-word key.
type Group struct {
	// Key is the first word of a step (command in a pipeline or chain).
	Key string
	// Members are the full command lines that contain this step-first-word.
	Members []string
}

var stepSplit = regexp.MustCompile(`\s*(?:&&|\|)\s*`)

// StepFirstWords returns the first word of each &&- or |-separated step in a command.
// Order is preserved, duplicates are dropped.
// For "ls | grep x", returns ["ls", "grep"].
// For "git pull && npm test", returns ["git", "npm"].
func StepFirstWords(cmd string) []string {
	s := strings.TrimSpace(cmd)
	if s == "" {
		return []string{}
	}

	parts := stepSplit.Split(s, -1)
	seen := make(map[string]bool)
	var result []string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Get the first whitespace-separated word
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}

		word := fields[0]
		if !seen[word] {
			seen[word] = true
			result = append(result, word)
		}
	}

	return result
}

// Build returns groups of commands organized by their step-first-words.
// Only the first `limit` commands are considered.
// Each command appears under every distinct step-first-word it contains.
// Groups are sorted by case-folded key.
func Build(cmds []string, limit int) []Group {
	if limit <= 0 {
		return []Group{}
	}

	if limit > len(cmds) {
		limit = len(cmds)
	}

	items := cmds[:limit]
	bucket := make(map[string][]string)

	for _, cmd := range items {
		for _, key := range StepFirstWords(cmd) {
			bucket[key] = append(bucket[key], cmd)
		}
	}

	// Convert to slice and sort by case-folded key
	result := make([]Group, 0, len(bucket))
	for key, members := range bucket {
		result = append(result, Group{
			Key:     key,
			Members: members,
		})
	}

	// Case-folded order; the raw key breaks ties so "Git" and "git" keep a
	// deterministic order despite map iteration being random.
	sort.Slice(result, func(i, j int) bool {
		a, b := strings.ToLower(result[i].Key), strings.ToLower(result[j].Key)
		if a != b {
			return a < b
		}
		return result[i].Key < result[j].Key
	})

	return result
}
