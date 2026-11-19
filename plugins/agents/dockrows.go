package main

import (
	"cmp"
	"strconv"

	"clubmate/plugins/agents/api"
	"clubmate/plugins/agents/graphs"
)

const (
	dockService = "service"
	dockGates   = "gates"
	dockGraphs  = "graphs"
	gatesIcon   = "checkmark.shield"
	graphsIcon  = "point.3.connected.trianglepath.dotted"
)

type gateCount struct {
	Gated   int
	Asking  int
	Trust   int
	Landing int
	Held    int
	asked   []string
	trusted []string
	landing []string
	held    []string
}

type gateRound struct {
	Round   int    `json:"round"`
	Label   string `json:"label"`
	Verdict string `json:"verdict"`
}

type reviewerNest struct {
	Lander  string
	Round   gateRound
	Tracked bool
	Asks    bool
	Shown   bool
	Open    bool
}

func gateCounts(rows []gateRow, reviewersAsking int) gateCount {
	c := gateCount{Gated: len(rows), Asking: reviewersAsking}
	for _, r := range rows {
		asking, landing := r.landingCounts()
		label := dockGateLabel(r.Name)
		c.Asking += asking
		if asking > 0 {
			c.asked = append(c.asked, label)
		}
		switch {
		case landing == 0:
		case r.Point == gatePointTurn:
			c.Held += landing
			c.held = append(c.held, label)
		default:
			c.Landing += landing
			c.landing = append(c.landing, label)
		}
		if r.State == gateStateTrust {
			c.Trust++
			c.trusted = append(c.trusted, label)
		}
	}
	if reviewersAsking > 0 {
		c.asked = nil
	}
	return c
}

func onlyOne(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return ""
}

func dockRows(service []sessionInfo, gates gateCount, defs []graphs.Definition) []api.DockRow {
	asks := 0
	for _, s := range service {
		if s.NeedsYou {
			asks++
		}
	}
	svc := countRow(dockService, "Service", serviceIcon, len(service))
	if asks > 0 {
		svc.State, svc.Attention = "asks you", true
	}
	gate := countRow(dockGates, "Gates", gatesIcon, gates.Gated)
	switch {
	case gates.Trust > 0:
		gate.State, gate.Which, gate.Attention = strconv.Itoa(gates.Trust)+" to trust", onlyOne(gates.trusted), true
	case gates.Asking > 0:
		gate.State, gate.Which = strconv.Itoa(gates.Asking)+" asking", onlyOne(gates.asked)
	case gates.Landing > 0:
		gate.State, gate.Which = landingCount(gates.Landing), onlyOne(gates.landing)
	case gates.Held > 0:
		gate.State, gate.Which = heldCount(gates.Held), onlyOne(gates.held)
	}
	var waiting []string
	for _, d := range defs {
		if d.WaitsTrust {
			waiting = append(waiting, cmp.Or(d.Title, d.Name))
		}
	}
	graph := countRow(dockGraphs, "Graphs", graphsIcon, len(defs))
	if len(waiting) > 0 {
		graph.State, graph.Which, graph.Attention = strconv.Itoa(len(waiting))+" to trust", onlyOne(waiting), true
	}
	return []api.DockRow{svc, gate, graph}
}

func countRow(id, label, icon string, n int) api.DockRow {
	return api.DockRow{ID: id, Label: label, Icon: icon, Detail: strconv.Itoa(n), PhoneDetail: strconv.Itoa(n)}
}
