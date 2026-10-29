package fuzzy

import (
	"container/heap"
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	scoreMatch       = 16
	bonusSegment     = 10
	bonusWord        = 8
	bonusCamel       = 7
	bonusConsecutive = 6
	bonusBasename    = 4
	penaltyGapStart  = 3
	penaltyGapExtend = 1
	noScore          = math.MinInt / 2
	shardMin         = 4096
)

type Match struct {
	Path      string
	Score     int
	Positions []int
}

type Corpus struct {
	paths []string
	lower []string
	ascii []bool
}

type candidate struct {
	index int
	score int
}

type topK struct {
	paths []string
	limit int
	items []candidate
}

type scorer struct {
	query []rune
	bytes []byte
	text  []rune
	lower []rune
	lo    int
	hi    int
	score []int
	chain []int
	from  []int
}

func Rank(paths []string, query string, limit int) []Match {
	return NewCorpus(paths).Rank(query, limit)
}

func NewCorpus(paths []string) Corpus {
	c := Corpus{paths: paths, lower: make([]string, len(paths)), ascii: make([]bool, len(paths))}
	for i, p := range paths {
		c.ascii[i] = isASCII(p)
		if c.ascii[i] {
			c.lower[i] = strings.ToLower(p)
		} else {
			c.lower[i] = strings.Map(unicode.ToLower, p)
		}
	}
	return c
}

func (c Corpus) Rank(query string, limit int) []Match {
	q := []rune(strings.Map(unicode.ToLower, query))
	if len(q) == 0 || limit <= 0 {
		return c.shortest(limit)
	}
	shards := 1
	if len(c.paths) >= shardMin {
		shards = runtime.GOMAXPROCS(0)
	}
	size := (len(c.paths) + shards - 1) / shards
	tops := make([]*topK, shards)
	var wg sync.WaitGroup
	for n := range shards {
		wg.Go(func() {
			s := newScorer(q)
			top := newTopK(c.paths, limit)
			for i := n * size; i < min((n+1)*size, len(c.paths)); i++ {
				if score, ok := s.align(c, i); ok {
					top.push(candidate{index: i, score: score})
				}
			}
			tops[n] = top
		})
	}
	wg.Wait()
	merged := newTopK(c.paths, limit)
	for _, top := range tops {
		for _, cand := range top.items {
			merged.push(cand)
		}
	}
	s := newScorer(q)
	best := merged.sorted()
	out := make([]Match, 0, len(best))
	for _, cand := range best {
		s.align(c, cand.index)
		out = append(out, Match{Path: c.paths[cand.index], Score: cand.score, Positions: s.positions()})
	}
	return out
}

func (c Corpus) Paths() []string {
	return c.paths
}

func (c Corpus) shortest(limit int) []Match {
	top := newTopK(c.paths, limit)
	for i := range c.paths {
		top.push(candidate{index: i})
	}
	best := top.sorted()
	out := make([]Match, 0, len(best))
	for _, cand := range best {
		out = append(out, Match{Path: c.paths[cand.index], Positions: []int{}})
	}
	return out
}

func newTopK(paths []string, limit int) *topK {
	return &topK{paths: paths, limit: max(limit, 0), items: make([]candidate, 0, max(limit, 0))}
}

func (t *topK) push(c candidate) {
	switch {
	case len(t.items) < t.limit:
		heap.Push(t, c)
	case t.limit > 0 && t.worse(t.items[0], c):
		t.items[0] = c
		heap.Fix(t, 0)
	}
}

func (t *topK) sorted() []candidate {
	slices.SortFunc(t.items, func(a, b candidate) int {
		switch {
		case t.worse(b, a):
			return -1
		case t.worse(a, b):
			return 1
		}
		return 0
	})
	return t.items
}

func (t *topK) worse(a, b candidate) bool {
	if a.score != b.score {
		return a.score < b.score
	}
	pa, pb := t.paths[a.index], t.paths[b.index]
	if len(pa) != len(pb) {
		return len(pa) > len(pb)
	}
	return pa > pb
}

func (t *topK) Len() int           { return len(t.items) }
func (t *topK) Less(i, j int) bool { return t.worse(t.items[i], t.items[j]) }
func (t *topK) Swap(i, j int)      { t.items[i], t.items[j] = t.items[j], t.items[i] }
func (t *topK) Push(x any)         { t.items = append(t.items, x.(candidate)) }

func (t *topK) Pop() any {
	last := t.items[len(t.items)-1]
	t.items = t.items[:len(t.items)-1]
	return last
}

func newScorer(q []rune) *scorer {
	s := &scorer{query: q}
	if isASCII(string(q)) {
		s.bytes = []byte(string(q))
	}
	return s
}

func (s *scorer) align(c Corpus, i int) (int, bool) {
	var base int
	var ok bool
	if c.ascii[i] {
		base, ok = s.loadASCII(c.paths[i], c.lower[i])
	} else {
		base, ok = s.loadRunes(c.paths[i], c.lower[i])
	}
	if !ok {
		return 0, false
	}
	s.fill(base)
	_, best := s.end()
	return best, true
}

func (s *scorer) loadASCII(path, lower string) (int, bool) {
	q := s.bytes
	if q == nil {
		return 0, false
	}
	first, j := -1, 0
	for k := 0; k < len(lower) && j < len(q); k++ {
		if lower[k] == q[j] {
			if j == 0 {
				first = k
			}
			j++
		}
	}
	if j < len(q) {
		return 0, false
	}
	s.lo, s.hi = first, strings.LastIndexByte(lower, q[len(q)-1])
	s.text, s.lower = s.text[:0], s.lower[:0]
	for k := range s.hi + 1 {
		s.text = append(s.text, rune(path[k]))
		s.lower = append(s.lower, rune(lower[k]))
	}
	return strings.LastIndexByte(path, '/') + 1, true
}

func (s *scorer) loadRunes(path, lower string) (int, bool) {
	s.text, s.lower = s.text[:0], s.lower[:0]
	base := 0
	for _, r := range path {
		if r == '/' {
			base = len(s.text) + 1
		}
		s.text = append(s.text, r)
	}
	for len(lower) > 0 {
		r, size := utf8.DecodeRuneInString(lower)
		s.lower = append(s.lower, r)
		lower = lower[size:]
	}
	q := s.query
	first, j := -1, 0
	for k := 0; k < len(s.lower) && j < len(q); k++ {
		if s.lower[k] == q[j] {
			if j == 0 {
				first = k
			}
			j++
		}
	}
	if j < len(q) {
		return 0, false
	}
	s.lo, s.hi = first, first
	for k := len(s.lower) - 1; k > first; k-- {
		if s.lower[k] == q[len(q)-1] {
			s.hi = k
			break
		}
	}
	return base, true
}

func (s *scorer) fill(base int) {
	lo, hi, m := s.lo, s.hi, len(s.query)
	w := hi - lo + 1
	s.score = resize(s.score, w*m)
	s.chain = resize(s.chain, w*m)
	s.from = resize(s.from, w*m)
	for j := range m {
		gap, gapFrom := noScore, -1
		for i := lo; i <= hi; i++ {
			at := j*w + i - lo
			s.score[at], s.from[at] = noScore, -1
			if j > 0 && i-2 >= lo {
				if gap > noScore {
					gap -= penaltyGapExtend
				}
				if v := s.score[at-w-2]; v > noScore && v-penaltyGapStart > gap {
					gap, gapFrom = v-penaltyGapStart, i-2
				}
			}
			if s.lower[i] != s.query[j] {
				continue
			}
			own := scoreMatch
			if i >= base {
				own += bonusBasename
			}
			bonus := boundary(s.text, i)
			if j == 0 {
				s.score[at], s.chain[at] = own+bonus, bonus
				continue
			}
			if i > lo && s.score[at-w-1] > noScore {
				chain := max(bonus, s.chain[at-w-1], bonusConsecutive)
				s.score[at], s.chain[at], s.from[at] = s.score[at-w-1]+own+chain, chain, i-1
			}
			if v := gap + own + bonus; gap > noScore && v > s.score[at] {
				s.score[at], s.chain[at], s.from[at] = v, bonus, gapFrom
			}
		}
	}
}

func (s *scorer) end() (int, int) {
	w := s.hi - s.lo + 1
	last := (len(s.query) - 1) * w
	end, best := -1, noScore
	for i := s.lo; i <= s.hi; i++ {
		if v := s.score[last+i-s.lo]; v > best {
			end, best = i, v
		}
	}
	return end, best
}

func (s *scorer) positions() []int {
	w := s.hi - s.lo + 1
	out := make([]int, len(s.query))
	i, _ := s.end()
	for j := len(s.query) - 1; j >= 0; j-- {
		out[j] = i
		i = s.from[j*w+i-s.lo]
	}
	return out
}

func boundary(text []rune, i int) int {
	if i == 0 {
		return bonusSegment
	}
	prev, cur := text[i-1], text[i]
	switch {
	case prev == '/':
		return bonusSegment
	case !isWordRune(prev) && isWordRune(cur):
		return bonusWord
	case unicode.IsLower(prev) && unicode.IsUpper(cur):
		return bonusCamel
	}
	return 0
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func resize(buf []int, n int) []int {
	if cap(buf) < n {
		return make([]int, n)
	}
	return buf[:n]
}
