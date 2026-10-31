package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
)

type memStore struct{}

func (memStore) save(context.Context, *Run) error { return nil }

func (memStore) remove(context.Context, *Run) error { return nil }

type fakeCore struct {
	calls []commandRequest
	next  int
}

func (f *fakeCore) run(_ context.Context, req commandRequest) (json.RawMessage, error) {
	f.calls = append(f.calls, req)
	f.next++
	switch req.ID {
	case "agents.new":
		return json.RawMessage(fmt.Sprintf(`{"session":"agt_%d"}`, f.next)), nil
	case "triggers.add":
		return json.RawMessage(fmt.Sprintf(`{"id":"tr_%d"}`, f.next)), nil
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
	a.render = func(context.Context, string) error { return nil }
	a.sessions = func(context.Context, string) ([]agentcall.Session, error) {
		return []agentcall.Session{{ID: "agt_boss", Worktree: "testdata"}}, nil
	}
	a.project = func(context.Context, string) (pluginsdk.Project, error) {
		return pluginsdk.Project{ID: "ws1", Roots: []string{"testdata"}}, nil
	}
	close(a.ready)
	return a
}

func runGraph(t *testing.T, a *app) *Run {
	t.Helper()
	out, err := a.command(t.Context(), nil, pluginsdk.CommandRun{
		ID:        "graphs.run",
		ProjectID: "ws1",
		Args:      json.RawMessage(`{"graph":"review-loop","input":"add a flag"}`),
		Agent:     true,
		Caller:    "agt_boss",
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

	var done, outcome triggerArgs
	require.NoError(t, json.Unmarshal(core.calls[2].Args, &done))
	require.NoError(t, json.Unmarshal(core.calls[3].Args, &outcome))
	assert.Equal(t, turnTopic, done.On)
	assert.JSONEq(t, `{"session":"agt_1","status":"done","asks":false}`, done.Match)
	assert.Equal(t, "event.graph."+r.ID+".reviewer", outcome.On)
	assert.JSONEq(t, `{"by":"agt_2","outcome":"changes"}`, outcome.Match)

	var toReviewer, kickoff promptArgs
	require.NoError(t, json.Unmarshal([]byte(done.Args), &toReviewer))
	require.NoError(t, json.Unmarshal(core.calls[5].Args, &kickoff))
	assert.Equal(t, "agt_2", toReviewer.Session)
	assert.Contains(t, toReviewer.Text, "Review the latest commits of session agt_1")
	assert.Contains(t, toReviewer.Text, `name "graph.`+r.ID+`.reviewer"`)
	assert.Equal(t, "agt_1", kickoff.Session)
	assert.Contains(t, kickoff.Text, "Task: add a flag")
}

func TestRunStopsAtTurnBudget(t *testing.T) {
	core := &fakeCore{}
	a := testApp(t, core)
	r := runGraph(t, a)
	core.calls = nil

	for range r.MaxTurns {
		a.countTurn(t.Context(), turnPayload{Workspace: "ws1", Session: "agt_2"})
	}

	assert.Equal(t, statusStopped, a.runs[r.ID].Status)
	assert.Equal(t, "turn budget of 12 reached", a.runs[r.ID].Reason)
	assert.Equal(t, []string{"triggers.delete", "triggers.delete", "triggers.delete"}, core.ids())
}

func TestRunUnwindsWhenATriggerFails(t *testing.T) {
	core := &fakeCore{}
	a := testApp(t, core)
	a.runCommand = func(ctx context.Context, req commandRequest) (json.RawMessage, error) {
		if req.ID == "triggers.add" && len(core.calls) == 3 {
			core.calls = append(core.calls, req)
			return nil, fmt.Errorf("boom")
		}
		return core.run(ctx, req)
	}

	_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.run", ProjectID: "ws1", Args: json.RawMessage(`{"graph":"review-loop"}`)})

	assert.ErrorContains(t, err, "connect reviewer → coder: boom")
	assert.Equal(t, []string{"agents.new", "agents.new", "triggers.add", "triggers.add", "triggers.delete", "agents.close", "agents.close"}, core.ids())
	assert.Empty(t, a.runs)
}

func TestListWithoutGraphsOrRuns(t *testing.T) {
	a := testApp(t, &fakeCore{})
	a.project = func(context.Context, string) (pluginsdk.Project, error) {
		return pluginsdk.Project{ID: "ws1", Roots: []string{t.TempDir()}}, nil
	}
	out, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.list", ProjectID: "ws1"})
	require.NoError(t, err)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.JSONEq(t, `{"graphs":[],"runs":[]}`, string(raw))
}
