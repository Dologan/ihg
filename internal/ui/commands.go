package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Dologan/ihg/internal/clipboard"
	"github.com/Dologan/ihg/internal/match"
	"github.com/Dologan/ihg/internal/result"
)

// activate is what Enter does on a command: run it, or copy it in copy mode.
func (m Model) activate(cmd string) (tea.Model, tea.Cmd) {
	if m.copyMode {
		return m.copyAndQuit(cmd)
	}
	return m.quit(result.Run, cmd)
}

// copyAndQuit copies text to the clipboard and quits with Action None.
//
// OSC 52 reaches the terminal through Bubble Tea's own output: tea.SetClipboard
// (outside tmux) or tea.Raw with the tmux-wrapped sequence from
// clipboard.OSC52 (inside tmux). Both are written to the program's output
// buffer, which is flushed before the program exits because the command is a
// tea.Sequence ending in tea.Quit. Config.Copy (pbcopy, xclip...) is called too.
func (m Model) copyAndQuit(text string) (tea.Model, tea.Cmd) {
	var osc tea.Cmd
	if m.cfg.InTmux {
		osc = tea.Raw(string(clipboard.OSC52(text, true)))
	} else {
		osc = tea.SetClipboard(text)
	}
	msg := "ihg: sent to terminal clipboard (OSC 52)"
	if m.cfg.Copy != nil {
		if method, err := m.cfg.Copy(text); err != nil {
			msg += fmt.Sprintf("; clipboard tool failed: %v", err)
		} else if method != "" {
			msg += ", " + method
		}
	}
	m.done = true
	m.res = Result{Action: result.None, Message: msg}
	return m, tea.Sequence(osc, tea.Quit)
}

// keyColon handles keys in modeColon.
func (m Model) keyColon(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.Key()
	switch msg.Keystroke() {
	case "enter":
		body := strings.TrimSpace(string(m.colon))
		m.mode, m.colon = m.ret, nil
		return m.execColon(body)
	case "esc":
		m.mode, m.colon = m.ret, nil
		return m, nil
	case "ctrl+c", "ctrl+g", "ctrl+q":
		return m.quit(result.None, "")
	case "backspace", "ctrl+h":
		if len(m.colon) == 0 {
			m.mode = m.ret
			return m, nil
		}
		m.colon = m.colon[: len(m.colon)-1 : len(m.colon)-1]
		return m, nil
	case "ctrl+u":
		m.colon = nil
		return m, nil
	}
	if k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) != 0 || k.Text == "" {
		return m, nil
	}
	in := make([]rune, 0, len(m.colon)+len(k.Text))
	in = append(in, m.colon...)
	for _, r := range k.Text {
		if r >= 0x20 && r != 0x7f {
			in = append(in, r)
		}
	}
	m.colon = in
	return m, nil
}

// execColon runs a colon command (body excludes the ':'). m.mode is already
// back at search or groups.
func (m Model) execColon(body string) (tea.Model, tea.Cmd) {
	switch {
	case body == "":
		return m, nil
	case body == "q":
		return m.quit(result.None, "")
	case body == "r":
		m.sort, m.copyMode, m.matchMode = sortRelevance, false, m.cfg.Match
		m.mode = modeSearch
		m.groups, m.grows, m.expanded = nil, nil, map[string]bool{}
		return m.setQuery(nil), nil
	case body == "s" || body == "S":
		m = m.inSearchView(func(m *Model) {
			m.sort = sortAZ
			if body == "S" {
				m.sort = sortZA
			}
			m.refilter()
		})
		m.status = "sort " + m.sort.tag()
		return m, nil
	case body == "f":
		return m.setMatchMode(toggleMode(m.matchMode, match.ModeFuzzy)), nil
	case body == "p":
		return m.setMatchMode(toggleMode(m.matchMode, match.ModeRegex)), nil
	case body == "g":
		if m.mode == modeGroups {
			return m, nil
		}
		return m.openGroups(), nil
	case body == "h" || body == "?":
		return m.openHelp(), nil
	case body == "e":
		return m.edit()
	case body == "c":
		if c, ok := m.target(); ok {
			return m.copyAndQuit(c)
		}
		m.status = m.noTarget()
		return m, nil
	case body == "d":
		return m.askDelete()
	case body[0] == '+' || body[0] == '-':
		if n, ok := parseHeightDelta(body); ok {
			return m.resize(n), nil
		}
	case body == "b" || strings.HasPrefix(body, "b "):
		if m.cfg.Backup == nil {
			m.status = "backup not available"
			return m, nil
		}
		path, err := m.cfg.Backup(strings.TrimSpace(body[1:]))
		if err != nil {
			m.status = "backup failed: " + err.Error()
		} else {
			m.status = "backup written: " + path
		}
		return m, nil
	case len(body) == 1 && body[0] >= '0' && body[0] <= '9':
		i, _ := strconv.Atoi(body)
		if i >= len(m.cmds) {
			m.status = fmt.Sprintf("no command at index %d", i)
			return m, nil
		}
		return m.activate(m.cmds[i])
	}
	m.status = "unknown command :" + body
	return m, nil
}

// noTarget is the status for a command that needs a selected command.
func (m Model) noTarget() string {
	if m.base() == modeGroups {
		return "select a command"
	}
	return "nothing selected"
}

// toggleMode implements :f and :p: switch to want, or back to exact if
// already there.
func toggleMode(cur, want match.Mode) match.Mode {
	if cur == want {
		return match.ModeExact
	}
	return want
}

// setMatchMode switches the matching mode and refilters.
func (m Model) setMatchMode(mode match.Mode) Model {
	m = m.inSearchView(func(m *Model) {
		m.matchMode = mode
		m.refilter()
	})
	m.status = "match: " + [...]string{"exact", "fuzzy", "regex"}[mode]
	return m
}

// parseHeightDelta parses "+N" / "-N" (N optional, default 1).
func parseHeightDelta(body string) (int, bool) {
	n := 1
	if rest := body[1:]; rest != "" {
		v, err := strconv.Atoi(rest)
		if err != nil || v < 0 || rest[0] < '0' || rest[0] > '9' {
			return 0, false
		}
		n = v
	}
	if body[0] == '-' {
		n = -n
	}
	return n, true
}

// resize changes the inline frame height by delta lines, within
// [minLines, terminal rows], and rescrolls the lists to the new size.
func (m Model) resize(delta int) Model {
	if m.cfg.Height.full() {
		m.status = "already full screen (use --height)"
		return m
	}
	n := min(max(m.frameLines()+delta, minLines), max(m.termH, minLines))
	m.cfg.Height = Height{Lines: n}
	fit := func(rows int) {
		m.offset = max(min(m.offset, rows-m.listRows()), 0)
		m.clampView()
	}
	fit(m.rowCount())
	if m.base() == modeGroups { // the parked search view follows too
		m.cursor, m.sCursor = m.sCursor, m.cursor
		m.offset, m.sOffset = m.sOffset, m.offset
		fit(len(m.hits))
		m.cursor, m.sCursor = m.sCursor, m.cursor
		m.offset, m.sOffset = m.sOffset, m.offset
	}
	m.status = fmt.Sprintf("height: %d lines", n)
	return m
}

// askDelete starts a delete confirmation for the selected command, or in the
// group view for the selected member or the whole group under a header.
func (m Model) askDelete() (tea.Model, tea.Cmd) {
	if m.cfg.Delete == nil {
		m.status = "delete not available"
		return m, nil
	}
	if m.mode == modeGroups && m.cursor >= 0 && m.cursor < len(m.grows) && m.grows[m.cursor].header {
		g := m.groups[m.grows[m.cursor].g]
		seen := map[string]bool{}
		var list []string
		for _, c := range g.Members {
			if !seen[c] {
				seen[c] = true
				list = append(list, c)
			}
		}
		m.pendList, m.pendKey = list, g.Key
		m.ret, m.mode = m.mode, modeConfirm
		return m, nil
	}
	c, ok := m.target()
	if !ok {
		m.status = m.noTarget()
		return m, nil
	}
	m.pending, m.pendList = c, nil
	m.ret, m.mode = m.mode, modeConfirm
	return m, nil
}

// keyConfirm handles keys in modeConfirm: y deletes, anything else cancels.
// Either way the picker returns to the view the delete was asked from.
func (m Model) keyConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	cmd, list := m.pending, m.pendList
	m.mode, m.pending, m.pendList, m.pendKey = m.ret, "", nil, ""
	if msg.Keystroke() != "y" && msg.Keystroke() != "Y" {
		m.status = "not deleted"
		return m, nil
	}
	if list == nil {
		n, err := m.cfg.Delete(cmd)
		if err != nil {
			m.status = "delete failed: " + err.Error()
			return m, nil
		}
		m = m.dropCmds(map[string]bool{cmd: true})
		m.status = fmt.Sprintf("deleted %d entries", n)
		return m, nil
	}
	// A group: delete each distinct member entirely, stopping at the first
	// error but keeping what already went.
	gone := map[string]bool{}
	total := 0
	var derr error
	for _, c := range list {
		n, err := m.cfg.Delete(c)
		if err != nil {
			derr = err
			break
		}
		total += n
		gone[c] = true
	}
	if len(gone) > 0 {
		m = m.dropCmds(gone)
	}
	switch {
	case derr == nil:
		m.status = fmt.Sprintf("deleted %d entries (%d commands)", total, len(gone))
	case len(gone) == 0:
		m.status = "delete failed: " + derr.Error()
	default:
		m.status = fmt.Sprintf("delete failed: %v (deleted %d entries, %d commands)", derr, total, len(gone))
	}
	return m, nil
}

// dropCmds removes every entry whose text is in gone from the in-memory
// history and refilters, keeping the cursor on the nearest surviving row.
func (m Model) dropCmds(gone map[string]bool) Model {
	cmds := make([]string, 0, len(m.cmds))
	times := make([]int64, 0, len(m.cmds))
	for i, c := range m.cmds {
		if !gone[c] {
			cmds = append(cmds, c)
			times = append(times, m.timeAt(i))
		}
	}
	return m.inSearchView(func(m *Model) {
		old := m.cursor
		m.cmds, m.times = cmds, times
		m.refilter()
		m.cursor = min(old, max(len(m.hits)-1, 0))
		m.clampView()
	})
}

// inSearchView runs fn with the search list's cursor and scroll in place (the
// group view parks them) and then regroups, keeping the group cursor row.
func (m Model) inSearchView(fn func(*Model)) Model {
	if m.base() != modeGroups {
		fn(&m)
		return m
	}
	var row grow
	if m.cursor >= 0 && m.cursor < len(m.grows) {
		row = m.grows[m.cursor]
	}
	m.cursor, m.sCursor = m.sCursor, m.cursor
	m.offset, m.sOffset = m.sOffset, m.offset
	fn(&m)
	m.cursor, m.sCursor = m.sCursor, m.cursor
	m.offset, m.sOffset = m.sOffset, m.offset
	return m.rebuildGroups(row)
}

// reload polls Config.Reload and, if the history changed, swaps in the new
// list keeping the selected command (by text) and the current mode.
func (m Model) reload() (tea.Model, tea.Cmd) {
	next := m.tick()
	if m.cfg.Reload == nil || m.done {
		return m, nil
	}
	cmds, times, changed, err := m.cfg.Reload()
	if err != nil {
		m.status = "reload failed: " + err.Error()
		return m, next
	}
	if !changed {
		return m, next
	}
	m = m.inSearchView(func(m *Model) {
		sel, hadSel := m.selected()
		old := m.cursor
		m.cmds, m.times = cmds, times
		m.refilter()
		m.cursor = min(old, max(len(m.hits)-1, 0))
		if hadSel {
			for i, h := range m.hits {
				if m.cmds[h.Index] == sel {
					m.cursor = i
					break
				}
			}
		}
		m.offset = min(old, m.offset)
		m.clampView()
	})
	return m, next
}
