package main

import (
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
	Gated  int
	Asking int
}

type gateRound struct {
	Round   int    `json:"round"`
	Label   string `json:"label"`
	Verdict string `json:"verdict"`
}

type reviewerNest struct {
	Lander string
	Round  gateRound
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
	gate := api.DockRow{ID: dockGates, Label: "Land gates", Icon: gatesIcon, Detail: strconv.Itoa(gates.Gated)}
	if gates.Asking > 0 {
		gate.Detail = strconv.Itoa(gates.Asking) + " asking"
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

func reviewerNesting(recs []record, reviewing func(lander string, st landReview) bool) map[string]reviewerNest {
	out := map[string]reviewerNest{}
	for _, rec := range recs {
		for _, st := range rec.LandReviews {
			if st.Reviewer == "" {
				continue
			}
			round := gateRound{Round: st.Rounds, Verdict: st.Last}
			if reviewing(rec.ID, st) {
				round.Round, round.Verdict = st.Rounds+1, gateStateReview
			}
			round.Round = max(round.Round, 1)
			round.Label = "Round " + strconv.Itoa(round.Round) + " review"
			out[st.Reviewer] = reviewerNest{Lander: rec.ID, Round: round}
		}
	}
	return out
}
