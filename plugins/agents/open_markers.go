package main

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	firstCircled = '①'
	lastCircled  = '⑳'

	sentenceEnds    = ".!?…"
	sentenceClosers = "*_`\"')]»”"
	paragraphBreak  = "\n\n"
)

var mayAskRe = regexp.MustCompile(`(?i)your call|up to you|should it|should I|if you want|let me know if|want me to|если хочешь|решать тебе|на твоё усмотрение|могу сделать|скажи, если`)

type openMark struct {
	at     int
	number int
}

func (s *session) markedLocked() map[*entry]string {
	if len(s.opens) == 0 {
		return nil
	}
	turns := map[int][]*entry{}
	for _, en := range s.opens {
		turns[en.Turn] = nil
	}
	for _, en := range s.entries {
		if _, ok := turns[en.Turn]; ok && en.Kind == entryAgent {
			turns[en.Turn] = append(turns[en.Turn], en)
		}
	}
	marks := map[*entry][]openMark{}
	numbers := openNumbers(s.opens)
	for _, en := range s.opens {
		for i, it := range en.Items {
			if numbers[en] == nil || numbers[en][i] == 0 {
				continue
			}
			for _, agent := range slices.Backward(turns[en.Turn]) {
				if at, ok := quoteEnd(agent.Text, it.Quote); ok {
					marks[agent] = append(marks[agent], openMark{at: at, number: numbers[en][i]})
					break
				}
			}
		}
	}
	out := make(map[*entry]string, len(marks))
	for en, list := range marks {
		out[en] = withMarks(en.Text, list)
	}
	return out
}

func quoteEnd(text, quote string) (int, bool) {
	i := strings.Index(text, quote)
	if i < 0 {
		return 0, false
	}
	return sentenceEnd(text, i+len(quote)), true
}

func sentenceEnd(text string, from int) int {
	if last, _ := utf8.DecodeLastRuneInString(text[:from]); strings.ContainsRune(sentenceEnds, last) {
		return afterClosers(text, from)
	}
	for i, r := range text[from:] {
		at := from + i
		if r == '\n' {
			return at
		}
		if !strings.ContainsRune(sentenceEnds, r) {
			continue
		}
		next := afterClosers(text, at+utf8.RuneLen(r))
		if rest, _ := utf8.DecodeRuneInString(text[next:]); next == len(text) || unicode.IsSpace(rest) {
			return next
		}
	}
	return len(text)
}

func afterClosers(text string, from int) int {
	for from < len(text) {
		r, size := utf8.DecodeRuneInString(text[from:])
		if !strings.ContainsRune(sentenceClosers, r) {
			break
		}
		from += size
	}
	return from
}

func withMarks(text string, marks []openMark) string {
	slices.SortStableFunc(marks, func(x, y openMark) int { return x.at - y.at })
	marks = slices.Compact(marks)
	var b strings.Builder
	prev := 0
	for _, m := range marks {
		digit := firstCircled + rune(m.number-1)
		if digit > lastCircled {
			continue
		}
		b.WriteString(text[prev:m.at])
		b.WriteByte(' ')
		b.WriteRune(digit)
		prev = m.at
	}
	b.WriteString(text[prev:])
	return b.String()
}

func openNumbers(entries []*entry) map[*entry][]int {
	decisions := 0
	out := make(map[*entry][]int, len(entries))
	for _, en := range entries {
		if !hasOpen(en) {
			continue
		}
		out[en] = make([]int, len(en.Items))
		for i, it := range en.Items {
			if it.Kind != itemOffer && it.State != itemHanded {
				decisions++
				out[en][i] = decisions
			}
		}
	}
	for en, numbers := range out {
		for i, it := range en.Items {
			if it.Kind == itemOffer && it.State != itemHanded {
				numbers[i] = decisions + 1
			}
		}
	}
	return out
}

func hasOpen(en *entry) bool {
	return slices.ContainsFunc(en.Items, func(it openItem) bool { return it.State == itemOpen })
}

func (s *session) mayAskLocked() bool {
	if s.rec.Question != "" || slices.ContainsFunc(s.opens, func(en *entry) bool { return en.Turn == s.turn }) {
		return false
	}
	for _, en := range slices.Backward(s.entries) {
		if en.Turn != s.turn {
			return false
		}
		if en.Kind == entryAgent {
			return mayAskRe.MatchString(lastParagraph(en.Text))
		}
	}
	return false
}

func lastParagraph(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.LastIndex(text, paragraphBreak); i >= 0 {
		return text[i+len(paragraphBreak):]
	}
	return text
}
