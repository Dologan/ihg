package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Dologan/ihg/internal/result"
)

// --- helpers ---------------------------------------------------------------

func press(keystroke string) tea.KeyPressMsg {
	k := tea.Key{}
	rest := keystroke
	for {
		switch {
		case strings.HasPrefix(rest, "ctrl+") && len(rest) > 5:
			k.Mod |= tea.ModCtrl
			rest = rest[5:]
			continue
		case strings.HasPrefix(rest, "alt+") && len(rest) > 4:
			k.Mod |= tea.ModAlt
			rest = rest[4:]
			continue
		}
		break
	}
	switch rest {
	case "enter":
		k.Code = tea.KeyEnter
	case "tab":
		k.Code = tea.KeyTab
	case "esc":
		k.Code = tea.KeyEscape
	case "backspace":
		k.Code = tea.KeyBackspace
	case "delete":
		k.Code = tea.KeyDelete
	case "up":
		k.Code = tea.KeyUp
	case "down":
		k.Code = tea.KeyDown
	case "left":
		k.Code = tea.KeyLeft
	case "right":
		k.Code = tea.KeyRight
	case "home":
		k.Code = tea.KeyHome
	case "end":
		k.Code = tea.KeyEnd
	case "f1":
		k.Code = tea.KeyF1
	case "pgup":
		k.Code = tea.KeyPgUp
	case "pgdown":
		k.Code = tea.KeyPgDown
	default:
		r := []rune(rest)
		k.Code = r[0]
		if k.Mod == 0 {
			k.Text = rest
		}
	}
	return tea.KeyPressMsg(k)
}

func feed(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	return m
}

func keys(m Model, ks ...string) Model {
	for _, k := range ks {
		m = feed(m, press(k))
	}
	return m
}

func typed(m Model, s string) Model {
	for _, r := range s {
		m = feed(m, press(string(r)))
	}
	return m
}

func size(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }

func newModel(cmds []string, w, h int) Model {
	m := New(Config{Commands: cmds, Height: Height{Percent: 100}})
	return feed(m, size(w, h))
}

func lines(m Model) []string {
	return strings.Split(ansi.Strip(m.View().Content), "\n")
}

var sample = []string{"git status", "git commit -m x", "ls -la", "make test", "go build ./...", "echo hi"}

// --- ParseHeight -----------------------------------------------------------

func TestParseHeight(t *testing.T) {
	tests := []struct {
		in      string
		want    Height
		wantErr bool
	}{
		{"40%", Height{Percent: 40}, false},
		{"100%", Height{Percent: 100}, false},
		{"15", Height{Lines: 15}, false},
		{" 7 ", Height{Lines: 7}, false},
		{"1", Height{Lines: 1}, false},
		{"0", Height{}, true},
		{"0%", Height{}, true},
		{"101%", Height{}, true},
		{"-5", Height{}, true},
		{"", Height{}, true},
		{"abc", Height{}, true},
		{"%", Height{}, true},
		{"1.5", Height{}, true},
	}
	for _, tt := range tests {
		got, err := ParseHeight(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseHeight(%q) = %+v, %v; want %+v err=%v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestHeightResolve(t *testing.T) {
	tests := []struct {
		h    Height
		rows int
		want int
	}{
		{Height{Percent: 40}, 50, 20},
		{Height{Percent: 10}, 20, 3}, // min 3
		{Height{Lines: 15}, 50, 15},
		{Height{Lines: 15}, 10, 10}, // capped to terminal
		{Height{Lines: 1}, 50, 3},
		{Height{Percent: 100}, 30, 30},
	}
	for _, tt := range tests {
		if got := tt.h.resolve(tt.rows); got != tt.want {
			t.Errorf("%+v.resolve(%d) = %d, want %d", tt.h, tt.rows, got, tt.want)
		}
	}
}

// --- query editing ---------------------------------------------------------

func TestQueryEditing(t *testing.T) {
	tests := []struct {
		name      string
		start     string
		keys      []string
		wantQuery string
		wantPos   int
	}{
		{"type", "", []string{"a", "b", "c"}, "abc", 3},
		{"backspace", "abc", []string{"backspace"}, "ab", 2},
		{"ctrl-h", "abc", []string{"ctrl+h"}, "ab", 2},
		{"backspace at 0", "abc", []string{"ctrl+a", "backspace"}, "abc", 0},
		{"delete", "abc", []string{"ctrl+a", "delete"}, "bc", 0},
		{"ctrl-d deletes char", "abc", []string{"ctrl+a", "ctrl+d"}, "bc", 0},
		{"insert at cursor", "ac", []string{"ctrl+b", "b"}, "abc", 2},
		{"right", "abc", []string{"ctrl+a", "right", "x"}, "axbc", 2},
		{"ctrl-f", "abc", []string{"ctrl+a", "ctrl+f", "x"}, "axbc", 2},
		{"left then insert", "abc", []string{"left", "x"}, "abxc", 3},
		{"home end", "abc", []string{"home", "x", "end", "y"}, "xabcy", 5},
		{"ctrl-u", "abc def", []string{"left", "left", "left", "ctrl+u"}, "def", 0},
		{"ctrl-w", "foo bar", []string{"ctrl+w"}, "foo ", 4},
		{"ctrl-w trailing space", "foo bar  ", []string{"ctrl+w"}, "foo ", 4},
		{"alt-backspace", "foo bar", []string{"alt+backspace"}, "foo ", 4},
		{"alt-b", "foo bar", []string{"alt+b", "x"}, "foo xbar", 5},
		{"alt-f", "foo bar", []string{"ctrl+a", "alt+f", "x"}, "foox bar", 4},
		{"alt-f skips spaces", "foo  bar", []string{"ctrl+a", "alt+f", "alt+f", "x"}, "foo  barx", 9},
		{"utf8 insert", "", []string{"é", "日", "x"}, "é日x", 3},
		{"utf8 backspace", "é日", []string{"backspace"}, "é", 1},
		{"unbound ctrl ignored", "ab", []string{"ctrl+z"}, "ab", 2},
		{"unbound alt ignored", "ab", []string{"alt+z"}, "ab", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(Config{Commands: sample, Query: tt.start})
			m = keys(m, tt.keys...)
			if string(m.query) != tt.wantQuery || m.qpos != tt.wantPos {
				t.Errorf("query=%q pos=%d, want %q pos=%d", string(m.query), m.qpos, tt.wantQuery, tt.wantPos)
			}
			if m.done {
				t.Error("unexpectedly done")
			}
		})
	}
}

func TestPaste(t *testing.T) {
	m := New(Config{Commands: sample})
	m = feed(m, tea.PasteMsg{Content: "git\nst\x1b[31m\tx"})
	if got := string(m.query); got != "git st[31m x" {
		t.Errorf("query = %q", got)
	}
}

func TestInitialQueryFilters(t *testing.T) {
	m := New(Config{Commands: sample, Query: "git"})
	if len(m.hits) != 2 || m.qpos != 3 {
		t.Errorf("hits=%d pos=%d", len(m.hits), m.qpos)
	}
}

func TestQueryChangeResetsCursor(t *testing.T) {
	m := newModel(sample, 40, 10)
	m = keys(m, "up", "up")
	if m.cursor != 2 {
		t.Fatalf("cursor = %d", m.cursor)
	}
	m = typed(m, "g")
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("cursor=%d offset=%d after query change", m.cursor, m.offset)
	}
}

// --- results ---------------------------------------------------------------

func TestResults(t *testing.T) {
	tests := []struct {
		name    string
		start   string
		keys    []string
		want    Result
		wantEnd bool
	}{
		{"enter runs top", "", []string{"enter"}, Result{Action: result.Run, Command: "git status"}, true},
		{"enter after move", "", []string{"up", "enter"}, Result{Action: result.Run, Command: "git commit -m x"}, true},
		{"enter after filter", "make", []string{"enter"}, Result{Action: result.Run, Command: "make test"}, true},
		{"tab edits", "", []string{"tab"}, Result{Action: result.Edit, Command: "git status"}, true},
		{"left at 0 edits", "", []string{"left"}, Result{Action: result.Edit, Command: "git status"}, true},
		{"left at 0 after home", "ls", []string{"home", "left"}, Result{Action: result.Edit, Command: "ls -la"}, true},
		{"left mid-query moves", "ls", []string{"left"}, Result{Action: result.None, Command: ""}, false},
		{"esc clears query", "ls", []string{"esc"}, Result{Action: result.None, Command: ""}, false},
		{"esc on empty cancels", "", []string{"esc"}, Result{Action: result.None, Command: ""}, true},
		{"ctrl-c", "ls", []string{"ctrl+c"}, Result{Action: result.None, Command: ""}, true},
		{"ctrl-g", "ls", []string{"ctrl+g"}, Result{Action: result.None, Command: ""}, true},
		{"ctrl-q", "ls", []string{"ctrl+q"}, Result{Action: result.None, Command: ""}, true},
		{"ctrl-d empty cancels", "", []string{"ctrl+d"}, Result{Action: result.None, Command: ""}, true},
		{"ctrl-d nonempty doesn't", "ls", []string{"ctrl+d"}, Result{Action: result.None, Command: ""}, false},
		{"no match enter ignored", "zzzzqqq", []string{"enter"}, Result{Action: result.None, Command: ""}, false},
		{"no match tab ignored", "zzzzqqq", []string{"tab"}, Result{Action: result.None, Command: ""}, false},
		{"no match left ignored", "zzzzqqq", []string{"home", "left"}, Result{Action: result.None, Command: ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(Config{Commands: sample, Query: tt.start})
			m = feed(m, size(80, 24))
			var cmd tea.Cmd
			for _, k := range tt.keys {
				var nm tea.Model
				nm, cmd = m.Update(press(k))
				m = nm.(Model)
			}
			if m.Result() != tt.want {
				t.Errorf("result = %+v, want %+v", m.Result(), tt.want)
			}
			if m.done != tt.wantEnd {
				t.Errorf("done = %v, want %v", m.done, tt.wantEnd)
			}
			if tt.wantEnd {
				if cmd == nil {
					t.Fatal("expected quit command")
				}
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Errorf("command did not produce QuitMsg")
				}
			}
		})
	}
}

func TestEscClearThenCancel(t *testing.T) {
	m := New(Config{Commands: sample, Query: "ls"})
	m = keys(m, "esc")
	if len(m.query) != 0 || m.done {
		t.Fatalf("after first esc: query=%q done=%v", string(m.query), m.done)
	}
	m = keys(m, "esc")
	if !m.done || m.Result().Action != result.None {
		t.Errorf("after second esc: %+v done=%v", m.Result(), m.done)
	}
}

// --- movement and scrolling -----------------------------------------------

func manyCmds(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "cmd" + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + string(rune('0'+i%10))
	}
	// make them unique so matching stays predictable
	for i := range out {
		out[i] += "#" + strings.Repeat("-", i/26) + string(rune('A'+i%26))
	}
	return out
}

func TestMovementDefaultLayout(t *testing.T) {
	m := newModel(manyCmds(30), 40, 10) // 8 list rows
	m = keys(m, "up")
	if m.cursor != 1 {
		t.Errorf("up: cursor=%d", m.cursor)
	}
	m = keys(m, "down", "down")
	if m.cursor != 0 {
		t.Errorf("down clamps at 0: cursor=%d", m.cursor)
	}
	for _, k := range []string{"ctrl+p", "ctrl+k"} {
		m = keys(m, k)
	}
	if m.cursor != 2 {
		t.Errorf("ctrl-p/k: cursor=%d", m.cursor)
	}
	for _, k := range []string{"ctrl+n", "ctrl+j"} {
		m = keys(m, k)
	}
	if m.cursor != 0 {
		t.Errorf("ctrl-n/j: cursor=%d", m.cursor)
	}
}

func TestMovementReverseLayout(t *testing.T) {
	m := New(Config{Commands: sample, Layout: LayoutReverse})
	m = keys(m, "down")
	if m.cursor != 1 {
		t.Errorf("down: cursor=%d", m.cursor)
	}
	m = keys(m, "up", "up")
	if m.cursor != 0 {
		t.Errorf("up clamps: cursor=%d", m.cursor)
	}
}

func TestScrolling(t *testing.T) {
	m := newModel(manyCmds(30), 40, 10) // 8 rows
	for i := 0; i < 10; i++ {
		m = keys(m, "up")
	}
	if m.cursor != 10 || m.offset != 3 {
		t.Errorf("cursor=%d offset=%d, want 10/3", m.cursor, m.offset)
	}
	m = keys(m, "down", "down")
	if m.cursor != 8 || m.offset != 3 {
		t.Errorf("scroll back inside window: cursor=%d offset=%d", m.cursor, m.offset)
	}
	for i := 0; i < 6; i++ {
		m = keys(m, "down")
	}
	if m.cursor != 2 || m.offset != 2 {
		t.Errorf("cursor=%d offset=%d, want 2/2", m.cursor, m.offset)
	}
	// The cursor row is always rendered.
	sel := 0
	for _, l := range lines(m) {
		if strings.HasPrefix(l, "> ") && strings.Contains(l, m.cmds[m.hits[m.cursor].Index]) {
			sel++
		}
	}
	if sel != 1 {
		t.Errorf("selected row rendered %d times:\n%s", sel, strings.Join(lines(m), "\n"))
	}
}

func TestPaging(t *testing.T) {
	m := newModel(manyCmds(30), 40, 10) // 8 rows
	m = keys(m, "pgup")
	if m.cursor != 8 {
		t.Errorf("pgup: cursor=%d", m.cursor)
	}
	m = keys(m, "pgup", "pgup", "pgup", "pgup")
	if m.cursor != 29 {
		t.Errorf("pgup clamps at last: cursor=%d", m.cursor)
	}
	m = keys(m, "pgdown")
	if m.cursor != 21 {
		t.Errorf("pgdown: cursor=%d", m.cursor)
	}
	m = keys(m, "pgdown", "pgdown", "pgdown", "pgdown")
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("pgdown clamps at 0: cursor=%d offset=%d", m.cursor, m.offset)
	}
}

func TestResizeKeepsCursorVisible(t *testing.T) {
	m := newModel(manyCmds(30), 40, 10)
	for i := 0; i < 7; i++ {
		m = keys(m, "up")
	}
	m = feed(m, size(40, 5)) // 3 rows
	if m.cursor < m.offset || m.cursor >= m.offset+m.listRows() {
		t.Errorf("cursor %d outside window [%d,%d)", m.cursor, m.offset, m.offset+m.listRows())
	}
}

// --- rendering -------------------------------------------------------------

func TestViewDefaultLayout(t *testing.T) {
	m := newModel(sample, 40, 8) // 6 rows, 6 commands
	got := lines(m)
	if len(got) != 8 {
		t.Fatalf("lines = %d, want 8: %q", len(got), got)
	}
	// Best (index 0) is nearest the info line; worst at the top.
	want := []string{
		"  5 echo hi",
		"  4 go build ./...",
		"  3 make test",
		"  2 ls -la",
		"  1 git commit -m x",
		"> 0 git status",
		"  6/6",
		">",
	}
	for i := range want {
		if strings.TrimRight(got[i], " ") != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestViewReverseLayout(t *testing.T) {
	m := New(Config{Commands: sample, Layout: LayoutReverse, Height: Height{Percent: 100}})
	m = feed(m, size(40, 8))
	got := lines(m)
	want := []string{
		">",
		"  6/6",
		"> 0 git status",
		"  1 git commit -m x",
		"  2 ls -la",
		"  3 make test",
		"  4 go build ./...",
		"  5 echo hi",
	}
	for i := range want {
		if strings.TrimRight(got[i], " ") != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	if c := m.View().Cursor; c == nil || c.Y != 0 || c.X != 2 {
		t.Errorf("cursor = %+v", c)
	}
}

func TestViewPromptAndCursor(t *testing.T) {
	m := newModel(sample, 40, 8)
	m = typed(m, "git")
	got := lines(m)
	if got[7] != "> git" || strings.TrimSpace(got[6]) != "2/6" {
		t.Errorf("prompt=%q info=%q", got[7], got[6])
	}
	if c := m.View().Cursor; c == nil || c.X != 5 || c.Y != 7 {
		t.Errorf("cursor = %+v", c)
	}
	m = keys(m, "left")
	if c := m.View().Cursor; c.X != 4 {
		t.Errorf("cursor after left = %+v", c)
	}
}

func TestViewLongQueryKeepsCursorVisible(t *testing.T) {
	m := newModel(sample, 20, 6)
	m = typed(m, strings.Repeat("a", 50))
	c := m.View().Cursor
	if c.X >= 20 {
		t.Errorf("cursor x=%d outside width", c.X)
	}
	for _, l := range lines(m) {
		if ansi.StringWidth(l) > 20 {
			t.Errorf("line too wide: %q", l)
		}
	}
}

func TestViewInlineHeight(t *testing.T) {
	m := New(Config{Commands: sample, Height: Height{Percent: 40}})
	m = feed(m, size(40, 20))
	if got := lines(m); len(got) != 8 {
		t.Errorf("40%% of 20 rows: %d lines, want 8", len(got))
	}
	if m.View().AltScreen {
		t.Error("inline view must not use alt screen")
	}
	m = New(Config{Commands: sample, Height: Height{Lines: 5}})
	m = feed(m, size(40, 20))
	if got := lines(m); len(got) != 5 {
		t.Errorf("5 lines: %d", len(got))
	}
	m = newModel(sample, 40, 20)
	if !m.View().AltScreen {
		t.Error("100% should use alt screen")
	}
}

func TestSelectedMarkerAndRecencyLabels(t *testing.T) {
	cmds := manyCmds(15)
	m := newModel(cmds, 60, 20)
	m = typed(m, "#") // everything matches; order by score then recency
	got := lines(m)
	joined := strings.Join(got, "\n")
	for _, l := range got {
		if strings.HasPrefix(l, "> ") && strings.TrimSpace(l) != ">" && !strings.Contains(l, "cmd") && !strings.HasPrefix(l, "> #") {
			t.Errorf("odd selected row %q", l)
		}
	}
	if !strings.Contains(joined, "  9 ") {
		t.Errorf("digit 9 label missing:\n%s", joined)
	}
	// index 10+ has no digit: the gutter is blank.
	m = newModel(cmds, 60, 20)
	for _, l := range lines(m) {
		if strings.Contains(l, cmds[10]) && !strings.HasPrefix(l, "    "+cmds[10]) {
			t.Errorf("index 10 row has a label: %q", l)
		}
	}
}

func TestRecencyLabelFollowsOriginalIndexNotRank(t *testing.T) {
	m := newModel(sample, 40, 8)
	m = typed(m, "^make")
	got := lines(m)
	found := false
	for _, l := range got {
		if strings.Contains(l, "make test") {
			found = true
			if !strings.HasPrefix(l, "> 3 make test") {
				t.Errorf("row = %q", l)
			}
		}
	}
	if !found {
		t.Errorf("make test not shown: %q", got)
	}
}

func TestSanitization(t *testing.T) {
	cmds := []string{"a\nb", "x\ty", "e\x1b[31mred", "d\x7fel", "bad\xffbyte"}
	m := newModel(cmds, 40, 9)
	got := strings.Join(lines(m), "\n")
	for _, w := range []string{"a↵b", "x y", "e·[31mred", "d·el", "bad�byte"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
	raw := m.View().Content
	if strings.Contains(raw, "\x1b[31m") && strings.Contains(raw, "e\x1b[31mred") {
		t.Error("raw escape sequence leaked into view")
	}
	if strings.ContainsAny(ansi.Strip(raw), "\t\x7f\x1b") {
		t.Error("control characters leaked into view")
	}
}

func TestSanitizeIsOneToOne(t *testing.T) {
	for _, s := range []string{"a\nb", "\t\t", "\x1b[0m", "héllo", "bad\xff\xfe", "日本語\x00"} {
		if got, want := len(sanitize(s)), len([]rune(s)); got != want {
			t.Errorf("sanitize(%q) has %d runes, want %d", s, got, want)
		}
	}
}

func TestWideCharTruncation(t *testing.T) {
	// 10 CJK chars = 20 columns; width 14 leaves 10 columns for text.
	cmds := []string{"日本語日本語日本語日"}
	m := newModel(cmds, 14, 5)
	for _, l := range lines(m) {
		if w := ansi.StringWidth(l); w > 14 {
			t.Errorf("line %q is %d columns wide", l, w)
		}
	}
	row := lines(m)[2]
	if !strings.HasSuffix(strings.TrimRight(row, " "), "…") {
		t.Errorf("row not truncated with ellipsis: %q", row)
	}
	// 4 prefix + 4 wide chars (8) + … = 13 columns, a 5th wide char would not fit.
	if !strings.Contains(row, "日本語日…") || strings.Contains(row, "日本語日本") {
		t.Errorf("unexpected truncation: %q", row)
	}
}

func TestNoTruncationWhenFits(t *testing.T) {
	m := newModel([]string{"exactly"}, 11, 5) // 4 + 7
	row := lines(m)[2]
	if strings.Contains(row, "…") {
		t.Errorf("fitting row was truncated: %q", row)
	}
	m = newModel([]string{"exactly!"}, 11, 5)
	row = lines(m)[2]
	if !strings.Contains(row, "exactl…") {
		t.Errorf("row = %q", row)
	}
}

func TestHighlightSurvivesSanitization(t *testing.T) {
	// The newline becomes ↵ at the same rune index; "c" follows it.
	cmds := []string{"ab\ncd"}
	m := newModel(cmds, 40, 5)
	m = typed(m, "'cd")
	raw := m.View().Content
	// Find the styled segment: the matched text "cd" must be wrapped in its
	// own SGR run, and "ab↵" must not be part of it.
	hl := styleSelMatch.Render("cd")
	if !strings.Contains(raw, hl) {
		t.Errorf("highlighted run %q not found in %q", hl, raw)
	}
	if !strings.Contains(ansi.Strip(raw), "ab↵cd") {
		t.Errorf("stripped view = %q", ansi.Strip(raw))
	}
}

func TestHighlightPositionsWithWideChars(t *testing.T) {
	cmds := []string{"日本 test"}
	m := newModel(cmds, 40, 5)
	m = typed(m, "'test")
	if !strings.Contains(m.View().Content, styleSelMatch.Render("test")) {
		t.Errorf("highlight not on 'test': %q", m.View().Content)
	}
}

func TestNonSelectedHighlightUsesMatchStyle(t *testing.T) {
	m := newModel(sample, 40, 8)
	m = typed(m, "'git")
	// cursor on 0; row index 1 ("git commit") is not selected.
	if !strings.Contains(m.View().Content, styleMatch.Render("git")) {
		t.Error("non-selected row highlight missing")
	}
}

// --- quit ------------------------------------------------------------------

func TestEmptyViewOnQuit(t *testing.T) {
	m := newModel(sample, 40, 8)
	m = keys(m, "enter")
	if !m.done {
		t.Fatal("not done")
	}
	v := m.View()
	if v.Content != "" || v.Cursor != nil {
		t.Errorf("view after quit not empty: %q cursor=%v", v.Content, v.Cursor)
	}
	m = New(Config{Commands: sample, Height: Height{Percent: 40}})
	m = keys(feed(m, size(40, 20)), "ctrl+c")
	if v := m.View(); v.Content != "" || v.AltScreen {
		t.Errorf("inline quit view: %q alt=%v", v.Content, v.AltScreen)
	}
}

func TestEmptyCommandList(t *testing.T) {
	m := newModel(nil, 40, 6)
	if got := lines(m); len(got) != 6 || strings.TrimSpace(got[4]) != "0/0" {
		t.Errorf("view = %q", got)
	}
	m = keys(m, "enter", "up", "down", "pgup", "tab")
	if m.done {
		t.Error("should not finish with nothing to select")
	}
}
