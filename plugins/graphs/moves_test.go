package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

func movesRun(t *testing.T, g *Graph) *Run {
	t.Helper()
	r, err := newRun(g, "ws1", "", false, "", time.Now())
	require.NoError(t, err)
	for i := range r.Nodes {
		r.Nodes[i].Session = "agt_" + r.Nodes[i].ID
	}
	return r
}

func reviewLoop() *Graph {
	return &Graph{
		Name:  "g",
		Nodes: []Node{{ID: "coder", Prompt: "p", Start: true}, {ID: "reviewer", Prompt: "p", Outcomes: []string{"approved", "changes"}, End: []string{"approved"}}},
		Edges: []Edge{{From: "coder", To: "reviewer", On: onDone}, {From: "reviewer", To: "coder", On: onOutcome, Outcome: "changes"}},
	}
}

func TestMovesFollowEdges(t *testing.T) {
	a := testApp(t, &fakeCore{})
	r := runGraph(t, a)

	a.countTurn(t.Context(), api.Turn{Workspace: "ws1", Session: "agt_1", Turn: 1, Status: "done"})
	a.busEvent(t.Context(), nil, "", pluginsdk.Event{Topic: eventPrefix + r.outcomeEvent("reviewer"), Payload: json.RawMessage(`{"workspace":"ws1","by":"agt_2","outcome":"changes"}`)})
	a.countTurn(t.Context(), api.Turn{Workspace: "ws1", Session: "agt_2", Turn: 1, Status: "done"})

	live := a.runs[r.ID]
	var nodes []string
	var causes []int
	var open []bool
	for _, m := range live.Moves {
		nodes, causes, open = append(nodes, m.Node), append(causes, m.Cause), append(open, m.Open)
	}
	assert.Equal(t, []string{"coder", "reviewer", "coder"}, nodes)
	assert.Equal(t, []int{-1, 0, 1}, causes)
	assert.Equal(t, []bool{false, false, true}, open)
	assert.Equal(t, []int{0}, live.Moves[0].Fired)
	assert.Equal(t, []int{1}, live.lastEdges())
	assert.Equal(t, []int{1, 1, 0}, live.edgeCounts())
}

func TestUserTurnIsAMove(t *testing.T) {
	r := movesRun(t, reviewLoop())
	now := time.Now()

	r.endTurn(api.Turn{Session: "agt_reviewer", Turn: 3, Status: "done"}, now)

	assert.Equal(t, Move{Node: "reviewer", User: true, Cause: -1, Edge: -1, At: now, Turn: 3}, r.Moves[1])
}

func TestBusyTargetGetsSecondOpenMove(t *testing.T) {
	r := movesRun(t, reviewLoop())
	now := time.Now()
	r.endTurn(api.Turn{Session: "agt_coder", Turn: 1, Status: "done"}, now)
	r.endTurn(api.Turn{Session: "agt_coder", Turn: 2, Status: "done"}, now)

	r.endTurn(api.Turn{Session: "agt_reviewer", Turn: 1, Status: "done"}, now)

	assert.Equal(t, []string{"coder", "reviewer", "coder", "reviewer"}, []string{r.Moves[0].Node, r.Moves[1].Node, r.Moves[2].Node, r.Moves[3].Node})
	assert.Equal(t, []bool{false, true}, []bool{r.Moves[1].Open, r.Moves[3].Open})
}

func TestDoneAndAsksFireOne(t *testing.T) {
	g := reviewLoop()
	g.Edges = append(g.Edges, Edge{From: "coder", To: "reviewer", On: onAsks})
	r := movesRun(t, g)
	now := time.Now()

	r.endTurn(api.Turn{Session: "agt_coder", Turn: 1, Status: "done", Asks: true}, now)
	r.endTurn(api.Turn{Session: "agt_coder", Turn: 2, Status: "done"}, now)

	assert.Equal(t, []int{2}, r.Moves[0].Fired)
	assert.Equal(t, []int{0}, r.Moves[2].Fired)
}

func TestEndOutcomeMoveClosesAfterStop(t *testing.T) {
	a := testApp(t, &fakeCore{})
	r := runGraph(t, a)
	a.countTurn(t.Context(), api.Turn{Workspace: "ws1", Session: "agt_1", Turn: 1, Status: "done"})
	a.busEvent(t.Context(), nil, "", pluginsdk.Event{Topic: eventPrefix + r.outcomeEvent("reviewer"), Payload: json.RawMessage(`{"workspace":"ws1","by":"agt_2","outcome":"approved"}`)})

	a.countTurn(t.Context(), api.Turn{Workspace: "ws1", Session: "agt_2", Turn: 2, Status: "done"})

	live := a.runs[r.ID]
	require.Len(t, live.Moves, 2)
	assert.Equal(t, Move{Node: "reviewer", Cause: 0, Edge: 0, End: "approved", At: live.Moves[1].At, Turn: 2}, live.Moves[1])
}

func TestStoppedRunShowsLastFiredEdge(t *testing.T) {
	g := reviewLoop()
	g.Edges = append(g.Edges, Edge{From: "coder", To: "reviewer", On: onAsks})
	r := movesRun(t, g)
	now := time.Now()
	r.endTurn(api.Turn{Session: "agt_coder", Turn: 1, Status: "done", Asks: true}, now)
	r.endTurn(api.Turn{Session: "agt_coder", Turn: 2, Status: "done"}, now)

	r.stop(reasonStopped, now)

	assert.Equal(t, []int{0}, r.lastEdges())
}

func TestOutcomeWithoutOpenMoveIsUserMove(t *testing.T) {
	r := movesRun(t, reviewLoop())
	now := time.Now()
	r.endTurn(api.Turn{Session: "agt_coder", Turn: 1, Status: "done"}, now)
	r.endTurn(api.Turn{Session: "agt_reviewer", Turn: 1, Status: "done"}, now)

	r.outcome("reviewer", "changes", now)
	r.endTurn(api.Turn{Session: "agt_reviewer", Turn: 2, Status: "done"}, now)

	require.Len(t, r.Moves, 4)
	assert.Nil(t, r.Moves[1].Fired)
	assert.Equal(t, Move{Node: "reviewer", Cause: -1, Edge: -1, User: true, Fired: []int{1}, At: now, Turn: 2}, r.Moves[2])
}

func TestWaitingTurnKeepsMoveOpen(t *testing.T) {
	a := testApp(t, &fakeCore{})
	r := runGraph(t, a)

	a.countTurn(t.Context(), api.Turn{Workspace: "ws1", Session: "agt_1", Turn: 1, Status: "waiting"})
	require.Len(t, a.runs[r.ID].Moves, 1)
	assert.True(t, a.runs[r.ID].Moves[0].Open)

	a.countTurn(t.Context(), api.Turn{Workspace: "ws1", Session: "agt_1", Turn: 1, Status: "done"})
	live := a.runs[r.ID]
	require.Len(t, live.Moves, 2)
	assert.False(t, live.Moves[0].Open)
	assert.Equal(t, []int{0}, live.Moves[0].Fired)
}

func TestClosedTurnClosesMoveAndFiresNothing(t *testing.T) {
	r := movesRun(t, reviewLoop())

	r.endTurn(api.Turn{Session: "agt_coder", Turn: 1, Status: api.StatusClosed}, time.Now())

	require.Len(t, r.Moves, 1)
	assert.False(t, r.Moves[0].Open)
	assert.Nil(t, r.Moves[0].Fired)
}

func TestTurnEdgesMatchTheirTriggers(t *testing.T) {
	turns := []api.Turn{{Status: "done"}, {Status: "done", Asks: true}, {Status: "error"}, {Status: "waiting"}, {Status: api.StatusClosed}}
	for _, asks := range []bool{false, true} {
		g := &Graph{
			Name:  "g",
			Nodes: []Node{{ID: "a", Prompt: "p", Start: true, Outcomes: []string{"ok"}}, {ID: "b", Prompt: "p"}},
			Edges: []Edge{{From: "a", To: "b", On: onDone}, {From: "a", To: "b", On: onError}, {From: "a", To: "b", On: onOutcome, Outcome: "ok"}},
		}
		if asks {
			g.Edges = append(g.Edges, Edge{From: "a", To: "b", On: onAsks})
		}
		x := &runner{run: movesRun(t, g)}
		for _, e := range g.Edges {
			trigger, err := x.edgeTrigger(g, e)
			require.NoError(t, err)
			var match map[string]any
			require.NoError(t, json.Unmarshal([]byte(trigger.Match), &match))
			for _, turn := range turns {
				turn.Session = "agt_a"
				raw, err := json.Marshal(turn)
				require.NoError(t, err)
				var payload map[string]any
				require.NoError(t, json.Unmarshal(raw, &payload))
				fires := trigger.On == api.TopicTurn
				for k, v := range match {
					fires = fires && payload[k] == v
				}
				assert.Equal(t, fires, edgeMatchesTurn(g, e, turn), "on %s, turn %+v, asks edge %v", e.On, turn, asks)
			}
		}
	}
}
