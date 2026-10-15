package ui

import "github.com/charmbracelet/x/ansi"

// sanitize converts a command to a displayable rune slice. The mapping is
// strictly 1:1 (one output rune per input rune) so that rune indices reported
// by the matcher remain valid. Invalid UTF-8 bytes become U+FFFD, exactly as
// the matcher's rune decoding sees them.
func sanitize(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			r = '↵'
		case r == '\t':
			r = ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			r = '·'
		}
		out = append(out, r)
	}
	return out
}

func runeWidth(r rune) int { return ansi.StringWidth(string(r)) }

func runesWidth(rs []rune) int { return ansi.StringWidth(string(rs)) }
