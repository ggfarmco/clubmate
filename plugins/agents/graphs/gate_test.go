package graphs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

const gatePipeline = `{"title":"Pipeline","nodes":[{"id":"simplify","prompt":"Simplify.","start":true,"model":"sonnet"},{"id":"review","prompt":"Review: {{input}}","model":"fable","outcomes":["approved","changes"]}],"edges":[{"from":"simplify","to":"review"}]}`

type gateFixture struct {
	a     *app
	e     *Engine
	core  *fakeCore
	repo  string
	nodes []GateNode
	ended []string
}

func newGateFixture(t *testing.T) *gateFixture {
	t.Helper()
	f := &gateFixture{core: &fakeCore{}, repo: t.TempDir()}
	f.a = testApp(t, f.core)
	f.a.spawn = func(fn func()) { fn() }
	f.a.gateEnded = func(workspace, run string) { f.ended = append(f.ended, workspace+"/"+run) }
	f.e = &Engine{a: f.a}
	f.write(t, "pipeline", gatePipeline)
	return f
}

func (f *gateFixture) write(t *testing.T, name, graph string) {
	t.Helper()
	dir := filepath.Join(f.repo, graphsDir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+graphExt), []byte(graph), 0o644))
}

func (f *gateFixture) start(t *testing.T, pin string) (string, string, error) {
	t.Helper()
	return f.e.StartGate(t.Context(), GateRun{Workspace: "ws1", Graph: "pipeline", Pin: pin, Repo: f.repo, Input: "Land {{title}} at tree T", Other: "Land it, no verdict", NewNode: func(_ context.Context, n GateNode) (string, error) {
		f.nodes = append(f.nodes, n)
		return fmt.Sprintf("agt_g%d", len(f.nodes)), nil
	}})
}

func (f *gateFixture) pin(t *testing.T) string {
	t.Helper()
	hash, valid, err := f.e.GateGraph(t.Context(), f.repo, "pipeline")
	require.NoError(t, err)
	require.True(t, valid)
	return hash
}

func TestGateGraphHashCoversPrompts(t *testing.T) {
	f := newGateFixture(t)
	before := f.pin(t)
	f.write(t, "pipeline", gatePipeline[:len(gatePipeline)-1]+`,"description":"x"}`)
	f.write(t, "solo", `{"nodes":[{"id":"a","prompt":"p","start":true}]}`)

	after := f.pin(t)
	_, valid, err := f.e.GateGraph(t.Context(), f.repo, "solo")

	require.NoError(t, err)
	assert.NotEqual(t, before, after)
	assert.False(t, valid)
}

func TestStartGateRefusesAnUnpinnedGraph(t *testing.T) {
	f := newGateFixture(t)

	_, _, err := f.start(t, "other")

	require.ErrorIs(t, err, ErrGateGraphChanged)
	assert.Empty(t, f.nodes)
}

func TestStartGateStartsNodesItself(t *testing.T) {
	f := newGateFixture(t)

	_, verdict, err := f.start(t, f.pin(t))

	require.NoError(t, err)
	assert.Equal(t, "agt_g2", verdict)
	assert.Equal(t, []GateNode{{Title: "simplify", Model: "sonnet"}, {Title: "review", Model: "fable", Verdict: true}}, stripRun(f.nodes))
	assert.NotContains(t, f.core.ids(), "agents.new")
	assert.NotContains(t, f.core.ids(), "triggers.add")
	first := prompts(t, f.core, "agt_g1")
	require.Len(t, first, 1)
	assert.Contains(t, first[0], "Land it, no verdict")
	assert.NotContains(t, first[0], "Land {{title}} at tree T")
	assert.Contains(t, first[0], "decides this gate (review): put your findings in your reply and don't call agents_land_verdict; it refuses you")
}

func TestGateRoundCarriesItsInputAlongEdges(t *testing.T) {
	f := newGateFixture(t)
	run, _, err := f.start(t, f.pin(t))
	require.NoError(t, err)
	turnDone(t, f.a, "agt_g1", 1)

	require.NoError(t, f.e.PromptGate(t.Context(), run, "round two delta", "round two delta, no verdict", ""))
	turnDone(t, f.a, "agt_g1", 2)

	assert.Equal(t, "round two delta, no verdict", prompts(t, f.core, "agt_g1")[1])
	review := prompts(t, f.core, "agt_g2")
	require.Len(t, review, 2)
	assert.Contains(t, review[0], "Review: Land {{title}} at tree T")
	assert.NotContains(t, review[0], "triggers_emit")
	assert.Contains(t, review[1], "Review: round two delta")
}

func TestGateRunEndsOnlyWhenStopped(t *testing.T) {
	f := newGateFixture(t)
	run, _, err := f.start(t, f.pin(t))
	require.NoError(t, err)

	f.e.GateOutcome(t.Context(), run, "agt_g2", "changes")
	running := f.e.GateRunning(t.Context(), run)
	require.NoError(t, f.e.StopGate(t.Context(), run))

	assert.True(t, running)
	assert.False(t, f.e.GateRunning(t.Context(), run))
	assert.Equal(t, []string{"ws1/" + run}, f.ended)
}

func TestGateRunStopsWhenItsRoundEndsBeforeTheVerdictNode(t *testing.T) {
	f := newGateFixture(t)
	f.write(t, "pipeline", `{"nodes":[{"id":"simplify","prompt":"Simplify.","start":true},{"id":"review","prompt":"Review: {{input}}","outcomes":["approved","changes"]}],"edges":[{"from":"simplify","to":"review","on":"error"}]}`)
	run, _, err := f.start(t, f.pin(t))
	require.NoError(t, err)

	turnDone(t, f.a, "agt_g1", 1)

	assert.False(t, f.e.GateRunning(t.Context(), run))
	assert.Equal(t, []string{"ws1/" + run}, f.ended)
}

func TestGateRoundWaitsForAnOutcomeThatComesAfterTheTurnEnd(t *testing.T) {
	f := newGateFixture(t)
	f.write(t, "pipeline", `{"nodes":[{"id":"simplify","prompt":"Simplify.","start":true,"outcomes":["ready"]},{"id":"review","prompt":"Review: {{input}}","outcomes":["approved","changes"]}],"edges":[{"from":"simplify","to":"review","on":"outcome","outcome":"ready"}]}`)
	var rechecks []func()
	f.a.later = func(_ time.Duration, fn func()) { rechecks = append(rechecks, fn) }
	run, _, err := f.start(t, f.pin(t))
	require.NoError(t, err)

	turnDone(t, f.a, "agt_g1", 1)
	waiting := f.e.GateRunning(t.Context(), run)
	f.a.busEvent(t.Context(), nil, "", pluginsdk.Event{Source: triggersSource, Topic: eventPrefix + f.a.runs[run].outcomeEvent("simplify"), Payload: json.RawMessage(`{"workspace":"ws1","by":"agt_g1","outcome":"ready"}`)})
	require.Len(t, rechecks, 1)
	rechecks[0]()

	assert.True(t, waiting)
	assert.True(t, f.e.GateRunning(t.Context(), run))
	assert.NotEmpty(t, prompts(t, f.core, "agt_g2"))
	assert.Empty(t, f.ended)
}

func TestGateRoundStopsWhenNoOutcomeComesAfterTheTurnEnd(t *testing.T) {
	f := newGateFixture(t)
	f.write(t, "pipeline", `{"nodes":[{"id":"simplify","prompt":"Simplify.","start":true,"outcomes":["ready"]},{"id":"review","prompt":"Review: {{input}}","outcomes":["approved","changes"]}],"edges":[{"from":"simplify","to":"review","on":"outcome","outcome":"ready"}]}`)
	var rechecks []func()
	f.a.later = func(_ time.Duration, fn func()) { rechecks = append(rechecks, fn) }
	run, _, err := f.start(t, f.pin(t))
	require.NoError(t, err)

	turnDone(t, f.a, "agt_g1", 1)
	require.Len(t, rechecks, 1)
	rechecks[0]()

	assert.False(t, f.e.GateRunning(t.Context(), run))
	assert.Equal(t, []string{"ws1/" + run}, f.ended)
}

func TestGateRunStateIsAnErrorWhileTheEngineIsNotReady(t *testing.T) {
	t.Parallel()
	e := &Engine{a: newApp()}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	running, err := e.GateRunState(ctx, "gr_1")

	assert.False(t, running)
	require.ErrorIs(t, err, context.Canceled)
}

func stripRun(nodes []GateNode) []GateNode {
	out := make([]GateNode, 0, len(nodes))
	for _, n := range nodes {
		n.Run = ""
		out = append(out, n)
	}
	return out
}

func TestStartGateClosesItsNodesWhenKickoffFails(t *testing.T) {
	f := newGateFixture(t)
	f.core.errs = map[string]error{"agents.prompt": errors.New("session is closed")}

	_, _, err := f.start(t, f.pin(t))

	require.Error(t, err)
	var closed []string
	for _, raw := range called(f.core, "agents.close") {
		var args sessionArgs
		require.NoError(t, json.Unmarshal(raw, &args))
		closed = append(closed, args.Session)
	}
	assert.ElementsMatch(t, []string{"agt_g1", "agt_g2"}, closed)
}

func TestGateGraphChoicesSayWhereEachComesFromAndWhetherItDecides(t *testing.T) {
	t.Parallel()
	f := newGateFixture(t)
	f.write(t, "notes", `{"nodes":[{"id":"write","prompt":"Write notes.","start":true}]}`)

	byName := map[string]GateGraphInfo{}
	for _, c := range f.e.GateGraphChoices(context.Background(), f.repo) {
		byName[c.Name] = c
	}

	assert.Equal(t, []any{FromBuiltin, true}, []any{byName["fable-review"].From, byName["fable-review"].Decides})
	assert.Equal(t, []any{FromRepo, false}, []any{byName["notes"].From, byName["notes"].Decides})
	pipeline := byName["pipeline"]
	require.Len(t, pipeline.Nodes, 2)
	review := pipeline.Nodes[1]
	assert.Equal(t, []any{"review", "Review: {{input}}", "fable", true}, []any{review.Title, review.Prompt, review.Model, review.Verdict})
	assert.JSONEq(t, `{"id":"review","outcomes":["approved","changes"]}`, review.Other)
	hash, _, err := f.e.GateGraph(context.Background(), f.repo, "pipeline")
	require.NoError(t, err)
	assert.Equal(t, hash, pipeline.Hash)
}

func TestGateEdgeTextGivesANonDecidingTargetTheOtherInput(t *testing.T) {
	f := newGateFixture(t)
	f.write(t, "pipeline", `{"nodes":[{"id":"review","prompt":"Review.","start":true,"outcomes":["approved","changes"]},{"id":"note","prompt":"Note."}],"edges":[{"from":"review","to":"note","text":"Context: {{input}}"}]}`)
	_, _, err := f.start(t, f.pin(t))
	require.NoError(t, err)

	turnDone(t, f.a, "agt_g1", 1)

	note := prompts(t, f.core, "agt_g2")
	require.NotEmpty(t, note)
	assert.Contains(t, note[len(note)-1], "Context: Land it, no verdict")
}

func TestGateInfoShowsEveryHashedChange(t *testing.T) {
	base := func() *Graph {
		return &Graph{Name: "g", Title: "G", Nodes: []Node{
			{ID: "a", Role: "A", Prompt: "Review.", Model: "fable", Start: true, Outcomes: []string{"approved", "changes"}},
			{ID: "b", Role: "B", Run: "make lint"},
		}, Edges: []Edge{{From: "a", To: "b", Outcome: "changes", Text: "Fix it."}}}
	}
	shown := func(g *Graph) string {
		info := gateInfo(g, fromProject)
		out := info.Head + info.Edges
		for _, n := range info.Nodes {
			out += fmt.Sprint(n.ID, n.Title, n.Command, n.Prompt, n.Model, n.Effort, n.Other)
		}
		return out
	}
	changes := map[string]func(g *Graph){
		"title":         func(g *Graph) { g.Title = "H" },
		"description":   func(g *Graph) { g.Description = "d" },
		"maxTurns":      func(g *Graph) { g.MaxTurns = 3 },
		"maxSteps":      func(g *Graph) { g.MaxSteps = 3 },
		"role":          func(g *Graph) { g.Nodes[0].Role = "Z" },
		"agent prompt":  func(g *Graph) { g.Nodes[0].Prompt = "Approve." },
		"step prompt":   func(g *Graph) { g.Nodes[1].Prompt = "x" },
		"step run":      func(g *Graph) { g.Nodes[1].Run = "make ship" },
		"agent command": func(g *Graph) { g.Nodes[0].Command, g.Nodes[0].Args = "", json.RawMessage(`{"x":1}`) },
		"step args":     func(g *Graph) { g.Nodes[1].Args = json.RawMessage(`{"x":1}`) },
		"in":            func(g *Graph) { g.Nodes[1].In = "main" },
		"timeout":       func(g *Graph) { g.Nodes[1].Timeout = 9 },
		"exits":         func(g *Graph) { g.Nodes[1].Exits = map[string]string{"1": "changes"} },
		"start":         func(g *Graph) { g.Nodes[1].Start = true },
		"branch":        func(g *Graph) { g.Nodes[0].Branch = "x" },
		"afk":           func(g *Graph) { g.Nodes[0].Afk = true },
		"model":         func(g *Graph) { g.Nodes[0].Model = "opus" },
		"effort":        func(g *Graph) { g.Nodes[0].Effort = "high" },
		"outcomes":      func(g *Graph) { g.Nodes[0].Outcomes = []string{"approved"} },
		"end":           func(g *Graph) { g.Nodes[0].End = []string{"approved"} },
		"node order":    func(g *Graph) { g.Nodes[0], g.Nodes[1] = g.Nodes[1], g.Nodes[0] },
		"edge":          func(g *Graph) { g.Edges[0].MaxFires = 2 },
	}
	for name, change := range changes {
		g := base()
		change(g)
		assert.NotEqual(t, graphHash(base()), graphHash(g), name)
		assert.NotEqual(t, shown(base()), shown(g), name)
	}
}
