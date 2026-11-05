package main

import (
	"cmp"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const (
	actElicitation    = "elicitation"
	actElicitPick     = "elicitationPick"
	actElicitDecline  = "elicitationDecline"
	actElicitOpen     = "elicitationOpen"
	maxOptionShortcut = 9
	customSuffix      = "_custom"
	otherLabel        = "Other…"
	otherPlaceholder  = "Your answer"
	cardTone          = "warning"
	questionGap       = 10
	elicitSendKey     = "cmd+return"
)

func elicitationCard(sc screen, en *entry) sdui.Node {
	el := en.Elicitation
	if el == nil {
		return sdui.Node{}
	}
	if en.Response != nil || !sc.Pending[en.ID] {
		return sdui.MutedText(elicitationOutcome(en) + " · " + firstLine(el.Message))
	}
	id := elicitationNode(sc.View.Session.ID, en.ID)
	if el.Mode == acp.ElicitationURL {
		open := sdui.WithVariant(sdui.WithAction(sdui.Button(id+"/open", "Open in browser", "safari"), actElicitOpen), "primary")
		decline := sdui.WithVariant(sdui.WithAction(sdui.Button(id+"/decline", "Decline", ""), actElicitDecline), "ghost")
		return sdui.Form(id, sdui.Card(cardTone, "person.badge.key.fill", sdui.Text(el.Message), sdui.MutedText(el.URL), sdui.HStack(open, decline)))
	}
	children := []sdui.Node{sdui.Text(el.Message), sdui.FitScroll(sdui.WithGap(sdui.VStack(fieldNodes(id, el.Fields)...), questionGap))}
	submit := sdui.WithVariant(sdui.Button(id+"/submit", "Send", ""), "primary")
	if !answersOnPick(el.Fields) {
		submit = sdui.WithShortcut(submit, elicitSendKey)
	}
	skip := sdui.WithVariant(sdui.WithAction(sdui.Button(id+"/decline", "Skip", ""), actElicitDecline), "ghost")
	children = append(children, sdui.HStack(submit, skip))
	form := sdui.WithAction(sdui.Form(id, sdui.Card(cardTone, "questionmark.bubble.fill", children...)), actElicitation)
	if el.NeedsAnswer {
		form = sdui.WithNeedsAny(form, fieldIDs(id, el.Fields))
	}
	return sdui.WithDock(form)
}

func (a *app) elicitAct(ev pluginsdk.UIEvent, s *session, node string) error {
	rest, ok := strings.CutPrefix(node, entryElicitation+"/")
	if !ok {
		return errNotAnElicitation
	}
	id, _, _ := strings.Cut(rest, "/")
	el, ok := s.elicitation(id)
	if !ok {
		return errNotAnElicitation
	}
	switch ev.Action {
	case actElicitDecline:
		return s.respond(id, acp.ElicitationResponse{Action: acp.ElicitationDecline})
	case actElicitOpen:
		if err := openInBrowser(el.URL); err != nil {
			return err
		}
		return s.respond(id, acp.ElicitationResponse{Action: acp.ElicitationAccept})
	}
	var values map[string]any
	if err := decode(ev.Value, &values); err != nil {
		return err
	}
	return s.respond(id, acp.ElicitationResponse{Action: acp.ElicitationAccept, Content: formContent(elicitationNode(s.snapshot().ID, id), el, values)})
}

func docked(sc screen) bool {
	_, ok := dockedPermission(sc)
	return ok
}

func elicitationOutcome(en *entry) string {
	el, resp := en.Elicitation, en.Response
	switch {
	case resp == nil || resp.Action == acp.ElicitationCancel:
		return "Cancelled"
	case resp.Action == acp.ElicitationDecline:
		if el.Mode == acp.ElicitationURL {
			return "Declined"
		}
		return "Skipped"
	case el.Mode == acp.ElicitationURL && en.Completed:
		return "Done"
	case el.Mode == acp.ElicitationURL:
		return "Opened in the browser"
	}
	var parts []string
	for _, f := range el.Fields {
		if text := answerText(f, resp.Content[f.Key]); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "Answered"
	}
	return strings.Join(parts, " · ")
}

func elicitationNode(sid, id string) string {
	return sid + "/" + entryElicitation + "/" + id
}

func fieldNode(id string, i int) string {
	return id + "/f/" + strconv.Itoa(i)
}

func fieldIDs(id string, fields []elicitField) []string {
	ids := make([]string, 0, len(fields))
	for i := range fields {
		ids = append(ids, fieldNode(id, i))
	}
	return ids
}

func singleChoice(fields []elicitField) int {
	found := -1
	for i, f := range fields {
		switch f.Type {
		case fieldChoice:
			if found >= 0 {
				return -1
			}
			found = i
		case fieldMulti, fieldBoolean:
			return -1
		}
	}
	return found
}

func answersOnPick(fields []elicitField) bool {
	return len(fields) == 1 && singleChoice(fields) == 0
}

func needsAnswer(fields []elicitField) bool {
	return len(fields) > 0 && !slices.ContainsFunc(fields, func(f elicitField) bool {
		return f.Required || !isChoice(f) && !isCustom(fields, f)
	})
}

func fieldNodes(id string, fields []elicitField) []sdui.Node {
	questions := 0
	for _, f := range fields {
		if isChoice(f) {
			questions++
		}
	}
	single, number := singleChoice(fields), 0
	nodes := make([]sdui.Node, 0, len(fields))
	for i, f := range fields {
		fid := fieldNode(id, i)
		switch {
		case isChoice(f):
			number++
			p := choiceProps(f, otherField(id, fields, f), i == single)
			if questions > 1 {
				p.Number, p.Open = number, number == 1
			}
			c := sdui.Choice(fid, p)
			if i == single {
				c = sdui.WithAction(c, actElicitPick)
			}
			nodes = append(nodes, c)
		case !isCustom(fields, f):
			nodes = append(nodes, fieldRow(fid, f))
		}
	}
	return nodes
}

func isChoice(f elicitField) bool {
	return f.Type == fieldChoice || f.Type == fieldMulti
}

func isCustom(fields []elicitField, f elicitField) bool {
	base, ok := strings.CutSuffix(f.Key, customSuffix)
	return ok && f.Type == fieldText && slices.ContainsFunc(fields, func(q elicitField) bool { return q.Key == base && isChoice(q) })
}

func otherField(id string, fields []elicitField, f elicitField) *sdui.ChoiceOther {
	i := slices.IndexFunc(fields, func(c elicitField) bool { return c.Key == f.Key+customSuffix && c.Type == fieldText })
	if i < 0 {
		return nil
	}
	return &sdui.ChoiceOther{ID: fieldNode(id, i), Label: otherLabel, Placeholder: otherPlaceholder}
}

func choiceProps(f elicitField, other *sdui.ChoiceOther, keys bool) sdui.ChoiceProps {
	p := sdui.ChoiceProps{Style: "radio", Title: f.Title, Text: f.Description, Other: other}
	if p.Text == "" {
		p.Title, p.Text = "", f.label()
	}
	if f.Type == fieldMulti {
		p.Style = "check"
	}
	p.Options = make([]sdui.ChoiceOption, 0, len(f.Options))
	for i, o := range f.Options {
		p.Options = append(p.Options, sdui.ChoiceOption{Value: o.Value, Label: o.Label, Description: o.Description, Shortcut: optionKey(keys, i)})
	}
	if other != nil {
		other.Shortcut = optionKey(keys, len(f.Options))
	}
	return p
}

func optionKey(keys bool, i int) string {
	if !keys || i >= maxOptionShortcut {
		return ""
	}
	return "opt+" + strconv.Itoa(i+1)
}

func fieldRow(fid string, f elicitField) sdui.Node {
	var value sdui.Node
	if f.Type == fieldBoolean {
		value = sdui.Toggle(fid, false)
	} else {
		value = sdui.TextField(fid, "", f.Description)
	}
	if f.Type != fieldText && f.Description != "" {
		value = sdui.VStack(sdui.Text(f.Description), value)
	}
	return sdui.HStack(sdui.MutedText(f.label()), value)
}

func formContent(id string, el elicitation, values map[string]any) map[string]any {
	content := make(map[string]any, len(el.Fields))
	for i, f := range el.Fields {
		if v, ok := values[fieldNode(id, i)]; ok {
			content[f.Key] = v
		}
	}
	return content
}

func answerText(f elicitField, v any) string {
	switch x := v.(type) {
	case string:
		return optionLabel(f, x)
	case []string:
		labels := make([]string, 0, len(x))
		for _, s := range x {
			labels = append(labels, optionLabel(f, s))
		}
		return strings.Join(labels, ", ")
	case []any:
		labels := make([]string, 0, len(x))
		for _, s := range x {
			if str, ok := s.(string); ok {
				labels = append(labels, optionLabel(f, str))
			}
		}
		return strings.Join(labels, ", ")
	case bool:
		if x {
			return f.label()
		}
		return ""
	case nil:
		return ""
	}
	raw, _ := json.Marshal(v)
	return f.label() + ": " + string(raw)
}

func optionLabel(f elicitField, value string) string {
	for _, o := range f.Options {
		if o.Value == value {
			return cmp.Or(o.Label, value)
		}
	}
	return value
}
