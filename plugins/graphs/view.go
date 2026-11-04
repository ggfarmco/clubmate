package main

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/automation"
)

const (
	sectionID   = "graphs"
	sidebarSlot = "graphs.sidebar"
	mainSlot    = "graphs.main"
	listID      = "runs"
	graphPrefix = "graph:"
	groupPrefix = "group:"
	groupGraphs = groupPrefix + "graphs"
	groupRuns   = groupPrefix + "runs"
	fieldInput  = "input"
	actRun      = "run"
	graphIcon   = "point.3.connected.trianglepath.dotted"
	actOpen     = "open"
	actStop     = "stop"
	actRemove   = "remove"
	timeLayout  = "2006-01-02T15:04:05Z"
	whenLayout  = "Jan 2 15:04"
	clockLayout = "15:04"
	maxBrief    = 160
	descLines   = 3
	statePlain  = "plain"
	tagChanged  = "changed"
)

var liveOrder = []string{"waiting", "running", "error"}

func sidebarView(graphs []graphFile, untrusted map[string]bool, runs []*Run, selected string, sessions map[string]api.Session, collapsed map[string]bool) sdui.Node {
	if len(graphs) == 0 && len(runs) == 0 {
		return sdui.Empty("No graphs or graph runs yet")
	}
	var groups []sdui.TreeItem
	if len(graphs) > 0 {
		items := make([]sdui.TreeItem, 0, len(graphs))
		for _, g := range graphs {
			items = append(items, graphItem(g, untrusted[g.Name]))
		}
		groups = append(groups, group(groupGraphs, "Graphs", items, collapsed))
	}
	if len(runs) > 0 {
		items := make([]sdui.TreeItem, 0, len(runs))
		for _, r := range runs {
			live, detail := runLive(r, sessions), runDetail(r)
			if n := r.need(); n != nil {
				live, detail = stepWaiting, needDetail(r, n)
			}
			icon, tone := runIcon(r, live)
			items = append(items, sdui.TreeItem{
				ID:     r.ID,
				Label:  r.Title,
				Detail: detail,
				Icon:   icon,
				Tone:   tone,
				Menu:   runMenu(r),
			})
		}
		groups = append(groups, group(groupRuns, "Runs", items, collapsed))
	}
	return sdui.Tree(listID, groups, selected)
}

func group(id, label string, items []sdui.TreeItem, collapsed map[string]bool) sdui.TreeItem {
	return sdui.TreeItem{ID: id, Label: label, Detail: strconv.Itoa(len(items)), Muted: true, Action: true, HasChildren: true, Expanded: !collapsed[id], Children: items}
}

func graphItem(g graphFile, untrusted bool) sdui.TreeItem {
	item := sdui.TreeItem{ID: graphPrefix + g.Name, Label: cmp.Or(g.Title, g.Name), Icon: "doc", Detail: fmt.Sprintf("%d nodes", g.Nodes)}
	switch {
	case g.Error != "":
		item.Icon, item.Tone, item.Detail = "xmark.circle", "danger", "invalid"
	case untrusted:
		item.Tone, item.Detail = "warning", "untrusted"
	case g.Builtin:
		item.Detail = "built in"
	}
	return item
}

func graphView(g graphFile, spec *Graph, d draft, starting string, tv trustView) sdui.Node {
	id := graphPrefix + g.Name
	props := sdui.HeaderProps{Icon: graphIcon}
	if g.Title != "" {
		props.Meta = g.Name
	}
	var rows []sdui.Node
	if tv.untrusted {
		rows = append(rows, trustNotice(tv))
	}
	if g.Description != "" {
		rows = append(rows, sdui.HStack(sdui.MutedText("Description"), sdui.Card("", "", sdui.WithLines(sdui.Text(g.Description), descLines))))
	}
	var actions []sdui.Node
	switch {
	case g.Error != "":
		props.Badges = []sdui.HeaderBadge{{Text: "invalid", Tone: "danger"}}
		rows = append(rows, sdui.HStack(sdui.MutedText("Error"), sdui.WithTone(sdui.Text(g.Error), "danger")))
	case tv.untrusted:
		props.Badges = []sdui.HeaderBadge{{Text: "untrusted", Tone: "warning"}}
		actions = append(actions, automation.ActionNode(id, automation.DetailAction{Action: actTrust, Label: "Trust…", Icon: "checkmark", Variant: sdui.VariantPrimary, Shortcut: "cmd+return"}))
		rows = append(rows, sdui.HStack(sdui.MutedText("Input"), sdui.TextField(fieldInput, "", "Trust the file to run it")))
	default:
		run := sdui.WithShortcut(sdui.WithVariant(sdui.Button(id+"/submit", "Run", "play.fill"), sdui.VariantPrimary), "cmd+return")
		if starting == g.Name {
			run = sdui.WithLoading(run, "Starting…")
		} else if starting != "" {
			run = sdui.WithDisabled(run)
		}
		actions = append(actions, run)
		var value string
		if d.graph == g.Name {
			value = d.input
		}
		rows = append(rows, sdui.HStack(sdui.MutedText("Input"), sdui.TextField(fieldInput, value, "What the run works on; ↩ runs it")))
		if d.graph == g.Name && d.err != "" {
			rows = append(rows, sdui.HStack(sdui.MutedText(""), sdui.WithTone(sdui.Text(d.err), "danger")))
		}
	}
	definition := graphsDir + "/" + g.Name + graphExt
	if g.Builtin {
		definition = "Built into Clubmate"
	}
	if tv.untrusted {
		definition = trustDefinition(definition, tv)
	}
	rows = append(rows, automation.Property("Definition", definition, ""))
	if spec != nil {
		rows = append(rows, automation.Property("Turns", fmt.Sprintf("at most %d", spec.maxTurns()), ""))
	}
	header := sdui.Header(cmp.Or(g.Title, g.Name), props, actions...)
	form := sdui.WithAction(sdui.Form(id+"/"+actRun, append([]sdui.Node{header}, rows...)...), actRun)
	if spec == nil {
		return sdui.VStack(form)
	}
	var changed []string
	if tv.last != nil {
		changed = tv.changed
	}
	out := []sdui.Node{form, definitionDiagram(id, runShape(spec), changed)}
	if card, ok := changedCard(spec, tv); ok {
		out = append(out, card)
	}
	if tv.confirming {
		out = append(out, trustConfirm(id, cmp.Or(g.Title, g.Name), spec, tv))
	}
	return sdui.VStack(out...)
}

func definitionDiagram(id string, r *Run, changed []string) sdui.Node {
	nodes := make([]sdui.GraphNode, 0, len(r.Nodes))
	for _, n := range r.Nodes {
		ids, ends := nodeOuts(r, n.ID)
		outs := make([]sdui.GraphOut, 0, len(ids))
		for _, o := range ids {
			outs = append(outs, sdui.GraphOut{ID: o, Label: o, State: statePlain, End: slices.Contains(ends, o)})
		}
		node := sdui.GraphNode{ID: n.ID, Label: n.Role, Start: n.Start, Kind: nodeKind(r.Spec, n.ID), Outs: outs}
		if n.Step {
			node.Command = cmpCommand(r.Spec.node(n.ID))
			node.Tooltip = node.Command
		}
		if slices.Contains(changed, n.ID) {
			node.Tag = tagChanged
		}
		nodes = append(nodes, node)
	}
	edges := make([]sdui.GraphEdge, 0, len(r.Edges))
	for i, e := range r.Edges {
		edge := sdui.GraphEdge{From: e.From, Out: edgeOut(e), To: e.To, State: statePlain, Tooltip: edgeTooltip(r, e)}
		if max := r.Spec.Edges[i].MaxFires; max > 0 {
			edge.Label = fmt.Sprintf("at most %d", max)
		}
		edges = append(edges, edge)
	}
	return sdui.Graph(id+graphSuffix, sdui.GraphProps{Nodes: nodes, Edges: edges})
}

func mainView(r *Run, sessions map[string]api.Session, x runExtras) sdui.Node {
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
	definition := graphsDir + "/" + r.Graph + graphExt
	if r.Builtin {
		definition = "Built into Clubmate"
	}
	rows = append(rows, automation.Property("Definition", definition, ""))
	need := r.need()
	var children []sdui.Node
	switch {
	case need == nil:
	case need.Kind == needCap:
		children = append(children, capCard(r, need))
	default:
		children = append(children, approvalCard(r, need, x.pending[need.Node]))
	}
	switch node, move := r.shownMove(x.selected); {
	case move < 0:
	case r.Moves[move].Step != nil:
		children = append(children, outputPanel(r, node, move, x.live, x.summarizing))
	default:
		children = append(children, agentPanel(r, node, move, x.summarizing))
	}
	return sdui.VStack(runHeader(r, sessions, need), automation.Properties(rows...), runDiagram(r, sessions, children...))
}

func emptyView() sdui.Node {
	return sdui.EmptyState("Graphs chain agents: when one finishes, the next starts. Add a graph file, then select it here and run it.", sdui.EmptyProps{Title: "No graphs yet", Icon: graphIcon, Hint: graphsDir + "/<name>" + graphExt})
}

func runHeader(r *Run, sessions map[string]api.Session, need *runNeed) sdui.Node {
	state := "stopped"
	if r.Status == statusRunning {
		state = "running"
	}
	var badges []sdui.HeaderBadge
	switch {
	case need != nil && need.Kind == needCap:
		badges = append(badges, sdui.HeaderBadge{Text: stateLabel(stepWaiting, false) + " · round cap", Tone: statusTone(stepWaiting)})
	case need != nil:
		badges = append(badges, sdui.HeaderBadge{Text: stateLabel(stepWaiting, false), Tone: statusTone(stepWaiting)})
	default:
		badges = append(badges, sdui.HeaderBadge{Text: fmt.Sprintf("%s · turn %d of %d", state, r.Turns, r.MaxTurns)})
	}
	if n, status := attentionNode(r, sessions); n != nil && (status == "waiting" || status == "error") {
		badges = append(badges, sdui.HeaderBadge{Text: n.Role + " " + stateLabel(status, true), Tone: statusTone(status)})
	}
	var actions []sdui.Node
	switch n, status := attentionNode(r, sessions); {
	case need != nil && need.Kind == needCap:
		actions = append(actions, automation.ActionNode(r.ID, automation.DetailAction{Action: actContinue, Label: "Continue", Icon: "play.fill", Variant: sdui.VariantPrimary, Shortcut: "cmd+return"}))
	case need != nil:
	case n != nil && (status == "waiting" || status == "error"):
		actions = append(actions, automation.ActionNode(r.ID, automation.DetailAction{Action: actOpen, Label: "Open " + n.Role, Icon: "sparkles", Variant: sdui.VariantPrimary, Shortcut: "cmd+return"}))
	case r.Status == statusRunning && r.runningStep() != "":
		actions = append(actions, automation.ActionNode(r.ID, automation.DetailAction{Action: actStepOutput, Label: "Open " + roleOf(r, r.runningStep()) + " output", Icon: "terminal", Variant: sdui.VariantPrimary, Shortcut: "cmd+return"}))
	case n != nil:
		actions = append(actions, automation.ActionNode(r.ID, automation.DetailAction{Action: actOpen, Label: "Open " + n.Role, Icon: "sparkles", Variant: sdui.VariantPrimary, Shortcut: "cmd+return"}))
	}
	if r.Status == statusRunning {
		actions = append(actions, automation.ActionNode(r.ID, automation.DetailAction{Action: actStop, Label: "Stop run", Icon: "stop.circle", Variant: sdui.VariantSecondary, Shortcut: "cmd+.", Confirm: stopConfirm(r)}))
	} else {
		actions = append(actions, sdui.WithOverflow(automation.ActionNode(r.ID, automation.DetailAction{Action: actRemove, Label: "Remove", Icon: "trash", Variant: sdui.VariantDanger, Shortcut: "cmd+delete", Confirm: removeConfirm(r)})))
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
	case slices.Contains(liveOrder, live):
		return statusIcon(live), statusTone(live)
	}
	return graphIcon, ""
}

func statusIcon(status string) string {
	switch status {
	case "waiting":
		return "exclamationmark.circle"
	case "error", "fail":
		return "xmark.circle"
	case "running":
		return "play.circle"
	case "idle":
		return "circle.dashed"
	}
	return "checkmark.circle"
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

func runDiagram(r *Run, sessions map[string]api.Session, children ...sdui.Node) sdui.Node {
	last, counts, end := r.lastEdges(), r.edgeCounts(), r.endMove()
	nodes := make([]sdui.GraphNode, 0, len(r.Nodes))
	for _, n := range r.Nodes {
		nodes = append(nodes, graphNode(r, n, sessions, end, last, counts))
	}
	edges := make([]sdui.GraphEdge, 0, len(r.Edges))
	for i, e := range r.Edges {
		held := r.Status == statusRunning && r.heldEdge(i)
		edge := sdui.GraphEdge{From: e.From, Out: edgeOut(e), To: e.To, State: edgeState(slices.Contains(last, i), counts[i]), Tooltip: edgeTooltip(r, e), Label: edgeLabel(r, i, counts[i], held)}
		if held {
			edge.State = sdui.EdgeHeld
		}
		if r.Status == statusRunning {
			edge.Ref = e.Trigger
		}
		edges = append(edges, edge)
	}
	return sdui.Graph(r.ID+graphSuffix, sdui.GraphProps{Nodes: nodes, Edges: edges, Moves: graphMoves(r, last, end), Reached: end >= 0}, children...)
}

func graphNode(r *Run, n RunNode, sessions map[string]api.Session, end int, last, counts []int) sdui.GraphNode {
	if n.Step {
		return stepNode(r, n, end, last, counts)
	}
	s := sessions[n.Session]
	status := nodeStatus(s, sessions != nil)
	turns, moved := 0, false
	for _, m := range r.Moves {
		if m.Node == n.ID {
			moved = true
			if !m.Open {
				turns++
			}
		}
	}
	outcome := ""
	if end >= 0 && r.Moves[end].Node == n.ID {
		outcome = r.Moves[end].End
	}
	state, glyph := nodeState(status, turnsLabel(r, turns), !moved && (len(r.Moves) > 0 || status == "idle"), outcome)
	return sdui.GraphNode{
		ID:        n.ID,
		Label:     n.Role,
		State:     state,
		Tone:      statusTone(glyph),
		Icon:      statusIcon(glyph),
		Attention: glyph == "waiting",
		Pending:   glyph == "idle",
		Start:     n.Start,
		Tooltip:   s.Title,
		Kind:      nodeKind(r.Spec, n.ID),
		Outs:      graphOuts(r, n.ID, end, last, counts),
	}
}

func nodeState(status, turns string, pending bool, outcome string) (string, string) {
	switch {
	case outcome != "":
		return outcome, "done"
	case status == "waiting" || status == "error":
		return stateLabel(status, false), status
	case status == "running":
		return status, status
	case status == "gone" || status == api.StatusClosed:
		return "closed" + turns, "done"
	case pending:
		return "not started", "idle"
	}
	return "idle" + turns, "done"
}

func turnsLabel(r *Run, turns int) string {
	if len(r.Moves) == 0 {
		return ""
	}
	return fmt.Sprintf(" · %d turns", turns)
}

func nodeOuts(r *Run, node string) ([]string, []string) {
	var ids, ends []string
	if r.Spec != nil {
		if spec := r.Spec.node(node); spec != nil {
			ids, ends = slices.Clone(spec.Outcomes), spec.End
			if spec.step() {
				return append(r.Spec.waysOut(node), onError), ends
			}
		}
	}
	for _, e := range r.Edges {
		if e.From == node && !slices.Contains(ids, edgeOut(e)) {
			ids = append(ids, edgeOut(e))
		}
	}
	return ids, ends
}

func graphOuts(r *Run, node string, end int, last, counts []int) []sdui.GraphOut {
	ids, ends := nodeOuts(r, node)
	outs := make([]sdui.GraphOut, 0, len(ids))
	for _, id := range ids {
		o := sdui.GraphOut{ID: id, Label: id, End: slices.Contains(ends, id)}
		lit := false
		for i, e := range r.Edges {
			if e.From == node && edgeOut(e) == id {
				o.Count = max(o.Count, counts[i])
				lit = lit || slices.Contains(last, i)
			}
		}
		if o.End {
			ended := 0
			for _, m := range r.Moves {
				if m.Node == node && m.End == id {
					ended++
				}
			}
			o.Count = max(o.Count, ended)
			lit = lit || end >= 0 && r.Moves[end].Node == node && r.Moves[end].End == id
		}
		o.State = edgeState(lit, o.Count)
		outs = append(outs, o)
	}
	return outs
}

func graphMoves(r *Run, last []int, end int) []sdui.GraphMove {
	running := r.Status == statusRunning
	final := -1
	for i, m := range r.Moves {
		if m.Edge >= 0 {
			final = i
		}
	}
	moves := make([]sdui.GraphMove, 0, len(r.Moves))
	for i, m := range r.Moves {
		g := sdui.GraphMove{
			Node:    m.Node,
			Cause:   m.Cause,
			Tooltip: fmt.Sprintf("move %d · %s · %s", i+1, roleOf(r, m.Node), m.At.Local().Format(clockLayout)),
			Current: m.Open && running,
			User:    m.User,
		}
		if m.User {
			g.Label = "you"
		}
		if m.Edge >= 0 && m.Edge < len(r.Edges) {
			g.Out = edgeOut(r.Edges[m.Edge])
			g.Last = slices.Contains(last, m.Edge) && (g.Current || i == final)
		}
		if i == end {
			g.End = m.End
		}
		if m.Step != nil {
			g.Bar, g.Tone = stepBar(m.Step), statusTone(stepGlyph(stepStatus(m.Step)))
		}
		moves = append(moves, g)
	}
	for i, n := range moveRounds(r.Moves) {
		moves[i].Round = n
	}
	return moves
}

func stepBar(st *StepMove) string {
	switch status := stepStatus(st); status {
	case stepRunning, stepWaiting:
		return stateLabel(status, false)
	case onError:
		return onError + " · " + cmp.Or(st.Reason, "failed")
	default:
		return status + " · " + clockDuration(time.Duration(st.Duration)*time.Millisecond)
	}
}

func clockDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

func moveRounds(moves []Move) []int {
	count := map[string]int{}
	head := ""
	for _, m := range moves {
		if count[m.Node]++; count[m.Node] == 2 {
			head = m.Node
			break
		}
	}
	out := make([]int, len(moves))
	if head == "" {
		return out
	}
	round := 0
	for i, m := range moves {
		if m.Node == head {
			round++
		}
		out[i] = round
	}
	return out
}

func (r *Run) endMove() int {
	if r.Status == statusRunning {
		return -1
	}
	return slices.IndexFunc(r.Moves, func(m Move) bool { return m.End != "" })
}

func edgeOut(e RunEdge) string {
	if e.On == onOutcome {
		return e.Outcome
	}
	return e.On
}

func edgeTooltip(r *Run, e RunEdge) string {
	role := func(id string) string {
		if n := r.node(id); n != nil {
			return n.Role
		}
		return id
	}
	var when string
	switch e.On {
	case onError:
		when = "fails"
	case onAsks:
		when = "asks you"
	case onOutcome:
		when = "reports " + e.Outcome
	default:
		when = "is done"
	}
	return "when " + role(e.From) + " " + when + " → prompt " + role(e.To)
}

func edgeState(last bool, count int) string {
	switch {
	case last:
		return "last"
	case count > 0:
		return "taken"
	}
	return "never"
}

func stepNode(r *Run, n RunNode, end int, last, counts []int) sdui.GraphNode {
	st := r.lastStep(n.ID)
	glyph := stepGlyph(stepStatus(st))
	command, timeout := "", 0
	if r.Spec != nil {
		spec := r.Spec.node(n.ID)
		command = cmpCommand(spec)
		if st != nil && st.Status == stepRunning {
			timeout = int(spec.timeout().Seconds())
		}
	}
	return sdui.GraphNode{
		ID:        n.ID,
		Label:     n.Role,
		State:     stepState(st),
		Tone:      statusTone(glyph),
		Icon:      statusIcon(glyph),
		Attention: glyph == stepWaiting,
		Pending:   glyph == "idle",
		Tooltip:   command,
		Kind:      nodeKind(r.Spec, n.ID),
		Command:   command,
		Since:     stepSince(st),
		Timeout:   timeout,
		Outs:      graphOuts(r, n.ID, end, last, counts),
	}
}

func stepState(st *StepMove) string {
	switch status := stepStatus(st); status {
	case notStarted, stepRunning:
		return status
	case stepWaiting:
		return stateLabel(stepWaiting, false)
	case onError:
		return onError + " · " + cmp.Or(st.Reason, "failed")
	default:
		return status + " · " + humanDuration(time.Duration(st.Duration)*time.Millisecond)
	}
}

func humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min %02d s", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d h %02d min", int(d.Hours()), int(d.Minutes())%60)
}
