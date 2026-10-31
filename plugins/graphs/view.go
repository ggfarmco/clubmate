package main

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

const (
	sidebarSlot = "graphs.sidebar"
	mainSlot    = "graphs.main"
	listID      = "runs"
	treeID      = "nodes"
	timeLayout  = "2006-01-02T15:04:05Z"
	whenLayout  = "Jan 2 15:04"
	maxBrief    = 160
)

var liveOrder = []string{"waiting", "running", "error"}

func sidebarView(runs []*Run, selected string, sessions map[string]api.Session) sdui.Node {
	if len(runs) == 0 {
		return sdui.Empty("No graph runs. Put a graph in .clubmate/graphs/<name>.json and start it with graphs.run.")
	}
	items := make([]sdui.Item, 0, len(runs))
	for _, r := range runs {
		live := runLive(r, sessions)
		items = append(items, sdui.Item{
			ID:     r.ID,
			Label:  r.Title,
			Detail: runDetail(r),
			Icon:   runIcon(r),
			Badge:  live,
			Menu:   runMenu(r),
		})
	}
	return sdui.List(listID, items, selected)
}

func mainView(r *Run, sessions map[string]api.Session) sdui.Node {
	if r == nil {
		return sdui.Empty("Select a graph run")
	}
	children := []sdui.Node{
		sdui.Text(r.Title + " · " + r.ID),
		sdui.MutedText(stateLine(r)),
	}
	if r.Input != "" {
		children = append(children, sdui.MutedText("Input: "+firstLine(r.Input)))
	}
	children = append(children, sdui.Tree(treeID, nodeItems(r, sessions), ""))
	return sdui.VStack(children...)
}

func nodeItems(r *Run, sessions map[string]api.Session) []sdui.TreeItem {
	items := make([]sdui.TreeItem, 0, len(r.Nodes))
	for _, n := range r.Nodes {
		s := sessions[n.Session]
		status := nodeStatus(s, sessions != nil)
		var edges []sdui.TreeItem
		for i, e := range r.Edges {
			if e.From != n.ID {
				continue
			}
			edges = append(edges, sdui.TreeItem{
				ID:     n.ID + "/e" + strconv.Itoa(i),
				Label:  e.To,
				Detail: edgeLabel(e),
				Icon:   "arrow.turn.down.right",
				Muted:  r.Status != statusRunning,
			})
		}
		items = append(items, sdui.TreeItem{
			ID:          n.ID,
			Label:       n.Role,
			Detail:      nodeDetail(n, s),
			Icon:        nodeIcon(n),
			Tone:        statusTone(status),
			Badge:       status,
			HasChildren: len(edges) > 0,
			Expanded:    len(edges) > 0,
			Children:    edges,
		})
	}
	return items
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

func stateLine(r *Run) string {
	turns := fmt.Sprintf("%d of %d turns", r.Turns, r.MaxTurns)
	if r.Status == statusRunning {
		return "Running · " + turns
	}
	return "Stopped: " + cmp.Or(r.Reason, reasonStopped) + " · " + turns
}

func runIcon(r *Run) string {
	if r.Status == statusRunning {
		return "point.3.connected.trianglepath.dotted"
	}
	return "stop.circle"
}

func runMenu(r *Run) []sdui.MenuEntry {
	var menu []sdui.MenuEntry
	if r.Status == statusRunning {
		menu = append(menu, sdui.MenuEntry{Action: "stop", Label: "Stop", Confirm: "Stop graph run “" + r.Title + "”? Its sessions stay open."})
	}
	return append(menu, sdui.MenuEntry{Action: "remove", Label: "Remove", Confirm: "Remove graph run “" + r.Title + "” from the list? Its sessions stay open."})
}

func nodeDetail(n RunNode, s api.Session) string {
	detail := n.ID
	if n.Start {
		detail += " · start"
	}
	return detail + " · " + cmp.Or(s.Title, n.Session)
}

func nodeIcon(n RunNode) string {
	if n.Start {
		return "play.circle"
	}
	return "circle"
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
