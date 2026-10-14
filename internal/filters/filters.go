package filters

import (
	"strings"
	"unicode"
)

// DefaultIgnore contains the default set of trivial single-token command names to ignore.
var DefaultIgnore = map[string]bool{
	"ls":      true,
	"ll":      true,
	"la":      true,
	"l":       true,
	"pwd":     true,
	"cd":      true,
	"clear":   true,
	"cls":     true,
	"exit":    true,
	"logout":  true,
	"fg":      true,
	"bg":      true,
	"jobs":    true,
	"history": true,
	"fc":      true,
	"dirs":    true,
	"pushd":   true,
	"popd":    true,
	"hash":    true,
	"type":    true,
	"which":   true,
	"whoami":  true,
	"date":    true,
	"uptime":  true,
	"echo":    true,
}

// IgnoreWords merges the default ignore set with extra comma-separated words.
// Extra words are trimmed and lowercased; empty parts are skipped.
func IgnoreWords(extra string) map[string]bool {
	result := make(map[string]bool)
	for k, v := range DefaultIgnore {
		result[k] = v
	}

	if extra == "" {
		return result
	}

	parts := strings.Split(extra, ",")
	for _, part := range parts {
		word := strings.TrimSpace(part)
		if word != "" {
			result[strings.ToLower(word)] = true
		}
	}

	return result
}

// splitShellWords performs POSIX-style shell word splitting.
// Returns nil on unbalanced quotes or other parsing errors.
// Handles single quotes (no expansion), double quotes (with backslash escapes),
// and backslash escapes outside quotes.
func splitShellWords(s string) []string {
	var words []string
	var current strings.Builder
	i := 0

	for i < len(s) {
		ch := s[i]

		// Single quote: everything literal until closing quote
		if ch == '\'' {
			i++
			for i < len(s) && s[i] != '\'' {
				current.WriteByte(s[i])
				i++
			}
			if i >= len(s) {
				// Unbalanced quote
				return nil
			}
			i++ // skip closing quote
			continue
		}

		// Double quote: allow backslash escapes
		if ch == '"' {
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					// Backslash escape in double quotes
					i++
					current.WriteByte(s[i])
					i++
				} else {
					current.WriteByte(s[i])
					i++
				}
			}
			if i >= len(s) {
				// Unbalanced quote
				return nil
			}
			i++ // skip closing quote
			continue
		}

		// Backslash outside quotes: escape next character
		if ch == '\\' && i+1 < len(s) {
			i++
			current.WriteByte(s[i])
			i++
			continue
		}

		// Whitespace: word boundary
		if unicode.IsSpace(rune(ch)) {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			i++
			continue
		}

		// Regular character
		current.WriteByte(ch)
		i++
	}

	if current.Len() > 0 {
		words = append(words, current.String())
	}

	return words
}

// IsTrivialSingleToken reports whether cmd is a trivial single-token command.
// A command is trivial if it:
//   - is empty or contains newline (false)
//   - contains pipe, semicolon, &&, ||, >, or < (false)
//   - does not split to exactly one shell word (false)
//   - the command basename (after last /) lowercased is in the ignore set (true)
func IsTrivialSingleToken(cmd string, ignore map[string]bool) bool {
	// Check for newline before trimming
	s := strings.TrimSpace(cmd)
	if s == "" || strings.Contains(s, "\n") {
		return false
	}

	// Check for shell operators
	if strings.ContainsAny(s, "|;><") {
		return false
	}
	// Check for && and ||
	if strings.Contains(s, "&&") || strings.Contains(s, "||") {
		return false
	}

	// Split into shell words
	parts := splitShellWords(s)
	if parts == nil || len(parts) != 1 {
		return false
	}

	word := parts[0]
	// Get basename after last /
	idx := strings.LastIndex(word, "/")
	if idx >= 0 {
		word = word[idx+1:]
	}
	word = strings.ToLower(word)

	return ignore[word]
}

// DedupeNewestFirst returns commands with duplicates removed, keeping first occurrence.
// Order is preserved.
func DedupeNewestFirst(cmds []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, cmd := range cmds {
		if !seen[cmd] {
			seen[cmd] = true
			result = append(result, cmd)
		}
	}

	return result
}

// BuildProcessed deduplicates commands (newest-first) and removes trivial single-token commands.
// The ignore set determines which single-token commands to filter out.
func BuildProcessed(newestFirst []string, ignore map[string]bool) []string {
	deduped := DedupeNewestFirst(newestFirst)
	var result []string

	for _, cmd := range deduped {
		if !IsTrivialSingleToken(cmd, ignore) {
			result = append(result, cmd)
		}
	}

	return result
}
