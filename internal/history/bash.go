package history

import (
	"strconv"
	"strings"
)

// bashEntries parses bash history: optional "#<ts>" lines followed by the
// command, with space-prefixed continuation lines. Entry.Raw is the file
// region to remove when deleting that entry (including its timestamp line).
// Blank lines are kept as non-command entries so a rewrite only drops deleted
// entries.
func bashEntries(data []byte) []Entry {
	text := string(data)
	lines := strings.Split(text, "\n")
	lastHasNL := strings.HasSuffix(text, "\n")
	if lastHasNL {
		lines = lines[:len(lines)-1]
	}
	n := len(lines)

	var (
		blocks  []Entry
		pending *string
		ts      int64
		block   []string
	)
	flush := func() {
		if len(block) > 0 {
			cmd := ""
			if pending != nil {
				cmd = strings.TrimSpace(*pending)
			}
			blocks = append(blocks, Entry{
				Command:   cmd,
				Timestamp: ts,
				Raw:       []byte(strings.Join(block, "\n") + "\n"),
				IsCommand: cmd != "",
			})
		}
		pending, ts, block = nil, 0, nil
	}

	for i := 0; i < n; {
		line := lines[i]
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			if pending != nil {
				block = append(block, line)
			} else {
				blocks = append(blocks, Entry{Raw: []byte(line + "\n")})
			}
			i++
			continue
		}
		if digits := strings.TrimPrefix(stripped, "#"); digits != stripped && isDigits(digits) {
			flush()
			ts, _ = strconv.ParseInt(digits, 10, 64)
			block = []string{line}
			i++
			if i < n {
				block = append(block, lines[i])
				p := lines[i]
				pending = &p
				i++
			}
			continue
		}
		if strings.HasPrefix(line, " ") && pending != nil {
			block = append(block, line)
			p := *pending + "\n" + line
			pending = &p
			i++
			continue
		}
		flush()
		block = []string{line}
		p := line
		pending = &p
		i++
	}
	flush()
	if len(blocks) > 0 && !lastHasNL {
		last := &blocks[len(blocks)-1]
		last.Raw = last.Raw[:len(last.Raw)-1]
	}
	return blocks
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
