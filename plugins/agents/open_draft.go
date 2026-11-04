package main

import (
	"reflect"
	"slices"
	"strings"
)

const (
	openNode      = "open"
	openNextNode  = "next"
	openOtherNode = "other"
)

type openDraft struct {
	answers map[string]openAnswer
	others  map[string]bool
	offers  map[string]bool
	cursor  string
	focus   int
}

func openFormNode(sid string) string {
	return sid + "/" + openNode
}

func openItemNode(sid, item string) string {
	return openFormNode(sid) + "/" + item
}

func openOtherID(sid, item string) string {
	return openItemNode(sid, item) + "/" + openOtherNode
}

func openStops(items []openItem) []openItem {
	stops := make([]openItem, 0, len(items))
	for _, it := range items {
		if it.Kind != itemOffer {
			stops = append(stops, it)
		}
	}
	for _, it := range items {
		if it.Kind == itemOffer {
			stops = append(stops, it)
		}
	}
	return stops
}

func offerItems(items []openItem) []openItem {
	return slices.DeleteFunc(slices.Clone(items), func(it openItem) bool { return it.Kind != itemOffer })
}

func (d *openDraft) init() {
	if d.answers == nil {
		d.answers, d.others, d.offers = map[string]openAnswer{}, map[string]bool{}, map[string]bool{}
	}
}

func (d *openDraft) fill(sid string, items []openItem, values map[string]any) {
	d.init()
	changed := ""
	for _, it := range items {
		if it.Kind == itemOffer {
			continue
		}
		next := openAnswer{Item: it.ID}
		switch v := values[openItemNode(sid, it.ID)].(type) {
		case string:
			next.Option = v
		case []any:
			next.Options = stringValues(v)
		}
		other, on := values[openOtherID(sid, it.ID)].(string)
		next.Other = other
		if !reflect.DeepEqual(next, d.answers[it.ID]) && (next.picked() || d.answers[it.ID].picked()) || on != d.others[it.ID] {
			changed = it.ID
		}
		d.answers[it.ID], d.others[it.ID] = next, on
	}
	raw, _ := values[openItemNode(sid, openNextNode)].([]any)
	ticked := stringValues(raw)
	for _, it := range offerItems(items) {
		on := slices.Contains(ticked, it.ID)
		if on != d.offers[it.ID] {
			changed = it.ID
		}
		d.offers[it.ID] = on
	}
	if changed != "" {
		d.cursor = changed
	}
}

func stringValues(values []any) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (ans openAnswer) picked() bool {
	return ans.Option != "" || len(ans.Options) > 0 || strings.TrimSpace(ans.Other) != ""
}

func (d openDraft) args(sid string, items []openItem) openAnswerArgs {
	args := openAnswerArgs{Session: sid}
	for _, it := range openStops(items) {
		switch {
		case it.Kind == itemOffer && d.offers[it.ID]:
			args.Offers = append(args.Offers, it.ID)
		case it.Kind != itemOffer && d.answers[it.ID].picked():
			args.Answers = append(args.Answers, d.answers[it.ID])
		}
	}
	return args
}

func (args openAnswerArgs) picked() int {
	return len(args.Answers) + len(args.Offers)
}

func (d *openDraft) pick(it openItem, i int) {
	if i >= len(it.Options) {
		return
	}
	d.init()
	label := it.Options[i].Label
	ans := d.answers[it.ID]
	ans.Item = it.ID
	switch {
	case !it.MultiSelect:
		ans.Option, ans.Other = label, ""
		d.others[it.ID] = false
	case slices.Contains(ans.Options, label):
		ans.Options = slices.DeleteFunc(slices.Clone(ans.Options), func(p string) bool { return p == label })
	default:
		ans.Options = append(slices.Clone(ans.Options), label)
	}
	d.answers[it.ID] = ans
}

func (d *openDraft) tick(id string) {
	d.init()
	d.offers[id] = !d.offers[id]
}

func (d *openDraft) openOther(it openItem) {
	d.init()
	if !it.MultiSelect {
		ans := d.answers[it.ID]
		ans.Item, ans.Option = it.ID, ""
		d.answers[it.ID] = ans
	}
	d.others[it.ID] = true
	d.focus++
}

func (d *openDraft) prune(items []openItem) {
	open := make(map[string]bool, len(items))
	for _, it := range items {
		open[it.ID] = true
	}
	stale := func(id string) bool { return !open[id] }
	for id := range d.answers {
		if stale(id) {
			delete(d.answers, id)
		}
	}
	for id := range d.others {
		if stale(id) {
			delete(d.others, id)
		}
	}
	for id := range d.offers {
		if stale(id) {
			delete(d.offers, id)
		}
	}
}

func (d openDraft) clone() openDraft {
	c := d
	c.answers, c.others, c.offers = map[string]openAnswer{}, map[string]bool{}, map[string]bool{}
	for k, v := range d.answers {
		c.answers[k] = v
	}
	for k, v := range d.others {
		c.others[k] = v
	}
	for k, v := range d.offers {
		c.offers[k] = v
	}
	return c
}

func (d openDraft) current(items []openItem) string {
	stops := openStops(items)
	if len(stops) == 0 {
		return ""
	}
	if slices.ContainsFunc(stops, func(it openItem) bool { return it.ID == d.cursor }) {
		return d.cursor
	}
	return stops[0].ID
}
