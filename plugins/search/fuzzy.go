package main

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"unicode"
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
)

type match struct {
	Path      string
	Score     int
	Positions []int
}

type candidate struct {
	index int
	score int
}

type scorer struct {
	query []rune
	text  []rune
	lower []rune
	bonus []int
	score []int
	chain []int
	from  []int
}

func rank(paths []string, query string, limit int) []match {
	s := scorer{query: []rune(strings.ToLower(query))}
	var found []candidate
	for i, p := range paths {
		if score, ok := s.align(p); ok {
			found = append(found, candidate{index: i, score: score})
		}
	}
	slices.SortFunc(found, func(a, b candidate) int {
		pa, pb := paths[a.index], paths[b.index]
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(len(pa), len(pb)), strings.Compare(pa, pb))
	})
	found = found[:min(max(limit, 0), len(found))]
	out := make([]match, 0, len(found))
	for _, c := range found {
		p := paths[c.index]
		s.align(p)
		out = append(out, match{Path: p, Score: c.score, Positions: s.positions()})
	}
	return out
}

func (s *scorer) align(path string) (int, bool) {
	s.text, s.lower = s.text[:0], s.lower[:0]
	matched := 0
	for _, r := range path {
		l := unicode.ToLower(r)
		s.text = append(s.text, r)
		s.lower = append(s.lower, l)
		if matched < len(s.query) && l == s.query[matched] {
			matched++
		}
	}
	if matched < len(s.query) {
		return 0, false
	}
	if len(s.query) == 0 {
		return 0, true
	}
	s.fill()
	_, best := s.end()
	return best, true
}

func (s *scorer) fill() {
	n, m := len(s.text), len(s.query)
	base := 0
	s.bonus = s.bonus[:0]
	for i, r := range s.text {
		if r == '/' {
			base = i + 1
		}
		s.bonus = append(s.bonus, boundary(s.text, i))
	}
	s.score = resize(s.score, n*m)
	s.chain = resize(s.chain, n*m)
	s.from = resize(s.from, n*m)
	for j := range m {
		gap, gapFrom := noScore, -1
		for i := range n {
			at := j*n + i
			s.score[at], s.from[at] = noScore, -1
			if j > 0 && i >= 2 {
				if gap > noScore {
					gap -= penaltyGapExtend
				}
				if v := s.score[at-n-2]; v > noScore && v-penaltyGapStart > gap {
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
			if j == 0 {
				s.score[at], s.chain[at] = own+s.bonus[i], s.bonus[i]
				continue
			}
			if i > 0 && s.score[at-n-1] > noScore {
				chain := max(s.bonus[i], s.chain[at-n-1], bonusConsecutive)
				s.score[at], s.chain[at], s.from[at] = s.score[at-n-1]+own+chain, chain, i-1
			}
			if v := gap + own + s.bonus[i]; gap > noScore && v > s.score[at] {
				s.score[at], s.chain[at], s.from[at] = v, s.bonus[i], gapFrom
			}
		}
	}
}

func (s *scorer) end() (int, int) {
	n, last := len(s.text), (len(s.query)-1)*len(s.text)
	end, best := -1, noScore
	for i := range n {
		if v := s.score[last+i]; v > best {
			end, best = i, v
		}
	}
	return end, best
}

func (s *scorer) positions() []int {
	m := len(s.query)
	out := make([]int, m)
	if m == 0 {
		return out
	}
	i, _ := s.end()
	for j := m - 1; j >= 0; j-- {
		out[j] = i
		i = s.from[j*len(s.text)+i]
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

func resize(buf []int, n int) []int {
	if cap(buf) < n {
		return make([]int, n)
	}
	return buf[:n]
}
