package main

import (
	"cmp"
	"slices"

	"clubmate/pkg/sdui"
)

type prefillView struct {
	Elicitation string `json:"elicitation,omitempty"`
	Field       string `json:"field,omitempty"`
	Open        string `json:"open,omitempty"`
	Value       string `json:"value"`
	Note        string `json:"note,omitempty"`
}

type optionAsideView struct {
	Elicitation string `json:"elicitation,omitempty"`
	Field       string `json:"field,omitempty"`
	Open        string `json:"open,omitempty"`
	Option      string `json:"option"`
	ID          string `json:"id"`
	State       string `json:"state"`
}

type asidePanel struct {
	Question    int    `json:"question,omitempty"`
	Option      int    `json:"option,omitempty"`
	Header      string `json:"header,omitempty"`
	Text        string `json:"text,omitempty"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Answer      bool   `json:"answer,omitempty"`
	Multi       bool   `json:"multi,omitempty"`
}

func (a *app) withAsideContent(v *transcriptView, s *session) {
	rec := v.Session
	if rec.isAside() {
		v.Pinned = a.pinnedStrip(rec)
		v.AsidePanel = a.asidePanelOf(rec)
		return
	}
	for _, c := range a.asides(rec.ID) {
		r := c.snapshot()
		an := r.Aside.Anchor
		if an.Option == "" {
			continue
		}
		mark := optionAsideView{Open: an.Open, Option: an.Option, ID: r.ID, State: asideState(r)}
		if an.Elicitation != "" {
			mark.Elicitation, mark.Field = an.Elicitation, s.fieldKey(an.Elicitation, an.Field)
		}
		v.OptionAsides = append(v.OptionAsides, mark)
	}
	for entry, fields := range s.prefillsFor() {
		for field, pf := range fields {
			v.Prefills = append(v.Prefills, prefillView{Elicitation: entry, Field: s.fieldKey(entry, field), Value: pf.Value, Note: pf.Note})
		}
	}
	open := s.openList()
	st := a.sessionState(rec.Workspace, rec.ID)
	a.uiMu.Lock()
	for item, pf := range st.openPrefills {
		if slices.ContainsFunc(open, func(it openItem) bool { return it.ID == item }) {
			v.Prefills = append(v.Prefills, prefillView{Open: item, Value: pf.Value, Note: pf.Note})
		}
	}
	a.uiMu.Unlock()
	slices.SortFunc(v.Prefills, func(x, y prefillView) int {
		return cmp.Compare(x.Elicitation+"/"+x.Field+"/"+x.Open, y.Elicitation+"/"+y.Field+"/"+y.Open)
	})
}

func (s *session) fieldKey(entry string, field int) string {
	el, ok := s.elicitation(entry)
	if !ok || field < 0 || field >= len(el.Fields) {
		return ""
	}
	return el.Fields[field].Key
}

func (a *app) asidePanelOf(rec record) *asidePanel {
	an := rec.Aside.Anchor
	p := &asidePanel{Text: cmp.Or(an.Quote, an.Question), Label: an.Option}
	parent := a.session(rec.ParentSession)
	if parent == nil || an.Option == "" {
		return p
	}
	if an.Elicitation != "" {
		el, ok := parent.elicitation(an.Elicitation)
		if !ok || an.Field < 0 || an.Field >= len(el.Fields) {
			return p
		}
		f := el.Fields[an.Field]
		cp := choiceProps(f, nil, false)
		p.Header, p.Text, p.Multi = cp.Title, cp.Text, f.Type == fieldMulti
		p.Question = 1 + len(slices.DeleteFunc(slices.Clone(el.Fields[:an.Field]), func(f elicitField) bool { return !isChoice(f) }))
		if o := slices.IndexFunc(cp.Options, func(o sdui.ChoiceOption) bool { return o.Value == an.Option }); o >= 0 {
			p.Option, p.Label, p.Description = o+1, cp.Options[o].Label, cp.Options[o].Description
		}
		p.withAnswer(a.answerQuestion(rec))
		return p
	}
	items := parent.numberedOpen()
	i := slices.IndexFunc(items, func(it openItem) bool { return it.ID == an.Open })
	if i < 0 {
		return p
	}
	it := items[i]
	p.Question, p.Header, p.Text, p.Multi = it.Number, it.Header, it.Question, it.MultiSelect
	switch o := slices.IndexFunc(it.Options, func(o openOption) bool { return o.Label == an.Option }); {
	case o >= 0:
		p.Option, p.Description = o+1, it.Options[o].Description
	case an.Option == otherOption:
		p.Option = len(it.Options) + 1
	}
	p.withAnswer(a.answerQuestion(rec))
	return p
}

func (p *asidePanel) withAnswer(n int, ok bool) {
	p.Answer = ok
	if ok {
		p.Question = n
	}
}
