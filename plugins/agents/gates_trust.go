package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/graphs"
	"clubmate/plugins/internal/automation"
)

const (
	gateNone         = "(none)"
	gateGraphWord    = "Graph"
	gateEdgesWord    = "Edges"
	gateHeadWhat     = "title, description or limits"
	gateEdgesWhat    = "which node runs after which, and what it is told"
	gateVersions     = "Pinned %s · now %s"
	gateStepsChanged = "%d of %d steps changed"
	gateStepWord     = "step"
	gateAgentWord    = "agent"
	gateChangedWord  = "%s changed"
	gateAddedWord    = "added"
	gateRemovedWord  = "removed"
	gateUnchanged    = "%s: unchanged"
	gateDiffCells    = 1 << 20
	gateOrderWhat    = "Order changed:"
	gateTrustNothing = "Until you trust it, the gate runs nothing."
	gateTurnWaits    = "1 done turn waits: %s"
	gateTurnsWait    = "%d done turns wait: %s +%d"
	gateLandWaits    = "1 landing waits: %s"
	gateLandsWait    = "%d landings wait: %s +%d"
	gateLandInto     = "%s into %s"
	gateCallIn       = "%s in %s"
	gateCallWaits    = "1 call waits: %s"
	gateCallsWait    = "%d calls wait: %s +%d"
)

type graphDiff struct {
	Nodes     []sdui.Node
	Changed   int
	Steps     int
	Unchanged []string
}

func trustCard(sc gateScreen) sdui.Node {
	r := sc.Row
	var lines []sdui.Node
	var buttons []sdui.Node
	if sc.Graph == nil {
		lines = append(lines, sdui.Text(fmt.Sprintf(gateUnreadable, r.Graph, cmp.Or(sc.GraphErr, "no such graph"))))
	} else {
		title := graphTitle(sc.Graph, r.Graph)
		diff := gateGraphDiff(r.ID, sc.Pinned, sc.Graph)
		versions := fmt.Sprintf(gateVersions, shortHash(r.Pin), shortHash(sc.Graph.Hash))
		if sc.Pinned != nil && diff.Changed > 0 {
			versions += " · " + fmt.Sprintf(gateStepsChanged, diff.Changed, diff.Steps)
		}
		lines = append(lines, sdui.Text(fmt.Sprintf(gateTrustTitle, title)), sdui.MutedText(versions))
		lines = append(lines, diff.Nodes...)
		if len(diff.Unchanged) > 0 {
			lines = append(lines, sdui.MutedText(fmt.Sprintf(gateUnchanged, strings.Join(diff.Unchanged, ", "))))
		}
		lines = append(lines, sdui.MutedText(trustWaitsText(r)))
		trust := automation.DetailAction{Action: actGateTrust, Label: "Trust and run", Variant: sdui.VariantPrimary}
		if sc.Mac {
			trust.Shortcut = "cmd+return"
		}
		button := automation.ActionNode(r.ID+"@"+sc.Graph.Hash, trust)
		if !sc.Mac {
			button = sdui.WithConfirmMessage(button, fmt.Sprintf(gateTrustAsk, title), gateTrustReview)
		}
		buttons = append(buttons, button)
		if sc.Mac {
			buttons = append(buttons, gateGraphButton(gateCardPrefix+r.ID))
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

func trustWaitsText(r gateRow) string {
	waits := r.trustWaits()
	if len(waits) == 0 {
		return trustText(r.Point)
	}
	first := waits[0]
	if r.Point == gatePointTurn {
		what := cmp.Or(first.Title, gateNone)
		if len(waits) == 1 {
			return gateTrustNothing + " " + fmt.Sprintf(gateTurnWaits, what) + "."
		}
		return gateTrustNothing + " " + fmt.Sprintf(gateTurnsWait, len(waits), what, len(waits)-1) + "."
	}
	if r.Point == gatePointTool {
		what := toolMatchName(r.gateConfig)
		if first.Title != "" {
			what = fmt.Sprintf(gateCallIn, what, first.Title)
		}
		if len(waits) == 1 {
			return gateTrustNothing + " " + fmt.Sprintf(gateCallWaits, what) + "."
		}
		return gateTrustNothing + " " + fmt.Sprintf(gateCallsWait, len(waits), what, len(waits)-1) + "."
	}
	what := fmt.Sprintf(gateLandInto, cmp.Or(first.Source, first.Title, gateNone), first.Into)
	if len(waits) == 1 {
		return gateTrustNothing + " " + fmt.Sprintf(gateLandWaits, what) + "."
	}
	return gateTrustNothing + " " + fmt.Sprintf(gateLandsWait, len(waits), what, len(waits)-1) + "."
}

func gateGraphDiff(gate string, old, cur *graphs.GateGraphInfo) graphDiff {
	if old != nil && old.Shape < cur.Shape {
		was := cur.AsShape(old.Shape)
		cur = &was
	}
	d := graphDiff{Steps: len(cur.Nodes)}
	block := func(id, head string, lines []sdui.DiffLine, fold bool) {
		d.Nodes = append(d.Nodes, sdui.Text(head), sdui.DiffLines(gateTrustID+"/"+gate+"/"+id, lines, fold))
	}
	if old == nil {
		d.Nodes = append(d.Nodes, sdui.MutedText(gateTrustLost))
		block("graph", gateGraphWord+" · "+gateHeadWhat, sameLines(cmp.Or(cur.Head, gateNone)), false)
		for _, n := range cur.Nodes {
			block("node:"+n.ID, n.Title+" · "+nodeKind(n), sameLines(nodeText(n)), false)
		}
		block("edges", gateEdgesWord+" · "+gateEdgesWhat, sameLines(cmp.Or(cur.Edges, gateNone)), false)
		return d
	}
	if old.Head != cur.Head {
		block("graph", gateGraphWord+" · "+fmt.Sprintf(gateChangedWord, gateHeadWhat), lineDiff(old.Head, cur.Head), true)
	}
	for _, n := range cur.Nodes {
		i := slices.IndexFunc(old.Nodes, func(o graphs.GateNodeInfo) bool { return o.ID == n.ID })
		if i < 0 {
			d.Changed++
			block("node:"+n.ID, n.Title+" · "+nodeKind(n)+" · "+gateAddedWord, signed(sdui.DiffAdded, nodeWords(n)), false)
			continue
		}
		o := old.Nodes[i]
		var fields []string
		var lines []sdui.DiffLine
		for _, f := range [][3]string{{"command", o.Command, n.Command}, {"prompt", o.Prompt, n.Prompt}, {"model", o.Model, n.Model}, {"effort", o.Effort, n.Effort}, {"settings", o.Other, n.Other}} {
			if f[1] != f[2] {
				fields = append(fields, f[0])
				lines = append(lines, lineDiff(f[1], f[2])...)
			}
		}
		if len(fields) == 0 {
			d.Unchanged = append(d.Unchanged, n.Title)
			continue
		}
		d.Changed++
		block("node:"+n.ID, n.Title+" · "+nodeKind(n)+" · "+fmt.Sprintf(gateChangedWord, strings.Join(fields, ", ")), lines, true)
	}
	for _, o := range old.Nodes {
		if !slices.ContainsFunc(cur.Nodes, func(n graphs.GateNodeInfo) bool { return n.ID == o.ID }) {
			d.Changed++
			d.Steps++
			block("node:"+o.ID, o.Title+" · "+nodeKind(o)+" · "+gateRemovedWord, signed(sdui.DiffRemoved, nodeWords(o)), false)
		}
	}
	if was, now := commonOrder(old.Nodes, cur.Nodes), commonOrder(cur.Nodes, old.Nodes); !slices.EqualFunc(was, now, func(x, y graphs.GateNodeInfo) bool { return x.ID == y.ID }) {
		d.Nodes = append(d.Nodes, sdui.Markdown(orderLine(lineDiff(nodeTitles(was), nodeTitles(now)))))
	}
	if old.Edges != cur.Edges {
		block("edges", gateEdgesWord+" · "+fmt.Sprintf(gateChangedWord, gateEdgesWhat), lineDiff(old.Edges, cur.Edges), true)
	}
	if len(d.Nodes) == 0 {
		d.Nodes = append(d.Nodes, sdui.MutedText(gateTrustOther))
		d.Unchanged = nil
	}
	return d
}

func commonOrder(nodes, other []graphs.GateNodeInfo) []graphs.GateNodeInfo {
	return slices.DeleteFunc(slices.Clone(nodes), func(n graphs.GateNodeInfo) bool {
		return !slices.ContainsFunc(other, func(o graphs.GateNodeInfo) bool { return o.ID == n.ID })
	})
}

func nodeTitles(nodes []graphs.GateNodeInfo) string {
	titles := make([]string, 0, len(nodes))
	for _, n := range nodes {
		title := n.Title
		if slices.ContainsFunc(nodes, func(o graphs.GateNodeInfo) bool { return o.ID != n.ID && o.Title == n.Title }) {
			title += " · " + n.ID
		}
		titles = append(titles, title)
	}
	return strings.Join(titles, "\n")
}

func orderLine(lines []sdui.DiffLine) string {
	var steps []string
	for _, l := range lines {
		switch l.Sign {
		case sdui.DiffRemoved:
		case sdui.DiffAdded:
			steps = append(steps, "**"+markdownEscaper.Replace(l.Text)+"**")
		default:
			steps = append(steps, markdownEscaper.Replace(l.Text))
		}
	}
	return gateOrderWhat + " " + strings.Join(steps, " · ")
}

var markdownEscaper = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`, "<", `\<`, "#", `\#`)

func nodeKind(n graphs.GateNodeInfo) string {
	if n.Step {
		return gateStepWord
	}
	return gateAgentWord
}

func nodeText(n graphs.GateNodeInfo) string {
	return strings.Join([]string{nodeWords(n), n.Other}, "\n")
}

func nodeWords(n graphs.GateNodeInfo) string {
	if n.Prompt == "" {
		return nodeDoes(n)
	}
	return nodeDoes(n) + "\n" + n.Prompt
}

func diffSplit(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func sameLines(text string) []sdui.DiffLine {
	return signed(sdui.DiffSame, text)
}

func signed(sign, text string) []sdui.DiffLine {
	var out []sdui.DiffLine
	for _, l := range diffSplit(text) {
		out = append(out, sdui.DiffLine{Sign: sign, Text: l})
	}
	return out
}

func lineDiff(was, now string) []sdui.DiffLine {
	a, b := diffSplit(was), diffSplit(now)
	if (len(a)+1)*(len(b)+1) > gateDiffCells {
		return append(signed(sdui.DiffRemoved, was), signed(sdui.DiffAdded, now)...)
	}
	common := make([][]int, len(a)+1)
	for i := range common {
		common[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var all []sdui.DiffLine
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			all = append(all, sdui.DiffLine{Sign: sdui.DiffSame, Text: a[i]})
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || common[i+1][j] >= common[i][j+1]):
			all = append(all, sdui.DiffLine{Sign: sdui.DiffRemoved, Text: a[i]})
			i++
		default:
			all = append(all, sdui.DiffLine{Sign: sdui.DiffAdded, Text: b[j]})
			j++
		}
	}
	return all
}
