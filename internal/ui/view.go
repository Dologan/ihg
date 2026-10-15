package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Styles use only basic ANSI colours so they follow the terminal theme.
var (
	styleMarker   = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(1)).Bold(true)
	styleGutter   = lipgloss.NewStyle().Faint(true)
	styleMatch    = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(2)).Bold(true)
	styleSelected = lipgloss.NewStyle().Reverse(true)
	styleSelMatch = lipgloss.NewStyle().Reverse(true).Bold(true)
	styleInfo     = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(3))
	stylePrompt   = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(4)).Bold(true)
)

const (
	promptText = "> "
	// rowPrefix is the width of "> N " (marker, recency digit, space).
	rowPrefix = 4
)

// View implements tea.Model. After the program has decided to quit the view is
// empty so the final frame erases the inline UI.
func (m Model) View() tea.View {
	if m.done {
		v := tea.NewView("")
		v.AltScreen = m.cfg.Height.full()
		return v
	}
	var v tea.View
	switch m.mode {
	default:
		v = m.viewSearch()
	}
	v.AltScreen = m.cfg.Height.full()
	if m.cfg.Mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// viewSearch renders modeSearch: list, info line and prompt.
func (m Model) viewSearch() tea.View {
	h := m.frameLines()
	n := m.listRows()
	w := max(m.termW, 1)

	// Rows in rank order (best first), padded with blanks when short.
	rows := make([]string, n)
	for i := range rows {
		rank := m.offset + i
		if rank < len(m.hits) {
			rows[i] = m.renderRow(rank, w)
		}
	}

	info := ansi.Truncate(styleInfo.Render(fmt.Sprintf("  %d/%d", len(m.hits), len(m.cmds))), w, "")
	prompt, cx := m.renderPrompt(w)

	var lines []string
	var cy int
	if m.cfg.Layout == LayoutReverse {
		lines = append(lines, prompt, info)
		lines = append(lines, rows...)
		cy = 0
	} else {
		for i := n - 1; i >= 0; i-- {
			lines = append(lines, rows[i])
		}
		lines = append(lines, info, prompt)
		cy = h - 1
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.Cursor = tea.NewCursor(cx, cy)
	return v
}

// renderPrompt returns the prompt line and the cursor column. The visible part
// of a long query scrolls horizontally to keep the cursor in view.
func (m Model) renderPrompt(w int) (string, int) {
	avail := max(w-len(promptText)-1, 1) // keep one column for the cursor at end
	start := 0
	for start < m.qpos && runesWidth(m.query[start:m.qpos]) > avail {
		start++
	}
	end := start
	used := 0
	for end < len(m.query) {
		rw := runeWidth(m.query[end])
		if used+rw > avail+1 {
			break
		}
		used += rw
		end++
	}
	cx := len(promptText) + runesWidth(m.query[start:m.qpos])
	return stylePrompt.Render(promptText) + string(m.query[start:end]), cx
}

// renderRow renders the hit at rank, exactly w columns wide at most.
func (m Model) renderRow(rank, w int) string {
	hit := m.hits[rank]
	cmd := m.cmds[hit.Index]
	runes := sanitize(cmd)
	selected := rank == m.cursor

	// Highlight positions are computed only for rows we actually draw.
	var hl map[int]bool
	if !m.pat.Empty() {
		if _, pos, ok := m.matcher.Match(m.pat, cmd); ok && len(pos) > 0 {
			hl = make(map[int]bool, len(pos))
			for _, p := range pos {
				hl[p] = true
			}
		}
	}

	plain, match := lipgloss.NewStyle(), styleMatch
	if selected {
		plain, match = styleSelected, styleSelMatch
	}

	var sb strings.Builder
	marker := " "
	if selected {
		marker = styleMarker.Reverse(true).Render(">")
	}
	digit := " "
	if hit.Index < 10 {
		digit = strconv.Itoa(hit.Index)
	}
	sb.WriteString(marker)
	if selected {
		sb.WriteString(styleSelected.Render(" "))
		sb.WriteString(styleSelected.Faint(true).Render(digit))
		sb.WriteString(styleSelected.Render(" "))
	} else {
		sb.WriteString(" ")
		sb.WriteString(styleGutter.Render(digit))
		sb.WriteString(" ")
	}
	used := rowPrefix

	// Truncate by display width, leaving one column for the ellipsis when cut.
	avail := w - rowPrefix
	total := runesWidth(runes)
	truncated := total > avail
	limit := avail
	if truncated {
		limit = avail - 1
	}

	var run []rune
	runHL := false
	flush := func() {
		if len(run) == 0 {
			return
		}
		st := plain
		if runHL {
			st = match
		}
		sb.WriteString(st.Render(string(run)))
		run = run[:0]
	}
	col := 0
	for i, r := range runes {
		rw := runeWidth(r)
		if col+rw > limit {
			break
		}
		if hl[i] != runHL {
			flush()
			runHL = hl[i]
		}
		run = append(run, r)
		col += rw
	}
	flush()
	used += col
	if truncated && avail >= 1 {
		sb.WriteString(plain.Render("…"))
		used++
	}
	if selected && used < w {
		sb.WriteString(styleSelected.Render(strings.Repeat(" ", w-used)))
	}
	return sb.String()
}
