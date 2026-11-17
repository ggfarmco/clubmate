package main

import (
	"cmp"
	"fmt"
	"slices"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/graphs"
	"clubmate/plugins/internal/automation"
)

const (
	gateNone      = "(none)"
	gateGraphWord = "Graph"
	gateEdgesWord = "Edges"
)

func trustCard(sc gateScreen) sdui.Node {
	r := sc.Row
	var lines []sdui.Node
	var buttons []sdui.Node
	if sc.Graph == nil {
		lines = append(lines, sdui.Text(fmt.Sprintf(gateUnreadable, r.Graph, cmp.Or(sc.GraphErr, "no such graph"))))
	} else {
		lines = append(lines,
			sdui.Text(fmt.Sprintf(gateTrustTitle, graphTitle(sc.Graph, r.Graph))),
			sdui.MutedText(fmt.Sprintf(trustText(r.Point), shortHash(r.Pin), shortHash(sc.Graph.Hash))),
		)
		lines = append(lines, gateGraphDiff(sc.Pinned, sc.Graph)...)
		if sc.Mac {
			trust := automation.DetailAction{Action: actGateTrust, Label: "Trust and run", Variant: sdui.VariantPrimary, Shortcut: "cmd+return"}
			buttons = append(buttons, automation.ActionNode(r.ID+"@"+sc.Graph.Hash, trust), gateGraphButton(gateCardPrefix+r.ID))
		} else {
			lines = append(lines, sdui.MutedText(gateTrustPhone))
		}
	}
	if !r.Paused {
		buttons = append(buttons, automation.ActionNode(gateCardPrefix+r.ID, automation.DetailAction{Action: actGatePause, Label: "Pause the gate", Variant: sdui.VariantSecondary}))
	}
	if len(buttons) > 0 {
		lines = append(lines, sdui.HStack(append([]sdui.Node{sdui.Spacer()}, buttons...)...))
	}
	return sdui.WithID(sdui.Card("warning", "exclamationmark.shield", lines...), gateTrustID)
}

func gateGraphDiff(old, cur *graphs.GateGraphInfo) []sdui.Node {
	if old == nil {
		out := []sdui.Node{sdui.MutedText(gateTrustLost), sdui.Text(gateGraphWord + " · title, description or limits"), sdui.MonoText(cmp.Or(cur.Head, gateNone))}
		for _, n := range cur.Nodes {
			out = append(out, sdui.Text(n.Title+" · "+nodeDoes(n)))
			if n.Prompt != "" {
				out = append(out, sdui.MonoText(n.Prompt))
			}
			out = append(out, sdui.MonoText(n.Other))
		}
		return append(out, sdui.Text(gateEdgesWord+" · which node runs after which, and what it is told"), sdui.MonoText(cmp.Or(cur.Edges, gateNone)))
	}
	var out []sdui.Node
	change := func(title, field, was, now string) {
		out = append(out, sdui.Text(title+" · "+field), sdui.MonoText("− "+cmp.Or(was, gateNone)), sdui.MonoText("+ "+cmp.Or(now, gateNone)))
	}
	if old.Head != cur.Head {
		change(gateGraphWord, "title, description or limits", old.Head, cur.Head)
	}
	for _, n := range cur.Nodes {
		i := slices.IndexFunc(old.Nodes, func(o graphs.GateNodeInfo) bool { return o.ID == n.ID })
		if i < 0 {
			out = append(out, sdui.Text(n.Title+" · added"), sdui.MonoText("+ "+nodeDoes(n)))
			if n.Prompt != "" {
				out = append(out, sdui.MonoText("+ "+n.Prompt))
			}
			out = append(out, sdui.MonoText("+ "+n.Other))
			continue
		}
		o := old.Nodes[i]
		if o.Command != n.Command {
			change(n.Title, "command", o.Command, n.Command)
		}
		if o.Prompt != n.Prompt {
			change(n.Title, "prompt", o.Prompt, n.Prompt)
		}
		if o.Model != n.Model {
			change(n.Title, "model", o.Model, n.Model)
		}
		if o.Effort != n.Effort {
			change(n.Title, "effort", o.Effort, n.Effort)
		}
		if o.Other != n.Other {
			change(n.Title, "settings", o.Other, n.Other)
		}
	}
	for _, o := range old.Nodes {
		if !slices.ContainsFunc(cur.Nodes, func(n graphs.GateNodeInfo) bool { return n.ID == o.ID }) {
			out = append(out, sdui.Text(o.Title+" · removed"), sdui.MonoText("− "+nodeDoes(o)))
			if o.Prompt != "" {
				out = append(out, sdui.MonoText("− "+o.Prompt))
			}
			out = append(out, sdui.MonoText("− "+o.Other))
		}
	}
	if old.Edges != cur.Edges {
		change(gateEdgesWord, "which node runs after which, and what it is told", old.Edges, cur.Edges)
	}
	if len(out) == 0 {
		out = append(out, sdui.MutedText(gateTrustOther))
	}
	return out
}
