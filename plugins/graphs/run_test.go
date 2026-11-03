package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/automation"
)

type memStore struct{}

func (memStore) Save(context.Context, *Run) error { return nil }

func (memStore) Remove(context.Context, *Run) error { return nil }

type fakeCore struct {
	calls     []automation.CommandRequest
	next      int
	worktrees string
	errs      map[string]error
	block     map[string]bool
}

func (f *fakeCore) run(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error) {
	f.calls = append(f.calls, req)
	if f.block[req.ID] {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := f.errs[req.ID]; err != nil {
		return nil, err
	}
	if req.ID != "git.worktrees" {
		f.next++
	}
	switch req.ID {
	case "agents.new":
		return json.RawMessage(fmt.Sprintf(`{"session":"agt_%d"}`, f.next)), nil
	case "triggers.add":
		return json.RawMessage(fmt.Sprintf(`{"id":"tr_%d"}`, f.next)), nil
	case "git.worktrees":
		return json.RawMessage(cmp.Or(f.worktrees, `[]`)), nil
	}
	return json.RawMessage(`{}`), nil
}

func (f *fakeCore) ids() []string {
	ids := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		ids = append(ids, c.ID)
	}
	return ids
}

func testApp(t *testing.T, core *fakeCore) *app {
	t.Helper()
	a := newApp()
	a.store = memStore{}
	a.now = func() time.Time { return time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC) }
	a.runCommand = core.run
	a.callStep = core.run
	kv := map[string]json.RawMessage{}
	a.kvGet = func(_ context.Context, key string, out any) (bool, error) {
		raw, ok := kv[key]
		if !ok {
			return false, nil
		}
		return true, json.Unmarshal(raw, out)
	}
	a.kvSet = func(_ context.Context, key string, v any) error {
		raw, err := json.Marshal(v)
		kv[key] = raw
		return err
	}
	a.home = t.TempDir()
	a.managedPath = filepath.Join(a.home, "managed.json")
	a.render = func(context.Context, string) error { return nil }
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_boss", Worktree: "testdata"}}, nil
	}
	a.workspace = func(context.Context, string) (pluginsdk.Workspace, error) {
		return pluginsdk.Workspace{ID: "ws1", Folders: []pluginsdk.Folder{{Path: "testdata"}}}, nil
	}
	close(a.ready)
	return a
}

func runGraph(t *testing.T, a *app) *Run {
	t.Helper()
	return runNamed(t, a, "review-loop")
}

func runNamed(t *testing.T, a *app, graph string) *Run {
	t.Helper()
	return runWith(t, a, graph, "add a flag")
}

func trustGraph(t *testing.T, a *app, graph string) error {
	t.Helper()
	_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.trust", WorkspaceID: "ws1", Args: json.RawMessage(`{"graph":"` + graph + `"}`)})
	return err
}

func runWith(t *testing.T, a *app, graph, input string) *Run {
	t.Helper()
	require.NoError(t, trustGraph(t, a, graph))
	args, err := json.Marshal(runArgs{Graph: graph, Input: input})
	require.NoError(t, err)
	out, err := a.command(t.Context(), nil, pluginsdk.CommandRun{
		ID:          "graphs.run",
		WorkspaceID: "ws1",
		Args:        args,
		Agent:       true,
		Caller:      "agt_boss",
	})
	require.NoError(t, err)
	r, ok := out.(*Run)
	require.True(t, ok)
	return r
}

func TestRunInstantiatesGraph(t *testing.T) {
	core := &fakeCore{}
	r := runGraph(t, testApp(t, core))

	assert.Equal(t, []string{"agents.new", "agents.new", "triggers.add", "triggers.add", "triggers.add", "agents.prompt"}, core.ids())
	for _, c := range core.calls {
		assert.True(t, c.Agent)
		assert.Equal(t, "agt_boss", c.Caller)
	}
	assert.Equal(t, []RunNode{{ID: "coder", Role: "Coder", Start: true, Session: "agt_1"}, {ID: "reviewer", Role: "Reviewer", Session: "agt_2"}}, r.Nodes)
	assert.Equal(t, "tr_4", r.Edges[1].Trigger)

	var node newArgs
	require.NoError(t, json.Unmarshal(core.calls[1].Args, &node))
	assert.Equal(t, []string{r.ID, r.Title}, []string{node.Run, node.RunTitle})
	assert.Equal(t, []api.RunAfter{{Role: "Coder", On: onDone}, {Role: "Coder", On: onError}}, node.RunAfter)

	var done, outcome triggerArgs
	require.NoError(t, json.Unmarshal(core.calls[2].Args, &done))
	require.NoError(t, json.Unmarshal(core.calls[3].Args, &outcome))
	assert.Equal(t, api.TopicTurn, done.On)
	assert.JSONEq(t, `{"session":"agt_1","status":"done"}`, done.Match)
	assert.Equal(t, "event.graph."+r.ID+".reviewer", outcome.On)
	assert.JSONEq(t, `{"by":"agt_2","outcome":"changes"}`, outcome.Match)

	var toReviewer, kickoff promptArgs
	require.NoError(t, json.Unmarshal([]byte(done.Args), &toReviewer))
	require.NoError(t, json.Unmarshal(core.calls[5].Args, &kickoff))
	assert.Equal(t, "agt_2", toReviewer.Session)
	assert.Contains(t, toReviewer.Text, "Review the latest commits of session agt_1")
	assert.Contains(t, toReviewer.Text, `name "graph.`+r.ID+`.reviewer"`)
	assert.Contains(t, toReviewer.Text, "Its last reply (possibly cut; the whole transcript is in the clubmate tool agents_transcript {\"session\":\"agt_1\"}):\n\n{{payload.reply}}")
	assert.Equal(t, "agt_1", kickoff.Session)
	assert.Contains(t, kickoff.Text, "Task: add a flag")
}

func TestRunCreatesNoSessionOrTriggerForSteps(t *testing.T) {
	core := &fakeCore{}
	a := testApp(t, core)
	r := runNamed(t, a, "gate")
	assert.Equal(t, []string{"agents.new", "agents.new", "triggers.add", "agents.prompt"}, slices.DeleteFunc(core.ids(), func(id string) bool { return id == "git.worktrees" }))
	assert.JSONEq(t, `true`, string(mustField(t, called(core, "agents.new")[0], "afk")))
	assert.Empty(t, r.node("tests").Session)
	assert.True(t, r.node("tests").Step)
	assert.True(t, r.Edges[0].Owned)
	assert.Empty(t, r.Edges[0].Trigger)
	assert.False(t, r.Edges[5].Owned)
	assert.NotEmpty(t, r.Edges[5].Trigger)
	assert.Equal(t, 50, r.MaxSteps)
}

func mustField(t *testing.T, raw json.RawMessage, key string) json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m))
	require.Contains(t, m, key)
	return m[key]
}

func TestDoneEdgeLeavesQuestionsToAsksEdge(t *testing.T) {
	g := &Graph{Name: "g", Nodes: []Node{{ID: "a", Prompt: "p", Start: true}, {ID: "b", Prompt: "p"}}, Edges: []Edge{{From: "a", To: "b", On: onDone}, {From: "a", To: "b", On: onAsks}}}
	r, err := newRun(g, "ws1", "", false, "", time.Now())
	require.NoError(t, err)
	r.Nodes[0].Session, r.Nodes[1].Session = "agt_a", "agt_b"
	x := &runner{run: r}

	done, err := x.edgeTrigger(g, g.Edges[0])

	require.NoError(t, err)
	assert.JSONEq(t, `{"session":"agt_a","status":"done","asks":false}`, done.Match)
}

func TestHandoffRepointsEdges(t *testing.T) {
	core := &fakeCore{}
	a := testApp(t, core)
	r := runGraph(t, a)
	core.calls = nil
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_boss"}, {ID: "agt_1", Status: api.StatusClosed}, {ID: "agt_9", HandoffFrom: "agt_1"}}, nil
	}

	a.sessionChanged(t.Context(), api.Changed{Workspace: "ws1", Session: "agt_9", Kind: "created"})

	assert.Equal(t, []string{"triggers.update", "triggers.update", "triggers.update"}, core.ids())
	assert.Equal(t, RunNode{ID: "coder", Role: "Coder", Start: true, Session: "agt_9", HandedOff: true}, a.runs[r.ID].Nodes[0])
	var done, back updateArgs
	require.NoError(t, json.Unmarshal(core.calls[0].Args, &done))
	require.NoError(t, json.Unmarshal(core.calls[1].Args, &back))
	assert.Equal(t, r.Edges[0].Trigger, done.ID)
	assert.JSONEq(t, `{"session":"agt_9","status":"done"}`, done.Match)
	var toCoder promptArgs
	require.NoError(t, json.Unmarshal([]byte(back.Args), &toCoder))
	assert.Equal(t, "agt_9", toCoder.Session)
	assert.Contains(t, toCoder.Text, "Implement the task in this worktree")
}

func TestRunStopsAtTurnBudget(t *testing.T) {
	core := &fakeCore{}
	a := testApp(t, core)
	r := runGraph(t, a)
	core.calls = nil

	for range r.MaxTurns {
		a.countTurn(t.Context(), api.Turn{Workspace: "ws1", Session: "agt_2"}, agentsSource)
	}

	assert.Equal(t, statusStopped, a.runs[r.ID].Status)
	assert.Equal(t, "turn budget of 12 reached", a.runs[r.ID].Reason)
	assert.Equal(t, []string{"triggers.delete", "triggers.delete", "triggers.delete"}, core.ids())
}

func TestEndOutcomeFinishesRun(t *testing.T) {
	core := &fakeCore{}
	a := testApp(t, core)
	r := runGraph(t, a)
	core.calls = nil
	topic := eventPrefix + r.outcomeEvent("reviewer")

	a.busEvent(t.Context(), nil, "", pluginsdk.Event{Source: triggersSource, Topic: topic, Payload: json.RawMessage(`{"workspace":"ws1","by":"agt_1","outcome":"approved"}`)})
	assert.Equal(t, statusRunning, a.runs[r.ID].Status)

	a.busEvent(t.Context(), nil, "", pluginsdk.Event{Source: triggersSource, Topic: topic, Payload: json.RawMessage(`{"workspace":"ws1","by":"agt_2","outcome":"approved"}`)})
	assert.Equal(t, statusStopped, a.runs[r.ID].Status)
	assert.Equal(t, "reviewer reported approved", a.runs[r.ID].Reason)
	assert.Equal(t, []string{"triggers.delete", "triggers.delete", "triggers.delete"}, core.ids())
}

func TestRunUnwindsWhenATriggerFails(t *testing.T) {
	core := &fakeCore{}
	a := testApp(t, core)
	a.runCommand = func(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error) {
		if req.ID == "triggers.add" && len(core.calls) == 3 {
			core.calls = append(core.calls, req)
			return nil, fmt.Errorf("boom")
		}
		return core.run(ctx, req)
	}

	_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.run", WorkspaceID: "ws1", Args: json.RawMessage(`{"graph":"review-loop"}`)})

	assert.ErrorContains(t, err, "connect reviewer → coder: boom")
	assert.Equal(t, []string{"agents.new", "agents.new", "triggers.add", "triggers.add", "triggers.delete", "agents.close", "agents.close"}, core.ids())
	assert.Empty(t, a.runs)
}

func TestListWithoutProjectGraphsOrRuns(t *testing.T) {
	a := testApp(t, &fakeCore{})
	a.workspace = func(context.Context, string) (pluginsdk.Workspace, error) {
		return pluginsdk.Workspace{ID: "ws1", Folders: []pluginsdk.Folder{{Path: t.TempDir()}}}, nil
	}
	out, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.list", WorkspaceID: "ws1"})
	require.NoError(t, err)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	var res struct {
		Graphs []graphFile     `json:"graphs"`
		Runs   json.RawMessage `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(raw, &res))
	require.Len(t, res.Graphs, 3)
	assert.Equal(t, []any{"pr-walkthrough", true}, []any{res.Graphs[1].Name, res.Graphs[1].Builtin})
	assert.JSONEq(t, `[]`, string(res.Runs))
}

func TestRunFromSectionSelectsNewRun(t *testing.T) {
	core := &fakeCore{}
	a := testApp(t, core)
	var chosen string
	a.choose = func(_ context.Context, _, id string) error {
		chosen = id
		return nil
	}

	require.NoError(t, a.startGraph(t.Context(), "ws1", "review-loop", "{{x}}"))
	assert.Equal(t, draft{graph: "review-loop", input: "{{x}}", err: "Input: must not contain {{."}, a.drafts["ws1"])
	assert.Empty(t, chosen)

	require.NoError(t, a.startGraph(t.Context(), "ws1", "review-loop", "add a flag"))
	require.Len(t, a.runs, 1)
	for id, r := range a.runs {
		assert.Equal(t, id, chosen)
		assert.False(t, r.Agent)
	}
	assert.Empty(t, a.drafts)
	assert.False(t, core.calls[0].Agent)
}
