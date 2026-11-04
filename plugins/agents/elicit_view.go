package main

import (
	"cmp"
	"encoding/json"
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
	noChoice          = "—"
	cardTone          = "warning"
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
	children := []sdui.Node{sdui.Text(el.Message)}
	single := singleChoice(el.Fields)
	for i, f := range el.Fields {
		fid := id + "/f/" + strconv.Itoa(i)
		if i == single {
			children = append(children, optionButtons(id, i, f)...)
			continue
		}
		children = append(children, fieldRow(fid, f))
	}
	submit := sdui.WithVariant(sdui.Button(id+"/submit", "Send", ""), "primary")
	skip := sdui.WithVariant(sdui.WithAction(sdui.Button(id+"/decline", "Skip", ""), actElicitDecline), "ghost")
	children = append(children, sdui.HStack(submit, skip))
	return sdui.WithAction(sdui.Form(id, sdui.Card(cardTone, "questionmark.bubble.fill", children...)), actElicitation)
}

func (a *app) elicitAct(ev pluginsdk.UIEvent, s *session, node string) error {
	rest, ok := strings.CutPrefix(node, entryElicitation+"/")
	if !ok {
		return errNotAnElicitation
	}
	id, sub, _ := strings.Cut(rest, "/")
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
	content := formContent(elicitationNode(s.snapshot().ID, id), el, values)
	if ev.Action == actElicitPick {
		f, o, ok := pickedOption(el, sub)
		if !ok {
			return errUnknownOption
		}
		content[f.Key] = o.Value
	}
	return s.respond(id, acp.ElicitationResponse{Action: acp.ElicitationAccept, Content: content})
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

func optionButtons(id string, field int, f elicitField) []sdui.Node {
	var out []sdui.Node
	if f.Title != "" {
		out = append(out, sdui.MutedText(f.Title))
	}
	for i, o := range f.Options {
		b := sdui.WithForm(sdui.WithVariant(sdui.WithAction(sdui.Button(id+"/pick/"+strconv.Itoa(field)+"/"+strconv.Itoa(i), o.Label, ""), actElicitPick), "secondary"))
		if i < maxOptionShortcut {
			b = sdui.WithShortcut(b, "opt+"+strconv.Itoa(i+1))
		}
		row := []sdui.Node{b}
		if o.Description != "" {
			row = append(row, sdui.MutedText(o.Description))
		}
		out = append(out, sdui.HStack(row...))
	}
	return out
}

func fieldRow(fid string, f elicitField) sdui.Node {
	var value sdui.Node
	switch f.Type {
	case fieldChoice:
		options := make([]sdui.Option, 0, len(f.Options)+1)
		options = append(options, sdui.Option{Value: "", Label: noChoice})
		for _, o := range f.Options {
			options = append(options, sdui.Option{Value: o.Value, Label: o.Label})
		}
		value = sdui.Select(fid, options, "")
	case fieldMulti:
		rows := make([]sdui.Node, 0, len(f.Options))
		for i, o := range f.Options {
			rows = append(rows, sdui.HStack(sdui.Toggle(fid+"/"+strconv.Itoa(i), false), sdui.Text(o.Label)))
		}
		value = sdui.VStack(rows...)
	case fieldBoolean:
		value = sdui.Toggle(fid, false)
	default:
		value = sdui.TextField(fid, "", f.Description)
	}
	if f.Type != fieldText && f.Description != "" {
		value = sdui.VStack(sdui.Text(f.Description), value)
	}
	return sdui.HStack(sdui.MutedText(f.label()), value)
}

func pickedOption(el elicitation, sub string) (elicitField, elicitOption, bool) {
	parts := strings.Split(sub, "/")
	if len(parts) != 3 || parts[0] != "pick" {
		return elicitField{}, elicitOption{}, false
	}
	fi, ferr := strconv.Atoi(parts[1])
	oi, oerr := strconv.Atoi(parts[2])
	if ferr != nil || oerr != nil || fi < 0 || fi >= len(el.Fields) || oi < 0 || oi >= len(el.Fields[fi].Options) {
		return elicitField{}, elicitOption{}, false
	}
	return el.Fields[fi], el.Fields[fi].Options[oi], true
}

func formContent(id string, el elicitation, values map[string]any) map[string]any {
	content := make(map[string]any, len(el.Fields))
	for i, f := range el.Fields {
		fid := id + "/f/" + strconv.Itoa(i)
		if f.Type != fieldMulti {
			if v, ok := values[fid]; ok {
				content[f.Key] = v
			}
			continue
		}
		var picks []any
		for j, o := range f.Options {
			if on, _ := values[fid+"/"+strconv.Itoa(j)].(bool); on {
				picks = append(picks, o.Value)
			}
		}
		if len(picks) > 0 {
			content[f.Key] = picks
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
