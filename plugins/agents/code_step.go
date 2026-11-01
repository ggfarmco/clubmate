package main

import (
	"cmp"
	"slices"

	"clubmate/pkg/sdui"
)

const (
	actOpenGraphs = "openGraphs"
	graphsSection = "graphs"
	graphsMain    = "graphs.main"
	stepNote      = "It starts on its own when the graph reaches it. You can write here once it has run."
	openGraphs    = "Open in Graphs"
	newChat       = "New chat"
)

var stepStates = map[string]struct{ label, tone string }{
	statusWaiting: {"waiting for you", "warning"},
	statusRunning: {"running", ""},
	statusDone:    {"done", ""},
	statusIdle:    {"not started", ""},
	statusError:   {"failed", "danger"},
	statusClosed:  {"closed", ""},
}

func defaultCodeSession(here []record) string {
	var best *record
	for i, rec := range here {
		if rec.Run == nil && !rec.closed() && (best == nil || rec.UpdatedAt.After(best.UpdatedAt)) {
			best = &here[i]
		}
	}
	if best == nil {
		return ""
	}
	return best.ID
}

func unstartedStep(sc screen) bool {
	return sc.View.Session.Run != nil && startable(sc)
}

func stepView(sc screen, recs []record) sdui.Node {
	rec := sc.View.Session
	sid := rec.ID
	nodes := []sdui.Node{sdui.VStack(
		sdui.Icon(graphIcon),
		sdui.Node{Type: "text", Props: map[string]any{"text": nodeLabel(rec) + " is a step of " + cmp.Or(rec.Run.Title, graphRun), "style": "title"}},
		sdui.MutedText(stepNote),
	)}
	steps := slices.DeleteFunc(slices.Clone(recs), func(r record) bool { return r.Run == nil || r.Run.ID != rec.Run.ID })
	slices.SortStableFunc(steps, func(a, b record) int { return a.CreatedAt.Compare(b.CreatedAt) })
	for _, step := range steps {
		state := cmp.Or(stepStates[step.Status], stepStates[statusIdle])
		value := sdui.MutedText(state.label)
		if state.tone != "" {
			value = sdui.Badge(state.label, state.tone)
		}
		nodes = append(nodes, sdui.HStack(sdui.MutedText(nodeLabel(step)), sdui.Icon(cmp.Or(statusIcons[step.Status], statusIcons[statusIdle])), value))
	}
	open := sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+actOpenGraphs, openGraphs, ""), actOpenGraphs), "secondary")
	chat := sdui.WithVariant(sdui.WithAction(sdui.Button(codeNewNode+"/step", newChat, ""), actCodeNew), "ghost")
	nodes = append(nodes, sdui.HStack(open, chat))
	return sdui.Transcript(sid+"/transcript", nodes...)
}
