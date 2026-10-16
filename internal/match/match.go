// Package match implements fzf-style extended search over history lines.
//
// NOTICE: the matching algorithm (github.com/junegunn/fzf/src/algo) and the
// extended-search query syntax are taken from fzf, Copyright (c) Junegunn Choi,
// MIT License. The query parser in this package is an independent
// implementation of fzf's documented semantics.
//
// Matching modes (see Mode). Syntax in ModeExact (the default) and ModeFuzzy;
// terms are separated by spaces and "\ " is a literal space:
//
//	foo     exact substring (ModeExact) or fuzzy match (ModeFuzzy)
//	'foo    the other one: fuzzy in ModeExact, exact in ModeFuzzy
//	^foo    exact prefix
//	foo$    exact suffix
//	^foo$   whole line equals foo
//	!foo    line must NOT contain foo (also !^foo and !foo$ for prefix/suffix)
//	a | b   OR group: terms joined by a lone "|" match if any alternative does
//
// Terms are ANDed. A lone ', ^, ! or $ is a literal term (plain kind of the
// mode). In ModeRegex the whole query is one Go (RE2) regular expression: no
// term splitting, no extended syntax, no lookaround or backreferences. Matching is
// case-insensitive unless the whole query contains an uppercase letter (smart
// case). Latin diacritics are normalized, so "cafe" matches "café".
//
// Deviations from fzf: the boundary-exact form 'foo' is not supported (the
// quotes are treated as part of a fuzzy/exact term as described above); smart
// case is decided once for the whole query rather than per term; a lone ^, !
// is literal rather than ignored.
package match

import (
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// Mode selects how a query is interpreted.
type Mode int

const (
	// ModeExact treats plain terms as exact substrings (the default).
	ModeExact Mode = iota
	// ModeFuzzy treats plain terms as fuzzy matches.
	ModeFuzzy
	// ModeRegex treats the whole query as one regular expression.
	ModeRegex
)

type termKind uint8

const (
	kindFuzzy termKind = iota
	kindExact
	kindPrefix
	kindSuffix
	kindEqual
)

type term struct {
	kind      termKind
	inverse   bool
	normalize bool
	runes     []rune
}

// Pattern is a parsed query. It is immutable and safe for concurrent use.
type Pattern struct {
	mode          Mode
	sets          [][]term // AND of OR-groups
	caseSensitive bool
	re            *regexp.Regexp // ModeRegex
	err           error          // ModeRegex: compile error; nothing matches
}

// Empty reports whether the pattern matches everything. An invalid regular
// expression is not empty: it matches nothing.
func (p *Pattern) Empty() bool {
	if p.mode == ModeRegex {
		return p.re == nil && p.err == nil
	}
	return len(p.sets) == 0
}

// Err returns the compile error of an invalid ModeRegex query, else nil.
func (p *Pattern) Err() error { return p.err }

const escSpace = "\x00"

// Parse parses a query in the given mode.
func Parse(query string, mode Mode) *Pattern {
	if mode == ModeRegex {
		return parseRegex(query)
	}
	p := &Pattern{mode: mode, caseSensitive: strings.ToLower(query) != query}
	query = strings.ReplaceAll(query, `\ `, escSpace)
	var set []term
	afterBar, open := false, false // open: next term joins current set
	flush := func() {
		if len(set) > 0 {
			p.sets = append(p.sets, set)
			set = nil
		}
	}
	for _, tok := range strings.Split(query, " ") {
		if tok == "" {
			continue
		}
		if tok == "|" && len(set) > 0 && !afterBar {
			afterBar, open = true, true
			continue
		}
		afterBar = false
		t, ok := p.parseTerm(strings.ReplaceAll(tok, escSpace, " "))
		if !ok {
			continue
		}
		if !open {
			flush()
		}
		set = append(set, t)
		open = false
	}
	flush()
	return p
}

// parseRegex compiles the query with smart case: case-insensitive unless it
// has an uppercase letter that is not part of an escape (\S, \D, ...).
func parseRegex(query string) *Pattern {
	p := &Pattern{mode: ModeRegex}
	if query == "" {
		return p
	}
	upper, esc := false, false
	for _, r := range query {
		switch {
		case esc:
			esc = false
		case r == '\\':
			esc = true
		case unicode.IsUpper(r):
			upper = true
		}
	}
	p.caseSensitive = upper
	expr := query
	if !upper {
		expr = "(?i)" + expr
	}
	p.re, p.err = regexp.Compile(expr)
	return p
}

// plainKind is the kind of a term without a prefix or suffix operator.
func (p *Pattern) plainKind() termKind {
	if p.mode == ModeFuzzy {
		return kindFuzzy
	}
	return kindExact
}

func (p *Pattern) parseTerm(text string) (term, bool) {
	if !p.caseSensitive {
		text = strings.ToLower(text)
	}
	def := p.plainKind()
	t := term{kind: def}
	if len(text) == 1 && strings.ContainsAny(text, "'^!$") {
		// lone special character: literal
	} else {
		if strings.HasPrefix(text, "!") {
			t.inverse, t.kind = true, kindExact
			text = text[1:]
		}
		if text != "$" && strings.HasSuffix(text, "$") {
			t.kind = kindSuffix
			text = text[:len(text)-1]
		}
		if strings.HasPrefix(text, "'") {
			if t.kind == def && !t.inverse { // the quote inverts the default
				if def == kindFuzzy {
					t.kind = kindExact
				} else {
					t.kind = kindFuzzy
				}
			}
			text = text[1:]
		} else if strings.HasPrefix(text, "^") {
			if t.kind == kindSuffix {
				t.kind = kindEqual
			} else {
				t.kind = kindPrefix
			}
			text = text[1:]
		}
	}
	if text == "" {
		return t, false
	}
	t.runes = []rune(text)
	// Like fzf, only normalize the term when it has no diacritics itself.
	if norm := algo.NormalizeRunes(t.runes); slices.Equal(norm, t.runes) {
		t.normalize = true
	}
	return t, true
}

var initOnce sync.Once

// Matcher runs patterns against text. It owns scratch memory and is NOT safe
// for concurrent use; create one per goroutine.
type Matcher struct {
	slab *util.Slab
	buf  []byte
	pos  []int
}

// NewMatcher returns a Matcher with a fresh fzf slab.
func NewMatcher() *Matcher {
	initOnce.Do(func() { algo.Init("default") })
	return &Matcher{slab: util.MakeSlab(100*1024, 2048)}
}

func (m *Matcher) chars(text string) util.Chars {
	m.buf = append(m.buf[:0], text...)
	return util.ToChars(m.buf)
}

// Match tests text against p. positions are sorted, de-duplicated rune indices
// of matched characters across non-inverse terms. The score is the sum over
// matched terms (inverse terms add 0). An empty pattern matches with score 0.
func (m *Matcher) Match(p *Pattern, text string) (score int, positions []int, ok bool) {
	return m.match(p, text, true)
}

func (m *Matcher) match(p *Pattern, text string, withPos bool) (int, []int, bool) {
	if p.mode == ModeRegex {
		return matchRegex(p, text, withPos)
	}
	if len(p.sets) == 0 {
		return 0, nil, true
	}
	chars := m.chars(text)
	total := 0
	m.pos = m.pos[:0]
	for _, set := range p.sets {
		matched := false
		for i := range set {
			t := &set[i]
			res, pos := m.run(p, t, &chars, withPos && !t.inverse)
			if t.inverse {
				if res.Start < 0 {
					matched = true
					break
				}
				continue
			}
			if res.Start < 0 {
				continue
			}
			total += res.Score
			if withPos {
				if pos != nil {
					m.pos = append(m.pos, *pos...)
				} else {
					for j := res.Start; j < res.End; j++ {
						m.pos = append(m.pos, j)
					}
				}
			}
			matched = true
			break
		}
		if !matched {
			return 0, nil, false
		}
	}
	if !withPos || len(m.pos) == 0 {
		return total, nil, true
	}
	slices.Sort(m.pos)
	out := slices.Compact(slices.Clone(m.pos))
	return total, out, true
}

// matchRegex matches a ModeRegex pattern. The score is always 0; positions
// are the rune indices covered by all non-empty matches.
func matchRegex(p *Pattern, text string, withPos bool) (int, []int, bool) {
	switch {
	case p.err != nil:
		return 0, nil, false
	case p.re == nil:
		return 0, nil, true
	case !withPos:
		return 0, nil, p.re.MatchString(text)
	}
	locs := p.re.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return 0, nil, false
	}
	var out []int
	li, idx := 0, 0
	for b := range text { // b is the byte offset of rune idx
		for li < len(locs) && b >= locs[li][1] {
			li++
		}
		if li == len(locs) {
			break
		}
		if b >= locs[li][0] && locs[li][1] > locs[li][0] {
			out = append(out, idx)
		}
		idx++
	}
	return 0, out, true
}

func (m *Matcher) run(p *Pattern, t *term, c *util.Chars, withPos bool) (algo.Result, *[]int) {
	cs, norm := p.caseSensitive, t.normalize
	switch t.kind {
	case kindExact:
		return algo.ExactMatchNaive(cs, norm, true, c, t.runes, false, m.slab)
	case kindPrefix:
		return algo.PrefixMatch(cs, norm, true, c, t.runes, false, m.slab)
	case kindSuffix:
		return algo.SuffixMatch(cs, norm, true, c, t.runes, false, m.slab)
	case kindEqual:
		return algo.EqualMatch(cs, norm, true, c, t.runes, false, m.slab)
	}
	return algo.FuzzyMatchV2(cs, norm, true, c, t.runes, withPos, m.slab)
}

// Hit is one MatchAll result.
type Hit struct {
	Index, Score int
	Positions    []int
}

// MatchAll matches p against every text (index 0 = newest) and returns hits
// sorted by Score descending, then Index ascending. An empty pattern returns
// every index in order with Score 0. Positions are filled only if withPos.
func (m *Matcher) MatchAll(p *Pattern, texts []string, withPos bool) []Hit {
	hits := make([]Hit, 0, len(texts))
	if p.Empty() {
		for i := range texts {
			hits = append(hits, Hit{Index: i})
		}
		return hits
	}
	for i, tx := range texts {
		score, pos, ok := m.match(p, tx, withPos)
		if ok {
			hits = append(hits, Hit{Index: i, Score: score, Positions: pos})
		}
	}
	slices.SortFunc(hits, func(a, b Hit) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return a.Index - b.Index
	})
	return hits
}
