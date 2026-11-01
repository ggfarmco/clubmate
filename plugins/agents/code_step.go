package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

const (
	actOpenGraphs = "openGraphs"
	graphsSection = "graphs"
	graphsMain    = "graphs.main"
	stepNote      = "It starts on its own when the graph reaches it. You can write here once it has run."
	stepNoteAfter = "It starts on its own when %s, and gets %s result. You can write here once it has run."

	onDone     = "done"
	onError    = "error"
	onAsks     = "asks"
	onOutcome  = "outcome"
	openGraphs = "Open in Graphs"
	newChat    = "New chat"
)

var (
	afterVerbs = map[string]string{
		onDone:  "finishes its turn",
		onError: "stops with an error",
		onAsks:  "asks a question",
	}
	stepStates = map[string]struct{ label, tone string }{
		statusWaiting: {"waiting for you", "warning"},
		statusRunning: {"running", ""},
		statusDone:    {"done", ""},
		statusIdle:    {"not started", ""},
		statusError:   {"failed", "danger"},
		statusClosed:  {"closed", ""},
	}
)

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
		sdui.Node{Type: "text", Props: map[string]any{"text": nodeLabel(rec) + " is a step of " + stepGraph(rec), "style": "title"}},
		sdui.MutedText(stepNoteFor(rec.Run.After)),
	)}
	steps := slices.DeleteFunc(slices.Clone(recs), func(r record) bool { return r.Run == nil || r.Run.ID != rec.Run.ID })
	slices.SortStableFunc(steps, func(a, b record) int { return a.CreatedAt.Compare(b.CreatedAt) })
	for _, step := range steps {
		state := cmp.Or(stepStates[step.Status], stepStates[statusIdle])
		label := state.label
		if roles, _ := afterByRole(step.Run.After); step.Status == statusIdle && len(roles) > 0 {
			label += " · runs after " + joinOr(roles)
		}
		glyph, value := sdui.Icon(cmp.Or(statusIcons[step.Status], statusIcons[statusIdle])), sdui.MutedText(label)
		if tone := statusTones[step.Status]; tone != "" {
			glyph = sdui.WithTone(glyph, tone)
		}
		if state.tone != "" {
			value = sdui.WithTone(sdui.Text(label), state.tone)
		}
		nodes = append(nodes, sdui.HStack(sdui.MutedText(nodeLabel(step)), glyph, value))
	}
	open := sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+actOpenGraphs, openGraphs, ""), actOpenGraphs), "secondary")
	chat := sdui.WithVariant(sdui.WithAction(sdui.Button(codeNewNode+"/step", newChat, ""), actCodeNew), "ghost")
	nodes = append(nodes, sdui.HStack(open, chat))
	return sdui.CenteredTranscript(sid+"/transcript", nodes...)
}

func stepComposer(sc screen) sdui.Node {
	rec := sc.View.Session
	return sdui.Composer(sc.Composer, sdui.ComposerProps{
		Placeholder: nodeLabel(rec) + " runs as part of " + stepGraph(rec),
		Agent:       agentLabel(rec.Agent),
		Modes:       modeOptions(sc.View),
		Mode:        rec.Mode,
		Disabled:    true,
	})
}

func stepGraph(rec record) string {
	return cmp.Or(rec.Run.Title, graphRun)
}

func stepNoteFor(after []api.RunAfter) string {
	roles, verbs := afterByRole(after)
	if len(roles) == 0 {
		return stepNote
	}
	whens := make([]string, 0, len(roles))
	for _, role := range roles {
		whens = append(whens, role+" "+joinOr(verbs[role]))
	}
	whose := "their"
	if len(roles) == 1 {
		whose = roles[0] + "'s"
	}
	return fmt.Sprintf(stepNoteAfter, joinOr(whens), whose)
}

func afterByRole(after []api.RunAfter) ([]string, map[string][]string) {
	var roles []string
	verbs := map[string][]string{}
	for _, a := range after {
		verb := cmp.Or(afterVerbs[a.On], afterVerbs[onDone])
		if a.On == onOutcome {
			verb = "says " + a.Outcome
		}
		if _, ok := verbs[a.Role]; !ok {
			roles = append(roles, a.Role)
		}
		verbs[a.Role] = append(verbs[a.Role], verb)
	}
	return roles, verbs
}

func joinOr(parts []string) string {
	return strings.Join(parts, " or ")
}
