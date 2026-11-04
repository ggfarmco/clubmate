package main

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
)

const (
	doesFlowID     = "does"
	stripNode      = "strip"
	whenMarker     = "when"
	actionCard     = "action"
	ownerMarker    = "owner"
	inboxMarker    = "inbox"
	runsMarker     = "runs"
	actOpen        = "open"
	promptedLayout = "Jan 2"
	outOK          = "ok"
	outFailed      = "failed"
	tipLayout      = "Jan 2 15:04"
	promptAction   = "agents.prompt"
	emitAction     = "triggers.emit"
)

func doesFlow(j *Job, titles, statuses map[string]string) sdui.Node {
	label, state, icon := actionLabel(j, titles)
	counts := map[string]int{}
	var lastOK time.Time
	for _, r := range j.History {
		switch r.Status {
		case automation.StatusOK:
			counts[outOK]++
			lastOK = r.At
		case automation.StatusError:
			counts[outFailed]++
		}
	}
	newest := ""
	for _, r := range automation.NewestFirst(j.History) {
		if newest = map[string]string{automation.StatusOK: outOK, automation.StatusError: outFailed}[r.Status]; newest != "" {
			break
		}
	}
	edgeState := func(out string) string {
		switch {
		case counts[out] == 0:
			return sdui.EdgeNever
		case !j.Enabled:
			return sdui.EdgeDim
		case out == newest:
			return sdui.EdgeLast
		}
		return sdui.EdgeTaken
	}
	outs := []sdui.FlowOut{
		{ID: outOK, Label: outOK, Count: counts[outOK], State: edgeState(outOK)},
		{ID: outFailed, Label: outFailed, Count: counts[outFailed], State: edgeState(outFailed)},
	}
	start := sdui.EdgeTaken
	switch {
	case !j.Enabled:
		start = sdui.EdgeDim
	case len(j.History) == 0:
		start = sdui.EdgeNever
	}
	markers := []sdui.FlowMarker{{ID: whenMarker, Kind: sdui.MarkerSchedule, Label: j.Schedule.Describe()}}
	edges := []sdui.FlowEdge{{From: whenMarker, To: actionCard, State: start}}
	cards := []sdui.FlowCard{{ID: actionCard, Label: label, State: state, Icon: icon, Tooltip: automation.Names(doesTooltip(j, titles), titles), Outs: outs}}
	route := func(out sdui.FlowOut, to, tooltip string) {
		edge := sdui.FlowEdge{From: actionCard, Out: out.ID, To: to, State: out.State, Tooltip: tooltip}
		if out.ID == outOK && !j.Enabled {
			edge.Label = "paused"
		}
		edges = append(edges, edge)
	}
	owner := func() string {
		if !slices.ContainsFunc(markers, func(m sdui.FlowMarker) bool { return m.ID == ownerMarker }) {
			markers = append(markers, sdui.FlowMarker{ID: ownerMarker, Kind: sdui.MarkerSession, Label: automation.Names(j.Owner, titles)})
		}
		return ownerMarker
	}
	failed := func() {
		if j.Owner != "" {
			route(outs[1], owner(), "failed runs go to the owner session")
			return
		}
		markers = append(markers, sdui.FlowMarker{ID: inboxMarker, Kind: sdui.MarkerInbox, Label: "Inbox"})
		route(outs[1], inboxMarker, "failed and missed runs post to the inbox; ok runs stay in Runs")
	}
	if target := promptTarget(j); target != "" {
		cards = append(cards, sessionCard(target, titles, statuses, lastOK, jobLocation(j)))
		route(outs[0], target, "ok: the prompt reached the session")
		if j.Owner == target {
			route(outs[1], target, "failed runs go to the owner session")
		} else {
			failed()
		}
		return sdui.Flow(doesFlowID, sdui.FlowProps{Cards: cards, Markers: markers, Edges: edges})
	}
	if j.Owner != "" {
		route(outs[0], owner(), "ok runs go to the owner session")
	} else {
		markers = append(markers, sdui.FlowMarker{ID: runsMarker, Kind: sdui.MarkerRuns})
		route(outs[0], runsMarker, "ok runs stay in Runs below")
	}
	failed()
	return sdui.Flow(doesFlowID, sdui.FlowProps{Cards: cards, Markers: markers, Edges: edges})
}

func promptTarget(j *Job) string {
	if j.Command != promptAction {
		return ""
	}
	var args struct {
		Session string `json:"session"`
	}
	_ = json.Unmarshal(j.Args, &args)
	if !strings.HasPrefix(args.Session, "agt_") {
		return ""
	}
	return args.Session
}

func sessionCard(id string, titles, statuses map[string]string, lastOK time.Time, loc *time.Location) sdui.FlowCard {
	card := sdui.FlowCard{ID: id, Label: titles[id], State: statuses[id], Icon: "sparkle", Tooltip: "Show the session"}
	if card.Label == "" {
		card.Label, card.State, card.Tooltip = "a closed session", "closed", ""
	}
	if !lastOK.IsZero() {
		card.State = strings.TrimPrefix(card.State+" · prompted "+lastOK.In(loc).Format(promptedLayout), " · ")
	}
	return card
}

func actionLabel(j *Job, titles map[string]string) (label, state, icon string) {
	var args map[string]any
	_ = json.Unmarshal(j.Args, &args)
	str := func(key string) string {
		s, _ := args[key].(string)
		return strings.TrimSpace(s)
	}
	switch j.Command {
	case agentNewCommand:
		state = cmp.Or(automation.Capitalize(str("agent")), "Claude") + " · new session"
		if branch := str("branch"); branch != "" {
			state = cmp.Or(automation.Capitalize(str("agent")), "Claude") + " · worktree " + branch
		}
		return "Start an agent", state, "sparkles"
	case promptAction:
		return "Prompt a session", automation.Names(cmp.Or(str("session"), "a session"), titles), "bubble.left"
	case emitAction:
		return "Emit an event", str("name"), "bolt"
	}
	if title := titles[j.Command]; title != "" {
		return automation.SentenceCase(title), "plugin command", "powerplug"
	}
	return "A command that is no longer installed", "", "powerplug"
}

func doesTooltip(j *Job, titles map[string]string) string {
	label, state, _ := actionLabel(j, titles)
	if state == "" {
		return label
	}
	return label + " · " + state
}

func ghostFlow() sdui.Node {
	does := sdui.FlowCard{ID: "does", Label: "Does", Ghost: true, Outs: []sdui.FlowOut{
		{ID: "agent", Label: "start an agent", State: sdui.EdgeGhost},
		{ID: "prompt", Label: "prompt a session", State: sdui.EdgeGhost},
		{ID: "command", Label: "run a plugin command", State: sdui.EdgeGhost},
	}}
	results := sdui.FlowCard{ID: "results", Label: "Results go to", Ghost: true, Outs: []sdui.FlowOut{
		{ID: "owner", Label: "its owner session", State: sdui.EdgeGhost},
		{ID: "inbox", Label: "or the inbox", State: sdui.EdgeGhost},
	}}
	return sdui.Flow(doesFlowID, sdui.FlowProps{
		Cards:   []sdui.FlowCard{does, results},
		Markers: []sdui.FlowMarker{{ID: whenMarker, Kind: sdui.MarkerSchedule, Label: "a schedule"}},
		Edges: []sdui.FlowEdge{
			{From: whenMarker, To: "does", State: sdui.EdgeGhost},
			{From: "does", Out: "agent", To: "results", State: sdui.EdgeGhost},
		},
	})
}

func runStrip(j *Job, history []Run, selected Run, titles map[string]string, now time.Time) sdui.Node {
	loc := jobLocation(j)
	runs := make([]sdui.StripRun, 0, len(history))
	for _, r := range history {
		label, _ := automation.StatusLabel(r)
		runs = append(runs, sdui.StripRun{
			At:      r.At,
			Status:  stripStatus(label),
			Tooltip: r.At.In(loc).Format(tipLayout) + " · " + label + " · " + automation.Words(j.Command, j.Args, r, titles),
			Ref:     automation.RunID(r),
		})
	}
	p := sdui.RunstripProps{Runs: runs, Now: now, Title: "Runs", Count: len(history)}
	if j.Enabled {
		p.Next = j.NextRun
	}
	for _, s := range j.Paused {
		p.Paused = append(p.Paused, sdui.PausedSpan{From: s.From, To: s.To})
	}
	if !selected.At.IsZero() {
		p.Selected = automation.RunID(selected)
	}
	return sdui.Runstrip(j.ID+"/"+stripNode, p)
}

func stripStatus(label string) string {
	switch label {
	case automation.LabelOK:
		return sdui.RunOK
	case automation.LabelFailed:
		return sdui.RunFailed
	case automation.LabelMissed:
		return sdui.RunMissed
	}
	return sdui.RunIgnored
}
