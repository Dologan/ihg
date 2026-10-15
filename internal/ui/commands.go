package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Dologan/ihg/internal/clipboard"
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
		m.mode, m.colon = modeSearch, nil
		return m.execColon(body)
	case "esc":
		m.mode, m.colon = modeSearch, nil
		return m, nil
	case "ctrl+c", "ctrl+g", "ctrl+q":
		return m.quit(result.None, "")
	case "backspace", "ctrl+h":
		if len(m.colon) == 0 {
			m.mode = modeSearch
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

// execColon runs a colon command (body excludes the ':').
func (m Model) execColon(body string) (tea.Model, tea.Cmd) {
	switch {
	case body == "":
		return m, nil
	case body == "q":
		return m.quit(result.None, "")
	case body == "r":
		m.sort, m.copyMode = sortRelevance, false
		m.groups, m.grows, m.expanded = nil, nil, map[string]bool{}
		return m.setQuery(nil), nil
	case body == "s":
		m.sort = sortAZ
		m.refilter()
		m.status = "sort A-Z"
		return m, nil
	case body == "S":
		m.sort = sortZA
		m.refilter()
		m.status = "sort Z-A"
		return m, nil
	case body == "g":
		return m.openGroups(), nil
	case body == "h":
		return m.openHelp(), nil
	case body == "e":
		return m.edit()
	case body == "c":
		if c, ok := m.selected(); ok {
			return m.copyAndQuit(c)
		}
		m.status = "nothing selected"
		return m, nil
	case body == "d":
		c, ok := m.selected()
		switch {
		case !ok:
			m.status = "nothing selected"
		case m.cfg.Delete == nil:
			m.status = "delete not available"
		default:
			m.pending, m.mode = c, modeConfirm
		}
		return m, nil
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

// keyConfirm handles keys in modeConfirm: y deletes, anything else cancels.
func (m Model) keyConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	cmd := m.pending
	m.mode, m.pending = modeSearch, ""
	if msg.Keystroke() != "y" && msg.Keystroke() != "Y" {
		m.status = "not deleted"
		return m, nil
	}
	n, err := m.cfg.Delete(cmd)
	if err != nil {
		m.status = "delete failed: " + err.Error()
		return m, nil
	}
	cmds := make([]string, 0, len(m.cmds))
	times := make([]int64, 0, len(m.cmds))
	for i, c := range m.cmds {
		if c != cmd {
			cmds = append(cmds, c)
			times = append(times, m.timeAt(i))
		}
	}
	old := m.cursor
	m.cmds, m.times = cmds, times
	m.refilter()
	m.cursor = min(old, max(len(m.hits)-1, 0))
	m.clampView()
	m.status = fmt.Sprintf("deleted %d entries", n)
	return m, nil
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
	inGroups := m.mode == modeGroups
	var row grow
	if inGroups && m.cursor < len(m.grows) {
		row = m.grows[m.cursor]
		m.cursor, m.sCursor = m.sCursor, m.cursor
		m.offset, m.sOffset = m.sOffset, m.offset
	}
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
	if inGroups {
		m.cursor, m.sCursor = m.sCursor, m.cursor
		m.offset, m.sOffset = m.sOffset, m.offset
		m = m.rebuildGroups(row)
	}
	return m, next
}
