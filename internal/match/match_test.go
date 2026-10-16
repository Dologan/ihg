package match

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestSyntax(t *testing.T) {
	tests := []struct {
		name, query, text string
		want              bool
	}{
		{"fuzzy hit", "gco", "git checkout main", true},
		{"fuzzy miss", "gxo", "git checkout main", false},
		{"exact hit", "'check", "git checkout main", true},
		{"exact miss (fuzzy only)", "'gco", "git checkout main", false},
		{"prefix hit", "^git", "git status", true},
		{"prefix miss", "^status", "git status", false},
		{"suffix hit", "status$", "git status", true},
		{"suffix miss", "git$", "git status", false},
		{"equal hit", "^ls$", "ls", true},
		{"equal miss", "^ls$", "ls -l", false},
		{"inverse hit", "!rm", "git status", true},
		{"inverse miss", "!git", "git status", false},
		{"inverse prefix ok", "!^status", "git status", true},
		{"inverse prefix miss", "!^git", "git status", false},
		{"inverse suffix ok", "!git$", "git status", true},
		{"inverse suffix miss", "!status$", "git status", false},
		{"and", "git stat", "git status", true},
		{"and miss", "git zzz", "git status", false},
		{"or first", "^core go$ | rb$ | py$", "core run.py", true},
		{"or second", "^core go$ | rb$ | py$", "core x.go", true},
		{"or none", "^core go$ | rb$ | py$", "core x.rs", false},
		{"or prefix gate", "^core go$ | rb$ | py$", "other x.go", false},
		{"escaped space", `foo\ bar`, "echo foo bar", true},
		{"escaped space is one term", `'foo\ bar`, "foo  bar", false},
		{"lone quote", "'", "it's", true},
		{"lone caret", "^", "a^b", true},
		{"lone bang", "!", "hi!", true},
		{"lone bang miss", "!", "hi", false},
		{"lone dollar", "$", "cost $5", true},
		{"smart case insensitive", "git", "GIT Status", true},
		{"smart case sensitive hit", "Git", "Git status", true},
		{"smart case sensitive miss", "Git", "git status", false},
		{"smart case whole query", "Git stat", "Git STAT", false},
		{"diacritic query plain", "cafe", "café au lait", true},
		{"diacritic query accented", "café", "cafe", false},
		{"empty", "", "anything", true},
		{"spaces only", "   ", "anything", true},
		{"multiline", "'two", "one\ntwo", true},
		{"multiline suffix", "two$", "one\ntwo", true},
	}
	m := NewMatcher()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, ok := m.Match(Parse(tc.query, ModeFuzzy), tc.text)
			if ok != tc.want {
				t.Fatalf("Match(%q, %q) = %v, want %v", tc.query, tc.text, ok, tc.want)
			}
		})
	}
}

func TestEmpty(t *testing.T) {
	for q, want := range map[string]bool{"": true, "  ": true, "a": false, "!x": false} {
		if got := Parse(q, ModeFuzzy).Empty(); got != want {
			t.Errorf("Empty(%q) = %v, want %v", q, got, want)
		}
	}
	s, pos, ok := NewMatcher().Match(Parse("", ModeFuzzy), "x")
	if s != 0 || pos != nil || !ok {
		t.Errorf("empty match = %d %v %v", s, pos, ok)
	}
}

func TestPositions(t *testing.T) {
	tests := []struct {
		query, text string
		want        []int
	}{
		{"gco", "git checkout", []int{0, 4, 9}},
		{"'check", "git checkout", []int{4, 5, 6, 7, 8}},
		{"^git", "git checkout", []int{0, 1, 2}},
		{"out$", "git checkout", []int{9, 10, 11}},
		{"^ls$", "ls", []int{0, 1}},
		{"!rm git", "git checkout", []int{0, 1, 2}},
		// non-ASCII: indices are runes, not bytes
		{"'au", "café au lait", []int{5, 6}},
		{"'lait", "日本 café lait", []int{8, 9, 10, 11}},
		{"^git 'it", "git it", []int{0, 1, 2, 4, 5}}, // de-duplicated, sorted
		{"'git git", "git", []int{0, 1, 2}},
	}
	m := NewMatcher()
	for _, tc := range tests {
		_, pos, ok := m.Match(Parse(tc.query, ModeFuzzy), tc.text)
		if !ok {
			t.Errorf("%q should match %q", tc.query, tc.text)
			continue
		}
		if !reflect.DeepEqual(pos, tc.want) {
			t.Errorf("positions(%q, %q) = %v, want %v", tc.query, tc.text, pos, tc.want)
		}
	}
}

func TestPositionsIndependentOfScratch(t *testing.T) {
	m := NewMatcher()
	_, p1, _ := m.Match(Parse("'ab", ModeFuzzy), "ab")
	_, _, _ = m.Match(Parse("'xyz", ModeFuzzy), "xyz")
	if !reflect.DeepEqual(p1, []int{0, 1}) {
		t.Errorf("positions mutated: %v", p1)
	}
}

func TestScoreSum(t *testing.T) {
	m := NewMatcher()
	s1, _, _ := m.Match(Parse("git", ModeFuzzy), "git status")
	s2, _, _ := m.Match(Parse("stat", ModeFuzzy), "git status")
	s12, _, _ := m.Match(Parse("git stat", ModeFuzzy), "git status")
	sInv, _, _ := m.Match(Parse("git !zzz", ModeFuzzy), "git status")
	if s12 != s1+s2 {
		t.Errorf("sum: %d != %d+%d", s12, s1, s2)
	}
	if sInv != s1 {
		t.Errorf("inverse should add 0: %d vs %d", sInv, s1)
	}
}

func TestMatchAllOrdering(t *testing.T) {
	texts := []string{
		"a long thing with g and i and t spread out",
		"git",
		"unrelated",
		"git",
		"digit",
	}
	m := NewMatcher()
	hits := m.MatchAll(Parse("git", ModeFuzzy), texts, true)
	var idx []int
	for _, h := range hits {
		idx = append(idx, h.Index)
		if len(h.Positions) == 0 {
			t.Errorf("hit %d lacks positions", h.Index)
		}
	}
	if want := []int{1, 3, 0, 4}; !reflect.DeepEqual(idx, want) {
		t.Fatalf("order = %v, want %v (hits %+v)", idx, want, hits)
	}
	for i := 1; i < len(hits); i++ {
		if hits[i-1].Score < hits[i].Score {
			t.Errorf("scores not descending: %+v", hits)
		}
	}
	for _, h := range m.MatchAll(Parse("git", ModeFuzzy), texts, false) {
		if h.Positions != nil {
			t.Errorf("withPos=false returned positions")
		}
	}
}

func TestMatchAllEmptyPattern(t *testing.T) {
	hits := NewMatcher().MatchAll(Parse(" ", ModeFuzzy), []string{"x", "y", "z"}, true)
	want := []Hit{{Index: 0}, {Index: 1}, {Index: 2}}
	if !reflect.DeepEqual(hits, want) {
		t.Errorf("got %+v", hits)
	}
}

// TestModes runs every syntax form in every mode. want is indexed by
// ModeExact, ModeFuzzy, ModeRegex; regex entries only where the query is a
// meaningful regular expression (otherwise "-" is skipped).
func TestModes(t *testing.T) {
	const (
		y = 'y'
		n = 'n'
		x = '-' // not applicable
	)
	tests := []struct {
		query, text string
		want        [3]byte // exact, fuzzy, regex
	}{
		{"checkout", "git checkout main", [3]byte{y, y, y}},
		{"gco", "git checkout main", [3]byte{n, y, n}},
		{"'gco", "git checkout main", [3]byte{y, n, x}},
		{"'chkt", "git checkout main", [3]byte{y, n, x}},
		{"^git", "git status", [3]byte{y, y, y}},
		{"^status", "git status", [3]byte{n, n, n}},
		{"status$", "git status", [3]byte{y, y, y}},
		{"git$", "git status", [3]byte{n, n, n}},
		{"^ls$", "ls", [3]byte{y, y, y}},
		{"^ls$", "ls -l", [3]byte{n, n, n}},
		{"!rm", "git status", [3]byte{y, y, x}},
		{"!git", "git status", [3]byte{n, n, x}},
		{"!^status", "git status", [3]byte{y, y, x}},
		{"!status$", "git status", [3]byte{n, n, x}},
		{"git stat", "git status", [3]byte{y, y, x}},
		{"git zzz", "git status", [3]byte{n, n, x}},
		{"gt stts", "git status", [3]byte{n, y, x}},
		{"^core go$ | rb$", "core x.go", [3]byte{y, y, x}},
		{"^core go$ | rb$", "core x.rb", [3]byte{y, y, x}},
		{"^core go$ | rb$", "core x.rs", [3]byte{n, n, x}},
		{`foo\ bar`, "echo foo bar", [3]byte{y, y, x}},
		{"'", "it's", [3]byte{y, y, y}},
		{"Git", "git status", [3]byte{n, n, n}},
		{"git", "GIT status", [3]byte{y, y, y}},
		{"", "x", [3]byte{y, y, y}},
	}
	m := NewMatcher()
	for _, tc := range tests {
		for mode, name := range []string{"exact", "fuzzy", "regex"} {
			w := tc.want[mode]
			if w == x {
				continue
			}
			_, _, ok := m.Match(Parse(tc.query, Mode(mode)), tc.text)
			if ok != (w == y) {
				t.Errorf("%s: Match(%q, %q) = %v, want %v", name, tc.query, tc.text, ok, w == y)
			}
		}
	}
}

func TestExactModeQuoteInvertsDefault(t *testing.T) {
	m := NewMatcher()
	_, pos, ok := m.Match(Parse("'gco", ModeExact), "git checkout")
	if !ok || !reflect.DeepEqual(pos, []int{0, 4, 9}) {
		t.Errorf("'gco in exact mode: ok=%v pos=%v", ok, pos)
	}
	// An inverse term stays exact in both modes; the quote after ! is dropped.
	for _, mode := range []Mode{ModeExact, ModeFuzzy} {
		if _, _, ok := m.Match(Parse("!'gco", mode), "git checkout"); !ok {
			t.Errorf("mode %d: !'gco should match (no literal gco)", mode)
		}
	}
	// exact plain term with a suffix operator is still a suffix.
	if _, _, ok := m.Match(Parse("out$", ModeExact), "git checkout"); !ok {
		t.Error("suffix in exact mode")
	}
}

func TestRegexSmartCase(t *testing.T) {
	m := NewMatcher()
	tests := []struct {
		query, text string
		want        bool
	}{
		{"git.*main", "GIT checkout MAIN", true}, // all lower: insensitive
		{"Git.*main", "git checkout main", false},
		{"Git.*main", "Git checkout main", true},
		{`\S+ status`, "GIT STATUS status", true}, // \S is not an uppercase hint
		{`\S+ status`, "git STATUS", true},
		{`\d+$`, "RUN 42", true},
		{`\D+`, "abc", true},
		{`\\S`, "a\\S", true}, // an escaped backslash, then a literal capital S
		{`\\S`, "a\\s", false},
	}
	for _, tc := range tests {
		_, _, ok := m.Match(Parse(tc.query, ModeRegex), tc.text)
		if ok != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tc.query, tc.text, ok, tc.want)
		}
	}
}

func TestRegexBasics(t *testing.T) {
	m := NewMatcher()
	p := Parse(`^git\s+(status|log)$`, ModeRegex)
	if p.Err() != nil || p.Empty() {
		t.Fatalf("err=%v empty=%v", p.Err(), p.Empty())
	}
	for text, want := range map[string]bool{"git status": true, "git  log": true, "git logs": false, "x git log": false} {
		s, _, ok := m.Match(p, text)
		if ok != want || s != 0 {
			t.Errorf("%q: ok=%v score=%d", text, ok, s)
		}
	}
	// No term splitting: spaces are part of the expression.
	if _, _, ok := m.Match(Parse("git  status", ModeRegex), "git status"); ok {
		t.Error("double space must not split into terms")
	}
	// Empty query matches everything.
	e := Parse("", ModeRegex)
	if !e.Empty() || e.Err() != nil {
		t.Error("empty regex pattern should be Empty")
	}
	if _, _, ok := m.Match(e, "anything"); !ok {
		t.Error("empty regex must match")
	}
}

func TestRegexInvalid(t *testing.T) {
	p := Parse("(unclosed", ModeRegex)
	if p.Err() == nil {
		t.Fatal("want a compile error")
	}
	if p.Empty() {
		t.Error("an invalid pattern is not Empty")
	}
	m := NewMatcher()
	if _, _, ok := m.Match(p, "(unclosed"); ok {
		t.Error("invalid pattern must not match")
	}
	if hits := m.MatchAll(p, []string{"a", "(b"}, true); len(hits) != 0 {
		t.Errorf("hits = %v", hits)
	}
	// Lookaround is not RE2.
	if Parse(`foo(?=bar)`, ModeRegex).Err() == nil {
		t.Error("lookahead should be rejected")
	}
	if Parse("x", ModeFuzzy).Err() != nil {
		t.Error("non-regex modes never error")
	}
}

func TestRegexPositions(t *testing.T) {
	cases := []struct {
		query, text string
		want        []int
	}{
		{"st", "git status", []int{4, 5}},
		{"t", "git status", []int{2, 5, 7}},
		{"é.", "日本 café lait", []int{6, 7}},
		{"日本", "日本 café", []int{0, 1}},
		{"a*", "baab", []int{1, 2}}, // zero-width matches are skipped
		{"x*", "abc", nil},
		{`\n`, "a\nb", []int{1}},
	}
	m := NewMatcher()
	for _, tc := range cases {
		_, pos, ok := m.Match(Parse(tc.query, ModeRegex), tc.text)
		if !ok {
			t.Errorf("%q should match %q", tc.query, tc.text)
			continue
		}
		if !reflect.DeepEqual(pos, tc.want) {
			t.Errorf("positions(%q, %q) = %v, want %v", tc.query, tc.text, pos, tc.want)
		}
	}
}

func TestRegexMatchAllKeepsRecencyOrder(t *testing.T) {
	hits := NewMatcher().MatchAll(Parse("git", ModeRegex), []string{"a git", "b", "git c", "d git"}, false)
	var idx []int
	for _, h := range hits {
		idx = append(idx, h.Index)
		if h.Score != 0 {
			t.Errorf("score = %d", h.Score)
		}
	}
	if !reflect.DeepEqual(idx, []int{0, 2, 3}) {
		t.Errorf("order = %v", idx)
	}
}

var benchTexts = func() []string {
	r := rand.New(rand.NewSource(1))
	branches := []string{"main", "feature/login", "fix-bug-123", "release/2.0", "café-menu"}
	pods := []string{"api", "web", "worker", "db"}
	out := make([]string, 100000)
	for i := range out {
		switch r.Intn(7) {
		case 0:
			out[i] = "git checkout " + branches[r.Intn(len(branches))]
		case 1:
			out[i] = fmt.Sprintf("git commit -m \"fix %d: handle ünicode\"", r.Intn(5000))
		case 2:
			out[i] = fmt.Sprintf("docker run --rm -it -v $PWD:/src -p %d:80 img%d:latest bash", 8000+r.Intn(100), r.Intn(50))
		case 3:
			out[i] = fmt.Sprintf("kubectl -n prod logs -f %s-%d --tail=%d", pods[r.Intn(4)], r.Intn(9), r.Intn(500))
		case 4:
			out[i] = fmt.Sprintf("cd ~/devel/project%d/src/%s", r.Intn(300), pods[r.Intn(4)])
		case 5:
			out[i] = fmt.Sprintf("cat /var/log/app%d.log | grep -i error | awk '{print $%d}' | sort | uniq -c | sort -rn | head -%d", r.Intn(40), r.Intn(9)+1, r.Intn(30)+1)
		default:
			out[i] = strings.Repeat("x", r.Intn(10)) + fmt.Sprintf("make test TARGET=%d", r.Intn(1000))
		}
	}
	return out
}()

func BenchmarkMatchAll100k(b *testing.B) {
	for _, bc := range []struct {
		name  string
		query string
		mode  Mode
	}{
		{"fuzzy_gco_main", "gco main", ModeFuzzy},
		{"fuzzy_kube", "kube", ModeFuzzy},
		{"exact_checkout_main", "checkout main", ModeExact},
		{"exact_kube", "kube", ModeExact},
		{"regex_kube", "kube.*prod", ModeRegex},
		{"regex_gco", `git (checkout|commit).*\d+`, ModeRegex},
	} {
		b.Run(bc.name, func(b *testing.B) {
			m, p := NewMatcher(), Parse(bc.query, bc.mode)
			b.ReportAllocs()
			for b.Loop() {
				m.MatchAll(p, benchTexts, false)
			}
		})
	}
}
