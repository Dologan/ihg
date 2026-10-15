package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Dologan/ihg/internal/result"
)

func newMouseModel(layout Layout, height Height, w, h int) Model {
	m := New(Config{Commands: sample, Height: height, Layout: layout, Mouse: true})
	return feed(m, size(w, h))
}

func wheel(b tea.MouseButton) tea.MouseWheelMsg { return tea.MouseWheelMsg{Button: b} }

func click(y int) tea.MouseClickMsg { return tea.MouseClickMsg{Y: y, Button: tea.MouseLeft} }

func TestWheelMovesVisually(t *testing.T) {
	tests := []struct {
		name   string
		layout Layout
		btn    tea.MouseButton
		want   int // cursor (hit rank) after one step from rank 0
	}{
		{"default layout, wheel up goes to worse hits", LayoutDefault, tea.MouseWheelUp, 1},
		{"default layout, wheel down stays at best", LayoutDefault, tea.MouseWheelDown, 0},
		{"reverse layout, wheel down goes to worse hits", LayoutReverse, tea.MouseWheelDown, 1},
		{"reverse layout, wheel up stays at best", LayoutReverse, tea.MouseWheelUp, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := feed(newMouseModel(tt.layout, Height{Percent: 40}, 80, 24), wheel(tt.btn))
			if m.cursor != tt.want {
				t.Errorf("cursor = %d, want %d", m.cursor, tt.want)
			}
		})
	}
}

func TestMouseIgnoredUnlessEnabled(t *testing.T) {
	m := newModel(sample, 80, 24) // Mouse: false
	m = feed(m, wheel(tea.MouseWheelUp), click(0))
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 with mouse disabled", m.cursor)
	}
	if v := m.View(); v.MouseMode != tea.MouseModeNone {
		t.Errorf("MouseMode = %v, want none", v.MouseMode)
	}
	if v := newMouseModel(LayoutDefault, Height{Percent: 40}, 80, 24).View(); v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("MouseMode = %v, want cell motion when enabled", v.MouseMode)
	}
}

func TestClickSelectsRow(t *testing.T) {
	// Full screen 80x10: 8 list rows, then info and prompt.
	tests := []struct {
		name   string
		layout Layout
		y      int
		want   int
	}{
		{"default: line above info is the best hit", LayoutDefault, 7, 0},
		{"default: two lines higher", LayoutDefault, 5, 2},
		{"reverse: first row under prompt and info", LayoutReverse, 2, 0},
		{"reverse: third row", LayoutReverse, 4, 2},
		{"default: info line is not a row", LayoutDefault, 8, 0},
		{"default: blank row past the hits", LayoutDefault, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := feed(newMouseModel(tt.layout, Height{Percent: 100}, 80, 10), click(tt.y))
			if m.cursor != tt.want {
				t.Errorf("cursor = %d, want %d", m.cursor, tt.want)
			}
			if m.done {
				t.Error("a single click must not run the command")
			}
		})
	}
}

func TestClickIgnoredInline(t *testing.T) {
	m := feed(newMouseModel(LayoutDefault, Height{Percent: 40}, 80, 24), click(5))
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0: inline mode cannot map mouse rows", m.cursor)
	}
}

func TestDoubleClickRuns(t *testing.T) {
	now := time.Unix(1000, 0)
	m := newMouseModel(LayoutDefault, Height{Percent: 100}, 80, 10)
	m.now = func() time.Time { return now }

	m = feed(m, click(5)) // select rank 2
	now = now.Add(200 * time.Millisecond)
	m = feed(m, click(5))
	if got := m.Result(); got.Action != result.Run || got.Command != sample[2] {
		t.Errorf("double click: result = %+v, want run %q", got, sample[2])
	}

	// Two clicks too far apart only select.
	m = newMouseModel(LayoutDefault, Height{Percent: 100}, 80, 10)
	m.now = func() time.Time { return now }
	m = feed(m, click(5))
	now = now.Add(time.Second)
	m = feed(m, click(5))
	if m.done {
		t.Error("slow second click ran the command")
	}

	// A double click spread over two different rows only selects.
	m = newMouseModel(LayoutDefault, Height{Percent: 100}, 80, 10)
	m.now = func() time.Time { return now }
	m = feed(m, click(5), click(6))
	if m.done || m.cursor != 1 {
		t.Errorf("clicks on two rows: done=%v cursor=%d, want not done, cursor 1", m.done, m.cursor)
	}
}
