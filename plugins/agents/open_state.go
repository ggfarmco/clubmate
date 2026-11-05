package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	kindOpenItems   = "open-items"
	kindOpenChanged = "open-items/changed"

	itemIDPrefix  = "oi-"
	openEntryID   = "open-"
	answerArrow   = " → "
	nonePrefix    = "None of these: "
	answersPrefix = "Answers: "
	doPrefix      = "do: "
)

type openState struct {
	opens    []*entry
	nextItem int
}

type openItemsPayload struct {
	Items    []openItem `json:"items,omitempty"`
	Overflow []int      `json:"overflow,omitempty"`
}

type openChange struct {
	Resolved []openResolution `json:"resolved,omitempty"`
	Told     []string         `json:"told,omitempty"`
	Later    bool             `json:"later,omitempty"`
}

type openResolution struct {
	Item   string `json:"item"`
	State  string `json:"state"`
	Answer string `json:"answer,omitempty"`
	Task   int    `json:"task,omitempty"`
}

type openPick struct {
	item   openItem
	answer string
}

type openPicks struct {
	decisions []openPick
	offers    []openPick
}

func (s *session) fitOpen(items []openItem) ([]openItem, []openItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	counts := map[string]int{}
	for _, it := range s.openItemsLocked() {
		counts[it.Kind]++
	}
	limits := map[string]int{itemDecision: maxOpenDecisions, itemOffer: maxOpenOffers}
	var kept, extra []openItem
	for _, it := range items {
		if counts[it.Kind] < limits[it.Kind] {
			counts[it.Kind]++
			kept = append(kept, it)
			continue
		}
		extra = append(extra, it)
	}
	return kept, extra
}

func (s *session) postOpen(items []openItem, overflow []int, state string) []string {
	s.mu.Lock()
	s.loadLocked()
	ids := make([]string, 0, len(items))
	posted := slices.Clone(items)
	for i := range posted {
		posted[i].ID, posted[i].State = itemIDPrefix+strconv.Itoa(s.nextItem+i+1), state
		ids = append(ids, posted[i].ID)
	}
	s.recordLocked(s.actor(), kindOpenItems, openItemsPayload{Items: posted, Overflow: overflow})
	s.mu.Unlock()
	s.changed(changeStatus, true)
	return ids
}

func (s *session) resolveOpen(change openChange) {
	s.mu.Lock()
	s.loadLocked()
	s.recordLocked(actorUser, kindOpenChanged, change)
	s.mu.Unlock()
	s.changed(changeStatus, true)
}

func (s *session) openList() []openItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return s.openItemsLocked()
}

func (s *session) numberedOpen() []openItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	numbers := openNumbers(s.opens)
	var out []openItem
	for _, en := range s.opens {
		for _, it := range withNumbers(en.Items, numbers[en]) {
			if it.State == itemOpen {
				out = append(out, it)
			}
		}
	}
	return out
}

func (s *session) openItemsLocked() []openItem {
	var out []openItem
	for _, en := range s.opens {
		for _, it := range en.Items {
			if it.State == itemOpen {
				out = append(out, it)
			}
		}
	}
	return out
}

func (s *session) itemLocked(id string) (openItem, bool) {
	for _, en := range s.opens {
		if i := slices.IndexFunc(en.Items, func(it openItem) bool { return it.ID == id }); i >= 0 {
			return en.Items[i], true
		}
	}
	return openItem{}, false
}

func (s *session) openPicked(ids []string) ([]openItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	if len(ids) == 0 {
		return s.openItemsLocked(), nil
	}
	out := make([]openItem, 0, len(ids))
	for _, id := range ids {
		it, ok := s.itemLocked(id)
		if !ok || it.State != itemOpen {
			return nil, fmt.Errorf("%w: %s", errNotOpen, id)
		}
		out = append(out, it)
	}
	return out, nil
}

func (s *session) answerPicks(args openAnswerArgs) (openPicks, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	var picks openPicks
	for _, ans := range args.Answers {
		it, ok := s.itemLocked(ans.Item)
		switch {
		case !ok || it.State != itemOpen:
			return openPicks{}, fmt.Errorf("%w: %s", errNotOpen, ans.Item)
		case it.Kind != itemDecision:
			return openPicks{}, fmt.Errorf("%w: %s", errNotDecision, ans.Item)
		}
		text, err := ans.text(it)
		if err != nil {
			return openPicks{}, fmt.Errorf("%s: %w", it.Header, err)
		}
		picks.decisions = append(picks.decisions, openPick{item: it, answer: text})
	}
	for _, id := range args.Offers {
		it, ok := s.itemLocked(id)
		switch {
		case !ok || it.State != itemOpen:
			return openPicks{}, fmt.Errorf("%w: %s", errNotOpen, id)
		case it.Kind != itemOffer:
			return openPicks{}, fmt.Errorf("%w: %s", errNotOffer, id)
		}
		picks.offers = append(picks.offers, openPick{item: it})
	}
	return picks, nil
}

func (s *session) takeNoneLocked() string {
	var told, titles []string
	for _, en := range s.opens {
		for _, it := range en.Items {
			if it.State == itemNo && !it.told {
				told, titles = append(told, it.ID), append(titles, it.title())
			}
		}
	}
	if len(told) == 0 {
		return ""
	}
	s.recordLocked(actorClubmate, kindOpenChanged, openChange{Told: told})
	return nonePrefix + strings.Join(titles, "; ")
}

func (s *session) applyOpenItems(e envelope) {
	var p openItemsPayload
	if json.Unmarshal(e.Payload, &p) != nil {
		return
	}
	id := openEntryID + strconv.Itoa(e.Turn)
	_, seen := s.index[key(entryOpenItems, id)]
	en := s.upsert(entryOpenItems, id, e.Turn)
	if !seen {
		s.opens = append(s.opens, en)
	}
	items := slices.Clone(en.Items)
	for _, it := range p.Items {
		it.At = e.TS
		items = append(items, it)
	}
	en.Items, en.Overflow, en.Later = items, append(slices.Clone(en.Overflow), p.Overflow...), false
	s.nextItem += len(p.Items)
	s.countOpen()
}

func (s *session) applyOpenChanged(e envelope) {
	var p openChange
	if json.Unmarshal(e.Payload, &p) != nil {
		return
	}
	for _, r := range p.Resolved {
		s.editItem(r.Item, func(it *openItem) {
			it.State, it.Answer, it.Task = r.State, r.Answer, r.Task
		})
	}
	for _, id := range p.Told {
		s.editItem(id, func(it *openItem) { it.told = true })
	}
	if p.Later {
		for _, en := range s.opens {
			if slices.ContainsFunc(en.Items, func(it openItem) bool { return it.State == itemOpen }) {
				en.Later, en.node = true, nil
			}
		}
	}
	s.countOpen()
}

func (s *session) editItem(id string, edit func(*openItem)) {
	for _, en := range s.opens {
		i := slices.IndexFunc(en.Items, func(it openItem) bool { return it.ID == id })
		if i < 0 {
			continue
		}
		en.Items = slices.Clone(en.Items)
		edit(&en.Items[i])
		en.node = nil
		return
	}
}

func (s *session) countOpen() {
	var since time.Time
	items := s.openItemsLocked()
	for _, it := range items {
		if since.IsZero() || it.At.Before(since) {
			since = it.At
		}
	}
	s.rec.OpenCount, s.rec.OpenSince = len(items), since
}

func (ans openAnswer) text(it openItem) (string, error) {
	picks := slices.DeleteFunc(append(slices.Clone(ans.Options), ans.Option), func(p string) bool { return p == "" })
	if len(picks) > 1 && !it.MultiSelect {
		return "", errOneOption
	}
	for _, p := range picks {
		if !slices.ContainsFunc(it.Options, func(o openOption) bool { return o.Label == p }) {
			return "", fmt.Errorf("%w: %q", errUnknownOption, p)
		}
	}
	if other := strings.TrimSpace(ans.Other); other != "" {
		picks = append(picks, other)
	}
	if len(picks) == 0 {
		return "", errNoAnswer
	}
	return strings.Join(picks, ", "), nil
}

func (p openPicks) prompt(note string) string {
	parts := make([]string, 0, len(p.decisions)+1)
	for _, d := range p.decisions {
		parts = append(parts, d.item.Header+answerArrow+d.answer)
	}
	if len(p.offers) > 0 {
		parts = append(parts, doPrefix+strings.Join(p.offerLabels(), ", "))
	}
	text := ""
	if len(parts) > 0 {
		text = answersPrefix + strings.Join(parts, "; ")
	}
	return strings.Join(nonEmpty(text, strings.TrimSpace(note)), "\n\n")
}

func (p openPicks) doText(note string) string {
	return strings.Join(nonEmpty(doPrefix+strings.Join(p.offerLabels(), ", "), strings.TrimSpace(note)), "\n\n")
}

func (p openPicks) offerLabels() []string {
	labels := make([]string, 0, len(p.offers))
	for _, o := range p.offers {
		labels = append(labels, o.item.Label)
	}
	return labels
}

func (p openPicks) resolved() []openResolution {
	out := p.offerResolved()
	for _, d := range p.decisions {
		out = append(out, openResolution{Item: d.item.ID, State: itemAnswered, Answer: d.answer})
	}
	return out
}

func (p openPicks) decisionItems() []openItem {
	out := make([]openItem, 0, len(p.decisions))
	for _, d := range p.decisions {
		d.item.Answer = d.answer
		out = append(out, d.item)
	}
	return out
}

func (p openPicks) offerItems() []openItem {
	out := make([]openItem, 0, len(p.offers))
	for _, o := range p.offers {
		out = append(out, o.item)
	}
	return out
}

func (p openPicks) offerResolved() []openResolution {
	out := make([]openResolution, 0, len(p.offers)+len(p.decisions))
	for _, o := range p.offers {
		out = append(out, openResolution{Item: o.item.ID, State: itemAnswered})
	}
	return out
}
