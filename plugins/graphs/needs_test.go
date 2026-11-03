package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

func TestRunNeedStep(t *testing.T) {
	a, _, _, r := approvalApp(t, "default")
	turnDone(t, a, "agt_1", 1)
	n := a.runs[r.ID].need()
	require.NotNil(t, n)
	assert.Equal(t, runNeed{Run: r.ID, Title: "Review gate", Number: 1, Kind: needStep, Request: "step-tests", Node: "tests", Role: "Tests", Text: "Tests wants to run make test", Command: "make test", Since: a.now()}, *n)
	assert.Equal(t, a.now(), stepMoves(a, r.ID, "tests")[0].Step.WaitingAt)
}

func TestRunNeedOldestFirst(t *testing.T) {
	a, _, _, r := stepApp(t, stepResult{ExitCode: 1}, stepResult{ExitCode: 1}, stepResult{ExitCode: 1})
	for turn := 1; turn <= 3; turn++ {
		turnDone(t, a, "agt_1", turn)
	}
	live := a.runs[r.ID]
	n := live.need()
	require.NotNil(t, n)
	assert.Equal(t, runNeed{Run: r.ID, Title: "Review gate", Number: 1, Kind: needCap, Request: "cap-1", Node: "tests", Edge: 1, Text: "Tests → Coder ran 2 of 2 rounds", Since: a.now()}, *n)
	live.Moves = append(live.Moves, Move{Node: "merge", Cause: -1, Edge: -1, Open: true, Step: &StepMove{Status: stepWaiting, Command: "git merge --squash x", WaitingAt: a.now().Add(-time.Minute)}})
	assert.Equal(t, needStep, live.need().Kind)
	live.stop(reasonStopped, a.now())
	assert.Nil(t, live.need())
}

func TestShowPendingApproval(t *testing.T) {
	a, _, _, r := approvalApp(t, "default")
	turnDone(t, a, "agt_1", 1)
	out, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.show", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
	require.NoError(t, err)
	for _, n := range out.(shownRun).Nodes {
		if n.ID == "tests" {
			require.NotNil(t, n.Pending)
			assert.Equal(t, pendingInfo{Command: "make test", Dir: "/wt/coder", Main: "/repo", Rule: "Bash(make test)"}, *n.Pending)
			return
		}
	}
	t.Fatal("no tests node")
}

func TestShowAgentTimesOutcomesAndCommands(t *testing.T) {
	a, _, _, r := approvalApp(t, "default")
	start := a.now()
	a.now = func() time.Time { return start.Add(8 * time.Minute) }
	turnDone(t, a, "agt_1", 1)
	a.mu.Lock()
	a.runs[r.ID].Moves = append(a.runs[r.ID].Moves, Move{Node: "reviewer", Cause: -1, Edge: -1, At: start, Ended: start.Add(3 * time.Minute), Fired: []int{4}}, Move{Node: "coder", Cause: -1, Edge: -1, At: start, Ended: start, Fired: []int{4}})
	a.mu.Unlock()
	sessions := a.sessions
	a.sessions = func(ctx context.Context, ws string) ([]api.Session, error) {
		list, err := sessions(ctx, ws)
		list[1].Status = "error"
		return list, err
	}
	out, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.show", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
	require.NoError(t, err)
	got := map[string][]any{}
	for _, n := range out.(shownRun).Nodes {
		got[n.ID] = []any{n.Outcome, n.DurationMs, n.Command}
	}
	assert.Equal(t, map[string][]any{
		"coder":    {"", int64(8 * 60_000), ""},
		"tests":    {"", int64(0), "make test"},
		"reviewer": {"approved", int64(3 * 60_000), ""},
		"merge":    {"", int64(0), "git merge --squash x"},
	}, got)
}

func TestRunNumbersCountPerGraph(t *testing.T) {
	a, _, _, first := approvalApp(t, "default")
	second := runNamed(t, a, "gate")
	other := runGraph(t, a)
	assert.Equal(t, []int{1, 2, 1}, []int{first.Number, second.Number, other.Number})
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runs[first.ID].StartedAt = a.runs[second.ID].StartedAt.Add(time.Hour)
	for _, r := range a.runs {
		r.Number = 0
	}
	a.numberRuns(t.Context())
	assert.Equal(t, []int{2, 1, 1}, []int{a.runs[first.ID].Number, a.runs[second.ID].Number, a.runs[other.ID].Number})
}

func TestFeedNeedsOnce(t *testing.T) {
	a, core, _, r := approvalApp(t, "default")
	turnDone(t, a, "agt_1", 1)
	flushAll := func() int {
		a.touch("ws1")
		a.flush(t.Context())
		return len(called(core, "agents.run-needs"))
	}
	require.Equal(t, 1, flushAll())
	var got runNeedsArgs
	require.NoError(t, json.Unmarshal(called(core, "agents.run-needs")[0], &got))
	require.Len(t, got.Runs, 1)
	assert.Equal(t, r.ID, got.Runs[0].Run)
	assert.Equal(t, 1, flushAll())
	a.readyAgain()
	assert.Equal(t, 2, flushAll())
	require.NoError(t, approve(t, a, r, "tests", "reject"))
	assert.Equal(t, 3, flushAll())
	assert.JSONEq(t, `{"runs":[]}`, string(called(core, "agents.run-needs")[2]))
}

func TestFeedNeedsAfterRestartClearsStaleEntries(t *testing.T) {
	a, core, _, _ := stepApp(t)
	a.resync = true
	a.touch("ws1")
	a.flush(t.Context())
	feeds := called(core, "agents.run-needs")
	require.Len(t, feeds, 1)
	assert.JSONEq(t, `{"runs":[]}`, string(feeds[0]))
}
