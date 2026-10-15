package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Dologan/ihg/internal/result"
)

// helpLines is the text of the help overlay.
var helpLines = []string{
	"ihg - quick help",
	"",
	"Move        Up/Down  Ctrl-P/N  Ctrl-K/J  PgUp/PgDn (mouse wheel with --mouse)",
	"Run         Enter (in copy mode: copies instead, then quits)",
	"Edit        Tab, or Left at the start of the query, or :e",
	"Copy mode   Right at the end of the query toggles [COPY]; Esc leaves it",
	"            :c copies the selected command once and quits",
	"Sort        Ctrl-R relevance > recency > A-Z    :s A-Z   :S Z-A",
	"Preview     ? (empty query) or Ctrl-/ shows the full command and last run time",
	"Help        :h or F1;  Esc or q closes",
	"Query       empty query + : starts a colon command",
	"Colon       :q quit   :r reset   :s :S sort   :g groups   :h help",
	"            :e edit   :d delete   :b [PATH] backup   :0 .. :9 run by recency",
	"Groups      Space/Enter on a header toggles it; + expands, - collapses",
	"            Enter on a member runs it, Tab edits it, Esc returns to search",
	"Query edit  Ctrl-A/E  Alt-B/F  Ctrl-W  Ctrl-U  Backspace  Delete",
	"Quit        Esc on an empty query, Ctrl-C, Ctrl-G, Ctrl-Q, Ctrl-D (empty)",
	"",
	"Query syntax (like fzf, terms separated by spaces are ANDed):",
	"  word      fuzzy match",
	"  'word     exact substring",
	"  ^word     prefix",
	"  word$     suffix",
	"  !word     does not match (also !^prefix, !suffix$, !'exact)",
	"  a | b     either term (OR)",
}

func (m Model) openHelp() Model {
	m.mode, m.helpOff = modeHelp, 0
	// Help always returns to the group view or search it was opened from;
	// remember which through the parked search cursor (group view keeps its
	// own rows), so just record the origin.
	m.helpFrom = m.fromMode()
	return m
}

// fromMode is the mode to return to when help closes.
func (m Model) fromMode() mode {
	if m.mode == modeHelp {
		return m.helpFrom
	}
	return m.mode
}

// helpRows is how many help lines fit (one line is kept for the footer).
func (m Model) helpRows() int { return max(m.frameLines()-1, 1) }

func (m Model) keyHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	maxOff := max(len(helpLines)-m.helpRows(), 0)
	switch msg.Keystroke() {
	case "esc", "q", "f1":
		m.mode = m.helpFrom
	case "ctrl+c", "ctrl+g", "ctrl+q":
		return m.quit(result.None, "")
	case "up", "k", "ctrl+p":
		m.helpOff = max(m.helpOff-1, 0)
	case "down", "j", "ctrl+n":
		m.helpOff = min(m.helpOff+1, maxOff)
	case "pgup":
		m.helpOff = max(m.helpOff-m.helpRows(), 0)
	case "pgdown", "space":
		m.helpOff = min(m.helpOff+m.helpRows(), maxOff)
	case "home":
		m.helpOff = 0
	case "end":
		m.helpOff = maxOff
	}
	return m, nil
}

func (m Model) viewHelp() tea.View {
	w, n := max(m.termW, 1), m.helpRows()
	maxOff := max(len(helpLines)-n, 0)
	off := min(m.helpOff, maxOff)
	out := make([]string, 0, n+1)
	for i := 0; i < n; i++ {
		line := ""
		if off+i < len(helpLines) {
			line = truncate(helpLines[off+i], w)
		}
		out = append(out, line)
	}
	foot := "Esc/q close"
	if maxOff > 0 {
		foot += "  Up/Down scroll"
	}
	out = append(out, truncate(styleInfo.Render("  "+foot), w))
	return tea.NewView(strings.Join(out, "\n"))
}
