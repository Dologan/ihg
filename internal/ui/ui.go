// Package ui implements the interactive picker: a Bubble Tea v2 program that
// lets the user fuzzy-search shell history and choose what to do with a
// command (run it, edit it, or cancel).
package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Dologan/ihg/internal/result"
)

// Layout selects where the prompt sits relative to the list.
type Layout int

const (
	// LayoutDefault is fzf's default: prompt at the bottom, best match just
	// above it.
	LayoutDefault Layout = iota
	// LayoutReverse puts the prompt at the top with the best match just below.
	LayoutReverse
)

// minLines is the smallest usable UI: one list row, the info line, the prompt.
const minLines = 3

// Height says how much of the terminal to use. Exactly one field is non-zero.
type Height struct {
	Percent int // 1..100; 100 means full screen (alternate screen)
	Lines   int // absolute number of lines
}

// ParseHeight parses "40%", "100%" or "15" (lines).
func ParseHeight(s string) (Height, error) {
	t := strings.TrimSpace(s)
	if pct, ok := strings.CutSuffix(t, "%"); ok {
		n, err := strconv.Atoi(pct)
		if err != nil || n < 1 || n > 100 {
			return Height{}, fmt.Errorf("invalid height %q: percentage must be 1-100", s)
		}
		return Height{Percent: n}, nil
	}
	n, err := strconv.Atoi(t)
	if err != nil || n < 1 {
		return Height{}, fmt.Errorf("invalid height %q: want N or N%%", s)
	}
	return Height{Lines: n}, nil
}

// full reports whether the height means the whole screen.
func (h Height) full() bool { return h.Percent >= 100 }

// resolve returns the number of lines to use on a terminal with rows rows.
func (h Height) resolve(rows int) int {
	n := rows
	switch {
	case h.Percent > 0:
		n = rows * h.Percent / 100
	case h.Lines > 0:
		n = h.Lines
	}
	n = min(n, rows)
	return max(n, minLines)
}

// Config configures a picker session.
type Config struct {
	Commands []string // newest first, already deduped/filtered; index = recency rank
	Query    string   // initial query
	Height   Height   // how much of the terminal to use
	Layout   Layout
}

// Result is what the user chose. Action is result.None when cancelled.
type Result struct {
	Action  result.Action
	Command string
}

// Run shows the picker with input and output on tty and returns the choice.
// It runs inline (no alternate screen) unless the height is 100%.
func Run(cfg Config, tty *os.File) (Result, error) {
	p := tea.NewProgram(New(cfg), tea.WithInput(tty), tea.WithOutput(tty))
	fm, err := p.Run()
	m, ok := fm.(Model)
	if err != nil || !ok {
		if err == nil {
			err = fmt.Errorf("unexpected final model %T", fm)
		}
		return Result{Action: result.None}, err
	}
	return m.Result(), nil
}
