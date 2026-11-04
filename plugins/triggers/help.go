package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"clubmate/internal/bus"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

const (
	eventsID   = "events"
	askTitle   = "New trigger"
	emitAny    = emitPrefix + "*"
	helpMeta   = "when something happens in this workspace, a trigger does something · cron starts things on a clock, triggers on events · every edge of a graph is a trigger"
	helpEvents = "Can wait for · ↩ asks an agent for a trigger on the selected event"
	askBase    = "Help me add a trigger in this workspace."
	askEvent   = "Help me add a trigger in this workspace that fires when %s (on: %s)."
	askRest    = " Ask me what should happen and anything you need to know (which session, filter), then add it with triggers_add and owner none so it outlives this session, and tell me its When and Does in one line each."
)

type eventKind struct {
	group   string
	topic   string
	phrase  string
	filters string
}

var catalog = []eventKind{
	{"Agents", api.TopicTurn, "an agent turn finishes", "done · error · asks"},
	{"", api.TopicChanged, "an agent session changes", "created · status · deleted"},
	{"Notifications", "notify.posted", "a notification is posted", "needs-input · question · done · stuck"},
	{"Your events", emitAny, "an agent or a cron job emits an event", "triggers_emit"},
	{"Graphs", graphOutcome + "*", "a graph step reports its outcome", "approved · changes · …"},
	{"Git", "git.changed", "the branch or worktree changes", ""},
	{"Backlog", "backlog.changed", "a backlog task changes", "added · claimed · done"},
	{"Docs", "docs.presented", "an agent presents a doc", ""},
	{"Review", "editor-nvim.review", "a review in nvim changes", "comments sent · closed"},
}

var helpFacts = [][2]string{
	{"Can do", "prompt a session (waits while it is busy) · start an agent · post a notification · any command an agent may run; a destructive one asks you first"},
	{"Only from now", "events from while Clubmate was closed are not replayed"},
	{"Owner", "an agent's trigger is deleted with its session unless it was added with owner none"},
	{"Loops", "more runs a minute than its limit (10) or 3 errors in a row pause it and notify you"},
	{"Overlap", "a run never starts while the previous one runs; that event is skipped, not queued"},
}

func catalogPhrase(topic string) (string, bool) {
	i := slices.IndexFunc(catalog, func(k eventKind) bool { return k.topic == topic && k.topic != emitAny })
	if i < 0 {
		return "", false
	}
	return catalog[i].phrase, true
}

func catalogRow(on string) (eventKind, bool) {
	var best eventKind
	for _, k := range catalog {
		if bus.Match(k.topic, on) && len(k.topic) > len(best.topic) {
			best = k
		}
	}
	return best, best.topic != ""
}

func listening(triggers []*Trigger) map[string]int {
	counts := map[string]int{}
	for _, t := range triggers {
		seen := map[string]bool{}
		for _, part := range strings.Split(t.On, ",") {
			if k, ok := catalogRow(strings.TrimSpace(part)); ok && !seen[k.topic] {
				seen[k.topic] = true
				counts[k.topic]++
			}
		}
	}
	return counts
}

func helpView(triggers []*Trigger) sdui.Node {
	header := automation.HelpHeader("How triggers work", helpMeta, "Ask an agent to add one")
	counts := listening(triggers)
	rows := make([]sdui.TableRow, 0, len(catalog))
	for _, k := range catalog {
		yours := ""
		switch n := counts[k.topic]; n {
		case 0:
		case 1:
			yours = "1 trigger"
		default:
			yours = strconv.Itoa(n) + " triggers"
		}
		rows = append(rows, sdui.TableRow{ID: k.topic, Cells: []sdui.Cell{
			{Text: k.group}, {Text: k.phrase}, {Text: k.filters}, {Text: yours}, {Action: automation.ActAsk, Label: "Ask an agent"},
		}})
	}
	columns := []sdui.Column{
		{ID: "group", Title: "", Width: 110, Priority: 2},
		{ID: "event", Title: "Event"},
		{ID: "filters", Title: "Filter by", Priority: 3},
		{ID: "yours", Title: "Yours", Width: 90, Priority: 1},
		{ID: "ask", Title: "", Width: 110},
	}
	body := sdui.VStack(
		ghostFlow(automation.HelpID),
		sdui.MutedText(helpEvents),
		sdui.Table(eventsID, columns, rows, "", sdui.Sort{}),
		automation.Facts(helpFacts),
	)
	return sdui.VStack(header, sdui.Scroll(body))
}

func ghostFlow(id string) sdui.Node {
	ghost := func(labels ...string) []sdui.FlowOut {
		outs := make([]sdui.FlowOut, len(labels))
		for i, l := range labels {
			outs[i] = sdui.FlowOut{ID: strconv.Itoa(i), Label: l, State: sdui.EdgeGhost}
		}
		return outs
	}
	return sdui.Flow(id+flowSuffix, sdui.FlowProps{
		Cards: []sdui.FlowCard{
			{ID: "when", Label: "When", Ghost: true, Outs: ghost("an agent turn finishes", "a notification is posted", "an agent emits an event")},
			{ID: "does", Label: "Does", Ghost: true, Outs: ghost("prompt a session", "start an agent", "run a plugin command")},
		},
		Edges: []sdui.FlowEdge{{From: "when", Out: "0", To: "does", State: sdui.EdgeGhost}},
	})
}

func askPrompt(topic string) string {
	k, ok := catalogRow(topic)
	if !ok {
		return askBase + askRest
	}
	on := k.topic
	if on == emitAny {
		on = emitPrefix + "<name>"
	}
	return fmt.Sprintf(askEvent, k.phrase, on) + askRest
}

func (a *app) askAgent(ctx context.Context, workspace, topic string) error {
	return agentcall.Ask(ctx, a.host, workspace, askTitle, askPrompt(topic))
}
