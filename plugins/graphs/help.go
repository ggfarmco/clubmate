package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

const (
	partsID   = "parts"
	askTitle  = "New graph"
	helpMeta  = "a graph chains agent sessions: when one ends its turn, the next is prompted · every edge is a trigger · a step runs a command and its exit code decides where the run goes"
	helpParts = "Parts of a graph · ↩ asks an agent for a graph that uses the selected part"
	askBase   = "Help me write an agent graph for this workspace."
	askPart   = "Help me write an agent graph for this workspace that uses %s (%s)."
	askRest   = " Ask me what roles and hand-offs I want, then write it to .clubmate/graphs/<name>.json in this repo (or ~/.clubmate/graphs/<name>.json if I want it in every workspace) and check with graphs_list that it shows without an error. Don't run it. Tell me it shows under Graphs and, if it has steps, that I must Trust it there first." +
		" The file is JSON and unknown fields are errors: {title, description, maxTurns?, nodes: [{id (lowercase), role, start?: true, and one of prompt (an agent), run (a shell command) or command + args (a Clubmate command) (a step), outcomes?: [words the agent reports], end?: [outcomes that finish the run], exits?: {\"3\": \"flaky\"} (a step's words by exit code)}], edges: [{from, to, on: done|asks|error|outcome, outcome?, text?, maxFires?}]}." +
		" At least one agent node starts, a step can't; a step with an on: outcome edge is a check (pass on exit 0, fail, or its exits word), any other step is done or error. In a prompt {{input}} is the run's input and {{node.<id>}} another node's session. graphs_run's description says how a run goes."
)

type graphPart struct {
	id     string
	name   string
	does   string
	field  string
	phrase string
	uses   func(*Graph) bool
}

var graphParts = []graphPart{
	{"agent", "Agent node", "an agent session plays a role", "prompt · start", "agent nodes", func(g *Graph) bool {
		return slices.ContainsFunc(g.Nodes, func(n Node) bool { return !n.step() })
	}},
	{"check", "Check step", "a command whose exit code routes", "run · exits", "a check step", func(g *Graph) bool {
		return slices.ContainsFunc(g.Nodes, func(n Node) bool { return g.check(n.ID) })
	}},
	{"action", "Action step", "a command that is done or errors", "run · command", "an action step", func(g *Graph) bool {
		return slices.ContainsFunc(g.Nodes, func(n Node) bool { return n.step() && !g.check(n.ID) })
	}},
	{onDone, "Hand-off", "the next node is prompted", "on: done", "a hand-off on done", edgeOn(onDone)},
	{onAsks, "Asks or fails", "a turn asks you or fails", "on: asks · error", "edges on asks or error", edgeOn(onAsks, onError)},
	{onOutcome, "Outcome", "an agent reports a word", "outcomes · on: outcome", "outcome edges", edgeOn(onOutcome)},
	{"maxFires", "Round cap", "past N rounds, waits for you", "maxFires", "a round cap", func(g *Graph) bool {
		return slices.ContainsFunc(g.Edges, func(e Edge) bool { return e.MaxFires > 0 })
	}},
	{"end", "End", "an outcome finishes the run", "end", "an end outcome", func(g *Graph) bool {
		return slices.ContainsFunc(g.Nodes, func(n Node) bool { return len(n.End) > 0 })
	}},
}

var helpFacts = [][2]string{
	{"Files", ".clubmate/graphs/<name>.json in the repo · ~/.clubmate/graphs/ for every workspace · copy a built-in one there to change it"},
	{"Trust", "a graph with steps runs once you trust its steps in this repo; changing a step or the wiring untrusts it"},
	{"Budget", "a run stops after maxTurns agent turns (default 20) or maxSteps steps (default 50)"},
	{"Needs you", "a step's command outside the allow list, or an edge past its round cap, waits for you"},
	{"Runs", "edges between agents are triggers and show in Triggers; stopping a run deletes them"},
}

func edgeOn(kinds ...string) func(*Graph) bool {
	return func(g *Graph) bool {
		return slices.ContainsFunc(g.Edges, func(e Edge) bool { return slices.Contains(kinds, e.On) })
	}
}

func yourGraphs(graphs []graphFile) []*Graph {
	var specs []*Graph
	for _, g := range graphs {
		if g.Builtin || g.Error != "" {
			continue
		}
		data, err := os.ReadFile(g.Path)
		if err != nil {
			logError(err)
			continue
		}
		if spec, err := parseGraph(g.Name, data); err == nil {
			specs = append(specs, spec)
		}
	}
	return specs
}

func helpView(specs []*Graph) sdui.Node {
	header := automation.HelpHeader("How graphs work", helpMeta, "Ask an agent to write one")
	rows := make([]sdui.TableRow, 0, len(graphParts))
	for _, p := range graphParts {
		n := 0
		for _, g := range specs {
			if p.uses(g) {
				n++
			}
		}
		yours := ""
		switch n {
		case 0:
		case 1:
			yours = "1 graph"
		default:
			yours = strconv.Itoa(n) + " graphs"
		}
		rows = append(rows, sdui.TableRow{ID: p.id, Cells: []sdui.Cell{
			{Text: p.name}, {Text: p.does}, {Text: p.field}, {Text: yours}, {Action: automation.ActAsk, Label: "Ask an agent"},
		}})
	}
	columns := []sdui.Column{
		{ID: "part", Title: "Part", Width: 130},
		{ID: "does", Title: "Does"},
		{ID: "field", Title: "Field", Width: 170, Priority: 3},
		{ID: "yours", Title: "Yours", Width: 90, Priority: 1},
		{ID: "ask", Title: "", Width: 110},
	}
	body := sdui.VStack(
		ghostFlow(),
		sdui.MutedText(helpParts),
		sdui.Table(partsID, columns, rows, "", sdui.Sort{}),
		automation.Facts(helpFacts),
	)
	return sdui.VStack(header, sdui.Scroll(body))
}

func ghostFlow() sdui.Node {
	ghost := func(outs ...[2]string) []sdui.FlowOut {
		flow := make([]sdui.FlowOut, len(outs))
		for i, o := range outs {
			flow[i] = sdui.FlowOut{ID: o[0], Label: o[1], State: sdui.EdgeGhost}
		}
		return flow
	}
	return sdui.Flow(automation.HelpID+"/flow", sdui.FlowProps{
		Cards: []sdui.FlowCard{
			{ID: "coder", Label: "Coder", Ghost: true, Outs: ghost([2]string{onDone, onDone}, [2]string{onError, onError})},
			{ID: "reviewer", Label: "Reviewer", Ghost: true, Outs: ghost([2]string{"approved", "approved → end"}, [2]string{"changes", "changes → Coder"})},
		},
		Edges: []sdui.FlowEdge{{From: "coder", Out: onDone, To: "reviewer", State: sdui.EdgeGhost}},
	})
}

func askPrompt(id string) string {
	i := slices.IndexFunc(graphParts, func(p graphPart) bool { return p.id == id })
	if i < 0 {
		return askBase + askRest
	}
	return fmt.Sprintf(askPart, graphParts[i].phrase, graphParts[i].field) + askRest
}

func (a *app) askAgent(ctx context.Context, workspace, id string) error {
	return agentcall.Ask(ctx, a.host, workspace, askTitle, askPrompt(id))
}

func (a *app) askFromRow(ctx context.Context, ev pluginsdk.UIEvent) error {
	if ev.Action != automation.ActAsk {
		return nil
	}
	var id string
	if err := json.Unmarshal(ev.Value, &id); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	return a.askAgent(ctx, ev.WorkspaceID, id)
}
