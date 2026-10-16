package ui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Dologan/ihg/internal/match"
	"github.com/Dologan/ihg/internal/result"
)

// --- matching modes --------------------------------------------------------

func infoOf(m Model) string { l := lines(m); return l[len(l)-2] }

func TestMatchModesAndColonToggles(t *testing.T) {
	cmds := []string{"git checkout main", "git status"}
	m := feature(Config{Commands: cmds})
	if m.matchMode != match.ModeExact {
		t.Fatal("default must be exact")
	}
	if strings.Contains(infoOf(m), "[") {
		t.Errorf("exact shows no tag: %q", infoOf(m))
	}
	m = typed(m, "gco")
	if len(m.hits) != 0 {
		t.Errorf("exact: gco hits = %d", len(m.hits))
	}
	m = keys(m, "ctrl+u")
	m = colon(m, "f")
	if m.matchMode != match.ModeFuzzy || !strings.Contains(status(m), "match: fuzzy") {
		t.Fatalf(":f: mode %v status %q", m.matchMode, status(m))
	}
	m = typed(m, "gco")
	if len(m.hits) != 1 || !strings.Contains(infoOf(m), "[fuzzy]") {
		t.Errorf("fuzzy: hits %d info %q", len(m.hits), infoOf(m))
	}
	steps := []struct {
		cmd  string
		want match.Mode
	}{
		{"f", match.ModeExact},
		{"f", match.ModeFuzzy},
		{"p", match.ModeRegex},
		{"f", match.ModeFuzzy},
		{"p", match.ModeRegex},
		{"p", match.ModeExact},
	}
	m = keys(m, "ctrl+u")
	for _, st := range steps {
		m = colon(m, st.cmd)
		if m.matchMode != st.want {
			t.Fatalf(":%s -> %v, want %v", st.cmd, m.matchMode, st.want)
		}
	}
}

func TestRegexModeUI(t *testing.T) {
	m := feature(Config{Commands: []string{"git checkout main", "git status", "ls"}, Match: match.ModeRegex})
	if !strings.Contains(infoOf(m), "[regex]") {
		t.Errorf("info = %q", infoOf(m))
	}
	m = typed(m, `^git\s+st`)
	if got := hitTexts(m); !reflect.DeepEqual(got, []string{"git status"}) {
		t.Errorf("hits = %v", got)
	}
	m = keys(m, "ctrl+u")
	m = typed(m, "(")
	if len(m.hits) != 0 || !strings.Contains(infoOf(m), "regex error") {
		t.Errorf("invalid: hits %d info %q", len(m.hits), infoOf(m))
	}
	if m2 := keys(m, "enter"); m2.done {
		t.Error("enter with no hits must not quit")
	}
	m = keys(m, "backspace")
	if len(m.hits) != 3 || strings.Contains(infoOf(m), "regex error") {
		t.Errorf("recovered: hits %d", len(m.hits))
	}
}

func TestColonResetRestoresConfigMatch(t *testing.T) {
	m := feature(Config{Match: match.ModeFuzzy})
	m = colon(m, "p")
	m = colon(m, "r")
	if m.matchMode != match.ModeFuzzy {
		t.Errorf("mode = %v, want fuzzy (Config.Match)", m.matchMode)
	}
	m = feature(Config{})
	m = colon(colon(m, "f"), "r")
	if m.matchMode != match.ModeExact {
		t.Errorf("mode = %v, want exact", m.matchMode)
	}
}

func TestMatchTogglesInGroupView(t *testing.T) {
	m := feature(Config{Commands: []string{"git status", "ls"}})
	m = colon(m, "g")
	m = colon(m, "f")
	if m.mode != modeGroups || m.matchMode != match.ModeFuzzy || len(m.grows) != 2 {
		t.Errorf("mode %v match %v rows %d", m.mode, m.matchMode, len(m.grows))
	}
}

// --- group view ------------------------------------------------------------

var groupCmds = []string{"git status", "ls | grep x", "git log", "grep y"}

func rowIndex(ls []string, sub string) int {
	for i, l := range ls {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

func lastLine(m Model) string { l := lines(m); return l[len(l)-1] }

func TestGroupsRenderTopDownInBothLayouts(t *testing.T) {
	for _, layout := range []Layout{LayoutDefault, LayoutReverse} {
		m := feature(Config{Commands: groupCmds, Layout: layout})
		m = keys(colon(m, "g"), "+")
		ls := lines(m)
		if len(ls) != 20 {
			t.Fatalf("layout %v: %d lines", layout, len(ls))
		}
		git, st, lg, grep := rowIndex(ls, "▾ [2] git"), rowIndex(ls, "    git status"), rowIndex(ls, "    git log"), rowIndex(ls, "▸ [2] grep")
		if !(git < st && st < lg && lg < grep) || git < 0 {
			t.Errorf("layout %v: git %d status %d log %d grep %d\n%s", layout, git, st, lg, grep, strings.Join(ls, "\n"))
		}
		if layout == LayoutDefault {
			if git != 0 || !strings.Contains(ls[19], "groups>") || !strings.Contains(ls[18], "groups") {
				t.Errorf("default layout: tree top-aligned, info+prompt at the bottom:\n%s", strings.Join(ls, "\n"))
			}
			if strings.TrimSpace(ls[10]) != "" {
				t.Errorf("blank padding expected between tree and info: %q", ls[10])
			}
		} else if !strings.Contains(ls[0], "groups>") || git != 2 {
			t.Errorf("reverse layout: prompt, info, then the tree:\n%s", strings.Join(ls, "\n"))
		}
	}
}

func TestGroupsUpDownIndependentOfLayout(t *testing.T) {
	for _, layout := range []Layout{LayoutDefault, LayoutReverse} {
		m := feature(Config{Commands: groupCmds, Layout: layout})
		m = keys(colon(m, "g"), "+")
		if m.cursor != 0 {
			t.Fatal("cursor starts on the first header")
		}
		m = keys(m, "down")
		if r := m.grows[m.cursor]; r.header || r.cmd != "git status" {
			t.Errorf("layout %v: down -> %+v", layout, r)
		}
		m = keys(m, "up")
		if m.cursor != 0 {
			t.Errorf("layout %v: up -> %d", layout, m.cursor)
		}
		m = keys(m, "pgdown")
		if m.cursor != len(m.grows)-1 {
			t.Errorf("pgdown -> %d", m.cursor)
		}
	}
}

func TestGroupsScrollKeepsCursorVisible(t *testing.T) {
	var cmds []string
	for i := 0; i < 40; i++ {
		cmds = append(cmds, fmt.Sprintf("c%02d", i))
	}
	for _, layout := range []Layout{LayoutDefault, LayoutReverse} {
		m := feature(Config{Commands: cmds, Layout: layout})
		m = colon(m, "g")
		for i := 0; i < 35; i++ {
			m = keys(m, "down")
			found := false
			for _, l := range lines(m) {
				if strings.HasPrefix(l, "> ") {
					found = true
				}
			}
			if !found {
				t.Fatalf("layout %v: cursor %d not visible", layout, m.cursor)
			}
		}
		if got := lines(m); rowIndex(got, "> ▸ [1] c35") < 0 {
			t.Errorf("layout %v: selected row wrong:\n%s", layout, strings.Join(got, "\n"))
		}
		m = keys(m, "pgup")
		if m.cursor != 35-m.listRows() {
			t.Errorf("pgup cursor = %d", m.cursor)
		}
	}
}

func TestGroupsWheel(t *testing.T) {
	m := feature(Config{Commands: groupCmds, Mouse: true})
	m = colon(m, "g")
	m = feed(m, wheel(tea.MouseWheelDown))
	if m.cursor != 1 {
		t.Errorf("wheel down -> %d", m.cursor)
	}
	m = feed(m, wheel(tea.MouseWheelUp))
	if m.cursor != 0 {
		t.Errorf("wheel up -> %d", m.cursor)
	}
	m = feature(Config{Commands: groupCmds})
	m = feed(colon(m, "g"), wheel(tea.MouseWheelDown))
	if m.cursor != 0 {
		t.Error("wheel must be ignored without --mouse")
	}
}

func TestGroupsMinusOnMemberCollapsesParent(t *testing.T) {
	m := feature(Config{Commands: groupCmds})
	m = keys(colon(m, "g"), "down", "+") // cursor on header grep, expand
	m = keys(m, "down")
	if r := m.grows[m.cursor]; r.header {
		t.Fatalf("setup: %+v", r)
	}
	n := len(m.grows)
	if p := keys(m, "+"); len(p.grows) != n || p.cursor != m.cursor {
		t.Error("+ on a member is a no-op")
	}
	m = keys(m, "-")
	r := m.grows[m.cursor]
	if !r.header || m.groups[r.g].Key != "grep" || m.expanded["grep"] || len(m.grows) != 3 {
		t.Errorf("after -: row %+v rows %d", r, len(m.grows))
	}
}

// delFake records Delete calls and answers removed counts per command.
type delFake struct {
	calls  []string
	failOn string
}

func (d *delFake) del(cmd string) (int, error) {
	if cmd == d.failOn {
		return 0, errors.New("boom")
	}
	d.calls = append(d.calls, cmd)
	return 3, nil
}

func TestGroupsDeleteMember(t *testing.T) {
	for _, key := range []string{"d", "delete"} {
		d := &delFake{}
		m := feature(Config{Commands: groupCmds, Delete: d.del})
		m = keys(colon(m, "g"), "+", "down", key)
		if m.mode != modeConfirm || !strings.Contains(lastLine(m), `delete "git status" from history? [y/N]`) {
			t.Fatalf("%s: mode %v prompt %q", key, m.mode, lastLine(m))
		}
		if x := keys(m, "n"); x.mode != modeGroups || len(d.calls) != 0 || x.status != "not deleted" {
			t.Errorf("cancel: mode %v calls %v", x.mode, d.calls)
		}
		m = keys(m, "y")
		if m.mode != modeGroups {
			t.Fatalf("must return to the group view, got %v", m.mode)
		}
		if !reflect.DeepEqual(d.calls, []string{"git status"}) || len(m.cmds) != 3 {
			t.Errorf("calls %v cmds %v", d.calls, m.cmds)
		}
		if !strings.Contains(m.status, "deleted 3 entries") || !m.expanded["git"] {
			t.Errorf("status %q expanded %v", m.status, m.expanded)
		}
		if r := m.grows[m.cursor]; r.header || r.cmd != "git log" {
			t.Errorf("cursor should sit on the next row (git log): %+v", r)
		}
	}
}

func TestGroupsDeleteHeader(t *testing.T) {
	d := &delFake{}
	m := feature(Config{Commands: groupCmds, Delete: d.del, Times: []int64{1, 2, 3, 4}})
	m = keys(colon(m, "g"), "down", "d") // header grep: "ls | grep x" and "grep y"
	if got := lastLine(m); !strings.Contains(got, `delete 2 commands in group "grep"? [y/N]`) {
		t.Fatalf("prompt = %q", got)
	}
	m = keys(m, "y")
	if m.mode != modeGroups {
		t.Fatalf("mode = %v", m.mode)
	}
	if !reflect.DeepEqual(d.calls, []string{"ls | grep x", "grep y"}) {
		t.Errorf("calls = %v", d.calls)
	}
	if !reflect.DeepEqual(m.cmds, []string{"git status", "git log"}) || !reflect.DeepEqual(m.times, []int64{1, 3}) {
		t.Errorf("cmds %v times %v", m.cmds, m.times)
	}
	if m.status != "deleted 6 entries (2 commands)" {
		t.Errorf("status = %q", m.status)
	}
	// the "ls" group vanished with its only member; the cursor stays valid
	if len(m.grows) != 1 || m.cursor != 0 {
		t.Errorf("rows %d cursor %d", len(m.grows), m.cursor)
	}
	// :d does the same in the group view
	m2 := feature(Config{Commands: groupCmds, Delete: (&delFake{}).del})
	m2 = colon(colon(m2, "g"), "d")
	if m2.mode != modeConfirm {
		t.Error(":d in the group view should confirm")
	}
}

func TestGroupsDeleteHeaderStopsOnError(t *testing.T) {
	d := &delFake{failOn: "grep y"}
	m := feature(Config{Commands: groupCmds, Delete: d.del})
	m = keys(colon(m, "g"), "down", "d", "y")
	if m.mode != modeGroups {
		t.Fatalf("mode = %v", m.mode)
	}
	if !reflect.DeepEqual(d.calls, []string{"ls | grep x"}) || len(m.cmds) != 3 {
		t.Errorf("calls %v cmds %v", d.calls, m.cmds)
	}
	if !strings.Contains(m.status, "boom") {
		t.Errorf("status = %q", m.status)
	}
	d = &delFake{failOn: "ls | grep x"}
	m = feature(Config{Commands: groupCmds, Delete: d.del})
	m = keys(colon(m, "g"), "down", "d", "y")
	if len(m.cmds) != 4 || m.status != "delete failed: boom" {
		t.Errorf("cmds %v status %q", m.cmds, m.status)
	}
}

func TestGroupsDeleteWithoutCallback(t *testing.T) {
	m := feature(Config{Commands: groupCmds})
	m = keys(colon(m, "g"), "d")
	if m.mode != modeGroups || !strings.Contains(m.status, "not available") {
		t.Errorf("mode %v status %q", m.mode, m.status)
	}
}

func TestGroupsCopyMode(t *testing.T) {
	m := feature(Config{Commands: groupCmds})
	m = keys(colon(m, "g"), "+", "down")
	m = keys(m, "right")
	if !m.copyMode || !strings.Contains(lastLine(m), "[COPY]") {
		t.Fatalf("copy mode %v prompt %q", m.copyMode, lastLine(m))
	}
	_, cmd := upd(m, "enter")
	if kind, payload, quit := oscOf(flatten(cmd)); kind != "clipboard" || payload != "git status" || !quit {
		t.Errorf("enter copies: %q %q %v", kind, payload, quit)
	}
	_, cmd = upd(colon2(m, "c"), "enter")
	if _, payload, _ := oscOf(flatten(cmd)); payload != "git status" {
		t.Errorf(":c payload %q", payload)
	}
	h := colon(keys(m, "up"), "c")
	if h.done || h.status != "select a command" {
		t.Errorf("header :c: done %v status %q", h.done, h.status)
	}
	// Esc leaves copy mode first, then the group view.
	m = keys(m, "esc")
	if m.mode != modeGroups || m.copyMode {
		t.Errorf("first esc: mode %v copy %v", m.mode, m.copyMode)
	}
	m = keys(m, "esc")
	if m.mode != modeSearch {
		t.Errorf("second esc: mode %v", m.mode)
	}
	m = keys(colon(m, "g"), "right", "right")
	if m.copyMode {
		t.Error("right twice should toggle off")
	}
}

func TestGroupsColonCommands(t *testing.T) {
	m := feature(Config{Commands: groupCmds})
	m = colon(m, "g")
	m = typed(m, ":")
	if m.mode != modeColon || m.ret != modeGroups || lastLine(m) != ":" {
		t.Fatalf("colon in groups: mode %v prompt %q", m.mode, lastLine(m))
	}
	m = keys(m, "esc")
	if m.mode != modeGroups {
		t.Error("esc must return to groups")
	}
	for _, c := range []string{"h", "?"} {
		h := colon(m, c)
		if h.mode != modeHelp {
			t.Fatalf(":%s did not open help", c)
		}
		if h = keys(h, "esc"); h.mode != modeGroups {
			t.Errorf("help from groups returned to %v", h.mode)
		}
	}
	e := colon(m, "e")
	if e.mode != modeGroups || e.status != "select a command" {
		t.Errorf(":e on header: mode %v status %q", e.mode, e.status)
	}
	e = colon(keys(m, "+", "down"), "e")
	if e.mode != modeEdit || e.ed.String() != "git status" {
		t.Fatalf(":e on member: mode %v buf %q", e.mode, e.ed.String())
	}
	if e = keys(e, "esc"); e.mode != modeGroups {
		t.Errorf("esc from edit -> %v", e.mode)
	}
	if g := colon(m, "g"); g.mode != modeGroups {
		t.Error(":g in groups should stay")
	}
}

// --- colon: :? and height --------------------------------------------------

func TestColonQuestionMarkIsHelp(t *testing.T) {
	m := colon(feature(Config{}), "?")
	if m.mode != modeHelp {
		t.Error(":? should open help")
	}
}

func inline(rows, termH int, cmds []string) Model {
	m := New(Config{Commands: cmds, Height: Height{Lines: rows}})
	return feed(m, size(60, termH))
}

func TestColonHeight(t *testing.T) {
	tests := []struct {
		name   string
		start  int
		termH  int
		cmd    string
		want   int
		status string
	}{
		{"grow", 10, 40, "+4", 14, "height: 14 lines"},
		{"shrink", 10, 40, "-3", 7, "height: 7 lines"},
		{"default one up", 10, 40, "+", 11, "height: 11 lines"},
		{"default one down", 10, 40, "-", 9, "height: 9 lines"},
		{"clamp at 3", 6, 40, "-100", 3, "height: 3 lines"},
		{"clamp at terminal rows", 10, 25, "+100", 25, "height: 25 lines"},
		{"zero is a no-op", 10, 40, "+0", 10, "height: 10 lines"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := inline(tt.start, tt.termH, manyCmds(60))
			m = colon(m, tt.cmd)
			if m.frameLines() != tt.want || m.cfg.Height != (Height{Lines: tt.want}) {
				t.Errorf("frame = %d height %+v, want %d lines", m.frameLines(), m.cfg.Height, tt.want)
			}
			if got := lines(m); len(got) != tt.want {
				t.Errorf("view has %d lines, want %d", len(got), tt.want)
			}
			if !strings.Contains(status(m), tt.status) {
				t.Errorf("status = %q", status(m))
			}
		})
	}
}

func TestColonHeightPercentAndFullScreen(t *testing.T) {
	m := New(Config{Commands: sample, Height: Height{Percent: 40}})
	m = feed(m, size(60, 30)) // 12 lines
	m = colon(m, "+2")
	if m.frameLines() != 14 {
		t.Errorf("frame = %d", m.frameLines())
	}
	for _, c := range []string{"+", "-3", "+5"} {
		f := colon(feature(Config{}), c)
		if !strings.Contains(status(f), "already full screen (use --height)") || f.frameLines() != 20 {
			t.Errorf(":%s full screen: status %q frame %d", c, status(f), f.frameLines())
		}
	}
	if u := colon(m, "+x"); !strings.Contains(u.status, "unknown command") {
		t.Errorf("status = %q", u.status)
	}
}

func TestColonHeightKeepsCursorPreviewAndConstantFrame(t *testing.T) {
	m := inline(8, 40, manyCmds(100))
	m = keys(m, "pgup", "pgup", "pgup") // deep into the list
	m = keys(m, "?")                    // preview on
	for _, c := range []string{"+10", "-12", "+3", "-3"} {
		m = colon(m, c)
		if n := len(lines(m)); n != m.frameLines() {
			t.Fatalf(":%s: view %d lines, frame %d", c, n, m.frameLines())
		}
		if m.cursor < m.offset || m.cursor >= m.offset+m.listRows() {
			t.Errorf(":%s: cursor %d outside [%d,%d)", c, m.cursor, m.offset, m.offset+m.listRows())
		}
		if m.previewH() == 0 && m.frameLines() >= 5 {
			t.Errorf(":%s: preview pane disappeared", c)
		}
	}
	// growing near the end of the list does not leave blank rows above
	m = inline(5, 40, manyCmds(30))
	m = keys(m, "pgup", "pgup", "pgup", "pgup", "pgup", "pgup", "pgup")
	m = colon(m, "+10")
	shown := 0
	for _, l := range lines(m)[:m.listRows()] {
		if strings.TrimSpace(l) != "" {
			shown++
		}
	}
	if shown != m.listRows() {
		t.Errorf("only %d of %d list rows filled", shown, m.listRows())
	}
}

func TestColonHeightInGroupViewAndHelp(t *testing.T) {
	m := inline(8, 40, groupCmds)
	m = colon(m, "g")
	m = colon(m, "+5")
	if m.mode != modeGroups || len(lines(m)) != 13 {
		t.Errorf("groups: mode %v lines %d", m.mode, len(lines(m)))
	}
	if h := colon(m, "h"); len(lines(h)) != 13 {
		t.Errorf("help frame = %d", len(lines(h)))
	}
}

// --- in-window edit --------------------------------------------------------

func TestEditEntryPoints(t *testing.T) {
	entries := map[string]func(Model) Model{
		"tab":       func(m Model) Model { return keys(m, "tab") },
		"left at 0": func(m Model) Model { return keys(m, "left") },
		":e":        func(m Model) Model { return colon(m, "e") },
	}
	for name, enter := range entries {
		m := enter(feature(Config{}))
		if m.mode != modeEdit || m.done || m.ed.String() != "git status" || m.ed.pos != len("git status") {
			t.Errorf("%s: mode %v done %v buf %q pos %d", name, m.mode, m.done, m.ed.String(), m.ed.pos)
		}
		if m.Result().Action != result.None {
			t.Errorf("%s: no result expected yet", name)
		}
	}
	m := keys(feature(Config{Query: "zzzz"}), "tab")
	if m.mode != modeSearch {
		t.Error("tab without a hit must not enter edit")
	}
}

func TestEditPromptAndCursor(t *testing.T) {
	m := keys(feature(Config{}), "tab")
	if got := lastLine(m); got != "edit> git status" {
		t.Errorf("prompt = %q", got)
	}
	c := m.View().Cursor
	if c == nil || c.X != len("edit> git status") || c.Y != 19 {
		t.Errorf("cursor = %+v", c)
	}
	m = keys(m, "ctrl+a")
	if c := m.View().Cursor; c.X != len("edit> ") {
		t.Errorf("cursor after ctrl+a = %d", c.X)
	}
	m = keys(feature(Config{}), "right", "tab")
	if got := lastLine(m); got != "[COPY] edit> git status" {
		t.Errorf("copy prompt = %q", got)
	}
	if len(lines(m)) != 20 {
		t.Errorf("frame = %d lines", len(lines(m)))
	}
	r := keys(feature(Config{Layout: LayoutReverse}), "tab")
	if lines(r)[0] != "edit> git status" || r.View().Cursor.Y != 0 {
		t.Errorf("reverse: %q cursor %+v", lines(r)[0], r.View().Cursor)
	}
}

func TestEditLongTextScrollsAndKeepsCursorVisible(t *testing.T) {
	long := strings.Repeat("abcdefghij", 12)
	m := feature(Config{Commands: []string{long}})
	m = keys(m, "tab")
	line := lastLine(m)
	c := m.View().Cursor
	if len([]rune(line)) > 60 || c.X >= 60 || !strings.HasSuffix(line, "ghij") {
		t.Errorf("line %q cursor %+v", line, c)
	}
	m = keys(m, "home")
	if line := lastLine(m); !strings.HasPrefix(line, "edit> abcdefghij") || m.View().Cursor.X != 6 {
		t.Errorf("after home: %q", line)
	}
}

func TestEditNewlinesShownAsArrow(t *testing.T) {
	m := feature(Config{Commands: []string{"echo a\necho b"}})
	m = keys(m, "tab")
	if got := lastLine(m); got != "edit> echo a↵echo b" {
		t.Errorf("prompt = %q", got)
	}
	if m.ed.String() != "echo a\necho b" {
		t.Errorf("buffer must keep the newline: %q", m.ed.String())
	}
	m = keys(m, "enter")
	if r := m.Result(); r.Action != result.Run || r.Command != "echo a\necho b" {
		t.Errorf("result = %+v", r)
	}
}

func TestEditKeys(t *testing.T) {
	tests := []struct {
		name    string
		keys    []string
		want    string
		wantPos int
	}{
		{"append", []string{"x"}, "foo barx", 8},
		{"home insert", []string{"home", "x"}, "xfoo bar", 1},
		{"ctrl-a ctrl-e", []string{"ctrl+a", "ctrl+e", "!"}, "foo bar!", 8},
		{"left right", []string{"left", "left", "right", "X"}, "foo baXr", 7},
		{"ctrl-b ctrl-f", []string{"ctrl+b", "ctrl+b", "ctrl+f", "X"}, "foo baXr", 7},
		{"end", []string{"home", "end", "!"}, "foo bar!", 8},
		{"alt-b", []string{"alt+b", "X"}, "foo Xbar", 5},
		{"alt-f", []string{"home", "alt+f", "X"}, "fooX bar", 4},
		{"alt-left", []string{"alt+left", "alt+left", "X"}, "Xfoo bar", 1},
		{"alt-right", []string{"home", "alt+right", "alt+right", "X"}, "foo barX", 8},
		{"backspace", []string{"backspace"}, "foo ba", 6},
		{"ctrl-h", []string{"ctrl+h"}, "foo ba", 6},
		{"delete", []string{"home", "delete"}, "oo bar", 0},
		{"delete at end", []string{"delete"}, "foo bar", 7},
		{"ctrl-w", []string{"ctrl+w"}, "foo ", 4},
		{"alt-backspace", []string{"alt+backspace"}, "foo ", 4},
		{"ctrl-u", []string{"left", "left", "ctrl+u"}, "ar", 0},
		{"ctrl-k", []string{"alt+b", "ctrl+k"}, "foo ", 4},
		{"utf8", []string{"é", "日", "backspace"}, "foo baré", 8},
		{"unbound ctrl ignored", []string{"ctrl+z"}, "foo bar", 7},
		{"unbound alt ignored", []string{"alt+z"}, "foo bar", 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := feature(Config{Commands: []string{"foo bar"}})
			m = keys(keys(m, "tab"), tt.keys...)
			if m.mode != modeEdit || m.ed.String() != tt.want || m.ed.pos != tt.wantPos {
				t.Errorf("mode %v buf %q pos %d, want %q pos %d", m.mode, m.ed.String(), m.ed.pos, tt.want, tt.wantPos)
			}
		})
	}
}

func TestEditPaste(t *testing.T) {
	m := keys(feature(Config{Commands: []string{"echo "}}), "tab")
	m = feed(m, tea.PasteMsg{Content: "a\nb\tc\x1b"})
	if m.ed.String() != "echo a b c" {
		t.Errorf("buffer = %q", m.ed.String())
	}
	if len(m.query) != 0 {
		t.Error("paste must not reach the filter")
	}
}

func TestEditEnterRunsEditedText(t *testing.T) {
	m := feature(Config{Commands: []string{"echo TAB"}})
	m = keys(m, "tab")
	m = typed(m, "; echo ED")
	nm, cmd := upd(m, "enter")
	if r := nm.Result(); r.Action != result.Run || r.Command != "echo TAB; echo ED" || !nm.done {
		t.Errorf("result = %+v", r)
	}
	if _, _, quit := oscOf(flatten(cmd)); !quit {
		t.Error("enter should quit")
	}
}

func TestEditEnterCopiesInCopyMode(t *testing.T) {
	m := feature(Config{Commands: []string{"echo TAB"}})
	m = keys(m, "right", "tab")
	m = typed(m, "!")
	nm, cmd := upd(m, "enter")
	kind, payload, quit := oscOf(flatten(cmd))
	if kind != "clipboard" || payload != "echo TAB!" || !quit || nm.Result().Action != result.None {
		t.Errorf("copy: %q %q quit=%v result=%+v", kind, payload, quit, nm.Result())
	}
}

func TestEditBlankIsIgnored(t *testing.T) {
	m := feature(Config{Commands: []string{"ab"}})
	m = keys(m, "tab", "ctrl+u")
	m = typed(m, "  ")
	m = keys(m, "enter")
	if m.done || m.mode != modeEdit || m.status != "empty command" {
		t.Errorf("done %v mode %v status %q", m.done, m.mode, m.status)
	}
	if !strings.Contains(status(m), "empty command") {
		t.Errorf("not shown: %q", status(m))
	}
}

func TestEditEscReturnsToOrigin(t *testing.T) {
	m := feature(Config{Commands: sample, Query: "ls"})
	m = keys(m, "right", "tab") // right at the end of the filter: copy mode
	m = keys(m, "x", "esc")
	if m.mode != modeSearch || string(m.query) != "ls" || m.done {
		t.Errorf("mode %v query %q", m.mode, string(m.query))
	}
	if !m.copyMode {
		t.Error("copy mode must survive")
	}
	if len(m.hits) != 1 {
		t.Errorf("hits = %d", len(m.hits))
	}
	g := feature(Config{Commands: groupCmds})
	g = keys(colon(g, "g"), "+", "down", "tab")
	if g.mode != modeEdit || g.ed.String() != "git status" {
		t.Fatalf("groups tab: mode %v buf %q", g.mode, g.ed.String())
	}
	g = keys(g, "esc")
	if g.mode != modeGroups || !g.expanded["git"] || g.cursor != 1 {
		t.Errorf("mode %v expanded %v cursor %d", g.mode, g.expanded, g.cursor)
	}
}

func TestEditCancelKeys(t *testing.T) {
	for _, k := range []string{"ctrl+c", "ctrl+g", "ctrl+q"} {
		m := keys(feature(Config{}), "tab", k)
		if !m.done || m.Result().Action != result.None {
			t.Errorf("%s: done %v %+v", k, m.done, m.Result())
		}
	}
}

func TestEditShellOptionKeepsShellPath(t *testing.T) {
	for name, f := range map[string]func(Model) Model{
		"tab":    func(m Model) Model { return keys(m, "tab") },
		"left":   func(m Model) Model { return keys(m, "left") },
		":e":     func(m Model) Model { return colon(m, "e") },
		"groups": func(m Model) Model { return keys(colon(m, "g"), "+", "down", "tab") },
	} {
		m := f(feature(Config{EditShell: true, Commands: groupCmds}))
		if r := m.Result(); r.Action != result.Edit || r.Command != "git status" || !m.done {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

func TestEditSurvivesReloadAndIgnoresMouse(t *testing.T) {
	next := []string{"new", "git status", "ls -la"}
	cfg := Config{
		Mouse:  true,
		Reload: func() ([]string, []int64, bool, error) { return next, nil, true, nil },
	}
	m := feature(cfg)
	m = keys(m, "tab")
	m = typed(m, " --x")
	m = feed(m, reloadMsg{})
	if m.mode != modeEdit || m.ed.String() != "git status --x" {
		t.Errorf("reload disturbed edit: mode %v buf %q", m.mode, m.ed.String())
	}
	cur := m.cursor
	m = feed(m, wheel(tea.MouseWheelDown), mousePress(5), mouseRelease(5))
	if m.mode != modeEdit || m.cursor != cur || m.runOnUp || m.done {
		t.Errorf("mouse not ignored: mode %v cursor %d", m.mode, m.cursor)
	}
	m = keys(m, "esc")
	if m.mode != modeSearch || len(m.cmds) != 3 {
		t.Errorf("after esc: mode %v cmds %d", m.mode, len(m.cmds))
	}
}

func TestEditFromGroupsSurvivesReload(t *testing.T) {
	next := append([]string{"zz"}, groupCmds...)
	m := feature(Config{
		Commands: groupCmds,
		Reload:   func() ([]string, []int64, bool, error) { return next, nil, true, nil },
	})
	m = keys(colon(m, "g"), "+", "down", "tab")
	m = feed(m, reloadMsg{})
	if m.mode != modeEdit || m.ed.String() != "git status" || m.ret != modeGroups {
		t.Errorf("mode %v buf %q ret %v", m.mode, m.ed.String(), m.ret)
	}
	if m = keys(m, "esc"); m.mode != modeGroups {
		t.Errorf("esc -> %v", m.mode)
	}
}

func TestLineEditUnit(t *testing.T) {
	e := newLineEdit("héllo wörld")
	if e.pos != 11 {
		t.Fatalf("pos = %d", e.pos)
	}
	orig := e.buf
	e2 := e.deleteRange(0, 6)
	if e2.String() != "wörld" || e2.pos != 0 || string(orig) != "héllo wörld" {
		t.Errorf("deleteRange: %q %d (orig %q)", e2.String(), e2.pos, string(orig))
	}
	if e3 := e2.insert([]rune("X")); e3.String() != "Xwörld" || e3.pos != 1 {
		t.Errorf("insert: %q %d", e3.String(), e3.pos)
	}
	if e4 := e.deleteRange(5, 99); e4.String() != "héllo" {
		t.Errorf("clamp: %q", e4.String())
	}
	if _, ok := e.handleKey("ctrl+z"); ok {
		t.Error("ctrl+z is not an editing key")
	}
}

// --- help ------------------------------------------------------------------

func TestHelpMentionsNewFeatures(t *testing.T) {
	text := strings.Join(helpLines, "\n")
	for _, want := range []string{
		":f", ":p", ":?", ":+N", ":-N", "--match", "--edit", "RE2", "Filter text", "Edit mode",
		"d or Delete", "Space", "[fuzzy]", "Ctrl-K", "smart case",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("help is missing %q", want)
		}
	}
	if strings.Contains(text, "Query edit") {
		t.Error("the confusing 'Query edit' line should be gone")
	}
	for i, l := range helpLines {
		if len([]rune(l)) > 78 {
			t.Errorf("help line %d is %d columns: %q", i, len([]rune(l)), l)
		}
	}
	m := feed(feature(Config{}), size(60, 10))
	m = keys(colon(m, "h"), "end")
	if got := lines(m); !strings.Contains(strings.Join(got, "\n"), "backreferences") || len(got) != 10 {
		t.Errorf("end of help: %d lines\n%s", len(got), strings.Join(got, "\n"))
	}
}
