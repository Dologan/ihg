package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// lineEdit is a one-line text buffer with a cursor, shared by the filter text
// and the in-window command editor. Methods return a new value and never
// modify the receiver's backing array, so copies of a Model stay valid.
type lineEdit struct {
	buf []rune
	pos int // cursor position in runes, 0..len(buf)
}

func newLineEdit(s string) lineEdit {
	rs := []rune(s)
	return lineEdit{buf: rs, pos: len(rs)}
}

func (e lineEdit) String() string { return string(e.buf) }

// insert inserts rs at the cursor.
func (e lineEdit) insert(rs []rune) lineEdit {
	if len(rs) == 0 {
		return e
	}
	buf := make([]rune, 0, len(e.buf)+len(rs))
	buf = append(buf, e.buf[:e.pos]...)
	buf = append(buf, rs...)
	buf = append(buf, e.buf[e.pos:]...)
	return lineEdit{buf: buf, pos: e.pos + len(rs)}
}

// deleteRange removes buf[from:to] (clamped) and puts the cursor at from.
func (e lineEdit) deleteRange(from, to int) lineEdit {
	from, to = max(from, 0), min(to, len(e.buf))
	if from >= to {
		return e
	}
	return lineEdit{buf: slices.Concat(e.buf[:from], e.buf[to:]), pos: from}
}

// handleKey applies the cursor-movement and deletion keys common to both
// editors. ok is false when the key is not one of them. Keys whose meaning
// depends on the editor (Left at 0, Right at the end, Ctrl-D, Ctrl-K, Enter,
// Esc) are left to the caller.
func (e lineEdit) handleKey(keystroke string) (_ lineEdit, ok bool) {
	switch keystroke {
	case "left":
		e.pos = max(e.pos-1, 0)
	case "right":
		e.pos = min(e.pos+1, len(e.buf))
	case "ctrl+f":
		e.pos = min(e.pos+1, len(e.buf))
	case "ctrl+b":
		e.pos = max(e.pos-1, 0)
	case "alt+b", "alt+left":
		e.pos = wordBack(e.buf, e.pos)
	case "alt+f", "alt+right":
		e.pos = wordForward(e.buf, e.pos)
	case "ctrl+a", "home":
		e.pos = 0
	case "ctrl+e", "end":
		e.pos = len(e.buf)
	case "backspace", "ctrl+h":
		e = e.deleteRange(e.pos-1, e.pos)
	case "delete":
		e = e.deleteRange(e.pos, e.pos+1)
	case "ctrl+u":
		e = e.deleteRange(0, e.pos)
	case "ctrl+w", "alt+backspace", "ctrl+alt+h":
		e = e.deleteRange(wordBack(e.buf, e.pos), e.pos)
	default:
		return e, false
	}
	return e, true
}

// keyRunes returns the printable text of an unmodified key press, or nil.
func keyRunes(msg tea.KeyPressMsg) []rune {
	k := msg.Key()
	if k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) != 0 || k.Text == "" {
		return nil
	}
	var in []rune
	for _, r := range k.Text {
		if r >= 0x20 && r != 0x7f {
			in = append(in, r)
		}
	}
	return in
}

// pasteRunes flattens pasted text for a single line: newlines and tabs become
// spaces, other control characters are dropped.
func pasteRunes(s string) []rune {
	var in []rune
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			in = append(in, ' ')
		case r < 0x20 || r == 0x7f:
		default:
			in = append(in, r)
		}
	}
	return in
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

// wordBack returns the start of the word before pos (skipping spaces first).
func wordBack(q []rune, pos int) int {
	for pos > 0 && isSpace(q[pos-1]) {
		pos--
	}
	for pos > 0 && !isSpace(q[pos-1]) {
		pos--
	}
	return pos
}

// wordForward returns the end of the word after pos (skipping spaces first).
func wordForward(q []rune, pos int) int {
	for pos < len(q) && isSpace(q[pos]) {
		pos++
	}
	for pos < len(q) && !isSpace(q[pos]) {
		pos++
	}
	return pos
}
