package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Dologan/ihg/internal/result"
)

// helpLines is the text of the help overlay.
var helpLines = []string{
	"ihg - quick help                      (Esc/q close, Up/Down scroll)",
	"",
	"Move        Up/Down  Ctrl-P/N  Ctrl-K/J  PgUp/PgDn  (wheel with --mouse)",
	"Run         Enter   (in copy mode: copies instead, then quits)",
	"Edit        Tab, Left at the start of the filter, or :e edit the command",
	"            inside ihg (with --edit shell: on the shell command line)",
	"Copy mode   Right at the end of the filter toggles [COPY]; Esc leaves it;",
	"            :c copies the selected command once and quits",
	"Sort        Ctrl-R relevance > recency > A-Z    :s A-Z   :S Z-A",
	"Preview     ? (empty filter) or Ctrl-/ : full command and last run time",
	"Help        :h  :?  or F1",
	"Quit        Esc on an empty filter, Ctrl-C, Ctrl-G, Ctrl-Q, Ctrl-D (empty)",
	"",
	"Filter text (when it has text)   Left/Right  Ctrl-A/E  Alt-B/F  Ctrl-W",
	"            Ctrl-U  Backspace  Delete       (they do nothing on an empty",
	"            filter, or in the group view)",
	"Edit mode   same keys, plus Ctrl-K (delete to end); Enter runs the text,",
	"            Esc goes back; newlines show as a down-arrow",
	"",
	"Colon commands   (type : on an empty filter, then Enter)",
	"  :q quit   :r reset   :g groups   :h :? help   :e edit   :c copy",
	"  :d delete   :b [PATH] backup   :0 .. :9 run by recency",
	"  :s :S sort A-Z / Z-A",
	"  :f  toggle fuzzy matching       :p  toggle regex matching",
	"  :+N :-N  grow / shrink the inline height by N lines (default 1)",
	"",
	"Groups (:g)  commands grouped by the programs in their pipelines",
	"  Up/Down move   Space or Enter on a header opens or closes it",
	"  + open   - close (also from a member)   Enter on a member runs it",
	"  d or Delete  deletes the member, or all commands of a group (asks y/N)",
	"  Tab edits   Right toggles copy mode   : colon commands",
	"  Esc leaves copy mode, then returns to the search",
	"",
	"Matching  --match exact|fuzzy|regex (default exact); :f and :p switch it",
	"  The mode shows in the info line as [fuzzy] or [regex].",
	"  exact / fuzzy  terms separated by spaces are ANDed; case is ignored",
	"  unless the filter has a capital letter (smart case):",
	"  word      exact substring in exact mode, fuzzy in fuzzy mode",
	"  'word     the other one: fuzzy in exact mode, exact in fuzzy mode",
	"  ^word     prefix",
	"  word$     suffix",
	"  !word     does not match (also !^prefix, !suffix$)",
	"  a | b     either term (OR)",
	"  regex     the whole filter is one Go (RE2) regular expression: no",
	"            lookaround or backreferences; smart case as above",
}

// openHelp shows the help, remembering the view (search or groups) to return
// to when it closes.
func (m Model) openHelp() Model {
	m.helpFrom = m.mode
	m.mode, m.helpOff = modeHelp, 0
	return m
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
