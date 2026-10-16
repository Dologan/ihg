package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Dologan/ihg/internal/match"
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

	styleCopyPrompt = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(5)).Bold(true)
	styleCopySel    = lipgloss.NewStyle().Reverse(true).Foreground(lipgloss.ANSIColor(5))
)

const (
	promptText     = "> "
	editPromptText = "edit> "
	copyPromptText = "[COPY]> "
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
	switch m.base() {
	case modeGroups:
		v = m.viewGroups()
	case modeHelp:
		v = m.viewHelp()
	default:
		v = m.viewSearch()
	}
	v.AltScreen = m.cfg.Height.full()
	if m.cfg.Mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// truncate cuts s to w display columns.
func truncate(s string, w int) string { return ansi.Truncate(s, w, "") }

// matchTag is the info-line tag of a non-default matching mode.
func (m Model) matchTag() string {
	switch m.matchMode {
	case match.ModeFuzzy:
		return "fuzzy"
	case match.ModeRegex:
		return "regex"
	}
	return ""
}

// infoLine is "  shown/total", the sort and match tags and the status message.
func (m Model) infoLine(w int, shown, total int) string {
	s := fmt.Sprintf("  %d/%d", shown, total)
	if t := m.sort.tag(); t != "" {
		s += "  [" + t + "]"
	}
	if t := m.matchTag(); t != "" {
		s += "  [" + t + "]"
	}
	if m.pat != nil && m.pat.Err() != nil {
		s += "  regex error"
	}
	if m.status != "" {
		s += "  " + sanitizeLine(m.status)
	}
	return truncate(styleInfo.Render(s), w)
}

// sanitizeLine makes s safe for a single line.
func sanitizeLine(s string) string { return string(sanitize(s)) }

// layout assembles a frame from rows in rank order (best first), the info and
// prompt lines, and the preview pane (separator + text lines, nil when off).
func (m Model) layout(rows []string, info, prompt string, pv []string) []string {
	var lines []string
	if m.cfg.Layout == LayoutReverse {
		lines = append(lines, prompt, info)
		lines = append(lines, rows...)
		lines = append(lines, pv...)
		return lines
	}
	lines = append(lines, pv...)
	for i := len(rows) - 1; i >= 0; i-- {
		lines = append(lines, rows[i])
	}
	return append(lines, info, prompt)
}

// promptRow is the line index of the prompt in the frame.
func (m Model) promptRow() int {
	if m.cfg.Layout == LayoutReverse {
		return 0
	}
	return m.frameLines() - 1
}

// viewSearch renders modeSearch (and its colon and confirm prompts): list,
// info line, prompt and, when on, the preview pane.
func (m Model) viewSearch() tea.View {
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

	info := m.infoLine(w, len(m.hits), len(m.cmds))
	prompt, cx, showCursor, ok := m.overlayPrompt(w)
	if !ok {
		prompt, cx = m.renderPrompt(w)
		showCursor = true
	}

	lines := m.layout(rows, info, prompt, m.previewLines(w))
	v := tea.NewView(strings.Join(lines, "\n"))
	if showCursor {
		v.Cursor = tea.NewCursor(cx, m.promptRow())
	}
	return v
}

// overlayPrompt renders the prompt line of the colon prompt, the delete
// confirmation and the editor, which draw over the search or group view. ok is
// false in the other modes.
func (m Model) overlayPrompt(w int) (prompt string, cx int, showCursor, ok bool) {
	switch m.mode {
	case modeColon:
		prompt = truncate(stylePrompt.Render(":")+string(m.colon), w)
		return prompt, min(1+runesWidth(m.colon), w-1), true, true
	case modeConfirm:
		var q string
		if m.pendList != nil {
			q = fmt.Sprintf("delete %d commands in group %q? [y/N]", len(m.pendList), truncate(sanitizeLine(m.pendKey), max(w-40, 10)))
		} else {
			q = fmt.Sprintf("delete %q from history? [y/N]", truncate(sanitizeLine(m.pending), max(w-30, 10)))
		}
		return truncate(stylePrompt.Render(q), w), 0, false, true
	case modeEdit:
		pt, ps := editPromptText, stylePrompt
		if m.copyMode {
			pt, ps = "[COPY] "+editPromptText, styleCopyPrompt
		}
		prompt, cx = renderLine(pt, ps, sanitize(m.ed.String()), m.ed.pos, w)
		return prompt, cx, true, true
	}
	return "", 0, false, false
}

// previewLines renders the preview pane: a separator and previewH-1 text
// lines showing the selected command in full, then its last run time.
func (m Model) previewLines(w int) []string {
	p := m.previewH()
	if p == 0 {
		return nil
	}
	sep := styleGutter.Render(strings.Repeat("─", w))
	text := make([]string, p-1)
	cmd, ok := m.selected()
	if ok {
		avail := len(text)
		var stamp string
		if ts := m.timeAt(m.hits[m.cursor].Index); ts > 0 {
			stamp = styleInfo.Render(truncate("last run: "+time.Unix(ts, 0).Local().Format("2006-01-02 15:04:05"), w))
			avail--
		}
		var body []string
		for _, ln := range strings.Split(cmd, "\n") {
			ln = string(sanitize(ln))
			body = append(body, strings.Split(ansi.Hardwrap(ln, w, true), "\n")...)
		}
		if len(body) > avail && avail > 0 {
			body = body[:avail]
			body[avail-1] = truncate(body[avail-1], max(w-1, 0)) + "…"
		}
		copy(text, body[:min(len(body), max(avail, 0))])
		if stamp != "" {
			text[len(text)-1] = stamp
		}
	}
	if m.cfg.Layout == LayoutReverse {
		return append([]string{sep}, text...)
	}
	return append(text, sep)
}

// renderPrompt returns the prompt line and the cursor column.
func (m Model) renderPrompt(w int) (string, int) {
	pt, ps := promptText, stylePrompt
	if m.copyMode {
		pt, ps = copyPromptText, styleCopyPrompt
	}
	return renderLine(pt, ps, m.query, m.qpos, w)
}

// renderLine draws a prompt followed by text with the cursor at rune pos. The
// visible part of long text scrolls horizontally to keep the cursor in view.
func renderLine(pt string, ps lipgloss.Style, text []rune, pos, w int) (string, int) {
	pw := len(pt)
	avail := max(w-pw-1, 1) // keep one column for the cursor at end
	start := 0
	for start < pos && runesWidth(text[start:pos]) > avail {
		start++
	}
	end := start
	used := 0
	for end < len(text) {
		rw := runeWidth(text[end])
		if used+rw > avail+1 {
			break
		}
		used += rw
		end++
	}
	cx := pw + runesWidth(text[start:pos])
	return ps.Render(pt) + string(text[start:end]), cx
}

// selStyles returns the styles of the selected row: reverse video, in a
// distinct colour in copy mode.
func (m Model) selStyles() (sel, selMatch lipgloss.Style) {
	if m.copyMode {
		return styleCopySel, styleCopySel.Bold(true)
	}
	return styleSelected, styleSelMatch
}

// viewGroups renders the group view: a top-down tree (headers alphabetical,
// members indented below their header) in both layouts.
func (m Model) viewGroups() tea.View {
	n := m.listRows()
	w := max(m.termW, 1)
	lines := make([]string, 0, m.frameLines())
	for i := 0; i < n; i++ {
		row := ""
		if rank := m.offset + i; rank < len(m.grows) {
			row = m.renderGroupRow(rank, w)
		}
		lines = append(lines, row)
	}
	infoText := fmt.Sprintf("  %d groups", len(m.groups))
	if m.status != "" {
		infoText += "  " + sanitizeLine(m.status)
	}
	info := truncate(styleInfo.Render(infoText), w)

	prompt, cx, showCursor, ok := m.overlayPrompt(w)
	if !ok {
		pt, ps := "groups> ", stylePrompt
		if m.copyMode {
			pt, ps = "[COPY] groups> ", styleCopyPrompt
		}
		prompt = truncate(ps.Render(pt)+styleGutter.Render("Space toggle  +/- expand  d delete  Esc back"), w)
	}
	if m.cfg.Layout == LayoutReverse {
		lines = append([]string{prompt, info}, lines...)
	} else {
		lines = append(lines, info, prompt)
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	if ok && showCursor {
		v.Cursor = tea.NewCursor(cx, m.promptRow())
	}
	return v
}

func (m Model) renderGroupRow(rank, w int) string {
	r := m.grows[rank]
	g := m.groups[r.g]
	var text string
	if r.header {
		arrow := "▸"
		if m.expanded[g.Key] {
			arrow = "▾"
		}
		text = fmt.Sprintf("%s [%d] %s", arrow, len(g.Members), sanitizeLine(g.Key))
	} else {
		text = "    " + sanitizeLine(r.cmd)
	}
	if rank != m.cursor {
		return "  " + truncate(text, max(w-2, 0))
	}
	sel, _ := m.selStyles()
	body := truncate(text, max(w-2, 0))
	line := styleMarker.Reverse(true).Render(">") + sel.Render(" "+body)
	if pad := w - 2 - ansi.StringWidth(body); pad > 0 {
		line += sel.Render(strings.Repeat(" ", pad))
	}
	return line
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

	sel, selMatch := m.selStyles()
	plain, match := lipgloss.NewStyle(), styleMatch
	if selected {
		plain, match = sel, selMatch
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
		sb.WriteString(sel.Render(" "))
		sb.WriteString(sel.Faint(true).Render(digit))
		sb.WriteString(sel.Render(" "))
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
		sb.WriteString(sel.Render(strings.Repeat(" ", w-used)))
	}
	return sb.String()
}
