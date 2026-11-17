package main

import (
	"strconv"
	"strings"

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
		c.Asking += asking
		if r.Point == gatePointTurn {
			c.Held += landing
		} else {
			c.Landing += landing
		}
		if r.State == gateStateTrust {
			c.Trust++
		}
	}
	return c
}

func dockRows(service []sessionInfo, gates gateCount, defs []graphs.Definition) []api.DockRow {
	asks := 0
	for _, s := range service {
		if s.NeedsYou {
			asks++
		}
	}
	svc := api.DockRow{ID: dockService, Label: "Service", Icon: serviceIcon, Detail: strconv.Itoa(len(service)), PhoneDetail: strconv.Itoa(len(service))}
	if asks > 0 {
		svc.Detail, svc.PhoneDetail, svc.Attention = "asks you", strconv.Itoa(asks)+" asks", true
	}
	gate := api.DockRow{ID: dockGates, Label: "Gates", Icon: gatesIcon, Detail: strconv.Itoa(gates.Gated)}
	switch {
	case gates.Trust > 0:
		gate.Detail, gate.Attention = strconv.Itoa(gates.Trust)+" to trust", true
	case gates.Asking > 0:
		gate.Detail = strconv.Itoa(gates.Asking) + " asking"
	case gates.Landing > 0 || gates.Held > 0:
		gate.Detail = strings.Join(nonEmpty(landingCount(gates.Landing), heldCount(gates.Held)), " · ")
	}
	gate.PhoneDetail = gate.Detail
	waits := 0
	for _, d := range defs {
		if d.WaitsTrust {
			waits++
		}
	}
	graph := api.DockRow{ID: dockGraphs, Label: "Graphs", Icon: graphsIcon, Detail: strconv.Itoa(len(defs))}
	if waits > 0 {
		graph.Detail, graph.Attention = strconv.Itoa(waits)+" to trust", true
	}
	graph.PhoneDetail = graph.Detail
	return []api.DockRow{svc, gate, graph}
}
