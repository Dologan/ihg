package ui

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Dologan/ihg/internal/groups"
	"github.com/Dologan/ihg/internal/match"
	"github.com/Dologan/ihg/internal/result"
)

// mode is the interaction state; each mode has its own key handler (see
// Model.handleKey) and view.
type mode int

const (
	modeSearch  mode = iota
	modeColon        // ":" command prompt
	modeConfirm      // delete confirmation
	modeGroups       // step-group view
	modeHelp         // full-frame help
	modeEdit         // in-window command editor
)

// sortMode orders the visible list.
type sortMode int

const (
	sortRelevance sortMode = iota
	sortRecency
	sortAZ
	sortZA
)

func (s sortMode) tag() string {
	switch s {
	case sortRecency:
		return "recency"
	case sortAZ:
		return "A-Z"
	case sortZA:
		return "Z-A"
	}
	return ""
}

// Model is the picker's Bubble Tea model.
type Model struct {
	cfg  Config
	mode mode

	cmds    []string
	times   []int64 // parallel to cmds (may be short or nil)
	sort    sortMode
	matcher *match.Matcher
	pat     *match.Pattern
	hits    []match.Hit // best first

	query []rune
	qpos  int // cursor position in query, in runes

	matchMode match.Mode

	// ret is the mode (search or groups) that the colon prompt, the delete
	// confirmation and the editor return to; see base.
	ret mode
	ed  lineEdit // buffer of modeEdit; independent of the history list

	cursor int // index into hits (0 = best)
	offset int // first visible hit (0 = best)

	colon    []rune   // text after the ':' in modeColon
	status   string   // result/error of the last action, shown in the info line
	pending  string   // command awaiting delete confirmation
	pendList []string // commands of a group awaiting delete confirmation
	pendKey  string   // key of that group
	copyMode bool
	preview  bool

	// Group view. The search cursor and scroll are parked in sCursor/sOffset
	// while it is open, so cursor/offset drive whichever list is on screen.
	groups           []groups.Group
	expanded         map[string]bool
	grows            []grow
	sCursor, sOffset int

	helpOff  int  // first visible help line
	helpFrom mode // mode to return to when help closes

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
		cfg:       cfg,
		cmds:      cfg.Commands,
		times:     cfg.Times,
		expanded:  map[string]bool{},
		matcher:   match.NewMatcher(),
		matchMode: cfg.Match,
		query:     []rune(cfg.Query),
		termW:     80,
		termH:     24,
		res:       Result{Action: result.None},
		now:       time.Now,
	}
	m.qpos = len(m.query)
	m.refilter()
	return m
}

// Result returns the user's choice (Action None if cancelled or unfinished).
func (m Model) Result() Result { return m.res }

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return m.tick() }

// reloadMsg fires once a second when Config.Reload is set.
type reloadMsg time.Time

// reloadEvery is the live-reload polling interval.
const reloadEvery = time.Second

func (m Model) tick() tea.Cmd {
	if m.cfg.Reload == nil {
		return nil
	}
	return tea.Tick(reloadEvery, func(t time.Time) tea.Msg { return reloadMsg(t) })
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termW, m.termH = msg.Width, msg.Height
		m.clampView()
		return m, nil
	case reloadMsg:
		return m.reload()
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
	m.status = ""
	switch m.mode {
	case modeColon:
		return m.keyColon(msg)
	case modeConfirm:
		return m.keyConfirm(msg)
	case modeGroups:
		return m.keyGroups(msg)
	case modeHelp:
		return m.keyHelp(msg)
	case modeEdit:
		return m.keyEdit(msg)
	default:
		return m.keySearch(msg)
	}
}

func (m Model) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeSearch:
		return m.setQline(m.qline().insert(pasteRunes(msg.Content))), nil
	case modeEdit:
		m.ed = m.ed.insert(pasteRunes(msg.Content))
	}
	return m, nil
}

// handleWheel moves the selection one row per wheel step, in the direction
// the list visibly scrolls.
func (m Model) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if !m.cfg.Mouse || (m.mode != modeSearch && m.mode != modeGroups) {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		return m.move(m.listStep(-1)), nil
	case tea.MouseWheelDown:
		return m.move(m.listStep(+1)), nil
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
		row = n - 1 - (y - m.previewH()) // best hit is just above the info line
	}
	if row < 0 || row >= n {
		return 0, false
	}
	rank := m.offset + row
	return rank, rank < len(m.hits)
}

func (m Model) quit(a result.Action, cmd string) (tea.Model, tea.Cmd) {
	m.done = true
	m.res = Result{Action: a, Command: cmd, Message: m.res.Message}
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
	ks := msg.Keystroke()
	switch ks {
	// Results.
	case "enter":
		if c, ok := m.selected(); ok {
			return m.activate(c)
		}
		return m, nil
	case "tab":
		return m.edit()
	case "f1":
		return m.openHelp(), nil
	case "ctrl+r":
		return m.cycleSort(), nil
	case "ctrl+/", "ctrl+_":
		m.preview = !m.preview
		m.clampView()
		return m, nil
	case "left":
		if m.qpos == 0 {
			return m.edit()
		}
	case "esc":
		wasCopy := m.copyMode
		m.copyMode = false
		if len(m.query) > 0 {
			return m.setQuery(nil), nil
		}
		if wasCopy {
			return m, nil
		}
		return m.quit(result.None, "")
	case "ctrl+c", "ctrl+g", "ctrl+q":
		return m.quit(result.None, "")
	case "ctrl+d":
		if len(m.query) == 0 {
			return m.quit(result.None, "")
		}
		return m.setQline(m.qline().deleteRange(m.qpos, m.qpos+1)), nil

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

	case "right":
		if m.qpos >= len(m.query) {
			m.copyMode = !m.copyMode
			return m, nil
		}
	}

	// Filter-text cursor and editing keys.
	if e, ok := m.qline().handleKey(ks); ok {
		return m.setQline(e), nil
	}

	// Anything with ctrl/alt that is not bound above is ignored; otherwise
	// printable text is inserted.
	in := keyRunes(msg)
	if len(in) == 0 {
		return m, nil
	}
	if len(m.query) == 0 {
		switch string(in) {
		case ":":
			m.mode, m.ret, m.colon = modeColon, modeSearch, nil
			return m, nil
		case "?":
			m.preview = !m.preview
			m.clampView()
			return m, nil
		}
	}
	return m.setQline(m.qline().insert(in)), nil
}

// edit edits the command under the cursor (or the selected group member).
func (m Model) edit() (tea.Model, tea.Cmd) {
	c, ok := m.target()
	if !ok {
		if m.base() == modeGroups {
			m.status = "select a command"
		}
		return m, nil
	}
	return m.startEdit(c)
}

// startEdit opens the in-window editor on cmd, or hands cmd to the shell's
// command line when Config.EditShell is set.
func (m Model) startEdit(cmd string) (tea.Model, tea.Cmd) {
	if m.cfg.EditShell {
		return m.quit(result.Edit, cmd)
	}
	m.ret, m.mode = m.mode, modeEdit
	m.ed = newLineEdit(cmd)
	return m, nil
}

// keyEdit handles keys in modeEdit.
func (m Model) keyEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ks := msg.Keystroke()
	switch ks {
	case "enter":
		text := m.ed.String()
		if strings.TrimSpace(text) == "" {
			m.status = "empty command"
			return m, nil
		}
		return m.activate(text)
	case "esc":
		m.mode = m.ret
		return m, nil
	case "ctrl+c", "ctrl+g", "ctrl+q":
		return m.quit(result.None, "")
	case "ctrl+k":
		m.ed = m.ed.deleteRange(m.ed.pos, len(m.ed.buf))
		return m, nil
	}
	if e, ok := m.ed.handleKey(ks); ok {
		m.ed = e
		return m, nil
	}
	m.ed = m.ed.insert(keyRunes(msg))
	return m, nil
}

// base is the mode whose list is on screen: the colon prompt, delete
// confirmation and editor draw over search or the group view.
func (m Model) base() mode {
	switch m.mode {
	case modeColon, modeConfirm, modeEdit:
		return m.ret
	}
	return m.mode
}

// target is the command the colon commands and Tab act on: the hit under the
// cursor, or in the group view the selected member (not a header).
func (m Model) target() (string, bool) {
	if m.base() == modeGroups {
		if m.cursor >= 0 && m.cursor < len(m.grows) && !m.grows[m.cursor].header {
			return m.grows[m.cursor].cmd, true
		}
		return "", false
	}
	return m.selected()
}

// qline is the filter text as a lineEdit.
func (m Model) qline() lineEdit { return lineEdit{buf: m.query, pos: m.qpos} }

// setQline stores an edited filter text, refiltering when its text changed.
func (m Model) setQline(e lineEdit) Model {
	if !slices.Equal(e.buf, m.query) {
		m = m.setQuery(e.buf)
	}
	m.qpos = e.pos
	return m
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

// listStep is visualStep for the search list; the group view is a plain
// top-down tree in every layout.
func (m Model) listStep(d int) int {
	if m.mode == modeGroups {
		return d
	}
	return m.visualStep(d)
}

func (m Model) move(delta int) Model {
	n := m.rowCount()
	if n == 0 {
		return m
	}
	m.cursor = min(max(m.cursor+delta, 0), n-1)
	m.clampView()
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
	m.pat = match.Parse(string(m.query), m.matchMode)
	m.hits = m.matcher.MatchAll(m.pat, m.cmds, false)
	m.applySort()
	m.cursor, m.offset = 0, 0
}

// applySort reorders hits (best-first from the matcher) per m.sort. The sort
// is stable and falls back to recency so equal keys keep a fixed order.
func (m *Model) applySort() {
	cmp := func(a, b match.Hit) int { return a.Index - b.Index }
	switch m.sort {
	case sortRelevance:
		return
	case sortRecency:
	case sortAZ, sortZA:
		sign := 1
		if m.sort == sortZA {
			sign = -1
		}
		cmp = func(a, b match.Hit) int {
			if c := strings.Compare(strings.ToLower(m.cmds[a.Index]), strings.ToLower(m.cmds[b.Index])); c != 0 {
				return sign * c
			}
			return a.Index - b.Index
		}
	}
	slices.SortStableFunc(m.hits, cmp)
}

// cycleSort steps relevance -> recency -> A-Z -> relevance.
func (m Model) cycleSort() Model {
	switch m.sort {
	case sortRelevance:
		m.sort = sortRecency
	case sortRecency:
		m.sort = sortAZ
	default:
		m.sort = sortRelevance
	}
	m.refilter()
	return m
}

// rowCount is the number of rows in the list on screen.
func (m Model) rowCount() int {
	if m.base() == modeGroups {
		return len(m.grows)
	}
	return len(m.hits)
}

// timeAt returns the newest timestamp of cmds[i], 0 if unknown.
func (m Model) timeAt(i int) int64 {
	if i < 0 || i >= len(m.times) {
		return 0
	}
	return m.times[i]
}

// frameLines is the number of lines the UI occupies.
func (m Model) frameLines() int {
	if m.cfg.Height.full() {
		return max(m.termH, minLines)
	}
	return m.cfg.Height.resolve(m.termH)
}

// listRows is the number of list rows in the frame (frame minus info, prompt
// and the preview pane).
func (m Model) listRows() int { return max(m.frameLines()-2-m.previewH(), 1) }

// previewH is the height of the preview pane including its separator line:
// 40% of the frame, at least 3, leaving the list at least one row. Zero when
// the pane is off, in the group view/help, or the frame is too small.
func (m Model) previewH() int {
	if !m.preview || m.base() == modeGroups || m.mode == modeHelp {
		return 0
	}
	h := m.frameLines()
	p := min(max(h*2/5, 3), h-3)
	if p < 2 {
		return 0
	}
	return p
}

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
