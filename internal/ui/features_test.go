package ui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Dologan/ihg/internal/clipboard"
	"github.com/Dologan/ihg/internal/result"
)

// flatten runs cmd and returns every message produced, expanding sequences and
// batches (their types are unexported, so reflection is used). tea.Quit shows
// up as tea.QuitMsg.
func flatten(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	return flattenMsg(cmd())
}

func flattenMsg(msg tea.Msg) []tea.Msg {
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
		var out []tea.Msg
		for i := 0; i < v.Len(); i++ {
			out = append(out, flatten(v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// oscOf returns the clipboard payload among msgs: either the text of a
// tea.SetClipboard message or the raw string of a tea.Raw message.
func oscOf(msgs []tea.Msg) (kind, payload string, quit bool) {
	for _, msg := range msgs {
		switch x := msg.(type) {
		case tea.QuitMsg:
			quit = true
		case tea.RawMsg:
			kind, payload = "raw", fmt.Sprint(x.Msg)
		default:
			if v := reflect.ValueOf(msg); v.Kind() == reflect.String {
				kind, payload = "clipboard", v.String()
			}
		}
	}
	return
}

func upd(m Model, k string) (Model, tea.Cmd) {
	nm, cmd := m.Update(press(k))
	return nm.(Model), cmd
}

func feature(cfg Config) Model {
	cfg.Height = Height{Percent: 100}
	if cfg.Commands == nil {
		cfg.Commands = sample
	}
	return feed(New(cfg), size(60, 20))
}

func colon(m Model, s string) Model { return keys(typed(m, ":"+s), "enter") }

func status(m Model) string { return strings.TrimSpace(lines(m)[len(lines(m))-2]) }

func TestColonEntryOnlyWithEmptyQuery(t *testing.T) {
	m := feature(Config{})
	m = typed(m, ":")
	if m.mode != modeColon {
		t.Fatal("not in colon mode")
	}
	if got := lines(m); got[len(got)-1] != ":" {
		t.Errorf("prompt = %q", got[len(got)-1])
	}
	m = keys(m, "backspace")
	if m.mode != modeSearch {
		t.Error("backspace on empty colon should leave")
	}
	m = typed(m, "g:")
	if m.mode != modeSearch || string(m.query) != "g:" {
		t.Errorf("colon with a query must be literal: %q mode %v", string(m.query), m.mode)
	}
	m = feature(Config{})
	m = keys(typed(m, ":x"), "esc")
	if m.mode != modeSearch || len(m.colon) != 0 {
		t.Error("esc should leave colon mode")
	}
}

func TestColonQuitAndEdit(t *testing.T) {
	m := colon(feature(Config{}), "q")
	if !m.done || m.Result().Action != result.None {
		t.Errorf("q: %+v", m.Result())
	}
	m = colon(feature(Config{}), "e")
	if r := m.Result(); r.Action != result.Edit || r.Command != "git status" {
		t.Errorf("e: %+v", r)
	}
}

func TestColonUnknown(t *testing.T) {
	m := colon(feature(Config{}), "zz")
	if !strings.Contains(status(m), "unknown command :zz") {
		t.Errorf("status line = %q", status(m))
	}
	if m.mode != modeSearch || m.done {
		t.Error("unknown command should stay in search")
	}
	if m = keys(m, "down"); m.status != "" {
		t.Error("status should clear on next key")
	}
}

func TestColonSort(t *testing.T) {
	cmds := []string{"b one", "C two", "a three"}
	m := colon(feature(Config{Commands: cmds}), "s")
	if got := hitTexts(m); !reflect.DeepEqual(got, []string{"a three", "b one", "C two"}) {
		t.Errorf("A-Z: %v", got)
	}
	if !strings.Contains(status(m), "[A-Z]") {
		t.Errorf("info = %q", status(m))
	}
	m = colon(m, "S")
	if got := hitTexts(m); !reflect.DeepEqual(got, []string{"C two", "b one", "a three"}) {
		t.Errorf("Z-A: %v", got)
	}
	m = colon(m, "r")
	if got := hitTexts(m); !reflect.DeepEqual(got, cmds) {
		t.Errorf("reset: %v", got)
	}
}

func hitTexts(m Model) []string {
	var out []string
	for _, h := range m.hits {
		out = append(out, m.cmds[h.Index])
	}
	return out
}

func TestColonReset(t *testing.T) {
	m := feature(Config{})
	m = keys(m, "ctrl+r")
	m = keys(typed(m, "git"), "right") // copy mode on
	m = colon(keys(m, "ctrl+u", "ctrl+u"), "g")
	m = keys(m, "esc")
	m = colon(m, "r")
	if len(m.query) != 0 || m.sort != sortRelevance || m.copyMode || len(m.hits) != len(sample) {
		t.Errorf("reset incomplete: q=%q sort=%v copy=%v", string(m.query), m.sort, m.copyMode)
	}
	// query + sort + copy reset together
	m = feature(Config{})
	m = keys(typed(m, "go"), "ctrl+r", "right")
	if !m.copyMode || m.sort != sortRecency {
		t.Fatal("setup")
	}
	m = keys(m, "ctrl+u")
	m = colon(m, "r")
	if m.copyMode || m.sort != sortRelevance {
		t.Error("reset left state behind")
	}
}

func TestColonDigitsPickByRecencyNotRank(t *testing.T) {
	m := feature(Config{})
	m = typed(m, "go") // best-ranked visible row is "go build ./..." (index 4)
	// the query must be empty to type ':'; clear it but keep a different order
	m = keys(m, "ctrl+u")
	m = keys(m, "ctrl+r", "ctrl+r") // A-Z: order differs from recency
	m = colon(m, "3")
	if r := m.Result(); r.Action != result.Run || r.Command != "make test" {
		t.Errorf(":3 = %+v, want recency index 3", r)
	}
	m = colon(feature(Config{}), "9")
	if m.done || !strings.Contains(status(m), "no command at index 9") {
		t.Errorf("out of range: done=%v status=%q", m.done, status(m))
	}
}

func TestColonBackup(t *testing.T) {
	var got []string
	cfg := Config{Backup: func(dest string) (string, error) {
		got = append(got, dest)
		if dest == "bad" {
			return "", errors.New("nope")
		}
		if dest == "" {
			return "/default/path", nil
		}
		return dest, nil
	}}
	m := colon(feature(cfg), "b")
	if !strings.Contains(status(m), "backup written: /default/path") {
		t.Errorf("status = %q", status(m))
	}
	m = colon(m, "b /tmp/x.bak")
	if !strings.Contains(status(m), "/tmp/x.bak") {
		t.Errorf("status = %q", status(m))
	}
	m = colon(m, "b bad")
	if !strings.Contains(status(m), "backup failed: nope") {
		t.Errorf("status = %q", status(m))
	}
	if !reflect.DeepEqual(got, []string{"", "/tmp/x.bak", "bad"}) {
		t.Errorf("dests = %q", got)
	}
	m = colon(feature(Config{}), "b")
	if !strings.Contains(status(m), "not available") {
		t.Errorf("nil Backup: %q", status(m))
	}
}

func TestDeleteConfirm(t *testing.T) {
	var deleted []string
	cfg := Config{
		Commands: []string{"a", "b", "a", "c"},
		Times:    []int64{1, 2, 3, 4},
		Delete: func(cmd string) (int, error) {
			deleted = append(deleted, cmd)
			return 2, nil
		},
	}
	m := feature(cfg)
	m = keys(m, "down") // cursor on "b" in reverse? default layout: up/down visual; use move directly
	m.cursor = 1
	m = colon(m, "d")
	if m.mode != modeConfirm {
		t.Fatal("not confirming")
	}
	if got := lines(m); !strings.Contains(got[len(got)-1], `delete "b" from history? [y/N]`) {
		t.Errorf("prompt = %q", got[len(got)-1])
	}
	// anything else cancels
	m2 := keys(m, "x")
	if m2.mode != modeSearch || len(deleted) != 0 || !strings.Contains(m2.status, "not deleted") || len(m2.cmds) != 4 {
		t.Errorf("cancel: mode=%v deleted=%v status=%q", m2.mode, deleted, m2.status)
	}
	m3 := keys(m, "enter")
	if len(deleted) != 0 || m3.mode != modeSearch {
		t.Error("enter must cancel")
	}
	m = keys(m, "y")
	if !reflect.DeepEqual(deleted, []string{"b"}) {
		t.Fatalf("Delete calls = %v", deleted)
	}
	if !reflect.DeepEqual(m.cmds, []string{"a", "a", "c"}) || !reflect.DeepEqual(m.times, []int64{1, 3, 4}) {
		t.Errorf("cmds=%v times=%v", m.cmds, m.times)
	}
	if m.cursor != 1 || !strings.Contains(m.status, "deleted 2 entries") {
		t.Errorf("cursor=%d status=%q", m.cursor, m.status)
	}
	// original config slices untouched
	if len(cfg.Commands) != 4 {
		t.Error("config slice mutated")
	}
}

func TestDeleteCursorClampAndErrors(t *testing.T) {
	cfg := Config{Commands: []string{"a", "b"}, Delete: func(string) (int, error) { return 1, nil }}
	m := feature(cfg)
	m.cursor = 1
	m = keys(colon(m, "d"), "y")
	if m.cursor != 0 || len(m.hits) != 1 {
		t.Errorf("cursor=%d hits=%d", m.cursor, len(m.hits))
	}
	cfg.Delete = func(string) (int, error) { return 0, errors.New("boom") }
	m = keys(colon(feature(cfg), "d"), "y")
	if len(m.cmds) != 2 || !strings.Contains(m.status, "delete failed: boom") {
		t.Errorf("status=%q cmds=%v", m.status, m.cmds)
	}
	m = colon(feature(Config{}), "d")
	if m.mode != modeSearch || !strings.Contains(m.status, "not available") {
		t.Errorf("nil Delete: mode=%v status=%q", m.mode, m.status)
	}
}

func TestCopyModeToggle(t *testing.T) {
	m := feature(Config{})
	m = keys(m, "right")
	if !m.copyMode {
		t.Fatal("right on empty query should toggle copy mode")
	}
	if got := lines(m); !strings.HasPrefix(got[len(got)-1], "[COPY]> ") {
		t.Errorf("prompt = %q", got[len(got)-1])
	}
	m = keys(m, "right")
	if m.copyMode {
		t.Error("second right should toggle off")
	}
	// mid-query: moves the cursor
	m = keys(typed(m, "git"), "left", "right")
	if m.copyMode || m.qpos != 3 {
		t.Errorf("mid: copy=%v qpos=%d", m.copyMode, m.qpos)
	}
	m = keys(m, "left", "left")
	m = keys(m, "right")
	if m.copyMode || m.qpos != 2 {
		t.Errorf("mid: copy=%v qpos=%d", m.copyMode, m.qpos)
	}
	m = keys(m, "end", "right")
	if !m.copyMode {
		t.Error("right at end should toggle")
	}
	// Esc: leaves copy mode and clears the query, without quitting.
	m = keys(m, "esc")
	if m.copyMode || len(m.query) != 0 || m.done {
		t.Errorf("esc: copy=%v q=%q done=%v", m.copyMode, string(m.query), m.done)
	}
	m = keys(m, "right", "esc")
	if m.copyMode || m.done {
		t.Error("esc in copy mode with empty query should only leave copy mode")
	}
	m = keys(m, "esc")
	if !m.done {
		t.Error("esc on plain empty query quits")
	}
}

func TestCopyModeSelectedStyle(t *testing.T) {
	m := feature(Config{})
	a := m.View().Content
	m = keys(m, "right")
	b := m.View().Content
	if a == b {
		t.Error("copy mode should change styling")
	}
	row := func(s string) string {
		for _, l := range strings.Split(s, "\n") {
			if strings.Contains(l, "git status") {
				return l
			}
		}
		return ""
	}
	if row(a) == row(b) {
		t.Error("selected row style must differ in copy mode")
	}
}

func TestCopyViaEnter(t *testing.T) {
	var copied []string
	cfg := Config{Copy: func(s string) (string, error) { copied = append(copied, s); return "pbcopy", nil }}
	m := keys(feature(cfg), "right")
	nm, cmd := upd(m, "enter")
	r := nm.Result()
	if r.Action != result.None || r.Command != "" || !nm.done {
		t.Errorf("result = %+v", r)
	}
	if r.Message != "ihg: sent to terminal clipboard (OSC 52), pbcopy" {
		t.Errorf("message = %q", r.Message)
	}
	if !reflect.DeepEqual(copied, []string{"git status"}) {
		t.Errorf("Copy calls = %v", copied)
	}
	kind, payload, quit := oscOf(flatten(cmd))
	if kind != "clipboard" || payload != "git status" || !quit {
		t.Errorf("kind=%q payload=%q quit=%v", kind, payload, quit)
	}
	// SetClipboard is ordered before Quit so it is flushed first.
	msgs := flatten(cmd)
	if _, isQuit := msgs[len(msgs)-1].(tea.QuitMsg); !isQuit || len(msgs) != 2 {
		t.Errorf("msgs = %#v", msgs)
	}
}

func TestCopyViaColonC(t *testing.T) {
	m := feature(Config{})
	m = typed(m, ":c")
	nm, cmd := upd(m, "enter")
	if r := nm.Result(); r.Action != result.None || r.Message != "ihg: sent to terminal clipboard (OSC 52)" {
		t.Errorf("result = %+v", r)
	}
	if kind, payload, quit := oscOf(flatten(cmd)); kind != "clipboard" || payload != "git status" || !quit {
		t.Errorf("kind=%q payload=%q", kind, payload)
	}
}

func TestCopyTmuxWrapsOSC52(t *testing.T) {
	m := feature(Config{InTmux: true, Copy: func(string) (string, error) { return "", errors.New("no tool") }})
	nm, cmd := upd(colon2(m, "c"), "enter")
	kind, payload, _ := oscOf(flatten(cmd))
	want := string(clipboard.OSC52("git status", true))
	if kind != "raw" || payload != want {
		t.Errorf("kind=%q payload=%q want %q", kind, payload, want)
	}
	if !strings.Contains(nm.Result().Message, "clipboard tool failed: no tool") {
		t.Errorf("message = %q", nm.Result().Message)
	}
	enc := base64.StdEncoding.EncodeToString([]byte("git status"))
	if !strings.Contains(payload, enc) || !strings.HasPrefix(payload, "\x1bPtmux;") {
		t.Errorf("not tmux-wrapped: %q", payload)
	}
}

// colon2 types a colon command without pressing enter.
func colon2(m Model, s string) Model { return typed(m, ":"+s) }

func TestEnterRunsOutsideCopyMode(t *testing.T) {
	m := feature(Config{})
	nm, cmd := upd(m, "enter")
	if r := nm.Result(); r.Action != result.Run || r.Message != "" {
		t.Errorf("result = %+v", r)
	}
	if _, _, quit := oscOf(flatten(cmd)); !quit {
		t.Error("enter should quit")
	}
}

func TestGroups(t *testing.T) {
	cmds := []string{"git status", "ls | grep x", "git log", "grep y"}
	m := feature(Config{Commands: cmds})
	m = colon(m, "g")
	if m.mode != modeGroups {
		t.Fatal("not in groups")
	}
	// keys: git(2) grep(2) ls(1), collapsed
	if len(m.grows) != 3 {
		t.Fatalf("rows = %d", len(m.grows))
	}
	text := strings.Join(lines(m), "\n")
	for _, want := range []string{"[2] git", "[2] grep", "[1] ls"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if strings.Contains(text, "git status") {
		t.Error("members must be collapsed by default")
	}
	// cursor on first header (git): expand with +
	m = keys(m, "+")
	if len(m.grows) != 5 || !strings.Contains(strings.Join(lines(m), "\n"), "    git status") {
		t.Fatalf("expand: rows=%d", len(m.grows))
	}
	m = keys(m, "-")
	if len(m.grows) != 3 {
		t.Errorf("collapse: rows=%d", len(m.grows))
	}
	m = keys(m, " ")
	if len(m.grows) != 5 {
		t.Error("space should expand")
	}
	m = keys(m, " ")
	if len(m.grows) != 3 {
		t.Error("space should collapse")
	}
	m = keys(m, "enter")
	if len(m.grows) != 5 {
		t.Error("enter on header should expand")
	}
	// move to a member (visual step: default layout, down = lower rank)
	m = keys(m, "up")
	if r := m.grows[m.cursor]; r.header || r.cmd != "git status" {
		t.Fatalf("cursor row = %+v", r)
	}
	// edit
	if e := keys(m, "tab"); e.Result().Action != result.Edit || e.Result().Command != "git status" {
		t.Errorf("tab = %+v", e.Result())
	}
	// tab on header is ignored
	if e := keys(m, "down", "tab"); e.done {
		t.Error("tab on header should do nothing")
	}
	// run
	if e := keys(m, "enter"); e.Result().Action != result.Run || e.Result().Command != "git status" {
		t.Errorf("enter = %+v", e.Result())
	}
	// copy mode copies the member
	e := keys(m, "esc", "right")
	e = colon(e, "g")
	e = keys(e, "+", "up")
	_, cmd := upd(e, "enter")
	if kind, payload, _ := oscOf(flatten(cmd)); kind != "clipboard" || payload != "git status" {
		t.Errorf("copy from groups: %q %q", kind, payload)
	}
	// esc returns to search with the query intact
	m = keys(m, "esc")
	if m.mode != modeSearch || m.cursor != 0 {
		t.Errorf("esc: mode=%v", m.mode)
	}
}

func TestGroupsUseVisibleOrderAndQuery(t *testing.T) {
	m := feature(Config{Commands: []string{"git a", "ls b", "git c"}, Query: "git"})
	m = keys(m, "ctrl+u") // clear so ':' can start... then filter via sort
	m = colon(m, "S")
	m = colon(m, "g")
	m = keys(m, "+")
	var members []string
	for _, r := range m.grows {
		if !r.header {
			members = append(members, r.cmd)
		}
	}
	if !reflect.DeepEqual(members, []string{"git c", "git a"}) {
		t.Errorf("members = %v", members)
	}
}

func TestHelp(t *testing.T) {
	m := feed(feature(Config{}), size(80, 40))
	m = colon(m, "h")
	if m.mode != modeHelp {
		t.Fatal("no help")
	}
	text := strings.Join(lines(m), "\n")
	for _, want := range []string{"quick help", "'word", "^word", "word$", "!word", "a | b", "F1", "Ctrl-R"} {
		if !strings.Contains(text, want) {
			t.Errorf("help missing %q", want)
		}
	}
	if len(lines(m)) != 40 {
		t.Errorf("help frame has %d lines", len(lines(m)))
	}
	m = keys(m, "esc")
	if m.mode != modeSearch || m.done {
		t.Error("esc should close help")
	}
	m = keys(m, "f1")
	if m.mode != modeHelp {
		t.Error("F1 opens help")
	}
	m = keys(m, "q")
	if m.mode != modeSearch {
		t.Error("q closes help")
	}
	// typing ? is not help
	if m = keys(m, "?"); m.mode != modeSearch {
		t.Error("? must not open help")
	}
}

func TestHelpScrolls(t *testing.T) {
	m := feature(Config{})
	m = feed(m, size(60, 8))
	m = keys(colon(m, "h"))
	first := lines(m)[0]
	m = keys(m, "down", "down")
	if lines(m)[0] == first || m.helpOff != 2 {
		t.Errorf("did not scroll: off=%d", m.helpOff)
	}
	m = keys(m, "end")
	if got := lines(m); !strings.Contains(strings.Join(got, "\n"), "a | b") {
		t.Errorf("end of help not shown: %v", got)
	}
	if len(lines(m)) != 8 {
		t.Errorf("frame = %d lines", len(lines(m)))
	}
	m = keys(m, "home", "up")
	if m.helpOff != 0 {
		t.Error("scrolled above top")
	}
}

func TestPreview(t *testing.T) {
	ts := time.Date(2025, 3, 4, 5, 6, 7, 0, time.Local).Unix()
	cmds := []string{"echo one\necho two\techo\x01three", "ls"}
	m := feature(Config{Commands: cmds, Times: []int64{ts, 0}})
	before := m.listRows()
	m = keys(m, "?")
	if !m.preview || len(m.query) != 0 {
		t.Fatalf("? should toggle preview, query %q", string(m.query))
	}
	if m.listRows() >= before {
		t.Errorf("list did not shrink: %d -> %d", before, m.listRows())
	}
	if got := m.previewH(); got != 8 { // 40% of 20
		t.Errorf("previewH = %d", got)
	}
	got := lines(m)
	if len(got) != 20 {
		t.Fatalf("frame height %d", len(got))
	}
	// Default layout: pane above the list. Row 0..6 text, 7 separator.
	if strings.TrimSpace(got[0]) != "echo one" || strings.TrimSpace(got[1]) != "echo two echo·three" {
		t.Errorf("preview text: %q %q", got[0], got[1])
	}
	if !strings.HasPrefix(got[7], "──") {
		t.Errorf("separator = %q", got[7])
	}
	want := "last run: " + time.Unix(ts, 0).Local().Format("2006-01-02 15:04:05")
	if strings.TrimSpace(got[6]) != want {
		t.Errorf("stamp line = %q, want %q", got[6], want)
	}
	// Second command has no time.
	m = keys(m, "up")
	if text := strings.Join(lines(m), "\n"); strings.Contains(text, "last run") {
		t.Error("no timestamp expected")
	}
	if !strings.HasPrefix(lines(m)[0], "ls") {
		t.Errorf("preview should show ls: %q", lines(m)[0])
	}
	// ctrl+/ always works, ? is typed when the query is non-empty.
	m = keys(m, "ctrl+/")
	if m.preview {
		t.Error("ctrl+/ should toggle off")
	}
	m = keys(typed(m, "l"), "?")
	if m.preview || string(m.query) != "l?" {
		t.Errorf("? with a query is text: q=%q preview=%v", string(m.query), m.preview)
	}
	m = keys(m, "ctrl+/")
	if !m.preview {
		t.Error("ctrl+/ should toggle on")
	}
}

func TestPreviewReverseLayoutAndWrap(t *testing.T) {
	long := strings.Repeat("abcdefghij", 12) // 120 columns
	cfg := Config{Commands: []string{long, "x"}, Height: Height{Percent: 100}, Layout: LayoutReverse}
	m := feed(New(cfg), size(40, 20))
	m = keys(m, "?")
	got := lines(m)
	if len(got) != 20 {
		t.Fatalf("frame %d", len(got))
	}
	if !strings.HasPrefix(got[0], "> ") {
		t.Errorf("prompt = %q", got[0])
	}
	sep := 20 - m.previewH()
	if !strings.HasPrefix(got[sep], "──") {
		t.Errorf("separator row %d = %q", sep, got[sep])
	}
	if strings.TrimSpace(got[sep+1]) != long[:40] || strings.TrimSpace(got[sep+3]) != long[80:] {
		t.Errorf("wrapped preview: %q", got[sep+1:sep+4])
	}
	// tiny frame: pane is suppressed rather than eating the list
	m = feed(m, size(40, 4))
	if m.previewH() != 0 || len(lines(m)) != 4 {
		t.Errorf("tiny: previewH=%d lines=%d", m.previewH(), len(lines(m)))
	}
}

func TestPreviewTruncatesTallCommands(t *testing.T) {
	cmd := strings.Repeat("line\n", 30)
	m := feature(Config{Commands: []string{cmd}})
	m = keys(m, "?")
	got := lines(m)
	if len(got) != 20 || !strings.HasSuffix(strings.TrimSpace(got[6]), "…") {
		t.Errorf("tall preview: %q", got[:8])
	}
}

func TestSortCycling(t *testing.T) {
	cmds := []string{"b x", "A y", "a y", "c z"}
	m := feature(Config{Commands: cmds})
	if got := hitTexts(m); !reflect.DeepEqual(got, cmds) {
		t.Fatalf("relevance (empty query) = %v", got)
	}
	m = keys(m, "ctrl+r")
	if m.sort != sortRecency || !reflect.DeepEqual(hitTexts(m), cmds) {
		t.Errorf("recency: %v", hitTexts(m))
	}
	if !strings.Contains(status(m), "[recency]") {
		t.Errorf("info = %q", status(m))
	}
	m = keys(m, "ctrl+r")
	// case-folded; equal folded keys keep recency order
	if got := hitTexts(m); !reflect.DeepEqual(got, []string{"A y", "a y", "b x", "c z"}) {
		t.Errorf("A-Z: %v", got)
	}
	m = keys(m, "ctrl+r")
	if m.sort != sortRelevance {
		t.Error("should wrap to relevance")
	}
	// recency vs relevance differ with a query
	m = New(Config{Commands: []string{"xxxgit", "git", "g i t"}, Query: "git"})
	rel := hitTexts(m)
	m = keys(m, "ctrl+r")
	rec := hitTexts(m)
	if !reflect.DeepEqual(rec, []string{"xxxgit", "git", "g i t"}) || reflect.DeepEqual(rel, rec) {
		t.Errorf("rel=%v rec=%v", rel, rec)
	}
}

func TestReloadKeepsSelectionAndMode(t *testing.T) {
	calls := 0
	next := []string{"new", "git status", "ls -la", "x"}
	cfg := Config{Reload: func() ([]string, []int64, bool, error) {
		calls++
		return next, []int64{9, 8, 7, 6}, true, nil
	}}
	m := feature(cfg)
	if m.Init() == nil {
		t.Fatal("Init should schedule a tick")
	}
	m.cursor = 2 // "ls -la"
	nm, cmd := m.Update(reloadMsg(time.Now()))
	m = nm.(Model)
	if cmd == nil {
		t.Error("tick must be rescheduled")
	}
	if sel, _ := m.selected(); sel != "ls -la" || m.cursor != 2 {
		t.Errorf("selected %q cursor %d", sel, m.cursor)
	}
	if m.timeAt(0) != 9 || len(m.cmds) != 4 {
		t.Error("times/cmds not replaced")
	}
	// selection moved: new list puts "make test" gone; stays by text
	next = []string{"a", "b", "c", "ls -la", "git status"}
	m = feed(m, reloadMsg{})
	if sel, _ := m.selected(); sel != "ls -la" || m.cursor != 3 {
		t.Errorf("selected %q cursor %d", sel, m.cursor)
	}
	// selected command vanished: rank clamped
	next = []string{"a"}
	m = feed(m, reloadMsg{})
	if m.cursor != 0 || len(m.hits) != 1 {
		t.Errorf("clamp: cursor %d", m.cursor)
	}
	if calls != 3 {
		t.Errorf("calls = %d", calls)
	}
}

func TestReloadPreservesModes(t *testing.T) {
	next := []string{"zero", "echo hi", "git status", "ls -la", "make test", "git log"}
	cfg := Config{
		Delete: func(string) (int, error) { return 1, nil },
		Reload: func() ([]string, []int64, bool, error) { return next, nil, true, nil },
	}
	m := feature(cfg)
	reload := func(m Model) Model { return feed(m, reloadMsg{}) }

	c := reload(typed(m, ":se"))
	if c.mode != modeColon || string(c.colon) != "se" {
		t.Error("colon input disturbed")
	}
	c = keys(m, "ctrl+r")
	c = typed(c, "")
	cf := colon(m, "d")
	cf = reload(cf)
	if cf.mode != modeConfirm || cf.pending != "git status" {
		t.Errorf("confirm disturbed: %v %q", cf.mode, cf.pending)
	}
	h := reload(colon(m, "h"))
	if h.mode != modeHelp {
		t.Error("help closed")
	}
	p := reload(keys(m, "?"))
	if !p.preview {
		t.Error("preview closed")
	}
	cp := reload(keys(m, "right"))
	if !cp.copyMode {
		t.Error("copy mode lost")
	}
	// groups: expansion and selected row survive
	g := colon(m, "g")
	g = keys(g, "+") // expand "echo"
	g = keys(g, "up")
	if r := g.grows[g.cursor]; r.header {
		t.Fatalf("setup: %+v", r)
	}
	sel := g.grows[g.cursor].cmd
	g = reload(g)
	if g.mode != modeGroups || !g.expanded["echo"] {
		t.Error("group state lost")
	}
	if r := g.grows[g.cursor]; r.header || r.cmd != sel {
		t.Errorf("group cursor row = %+v, want member %q", r, sel)
	}
	g = keys(g, "esc")
	if g.mode != modeSearch || len(g.hits) != 6 {
		t.Errorf("after esc: hits=%d", len(g.hits))
	}
}

func TestReloadErrorAndNoChange(t *testing.T) {
	var err error
	changed := false
	cfg := Config{Reload: func() ([]string, []int64, bool, error) {
		return []string{"other"}, nil, changed, err
	}}
	m := feature(cfg)
	m = feed(m, reloadMsg{})
	if len(m.cmds) != len(sample) {
		t.Error("unchanged reload replaced the list")
	}
	err = errors.New("disk on fire")
	nm, cmd := m.Update(reloadMsg{})
	m = nm.(Model)
	if cmd == nil {
		t.Error("ticking must continue after an error")
	}
	if len(m.cmds) != len(sample) || !strings.Contains(m.status, "reload failed: disk on fire") {
		t.Errorf("status=%q cmds=%d", m.status, len(m.cmds))
	}
	if !strings.Contains(status(m), "reload failed") {
		t.Errorf("not shown: %q", status(m))
	}
	// no Reload: no tick
	if feature(Config{}).Init() != nil {
		t.Error("Init without Reload should not tick")
	}
}

func TestFrameHeightConstant(t *testing.T) {
	m := feature(Config{Commands: sample, Times: []int64{1}})
	steps := []func(Model) Model{
		func(m Model) Model { return keys(m, "?") },
		func(m Model) Model { return colon(m, "zzz") },
		func(m Model) Model { return typed(m, ":d") },
		func(m Model) Model { return keys(m, "esc", "ctrl+r") },
		func(m Model) Model { return colon(m, "g") },
		func(m Model) Model { return keys(m, "+", "f1") },
	}
	for i, s := range steps {
		m = s(m)
		if n := len(lines(m)); n != 20 {
			t.Errorf("step %d: %d lines", i, n)
		}
	}
}

func TestCopyOSC52ReachesTerminal(t *testing.T) {
	cfg := Config{Commands: sample, Height: Height{Percent: 40}}
	r, out := runPTY(t, cfg, "\x1b[C", "\r") // right (copy mode), enter
	if r.Action != result.None || !strings.Contains(r.Message, "OSC 52") {
		t.Fatalf("result = %+v", r)
	}
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("git status"))
	if !bytes.Contains([]byte(out), []byte(want)) {
		t.Errorf("OSC 52 sequence not written to the terminal: %q", out)
	}
}

func TestCopyOSC52TmuxReachesTerminal(t *testing.T) {
	cfg := Config{Commands: sample, Height: Height{Percent: 40}, InTmux: true}
	_, out := runPTY(t, cfg, "\x1b[C", "\r")
	if !bytes.Contains([]byte(out), clipboard.OSC52("git status", true)) {
		t.Errorf("tmux-wrapped OSC 52 not written: %q", out)
	}
}
