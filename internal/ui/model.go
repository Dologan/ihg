package ui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Dologan/ihg/internal/match"
	"github.com/Dologan/ihg/internal/result"
)

// mode is the interaction state. Only modeSearch exists for now; later phases
// add more (each with its own key handler, see Model.handleKey).
type mode int

const (
	modeSearch mode = iota
)

// Model is the picker's Bubble Tea model.
type Model struct {
	cfg  Config
	mode mode

	cmds    []string
	matcher *match.Matcher
	pat     *match.Pattern
	hits    []match.Hit // best first

	query []rune
	qpos  int // cursor position in query, in runes

	cursor int // index into hits (0 = best)
	offset int // first visible hit (0 = best)

	termW, termH int
	done         bool
	res          Result

	now       func() time.Time // clock, replaceable in tests
	lastClick time.Time        // for double-click detection
	lastRank  int              // hit rank of the last click
	runOnUp   bool             // second press of a double click seen; run on its release
}

// doubleClick is the longest gap between two clicks on a row that runs it.
const doubleClick = 500 * time.Millisecond

// New returns a model for cfg. Until a WindowSizeMsg arrives it assumes an
// 80x24 terminal.
func New(cfg Config) Model {
	m := Model{
		cfg:     cfg,
		cmds:    cfg.Commands,
		matcher: match.NewMatcher(),
		query:   []rune(cfg.Query),
		termW:   80,
		termH:   24,
		res:     Result{Action: result.None},
		now:     time.Now,
	}
	m.qpos = len(m.query)
	m.refilter()
	return m
}

// Result returns the user's choice (Action None if cancelled or unfinished).
func (m Model) Result() Result { return m.res }

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termW, m.termH = msg.Width, msg.Height
		m.clampView()
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		return m.handlePaste(msg)
	case tea.MouseWheelMsg:
		return m.handleWheel(msg)
	case tea.MouseClickMsg:
		return m.handleClick(msg)
	case tea.MouseReleaseMsg:
		return m.handleRelease(msg)
	}
	return m, nil
}

// handleKey dispatches to the handler for the current mode.
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	default:
		return m.keySearch(msg)
	}
}

func (m Model) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeSearch {
		return m, nil
	}
	var in []rune
	for _, r := range msg.Content {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			in = append(in, ' ')
		case r < 0x20 || r == 0x7f:
		default:
			in = append(in, r)
		}
	}
	return m.insert(in), nil
}

// handleWheel moves the selection one row per wheel step, in the direction
// the list visibly scrolls.
func (m Model) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if !m.cfg.Mouse || m.mode != modeSearch {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		return m.move(m.visualStep(-1)), nil
	case tea.MouseWheelDown:
		return m.move(m.visualStep(+1)), nil
	}
	return m, nil
}

// handleClick selects the clicked row; a second click on the same row within
// doubleClick runs it. Only in full-screen mode, where the frame starts at the
// top of the screen so mouse rows map directly onto view lines.
func (m Model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if !m.cfg.Mouse || m.mode != modeSearch || !m.cfg.Height.full() || msg.Button != tea.MouseLeft {
		return m, nil
	}
	rank, ok := m.rankAtLine(msg.Y)
	if !ok {
		return m, nil
	}
	now := m.now()
	if rank == m.cursor && rank == m.lastRank && now.Sub(m.lastClick) <= doubleClick {
		// Run on the matching release, not now: quitting here would leave the
		// release event to arrive after exit and be typed into the shell.
		m.runOnUp = true
		return m, nil
	}
	m.runOnUp = false
	m.cursor, m.lastRank, m.lastClick = rank, rank, now
	m.clampView()
	return m, nil
}

// handleRelease completes a double click started in handleClick.
func (m Model) handleRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if !m.runOnUp {
		return m, nil
	}
	m.runOnUp = false
	if c, ok := m.selected(); ok {
		return m.quit(result.Run, c)
	}
	return m, nil
}

// rankAtLine maps a view line to the hit rank drawn there (see viewSearch).
func (m Model) rankAtLine(y int) (int, bool) {
	n := m.listRows()
	var row int
	if m.cfg.Layout == LayoutReverse {
		row = y - 2 // prompt and info lines come first
	} else {
		row = n - 1 - y // best hit is on the line just above the info line
	}
	if row < 0 || row >= n {
		return 0, false
	}
	rank := m.offset + row
	return rank, rank < len(m.hits)
}

func (m Model) quit(a result.Action, cmd string) (tea.Model, tea.Cmd) {
	m.done = true
	m.res = Result{Action: a, Command: cmd}
	return m, tea.Quit
}

// selected returns the command under the cursor.
func (m Model) selected() (string, bool) {
	if m.cursor < 0 || m.cursor >= len(m.hits) {
		return "", false
	}
	return m.cmds[m.hits[m.cursor].Index], true
}

// keySearch handles keys in modeSearch.
func (m Model) keySearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.Key()
	switch msg.Keystroke() {
	// Results.
	case "enter":
		if c, ok := m.selected(); ok {
			return m.quit(result.Run, c)
		}
		return m, nil
	case "tab":
		return m.edit()
	case "left":
		if m.qpos == 0 {
			return m.edit()
		}
		m.qpos--
		return m, nil
	case "esc":
		if len(m.query) > 0 {
			return m.setQuery(nil), nil
		}
		return m.quit(result.None, "")
	case "ctrl+c", "ctrl+g", "ctrl+q":
		return m.quit(result.None, "")
	case "ctrl+d":
		if len(m.query) == 0 {
			return m.quit(result.None, "")
		}
		return m.deleteRange(m.qpos, m.qpos+1), nil

	// Movement of the list cursor. "Up"/"down" are visual, so they depend on
	// the layout.
	case "up", "ctrl+p", "ctrl+k":
		return m.move(m.visualStep(-1)), nil
	case "down", "ctrl+n", "ctrl+j":
		return m.move(m.visualStep(+1)), nil
	case "pgup":
		return m.move(m.visualStep(-m.listRows())), nil
	case "pgdown":
		return m.move(m.visualStep(+m.listRows())), nil

	// Query cursor.
	case "right", "ctrl+f":
		m.qpos = min(m.qpos+1, len(m.query))
		return m, nil
	case "ctrl+b":
		m.qpos = max(m.qpos-1, 0)
		return m, nil
	case "alt+b", "alt+left":
		m.qpos = wordBack(m.query, m.qpos)
		return m, nil
	case "alt+f", "alt+right":
		m.qpos = wordForward(m.query, m.qpos)
		return m, nil
	case "ctrl+a", "home":
		m.qpos = 0
		return m, nil
	case "ctrl+e", "end":
		m.qpos = len(m.query)
		return m, nil

	// Query editing.
	case "backspace", "ctrl+h":
		return m.deleteRange(m.qpos-1, m.qpos), nil
	case "delete":
		return m.deleteRange(m.qpos, m.qpos+1), nil
	case "ctrl+u":
		return m.deleteRange(0, m.qpos), nil
	case "ctrl+w", "alt+backspace", "ctrl+alt+h":
		return m.deleteRange(wordBack(m.query, m.qpos), m.qpos), nil
	}

	// Anything with ctrl/alt that is not bound above is ignored; otherwise
	// printable text is inserted.
	if k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) != 0 || k.Text == "" {
		return m, nil
	}
	var in []rune
	for _, r := range k.Text {
		if r >= 0x20 && r != 0x7f {
			in = append(in, r)
		}
	}
	return m.insert(in), nil
}

func (m Model) edit() (tea.Model, tea.Cmd) {
	if c, ok := m.selected(); ok {
		return m.quit(result.Edit, c)
	}
	return m, nil
}

// visualStep converts a visual delta (negative = up the screen) into a change
// of the hit index. In the default layout the best hit is at the bottom, so
// up means a worse hit; in reverse layout the best hit is at the top.
func (m Model) visualStep(d int) int {
	if m.cfg.Layout == LayoutReverse {
		return d
	}
	return -d
}

func (m Model) move(delta int) Model {
	if len(m.hits) == 0 {
		return m
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.hits)-1)
	m.clampView()
	return m
}

// insert inserts rs at the query cursor.
func (m Model) insert(rs []rune) Model {
	if len(rs) == 0 {
		return m
	}
	q := make([]rune, 0, len(m.query)+len(rs))
	q = append(q, m.query[:m.qpos]...)
	q = append(q, rs...)
	q = append(q, m.query[m.qpos:]...)
	pos := m.qpos + len(rs)
	m = m.setQuery(q)
	m.qpos = pos
	return m
}

// deleteRange removes query[from:to] (clamped) and puts the cursor at from.
func (m Model) deleteRange(from, to int) Model {
	from, to = max(from, 0), min(to, len(m.query))
	if from >= to {
		return m
	}
	q := slices.Concat(m.query[:from], m.query[to:])
	m = m.setQuery(q)
	m.qpos = from
	return m
}

// setQuery replaces the query (cursor goes to the end) and refilters.
func (m Model) setQuery(q []rune) Model {
	m.query = q
	m.qpos = len(q)
	m.refilter()
	return m
}

// refilter recomputes hits for the current query and resets the cursor to the
// top hit. It mutates m's matcher state but only replaces slices, so copies of
// the model made earlier stay valid.
func (m *Model) refilter() {
	m.pat = match.Parse(string(m.query))
	m.hits = m.matcher.MatchAll(m.pat, m.cmds, false)
	m.cursor, m.offset = 0, 0
}

// frameLines is the number of lines the UI occupies.
func (m Model) frameLines() int {
	if m.cfg.Height.full() {
		return max(m.termH, minLines)
	}
	return m.cfg.Height.resolve(m.termH)
}

// listRows is the number of list rows in the frame (frame minus info and prompt).
func (m Model) listRows() int { return max(m.frameLines()-2, 1) }

// clampView scrolls offset so the cursor is visible.
func (m *Model) clampView() {
	n := m.listRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+n {
		m.offset = m.cursor - n + 1
	}
	m.offset = max(m.offset, 0)
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
