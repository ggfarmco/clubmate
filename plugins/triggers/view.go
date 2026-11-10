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

	actOpenSession = "openSession"
	runJSONFormID  = "runJson"
)

func sidebarView(triggers []*Trigger, selected string, expanded map[string]bool, titles map[string]string) sdui.Node {
	items := []sdui.TreeItem{automation.HelpItem("How triggers work")}
	var groups []sdui.TreeItem
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
			groups = append(groups, sdui.TreeItem{ID: id, Label: runName(t), Icon: graphIcon, Muted: true, Action: true, HasChildren: true})
		}
		groups[i].Children = append(groups[i].Children, item)
		if t.ID == selected {
			groups[i].Expanded = true
		}
	}
	for i := range groups {
		if open, ok := expanded[groups[i].ID]; ok {
			groups[i].Expanded = open
		}
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

func mainView(t *Trigger, triggers []*Trigger, titles map[string]string, shape *graphShape, open automation.Open, runs automation.RunsView) sdui.Node {
	if t == nil {
		return helpView(triggers)
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
	titles = chainTitles(t, titles)
	when := whenLine(t, titles)
	rows := []sdui.Node{automation.Property("When", when, "")}
	if pace := paceLine(t); pace != "" {
		rows = append(rows, automation.Property("Limits", pace, ""))
	}
	if !t.Enabled && t.LastError != "" {
		rows = append(rows, automation.Property("Paused", automation.ErrorWords(t.LastError), ""))
	}
	rows = append(rows, automation.Property("Owner", ownerLine(t, titles), ""))
	rows = append(rows, automation.Does(t.ID, t.Command, t.Args, titles, open)...)
	caption, flow, lanes, drawn := drawnRuns(t, triggers, titles, shape, time.Local)
	var body []sdui.Node
	if drawn {
		body = append(body, sdui.Form(t.ID+captionSuffix, sdui.MutedText(caption)), flow)
	}
	if len(t.History) == 0 {
		body = append(body, automation.Properties(append(rows, automation.Property("Runs", "No runs yet. It runs when "+when+".", ""))...))
		return sdui.VStack(header, sdui.Scroll(sdui.VStack(body...)))
	}
	cells := func(r Run) ([]string, string) {
		return []string{eventWords(r, titles)}, automation.Words(t.Command, t.Args, r, titles)
	}
	link := func(r Run) (string, string) {
		if r.Status != automation.StatusError || titles[runSession(t, r)] == "" {
			return "", ""
		}
		return actOpenSession, "Open session"
	}
	event := []sdui.Column{{ID: "event", Title: "Event", Priority: 1}}
	table, run, picked := automation.RunsTable(t.History, time.Local, event, cells, link, runs)
	if drawn {
		body = append(body, automation.Properties(rows...), lanes)
	} else {
		body = append(body, automation.Properties(append(rows, automation.Property("Runs", automation.RunCounts(t.History), ""))...))
	}
	body = append(body, table, sdui.Form(runJSONFormID, automation.RunJSONRows(t.ID, run, picked, open.RunJSON)...))
	return sdui.VStack(header, sdui.Scroll(sdui.VStack(body...)))
}

func runSession(t *Trigger, r Run) string {
	var result struct {
		Session string `json:"session"`
	}
	if t.Command == agentNewCommand {
		_ = json.Unmarshal([]byte(r.Result), &result)
	}
	session := cmp.Or(r.Target, result.Session)
	if session == "" && r.Chain != nil {
		session = r.Chain.Session
	}
	return session
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

func paceLine(t *Trigger) string {
	var parts []string
	if t.Limit > 0 {
		parts = append(parts, "up to "+strconv.Itoa(t.Limit)+" runs a minute")
	}
	if t.Parallel {
		parts = append(parts, "runs overlap")
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
	case strings.HasPrefix(on, graphOutcome):
		k, _ := catalogRow(on)
		return k.phrase
	case on == emitAny:
		return "any event is emitted"
	case strings.HasPrefix(on, emitPrefix):
		return "the event “" + strings.TrimPrefix(on, emitPrefix) + "” is emitted"
	case strings.HasSuffix(on, ".*"):
		return "any “" + strings.TrimSuffix(on, ".*") + "” event arrives"
	}
	if phrase, ok := catalogPhrase(on); ok {
		return phrase
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
	if s, ok := v.(string); ok && key == cronField {
		return "cron job " + cmp.Or(titles[s], s)
	}
	return key + " " + automation.Names(fmt.Sprint(v), titles)
}

func ownerLine(t *Trigger, titles map[string]string) string {
	if t.Run != "" {
		return "graph " + runName(t)
	}
	if plugin, ok := strings.CutPrefix(t.CreatedBy, automation.PluginCreatorPrefix); ok {
		return "added by " + plugin
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
	run, toggle := sdui.MenuEntry{Action: automation.ActRun, Label: "Run now"}, sdui.MenuEntry{Action: automation.ActPause, Label: "Pause"}
	if !t.Enabled {
		run.Label, toggle = "Run once", sdui.MenuEntry{Action: automation.ActResume, Label: "Resume"}
	}
	return []sdui.MenuEntry{
		run,
		toggle,
		{Action: automation.ActDelete, Label: "Delete", Confirm: deleteConfirm(t)},
	}
}
