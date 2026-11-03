package main

import (
	"cmp"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/automation"
)

const (
	flowSuffix      = "/flow"
	captionSuffix   = "/caption"
	lanesSuffix     = "/lanes"
	actOpen         = "open"
	actOpenEdge     = "openEdge"
	actOpenMove     = "openMove"
	agentIcon       = "sparkle"
	emitIcon        = "dot.radiowaves.left.and.right"
	causeActor      = "cause"
	effectActor     = "effect"
	eventOut        = "event"
	effectCardID    = "effect"
	agentNewCommand = "agents.new"
	cronMarker      = "cron"
	clockLayout     = "15:04"
	moveLayout      = "Jan 2 15:04:05"
	maxCards        = 8
	maxEdges        = 32
	maxActors       = 8
	emptyText       = "A trigger waits for an event and then acts. Agents add them to chain sessions, and every edge of a graph run is one."
)

type edge struct {
	trigger *Trigger
	from    string
	out     string
	to      string
}

func graphEdge(t *Trigger) (edge, bool) {
	var match, args map[string]any
	_ = json.Unmarshal(t.Match, &match)
	_ = json.Unmarshal(t.Args, &args)
	str := func(m map[string]any, key string) string {
		s, _ := m[key].(string)
		return s
	}
	e := edge{trigger: t, to: str(args, "session")}
	switch {
	case t.On == api.TopicTurn:
		e.from, e.out = str(match, "session"), "done"
		switch {
		case str(match, "status") == automation.StatusError:
			e.out = "error"
		case match["asks"] == true:
			e.out = "asks"
		}
	case strings.HasPrefix(t.On, graphOutcome):
		e.from, e.out = str(match, "by"), str(match, "outcome")
	}
	return e, e.from != "" && e.to != "" && e.out != ""
}

func graphFlow(t *Trigger, triggers []*Trigger, titles map[string]string, shape *graphShape) (sdui.Node, bool) {
	var edges []edge
	known := map[string]bool{}
	for _, s := range triggers {
		if s.Run != t.Run {
			continue
		}
		known[s.ID] = true
		if e, ok := graphEdge(s); ok {
			edges = append(edges, e)
		}
	}
	var cards []sdui.FlowCard
	index := map[string]int{}
	card := func(id, label, icon string) int {
		if i, ok := index[id]; ok {
			return i
		}
		index[id] = len(cards)
		cards = append(cards, sdui.FlowCard{ID: id, Label: label, State: runName(t), Icon: icon})
		return len(cards) - 1
	}
	session := func(id string) int { return card(id, role(t, id, titles), agentIcon) }
	out := func(from int, id string) int {
		i := slices.IndexFunc(cards[from].Outs, func(o sdui.FlowOut) bool { return o.ID == id })
		if i < 0 {
			cards[from].Outs = append(cards[from].Outs, sdui.FlowOut{ID: id, Label: id, State: sdui.EdgeNever})
			i = len(cards[from].Outs) - 1
		}
		return i
	}
	var flowEdges []sdui.FlowEdge
	for _, e := range edges {
		from := session(e.from)
		session(e.to)
		ok := okRuns(e.trigger.History)
		fired := ""
		if ok > 0 {
			fired = " · fired ×" + strconv.Itoa(ok)
		}
		fe := sdui.FlowEdge{From: e.from, Out: e.out, To: e.to, State: sdui.EdgeDim, Ref: e.trigger.ID, Tooltip: strings.TrimPrefix(e.trigger.Name, t.RunTitle+": ") + fired}
		if e.trigger.ID == t.ID {
			fe.State, fe.Ref, fe.Tooltip, fe.Label = sdui.EdgeSelected, "", "this trigger"+fired, "this trigger"+fired
		}
		flowEdges = append(flowEdges, fe)
		i := out(from, e.out)
		cards[from].Outs[i].Count += ok
		if o := &cards[from].Outs[i]; fe.State == sdui.EdgeSelected || o.State != sdui.EdgeSelected {
			o.State = fe.State
		}
	}
	if !slices.ContainsFunc(flowEdges, func(e sdui.FlowEdge) bool { return e.State == sdui.EdgeSelected }) {
		return sdui.Node{}, false
	}
	var markers []sdui.FlowMarker
	if shape != nil {
		nodes := map[string]string{}
		for _, n := range shape.Nodes {
			nodes[n.ID] = n.Session
			if n.Step || n.Session == "" {
				nodes[n.ID] = "step:" + n.ID
				card(nodes[n.ID], automation.Capitalize(n.Role), "terminal")
			}
		}
		for _, e := range shape.Edges {
			from, to := nodes[e.From], nodes[e.To]
			if from == "" || to == "" || known[e.Trigger] || slices.ContainsFunc(flowEdges, func(f sdui.FlowEdge) bool { return f.From == from && f.Out == e.out() && f.To == to }) {
				continue
			}
			i := session(from)
			session(to)
			out(i, e.out())
			flowEdges = append(flowEdges, sdui.FlowEdge{From: from, Out: e.out(), To: to, State: sdui.EdgeNever, Tooltip: e.out() + " → " + role(t, to, titles) + " · the graph passes it on itself"})
		}
		for _, n := range shape.Nodes {
			if _, ok := index[nodes[n.ID]]; !n.Start || !ok {
				continue
			}
			id := "start:" + n.ID
			markers = append(markers, sdui.FlowMarker{ID: id, Kind: sdui.MarkerStart})
			flowEdges = append(flowEdges, sdui.FlowEdge{From: id, To: nodes[n.ID], State: sdui.EdgeDim})
		}
	}
	if len(cards) > maxCards || len(flowEdges) > maxEdges || len(markers) > maxCards {
		return sdui.Node{}, false
	}
	return sdui.Flow(t.ID+flowSuffix, sdui.FlowProps{Cards: cards, Markers: markers, Edges: flowEdges}), true
}

func role(t *Trigger, session string, titles map[string]string) string {
	title := titles[session]
	if title == "" {
		return "a closed session"
	}
	return strings.TrimPrefix(title, t.RunTitle+" · ")
}

func okRuns(history []Run) int {
	n := 0
	for _, r := range history {
		if r.Status == automation.StatusOK {
			n++
		}
	}
	return n
}

func lastChain(history []Run) (Run, bool) {
	for _, r := range automation.NewestFirst(history) {
		if c := r.Chain; c != nil && (c.Session != "" || c.CronJob != "" || c.Trigger != "") {
			return r, true
		}
	}
	return Run{}, false
}

func chainFlow(t *Trigger, titles map[string]string, loc *time.Location) (string, sdui.Node) {
	last, _ := automation.LastRun(t.History)
	effect := effectCard(t, &last, titles, loc)
	r, ok := lastChain(t.History)
	if !ok {
		cause := sdui.FlowCard{ID: "when", Label: causeLabel(t, titles), Icon: causeIcon(t), Outs: []sdui.FlowOut{{ID: eventOut, Label: outWords(t.On, ""), State: sdui.EdgeSelected}}}
		if s := matchSession(t); s != "" {
			cause.ID = s
		}
		if effect.ID == cause.ID {
			effect.ID = effectCardID
		}
		edges := []sdui.FlowEdge{{From: cause.ID, Out: eventOut, To: effect.ID, State: sdui.EdgeSelected, Tooltip: "this trigger", Label: "this trigger"}}
		return "what it waits for and what it does", sdui.Flow(t.ID+flowSuffix, sdui.FlowProps{Cards: []sdui.FlowCard{cause, effect}, Edges: edges})
	}
	c := r.Chain
	if effect.ID == c.Session || effect.ID == c.Trigger {
		effect.ID = effectCardID
	}
	var origin string
	var cards []sdui.FlowCard
	var markers []sdui.FlowMarker
	var edges []sdui.FlowEdge
	source := ""
	switch {
	case c.CronJob != "":
		name := cmp.Or(c.CronName, "a deleted cron job")
		origin = "cron " + name
		markers = append(markers, sdui.FlowMarker{ID: cronMarker, Kind: sdui.MarkerSchedule, Label: name})
		source = cronMarker
	case c.Trigger != "":
		name := cmp.Or(c.TriggerName, "a deleted trigger")
		origin = "trigger " + name
		cards = append(cards, sdui.FlowCard{ID: c.Trigger, Label: name, State: "trigger", Icon: "bolt", Outs: []sdui.FlowOut{{ID: "started", Label: "starts a session", State: sdui.EdgeTaken}}})
		source = c.Trigger
	}
	emitted := strings.HasPrefix(r.Topic, emitPrefix)
	verb := map[bool]string{true: "emitted it", false: "ended the turn"}[emitted]
	caption := "last time · " + origin + " " + verb
	to := effect.ID
	if c.Session != "" {
		session := cmp.Or(titles[c.Session], c.Title, "a closed session")
		state, icon, tone := "session · "+r.At.In(loc).Format(clockLayout), agentIcon, ""
		switch c.Status {
		case "done":
			state, icon, tone = "session · done "+r.At.In(loc).Format(clockLayout), "checkmark.circle", "success"
		case automation.StatusError:
			state, icon, tone = "session · error "+r.At.In(loc).Format(clockLayout), "xmark.circle", "danger"
		}
		count := 0
		for _, h := range t.History {
			if h.Topic == r.Topic {
				count++
			}
		}
		if count < 2 {
			count = 0
		}
		cards = append(cards, sdui.FlowCard{ID: c.Session, Label: session, State: state, Icon: icon, Tone: tone,
			Outs: []sdui.FlowOut{{ID: eventOut, Label: outWords(r.Topic, c.Status), Count: count, State: sdui.EdgeSelected}}})
		if source != "" {
			edge := sdui.FlowEdge{From: source, To: c.Session, State: sdui.EdgeTaken}
			if source == c.Trigger {
				edge.Out = "started"
			}
			edges = append(edges, edge)
		}
		caption = "last time · " + origin + " started the session that " + verb
		if origin == "" {
			caption = "last time · session " + session + " " + map[bool]string{true: "emitted it", false: "ended its turn"}[emitted]
		}
		source = c.Session
	}
	edge := sdui.FlowEdge{From: source, To: to, State: sdui.EdgeSelected, Tooltip: "this trigger", Label: "this trigger"}
	if source == c.Session {
		edge.Out = eventOut
	} else if source == c.Trigger {
		edge.Out = "started"
	}
	edges = append(edges, edge)
	cards = append(cards, effect)
	return caption, sdui.Flow(t.ID+flowSuffix, sdui.FlowProps{Cards: cards, Markers: markers, Edges: edges})
}

func effectCard(t *Trigger, last *Run, titles map[string]string, loc *time.Location) sdui.FlowCard {
	var args map[string]any
	_ = json.Unmarshal(t.Args, &args)
	card := sdui.FlowCard{ID: effectCardID, Label: effectLabel(t, titles), Icon: agentIcon}
	switch t.Command {
	case automation.PromptCommand:
		if s, _ := args["session"].(string); s != "" {
			card.ID = s
		}
		if last != nil && last.Target != "" {
			card.ID, card.Label = last.Target, cmp.Or(titles[last.Target], "a closed session")
		}
	case agentNewCommand:
		var result struct {
			Session string `json:"session"`
		}
		if last != nil && json.Unmarshal([]byte(last.Result), &result) == nil && result.Session != "" {
			card.ID = result.Session
			card.Label = cmp.Or(titles[result.Session], card.Label)
		}
	default:
		card.Icon = "gearshape"
	}
	if last == nil || last.At.IsZero() {
		return card
	}
	words := automation.Words(t.Command, t.Args, *last, titles)
	card.Tooltip = words
	when := last.At.In(loc).Format(clockLayout)
	switch last.Status {
	case automation.StatusOK:
		card.State, card.Icon, card.Tone = "ok · "+when, "checkmark.circle", "success"
	case automation.StatusError:
		card.State, card.Icon, card.Tone = "failed · "+words, "xmark.circle", "danger"
	default:
		card.State = "skipped · " + when
	}
	return card
}

func effectLabel(t *Trigger, titles map[string]string) string {
	var args map[string]any
	_ = json.Unmarshal(t.Args, &args)
	switch t.Command {
	case automation.PromptCommand:
		s, _ := args["session"].(string)
		return cmp.Or(titles[s], "a closed session")
	case agentNewCommand:
		return "new session"
	}
	return cmp.Or(automation.SentenceCase(titles[t.Command]), t.Command)
}

func matchSession(t *Trigger) string {
	if t.On != api.TopicTurn {
		return ""
	}
	var match map[string]any
	_ = json.Unmarshal(t.Match, &match)
	s, _ := match["session"].(string)
	return s
}

func causeLabel(t *Trigger, titles map[string]string) string {
	switch {
	case matchSession(t) != "":
		return cmp.Or(titles[matchSession(t)], "a closed session")
	case t.On == api.TopicTurn:
		return "agent turns"
	case strings.HasPrefix(t.On, emitPrefix):
		return "“" + strings.TrimPrefix(t.On, emitPrefix) + "”"
	}
	return t.On
}

func causeIcon(t *Trigger) string {
	if strings.HasPrefix(t.On, emitPrefix) {
		return emitIcon
	}
	if strings.HasPrefix(t.On, agentsPrefix) {
		return agentIcon
	}
	return "bolt"
}

func outWords(topic, status string) string {
	switch {
	case strings.HasPrefix(topic, emitPrefix):
		return "emits " + strings.TrimPrefix(topic, emitPrefix)
	case topic == api.TopicTurn && status != "":
		return "turn ends · " + status
	case topic == api.TopicTurn:
		return "turn ends"
	}
	return topic
}

func hopVerb(command string) string {
	switch command {
	case automation.PromptCommand:
		return "prompt"
	case agentNewCommand:
		return "start"
	}
	return "run"
}

func runLanes(t *Trigger, cause, effect sdui.LaneActor, out string, titles map[string]string, loc *time.Location) sdui.Node {
	cause.ID, effect.ID = causeActor, effectActor
	actors := []sdui.LaneActor{cause, effect}
	var moves []sdui.LaneMove
	runs := automation.NewestFirst(t.History)
	slices.Reverse(runs)
	last := -1
	for _, r := range runs {
		at := r.At.In(loc).Format(moveLayout)
		ref := automation.RunID(r)
		from := len(moves)
		move := sdui.LaneMove{Actor: causeActor, Cause: -1, Tooltip: at + " · " + eventWords(r, titles), Ref: ref}
		if r.Status == automation.StatusSkipped {
			move.Tone = "neutral"
		}
		moves = append(moves, move)
		if r.Status == automation.StatusSkipped {
			continue
		}
		hop := cmp.Or(out, hopOut(r))
		effectMove := sdui.LaneMove{Actor: effectActor, Cause: from, Label: hop + " → " + hopVerb(t.Command), Tooltip: at + " · " + automation.Words(t.Command, t.Args, r, titles), Tone: "success", Ref: ref}
		if r.Status == automation.StatusError {
			effectMove.Tone = "danger"
		}
		last = len(moves)
		moves = append(moves, effectMove)
	}
	if last >= 0 {
		moves[last].Last = true
	}
	return sdui.Lanes(t.ID+lanesSuffix, sdui.LanesProps{Actors: actors, Moves: moves, Title: "Runs", Count: len(t.History)})
}

func hopOut(r Run) string {
	switch {
	case strings.HasPrefix(r.Topic, emitPrefix):
		return strings.TrimPrefix(r.Topic, emitPrefix)
	case r.Topic == manualTopic:
		return "run now"
	case r.Chain != nil && r.Chain.Status != "":
		return r.Chain.Status
	}
	return "event"
}

func drawnRuns(t *Trigger, triggers []*Trigger, titles map[string]string, shape *graphShape, loc *time.Location) (caption string, flow sdui.Node, lanes sdui.Node, ok bool) {
	if t.Run != "" {
		e, found := graphEdge(t)
		flow, drawn := graphFlow(t, triggers, titles, shape)
		if !found || !drawn {
			return "", sdui.Node{}, sdui.Node{}, false
		}
		caption = "In graph " + runName(t) + " — this trigger is one of its edges"
		lanes = runLanes(t, sdui.LaneActor{Label: role(t, e.from, titles), Icon: agentIcon}, sdui.LaneActor{Label: role(t, e.to, titles), Icon: agentIcon}, e.out, titles, loc)
		return caption, flow, lanes, true
	}
	caption, flow = chainFlow(t, titles, loc)
	cause := sdui.LaneActor{Label: causeLabel(t, titles), Icon: causeIcon(t)}
	if r, ok := lastChain(t.History); ok && matchSession(t) == "" {
		switch c := r.Chain; {
		case c.CronName != "":
			cause = sdui.LaneActor{Label: "cron " + c.CronName, Icon: "clock"}
		case c.TriggerName != "":
			cause = sdui.LaneActor{Label: "trigger " + c.TriggerName, Icon: "bolt"}
		}
	}
	lanes = runLanes(t, cause, sdui.LaneActor{Label: effectLabel(t, titles), Icon: agentIcon}, "", titles, loc)
	return caption, flow, lanes, true
}

func emptyView() sdui.Node {
	ghost := func(labels ...string) []sdui.FlowOut {
		outs := make([]sdui.FlowOut, len(labels))
		for i, l := range labels {
			outs[i] = sdui.FlowOut{ID: strconv.Itoa(i), Label: l, State: sdui.EdgeGhost}
		}
		return outs
	}
	flow := sdui.Flow("empty"+flowSuffix, sdui.FlowProps{
		Cards: []sdui.FlowCard{
			{ID: "when", Label: "When", Ghost: true, Outs: ghost("an agent turn finishes", "a cron job's session", "an agent emits an event")},
			{ID: "does", Label: "Does", Ghost: true, Outs: ghost("start an agent", "prompt a session", "run a plugin command")},
		},
		Edges: []sdui.FlowEdge{{From: "when", Out: "0", To: "does", State: sdui.EdgeGhost}},
	})
	return sdui.EmptyState(emptyText, sdui.EmptyProps{Title: "No triggers", Icon: "bolt", Action: &flow})
}

func chainTitles(t *Trigger, titles map[string]string) map[string]string {
	out := maps.Clone(titles)
	for _, r := range t.History {
		if c := r.Chain; c != nil && c.CronJob != "" && c.CronName != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[c.CronJob] = c.CronName
		}
	}
	return out
}
