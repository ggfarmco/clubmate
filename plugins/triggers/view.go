package main

import (
	"cmp"
	"encoding/json"
	"fmt"
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
	sidebarSlot  = "triggers.sidebar"
	mainSlot     = "triggers.main"
	listID       = "triggers"
	groupPrefix  = "graph:"
	actGraph     = "graph"
	graphIcon    = "point.3.connected.trianglepath.dotted"
	graphOutcome = emitPrefix + "graph."
)

func sidebarView(triggers []*Trigger, selected string, collapsed map[string]bool, titles map[string]string) sdui.Node {
	if len(triggers) == 0 {
		return sdui.Empty("No triggers yet. Agents add them, and each graph run adds one per edge.")
	}
	var items, groups []sdui.TreeItem
	index := map[string]int{}
	for _, t := range triggers {
		item := triggerItem(t, titles)
		if t.Run == "" {
			items = append(items, item)
			continue
		}
		item.Label = strings.TrimPrefix(t.Name, t.RunTitle+": ")
		i, ok := index[t.Run]
		if !ok {
			i = len(groups)
			index[t.Run] = i
			id := groupPrefix + t.Run
			groups = append(groups, sdui.TreeItem{ID: id, Label: runName(t), Icon: graphIcon, Muted: true, HasChildren: true, Expanded: !collapsed[id]})
		}
		groups[i].Children = append(groups[i].Children, item)
	}
	for i := range groups {
		groups[i].Label = "from graph " + groups[i].Label
		groups[i].Detail = strconv.Itoa(len(groups[i].Children))
	}
	slices.SortStableFunc(groups, func(x, y sdui.TreeItem) int { return cmp.Compare(x.Label, y.Label) })
	return sdui.Tree(listID, append(items, groups...), selected)
}

func triggerItem(t *Trigger, titles map[string]string) sdui.TreeItem {
	icon, tone := automation.ListIcon(t.Enabled, t.History, "bolt")
	return sdui.TreeItem{
		ID:     t.ID,
		Label:  t.Name,
		Detail: triggerDetail(t, titles),
		Icon:   icon,
		Tone:   tone,
		Menu:   triggerMenu(t),
	}
}

func mainView(t *Trigger, titles map[string]string, open automation.Open, runs automation.RunsView) sdui.Node {
	if t == nil {
		return sdui.Empty("Select a trigger")
	}
	props := sdui.HeaderProps{Icon: headerIcon(t), Badges: automation.StateBadges(t.Enabled, t.History)}
	var actions []sdui.Node
	for _, act := range automation.StateActions(t.Enabled, t.Run == "") {
		actions = append(actions, automation.ActionNode(t.ID, act))
	}
	if t.Run != "" {
		props.Meta = "created by graph " + runName(t)
		actions = append(actions,
			automation.ActionNode(t.ID, automation.DetailAction{Action: actGraph, Label: "Open graph", Icon: graphIcon, Variant: sdui.VariantGhost, Shortcut: "g"}),
			sdui.WithOverflow(automation.ActionNode(t.ID, automation.DeleteAction(deleteConfirm(t)))),
		)
	} else {
		actions = append(actions, automation.ActionNode(t.ID, automation.DeleteAction(deleteConfirm(t))))
	}
	header := sdui.Header(t.Name, props, actions...)
	when := whenLine(t, titles)
	rows := []sdui.Node{automation.Property("When", when, "")}
	if !t.Enabled && t.LastError != "" {
		rows = append(rows, automation.Property("Paused", automation.ErrorWords(t.LastError), ""))
	}
	rows = append(rows, automation.Property("Owner", ownerLine(t, titles), ""))
	rows = append(rows, automation.Does(t.ID, t.Command, t.Args, titles, open)...)
	if len(t.History) == 0 {
		return sdui.VStack(header, automation.Properties(append(rows, automation.Property("Runs", "No runs yet. It runs when "+when+".", ""))...))
	}
	cells := func(r Run) ([]string, string) {
		return []string{eventWords(r, titles)}, automation.Words(t.Command, t.Args, r, titles)
	}
	event := []sdui.Column{{ID: "event", Title: "Event", Priority: 1}}
	table, run := automation.RunsTable(t.History, time.Local, event, cells, runs)
	rows = append(rows, automation.RunsRows(t.ID, t.History, run, open.RunJSON)...)
	return sdui.VStack(header, automation.Properties(rows...), sdui.Scroll(table))
}

func whenLine(t *Trigger, titles map[string]string) string {
	parts := []string{topicPhrase(t.On)}
	var match map[string]any
	_ = json.Unmarshal(t.Match, &match)
	keys := slices.SortedFunc(maps.Keys(match), func(x, y string) int {
		return cmp.Or(cmp.Compare(rank(x), rank(y)), cmp.Compare(x, y))
	})
	for _, k := range keys {
		parts = append(parts, matchPart(k, match[k], titles))
	}
	return strings.Join(parts, " · ")
}

func rank(key string) int {
	if key == "status" {
		return 0
	}
	return 1
}

func topicPhrase(on string) string {
	switch {
	case on == manualTopic:
		return "run now"
	case on == api.TopicTurn:
		return "an agent turn finishes"
	case on == api.TopicChanged:
		return "an agent session changes"
	case strings.HasPrefix(on, graphOutcome):
		return "a graph step reports its outcome"
	case on == emitPrefix+"*":
		return "any event is emitted"
	case strings.HasPrefix(on, emitPrefix):
		return "the event “" + strings.TrimPrefix(on, emitPrefix) + "” is emitted"
	case strings.HasSuffix(on, ".*"):
		return "any “" + strings.TrimSuffix(on, ".*") + "” event arrives"
	}
	return "“" + on + "” arrives"
}

func matchPart(key string, v any, titles map[string]string) string {
	switch x := v.(type) {
	case bool:
		if key == "asks" {
			return map[bool]string{true: "asks a question", false: "asks nothing"}[x]
		}
		return key + " " + map[bool]string{true: "yes", false: "no"}[x]
	case []any:
		values := make([]string, 0, len(x))
		for _, e := range x {
			values = append(values, automation.Names(fmt.Sprint(e), titles))
		}
		return key + " " + strings.Join(values, " or ")
	}
	return key + " " + automation.Names(fmt.Sprint(v), titles)
}

func ownerLine(t *Trigger, titles map[string]string) string {
	if t.Run != "" {
		return "graph " + runName(t)
	}
	return automation.OwnerLabel(t.Owner, titles)
}

func runName(t *Trigger) string {
	return cmp.Or(t.RunTitle, "a graph run")
}

func triggerDetail(t *Trigger, titles map[string]string) string {
	if t.Enabled {
		return whenLine(t, titles)
	}
	if t.LastError == "" {
		return "paused"
	}
	return "paused · " + automation.ErrorWords(t.LastError)
}

func headerIcon(t *Trigger) string {
	if t.Enabled {
		return "bolt"
	}
	return "pause.circle"
}

func deleteConfirm(t *Trigger) string {
	if t.Run != "" {
		return "Delete trigger “" + t.Name + "”? Graph " + runName(t) + " passes work along it; without it the run stops at this step."
	}
	return "Delete trigger “" + t.Name + "”?"
}

func triggerMenu(t *Trigger) []sdui.MenuEntry {
	toggle := sdui.MenuEntry{Action: automation.ActPause, Label: "Pause"}
	if !t.Enabled {
		toggle = sdui.MenuEntry{Action: automation.ActResume, Label: "Resume"}
	}
	return []sdui.MenuEntry{
		{Action: automation.ActRun, Label: "Run now"},
		toggle,
		{Action: automation.ActDelete, Label: "Delete", Confirm: deleteConfirm(t)},
	}
}
