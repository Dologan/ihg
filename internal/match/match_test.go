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
			_, _, ok := m.Match(Parse(tc.query), tc.text)
			if ok != tc.want {
				t.Fatalf("Match(%q, %q) = %v, want %v", tc.query, tc.text, ok, tc.want)
			}
		})
	}
}

func TestEmpty(t *testing.T) {
	for q, want := range map[string]bool{"": true, "  ": true, "a": false, "!x": false} {
		if got := Parse(q).Empty(); got != want {
			t.Errorf("Empty(%q) = %v, want %v", q, got, want)
		}
	}
	s, pos, ok := NewMatcher().Match(Parse(""), "x")
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
		_, pos, ok := m.Match(Parse(tc.query), tc.text)
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
	_, p1, _ := m.Match(Parse("'ab"), "ab")
	_, _, _ = m.Match(Parse("'xyz"), "xyz")
	if !reflect.DeepEqual(p1, []int{0, 1}) {
		t.Errorf("positions mutated: %v", p1)
	}
}

func TestScoreSum(t *testing.T) {
	m := NewMatcher()
	s1, _, _ := m.Match(Parse("git"), "git status")
	s2, _, _ := m.Match(Parse("stat"), "git status")
	s12, _, _ := m.Match(Parse("git stat"), "git status")
	sInv, _, _ := m.Match(Parse("git !zzz"), "git status")
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
	hits := m.MatchAll(Parse("git"), texts, true)
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
	for _, h := range m.MatchAll(Parse("git"), texts, false) {
		if h.Positions != nil {
			t.Errorf("withPos=false returned positions")
		}
	}
}

func TestMatchAllEmptyPattern(t *testing.T) {
	hits := NewMatcher().MatchAll(Parse(" "), []string{"x", "y", "z"}, true)
	want := []Hit{{Index: 0}, {Index: 1}, {Index: 2}}
	if !reflect.DeepEqual(hits, want) {
		t.Errorf("got %+v", hits)
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
	for _, q := range []string{"gco main", "kube"} {
		b.Run(strings.ReplaceAll(q, " ", "_"), func(b *testing.B) {
			m, p := NewMatcher(), Parse(q)
			b.ReportAllocs()
			for b.Loop() {
				m.MatchAll(p, benchTexts, false)
			}
		})
	}
}
