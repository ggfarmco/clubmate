package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/automation"
)

const (
	sectionID   = "graphs"
	sidebarSlot = "graphs.sidebar"
	mainSlot    = "graphs.main"
	listID      = "runs"
	tableID     = "nodes"
	graphIcon   = "point.3.connected.trianglepath.dotted"
	actOpen     = "open"
	actStop     = "stop"
	actRemove   = "remove"
	timeLayout  = "2006-01-02T15:04:05Z"
	whenLayout  = "Jan 2 15:04"
	maxBrief    = 160
)

var liveOrder = []string{"waiting", "running", "error"}

func sidebarView(runs []*Run, selected string, sessions map[string]api.Session) sdui.Node {
	if len(runs) == 0 {
		return sdui.Empty("No graph runs yet")
	}
	items := make([]sdui.Item, 0, len(runs))
	for _, r := range runs {
		icon, tone := runIcon(r, runLive(r, sessions))
		items = append(items, sdui.Item{
			ID:     r.ID,
			Label:  r.Title,
			Detail: runDetail(r),
			Icon:   icon,
			Tone:   tone,
			Menu:   runMenu(r),
		})
	}
	return sdui.List(listID, items, selected)
}

func mainView(r *Run, sessions map[string]api.Session) sdui.Node {
	if r == nil {
		return emptyView()
	}
	rows := []sdui.Node{
		automation.Property("Input", cmp.Or(firstLine(r.Input), "none"), ""),
		automation.Property("Started", r.StartedAt.Local().Format(whenLayout), ""),
		automation.Property("Turns", fmt.Sprintf("%d of %d", r.Turns, r.MaxTurns), ""),
	}
	if r.Status != statusRunning {
		stopped := cmp.Or(r.Reason, reasonStopped)
		if r.StoppedAt != nil {
			stopped = r.StoppedAt.Local().Format(whenLayout) + " · " + stopped
		}
		rows = append(rows, automation.Property("Stopped", stopped, ""))
	}
	rows = append(rows, automation.Property("Definition", graphsDir+"/"+r.Graph+graphExt, ""))
	nodes := sdui.Table(tableID, nodeColumns, nodeRows(r, sessions), "", sdui.Sort{})
	return sdui.VStack(runHeader(r, sessions), automation.Properties(rows...), sdui.Scroll(nodes))
}

func emptyView() sdui.Node {
	return sdui.EmptyState("Graphs chain agents: when one finishes, the next starts. Start one with graphs.run.", sdui.EmptyProps{Title: "No graph runs yet", Icon: graphIcon, Hint: graphsDir + "/<name>" + graphExt})
}

func runHeader(r *Run, sessions map[string]api.Session) sdui.Node {
	state := "stopped"
	if r.Status == statusRunning {
		state = "running"
	}
	badges := []sdui.HeaderBadge{{Text: fmt.Sprintf("%s · turn %d of %d", state, r.Turns, r.MaxTurns)}}
	if n, status := attentionNode(r, sessions); n != nil && (status == "waiting" || status == "error") {
		badges = append(badges, sdui.HeaderBadge{Text: n.Role + " " + stateLabel(status, true), Tone: statusTone(status)})
	}
	var actions []sdui.Node
	if n, _ := attentionNode(r, sessions); n != nil {
		actions = append(actions, automation.ActionNode(r.ID, automation.DetailAction{Action: actOpen, Label: "Open " + n.Role, Icon: "sparkles", Variant: sdui.VariantPrimary, Shortcut: "cmd+return"}))
	}
	if r.Status == statusRunning {
		actions = append(actions, automation.ActionNode(r.ID, automation.DetailAction{Action: actStop, Label: "Stop run", Icon: "stop.circle", Variant: sdui.VariantSecondary, Shortcut: "cmd+.", Confirm: stopConfirm(r)}))
	} else {
		actions = append(actions, automation.ActionNode(r.ID, automation.DetailAction{Action: actRemove, Label: "Remove", Icon: "trash", Variant: sdui.VariantDanger, Shortcut: "cmd+delete", Confirm: removeConfirm(r)}))
	}
	return sdui.Header(r.Title, sdui.HeaderProps{Icon: graphIcon, Badges: badges}, actions...)
}

func attentionNode(r *Run, sessions map[string]api.Session) (*RunNode, string) {
	for _, want := range []string{"waiting", "error", "running"} {
		for i, n := range r.Nodes {
			if s, ok := sessions[n.Session]; ok && s.Status == want {
				return &r.Nodes[i], want
			}
		}
	}
	for i, n := range r.Nodes {
		if _, ok := sessions[n.Session]; ok && n.Start {
			return &r.Nodes[i], nodeStatus(sessions[n.Session], true)
		}
	}
	return nil, ""
}

var nodeColumns = []sdui.Column{
	{ID: "node", Title: "Node"},
	{ID: "state", Title: "State"},
	{ID: "session", Title: "Session", Priority: 1},
}

func nodeRows(r *Run, sessions map[string]api.Session) []sdui.TableRow {
	rows := make([]sdui.TableRow, 0, len(r.Nodes)+len(r.Edges))
	for _, n := range r.Nodes {
		s := sessions[n.Session]
		status := nodeStatus(s, sessions != nil)
		label := n.Role
		if n.Start {
			label += " · start"
		}
		rows = append(rows, sdui.TableRow{
			ID:    n.ID,
			Muted: r.Status != statusRunning,
			Cells: []sdui.Cell{{Text: label}, {Text: stateLabel(status, false), Tone: cellTone(status)}, {Text: sessionLabel(s, status)}},
		})
		rows = append(rows, edgeRows(r, n.ID)...)
	}
	return rows
}

func edgeRows(r *Run, from string) []sdui.TableRow {
	var targets []string
	ons := map[string][]string{}
	for _, e := range r.Edges {
		if e.From != from {
			continue
		}
		if _, ok := ons[e.To]; !ok {
			targets = append(targets, e.To)
		}
		ons[e.To] = append(ons[e.To], edgeLabel(e))
	}
	rows := make([]sdui.TableRow, 0, len(targets))
	for _, to := range targets {
		rows = append(rows, sdui.TableRow{
			ID:    from + "/" + to,
			Depth: 1,
			Muted: true,
			Cells: []sdui.Cell{{Text: "→ " + roleOf(r, to)}, {Text: strings.Join(ons[to], " · ")}, {}},
		})
	}
	return rows
}

func sessionLabel(s api.Session, status string) string {
	switch status {
	case "idle":
		return "not started"
	case "gone", "unknown":
		return ""
	}
	return s.Title
}

func roleOf(r *Run, id string) string {
	if n := r.node(id); n != nil {
		return n.Role
	}
	return id
}

func stateLabel(status string, short bool) string {
	switch status {
	case "waiting":
		if short {
			return "needs you"
		}
		return "waiting for you"
	case "error":
		return "failed"
	case "gone":
		return "session closed"
	}
	return status
}

func runLive(r *Run, sessions map[string]api.Session) string {
	if r.Status != statusRunning {
		return ""
	}
	statuses := make([]string, 0, len(r.Nodes))
	for _, n := range r.Nodes {
		statuses = append(statuses, sessions[n.Session].Status)
	}
	for _, s := range liveOrder {
		if slices.Contains(statuses, s) {
			return s
		}
	}
	return ""
}

func runDetail(r *Run) string {
	when := r.StartedAt.Local().Format(whenLayout)
	if r.Status == statusRunning {
		return fmt.Sprintf("%s · %d/%d turns", when, r.Turns, r.MaxTurns)
	}
	return when + " · " + cmp.Or(r.Reason, r.Status)
}

func runIcon(r *Run, live string) (string, string) {
	switch {
	case r.Status != statusRunning:
		return "stop.circle", ""
	case live == "waiting":
		return "exclamationmark.circle", statusTone(live)
	case live == "error":
		return "xmark.circle", statusTone(live)
	case live == "running":
		return "play.circle", statusTone(live)
	}
	return graphIcon, ""
}

func runMenu(r *Run) []sdui.MenuEntry {
	if r.Status == statusRunning {
		return []sdui.MenuEntry{{Action: actStop, Label: "Stop", Confirm: stopConfirm(r)}}
	}
	return []sdui.MenuEntry{{Action: actRemove, Label: "Remove", Confirm: removeConfirm(r)}}
}

func stopConfirm(r *Run) string {
	return "Stop graph run “" + r.Title + "”? Its sessions stay open."
}

func removeConfirm(r *Run) string {
	return "Remove graph run “" + r.Title + "” from the list? Its sessions stay open."
}

func edgeLabel(e RunEdge) string {
	if e.On == onOutcome {
		return "on outcome " + e.Outcome
	}
	return "on " + e.On
}

func statusTone(status string) string {
	switch status {
	case "running":
		return "accent"
	case "waiting":
		return "warning"
	case "done":
		return "success"
	case "error", "gone":
		return "danger"
	}
	return "neutral"
}

func cellTone(status string) string {
	switch status {
	case "waiting":
		return "warn"
	case "done":
		return "ok"
	case "error", "gone":
		return "danger"
	}
	return ""
}

func firstLine(s string) string {
	line, _, cut := strings.Cut(strings.TrimSpace(s), "\n")
	if runes := []rune(line); len(runes) > maxBrief {
		line, cut = string(runes[:maxBrief]), true
	}
	if cut {
		line += "…"
	}
	return line
}
