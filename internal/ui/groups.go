package ui

import (
	"github.com/Dologan/ihg/internal/groups"
	"github.com/Dologan/ihg/internal/result"

	tea "charm.land/bubbletea/v2"
)

// groupLimit is how many commands the group view considers.
const groupLimit = 500

// grow is one row of the group view: a header, or a member of an expanded group.
type grow struct {
	header bool
	g      int    // index into Model.groups
	cmd    string // member command (members only)
}

// openGroups builds the group view from the visible commands in current order.
func (m Model) openGroups() Model {
	m.sCursor, m.sOffset = m.cursor, m.offset
	m.mode = modeGroups
	m.cursor, m.offset = 0, 0
	m.expanded = map[string]bool{}
	return m.rebuildGroups(grow{})
}

// rebuildGroups regroups the visible commands and flattens them to rows,
// moving the cursor to prev (a row of the previous build) when it still exists.
func (m Model) rebuildGroups(prev grow) Model {
	vis := make([]string, len(m.hits))
	for i, h := range m.hits {
		vis[i] = m.cmds[h.Index]
	}
	var prevKey string
	if prev.header || prev.cmd != "" {
		prevKey = m.groupKey(prev)
	}
	m.groups = groups.Build(vis, groupLimit)
	m.grows = m.grows[:0:0]
	found := -1
	for gi, g := range m.groups {
		if g.Key == prevKey && prev.header {
			found = len(m.grows)
		}
		m.grows = append(m.grows, grow{header: true, g: gi})
		if m.expanded[g.Key] {
			for _, c := range g.Members {
				if !prev.header && prev.cmd == c && g.Key == prevKey {
					found = len(m.grows)
				}
				m.grows = append(m.grows, grow{g: gi, cmd: c})
			}
		}
	}
	if found >= 0 {
		m.cursor = found
	}
	m.cursor = min(m.cursor, max(len(m.grows)-1, 0))
	m.clampView()
	return m
}

// groupKey returns the key of the group a row belonged to when it was built
// (rows index the groups slice that was current then, which is still m.groups
// at the call site of rebuildGroups).
func (m Model) groupKey(r grow) string {
	if r.g < len(m.groups) {
		return m.groups[r.g].Key
	}
	return ""
}

// keyGroups handles keys in modeGroups. The tree is drawn top-down in every
// layout, so Up/Down move to the previous/next row.
func (m Model) keyGroups(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var row grow
	haveRow := m.cursor >= 0 && m.cursor < len(m.grows)
	if haveRow {
		row = m.grows[m.cursor]
	}
	setOpen := func(open bool) Model {
		if haveRow {
			row.header = true // a member row acts on its parent header
			m.expanded[m.groups[row.g].Key] = open
			m = m.rebuildGroups(row)
		}
		return m
	}
	switch msg.Keystroke() {
	case "esc":
		if m.copyMode {
			m.copyMode = false
			return m, nil
		}
		m.mode = modeSearch
		m.cursor, m.offset = m.sCursor, m.sOffset
		m.clampView()
		return m, nil
	case "ctrl+c", "ctrl+g", "ctrl+q":
		return m.quit(result.None, "")
	case "f1":
		return m.openHelp(), nil
	case "up", "ctrl+p", "ctrl+k":
		return m.move(-1), nil
	case "down", "ctrl+n", "ctrl+j":
		return m.move(+1), nil
	case "pgup":
		return m.move(-m.listRows()), nil
	case "pgdown":
		return m.move(+m.listRows()), nil
	case "right":
		m.copyMode = !m.copyMode
		return m, nil
	case "space", " ":
		if haveRow && row.header {
			return setOpen(!m.expanded[m.groups[row.g].Key]), nil
		}
	case "enter":
		if !haveRow {
			return m, nil
		}
		if row.header {
			return setOpen(!m.expanded[m.groups[row.g].Key]), nil
		}
		return m.activate(row.cmd)
	case "+":
		if haveRow && row.header {
			return setOpen(true), nil
		}
	case "-":
		return setOpen(false), nil
	case "tab":
		return m.edit()
	case "d", "delete":
		return m.askDelete()
	case ":":
		m.mode, m.ret, m.colon = modeColon, modeGroups, nil
	}
	return m, nil
}
