package history

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// zsh writes each entry "metafied": bytes 0x00 and 0x83-0xa2 are stored as
// 0x83 followed by (byte ^ 0x20). A newline inside a command is stored as a
// backslash-newline, and an entry ending in backslash(-spaces) gets one extra
// trailing space so it is not mistaken for a continuation. The code below
// mirrors zsh's readhistline / savehistfile (Src/hist.c).

const zshMeta = 0x83

var zshHead = regexp.MustCompile(`^: (\d+):(\d+);`)

func isIMeta(b byte) bool { return b == 0 || (b >= 0x83 && b <= 0xA2) }

// Metafy encodes b the way zsh stores it on disk.
func Metafy(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if isIMeta(c) {
			out = append(out, zshMeta, c^0x20)
		} else {
			out = append(out, c)
		}
	}
	return out
}

// Unmetafy reverses Metafy. A trailing lone 0x83 is kept as is.
func Unmetafy(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == zshMeta && i+1 < len(b) {
			out = append(out, b[i+1]^0x20)
			i++
		} else {
			out = append(out, b[i])
		}
	}
	return out
}

// IsZshExtended reports whether data is zsh EXTENDED_HISTORY format: at least
// max(1, n/4) of the first 40 non-blank lines carry a ": ts:elapsed;" header.
func IsZshExtended(data []byte) bool {
	var lines [][]byte
	for _, ln := range bytes.Split(data, []byte("\n")) {
		if len(bytes.Trim(ln, " \t\r\v\f")) > 0 {
			lines = append(lines, ln)
			if len(lines) == 40 {
				break
			}
		}
	}
	if len(lines) == 0 {
		return false
	}
	hits := 0
	for _, ln := range lines {
		if zshHead.Match(ln) {
			hits++
		}
	}
	return hits >= max(1, len(lines)/4)
}

// zshEntries splits a zsh history file into logical entries. In extended files
// header-less lines are non-commands kept verbatim; in plain files every
// logical line is a command.
func zshEntries(data []byte) []Entry {
	extended := IsZshExtended(data)
	lines := bytes.Split(data, []byte("\n"))
	lastHasNL := bytes.HasSuffix(data, []byte("\n"))
	if lastHasNL {
		lines = lines[:len(lines)-1] // Split leaves "" after the final newline
	}
	var out []Entry
	n := len(lines)
	for i := 0; i < n; {
		start := i
		buf := lines[i]
		i++
		// Backslash-newline continues the entry (only if a newline followed).
		for bytes.HasSuffix(buf, []byte("\\")) && i < n {
			nb := make([]byte, 0, len(buf)+len(lines[i]))
			nb = append(nb, buf[:len(buf)-1]...)
			nb = append(nb, '\n')
			nb = append(nb, lines[i]...)
			buf = nb
			i++
		}
		// Undo zsh's "trailing backslash + space" guard: drop one space.
		trimmed := bytes.TrimRight(buf, " ")
		if len(trimmed) != len(buf) && bytes.HasSuffix(trimmed, []byte("\\")) {
			buf = buf[:len(buf)-1]
		}
		raw := bytes.Join(lines[start:i], []byte("\n"))
		if i < n || lastHasNL {
			raw = append(raw, '\n')
		}
		e := Entry{Raw: raw}
		if m := zshHead.FindSubmatch(buf); m != nil {
			e.Command = strings.TrimSpace(string(Unmetafy(buf[len(m[0]):])))
			e.Timestamp, _ = strconv.ParseInt(string(m[1]), 10, 64)
		} else if !extended {
			e.Command = strings.TrimSpace(string(Unmetafy(buf)))
		}
		e.IsCommand = e.Command != ""
		out = append(out, e)
	}
	return out
}

// encodeZshEntry renders one zsh history entry. extended=false writes the
// plain (header-less) form and ignores ts.
func encodeZshEntry(command string, ts int64, extended bool) []byte {
	body := Metafy([]byte(command))
	body = bytes.ReplaceAll(body, []byte("\n"), []byte("\\\n"))
	if bytes.HasSuffix(bytes.TrimRight(body, " "), []byte("\\")) {
		body = append(body, ' ')
	}
	body = append(body, '\n')
	if !extended {
		return body
	}
	return append([]byte(": "+strconv.FormatInt(ts, 10)+":0;"), body...)
}
