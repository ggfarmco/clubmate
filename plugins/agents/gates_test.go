package main_test

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/agentcall"
)

type gateListed struct {
	ID       string   `json:"id"`
	Repo     string   `json:"repo"`
	Rounds   int      `json:"rounds"`
	Paused   bool     `json:"paused"`
	Point    string   `json:"point"`
	Into     string   `json:"into"`
	Sessions []string `json:"sessions"`
	Wrote    []string `json:"wrote"`
	Graph    string   `json:"graph"`
	Name     string   `json:"name"`
	Words    string   `json:"words"`
	State    string   `json:"state"`
	Pin      string   `json:"pin"`
	Hash     string   `json:"hash"`
	Last     *struct {
		Outcome string `json:"outcome"`
	} `json:"last"`
}

type gateHistory struct {
	Landings []struct {
		Lander   string `json:"lander"`
		Call     string `json:"call"`
		State    string `json:"state"`
		Round    int    `json:"round"`
		Reviewer string `json:"reviewer"`
	} `json:"landings"`
	Verdicts []gateVerdictRow `json:"verdicts"`
}

type gateVerdictRow struct {
	Gate     string    `json:"gate"`
	Outcome  string    `json:"outcome"`
	By       string    `json:"by"`
	Lander   string    `json:"lander"`
	Into     string    `json:"into"`
	Call     string    `json:"call"`
	Round    int       `json:"round"`
	Reviewer string    `json:"reviewer"`
	Findings []string  `json:"findings"`
	Summary  string    `json:"summary"`
	At       time.Time `json:"at"`
}

func (f *fixture) gates(t *testing.T, workspace string) []gateListed {
	t.Helper()
	var res struct {
		Gates []gateListed `json:"gates"`
	}
	require.NoError(t, f.run(t, workspace, "agents.gates", map[string]any{}, &res))
	return res.Gates
}

func TestGateKeepsVerdictHistoryAndSaysTheLandingState(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	gate := g.f.gates(t, g.workspace)[0]

	require.ErrorContains(t, g.land(t), "review pending")
	assert.Equal(t, "reviewing", g.f.session(t, g.workspace, g.lander).Gate)
	assert.Equal(t, "reviewing", g.f.gates(t, g.workspace)[0].State)
	first := g.reviewer(t).ID
	assert.True(t, g.f.session(t, g.workspace, first).Hidden)
	landing := g.f.session(t, g.workspace, g.lander).Landing
	require.NotNil(t, landing)
	assert.Equal(t, []any{"review…", "accent", 1, first}, []any{landing.Words, landing.Tone, landing.Round, landing.Reviewer})
	g.verdict(t, "changes", "b.go:1 — breaks")
	assert.Equal(t, "changes", g.f.session(t, g.workspace, g.lander).Gate)
	landing = g.f.session(t, g.workspace, g.lander).Landing
	assert.Equal(t, []any{"1 change", "plain", 1}, []any{landing.Words, landing.Tone, landing.Round})
	require.NoError(t, g.f.run(t, g.workspace, "agents.gate-update", map[string]any{"id": gate.ID, "paused": true}, nil))
	assert.Empty(t, g.f.session(t, g.workspace, g.lander).Gate)
	require.NoError(t, g.f.run(t, g.workspace, "agents.gate-update", map[string]any{"id": gate.ID, "paused": false}, nil))
	assert.Equal(t, "changes", g.f.session(t, g.workspace, g.lander).Gate)
	g.commit(t, "c.go", "fix\n")
	require.ErrorContains(t, g.land(t), "review pending")
	g.verdict(t, "approved")

	var h gateHistory
	require.NoError(t, g.f.run(t, g.workspace, "agents.gate-verdicts", map[string]any{"id": gate.ID}, &h))
	require.Len(t, h.Verdicts, 2)
	assert.Equal(t, []string{"approved", "changes"}, []string{h.Verdicts[0].Outcome, h.Verdicts[1].Outcome})
	assert.Equal(t, []int{2, 1}, []int{h.Verdicts[0].Round, h.Verdicts[1].Round})
	assert.Equal(t, []string{gate.ID, g.lander, "main", "reviewer", first}, []string{h.Verdicts[1].Gate, h.Verdicts[1].Lander, h.Verdicts[1].Into, h.Verdicts[1].By, h.Verdicts[1].Reviewer})
	assert.Equal(t, []string{"b.go:1 — breaks"}, h.Verdicts[1].Findings)
	assert.Empty(t, h.Landings)
	assert.Equal(t, "approved", g.f.session(t, g.workspace, g.lander).Gate)
	listed := g.f.gates(t, g.workspace)[0]
	assert.Equal(t, []string{"active", "approved"}, []string{listed.State, listed.Last.Outcome})

	row := strconv.FormatInt(h.Verdicts[1].At.UnixNano(), 10) + "/" + first
	open := func(nodeID, action string, value any) agentcall.Opened {
		var opened agentcall.Opened
		require.NoError(t, g.f.h.Client(t).Call(t.Context(), "ui.event", map[string]any{"workspaceId": g.workspace, "slot": "agents.gates.main", "nodeId": nodeID, "action": action, "value": value}, &opened))
		return opened
	}
	assert.Equal(t, agentcall.Open(g.lander, 0), open(g.lander+"/gateLanding", "gateLanding", nil))
	assert.Equal(t, agentcall.Open(first, 0), open(first+"/gateReview", "gateReview", nil))
	assert.Equal(t, agentcall.Open(first, 0), open("gateVerdicts", "gateReview", row))
	g.f.h.WaitSlot(t, g.workspace, "agents.sidebar", func(n sdui.Node) bool {
		return slices.ContainsFunc(sheetNodes(n, "tree"), func(tree sdui.Node) bool { return tree.Props["selected"] == first })
	})
	other := g.f.workspace(t, newRepo(t))
	var seen struct {
		Gates []gateListed `json:"gates"`
	}
	require.NoError(t, g.f.runAs(t, other, g.f.newSession(t, other), "agents.gates", map[string]any{}, &seen))
	assert.Empty(t, seen.Gates)
	assert.ErrorContains(t, g.f.run(t, other, "agents.gate-verdicts", map[string]any{"id": gate.ID}, nil), "no such land gate")
	assert.Equal(t, []string{gate.ID}, []string{g.f.gates(t, other)[0].ID})
	assert.ErrorContains(t, g.f.run(t, other, "agents.gate-add", map[string]any{"repo": g.repo}, nil), gate.ID)

	require.NoError(t, g.land(t))
	assert.Empty(t, g.f.session(t, g.workspace, g.lander).Gate)
}

func TestGatesSectionAddsPausesAndDeletesAGateAsTheUser(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)

	require.NoError(t, f.run(t, workspace, "agents.gates.new", map[string]any{}, nil))
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 1 })
	f.h.Event(t, workspace, "agents.sheet", "gateSheet/rounds", "gateRounds", "3")
	f.h.Event(t, workspace, "agents.sheet", "gateSheet/save", "gateSave", nil)

	gates := f.gates(t, workspace)
	require.Len(t, gates, 1)
	assert.Equal(t, []any{3, "default", "fable-review"}, []any{gates[0].Rounds, gates[0].Into, gates[0].Graph})
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 0 })
	f.h.WaitSlot(t, workspace, "agents.gates.sidebar", func(n sdui.Node) bool {
		items, _ := n.Props["items"].([]any)
		return len(items) == 2 && n.Props["selected"] == gates[0].ID
	})
	f.h.WaitSlot(t, workspace, "agents.gates.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(sheetNodes(n, "header"), func(h sdui.Node) bool { return h.Props["title"] == gates[0].Name })
	})

	agent := f.newSession(t, workspace)
	for _, origin := range agentOrigins(agent) {
		params := map[string]any{"workspaceId": workspace, "slot": "agents.gates.main", "nodeId": gates[0].ID + "/gatePause", "action": "gatePause", "value": nil}
		for k, v := range origin {
			params[k] = v
		}
		assert.ErrorContains(t, f.h.Client(t).Call(t.Context(), "ui.event", params, nil), "only the user presses this")
	}
	assert.Error(t, f.runAs(t, workspace, agent, "agents.gates.new", map[string]any{}, nil))
	assert.False(t, f.gates(t, workspace)[0].Paused)

	f.h.Event(t, workspace, "agents.gates.main", gates[0].ID+"/gatePause", "gatePause", nil)
	assert.True(t, f.gates(t, workspace)[0].Paused)
	f.h.Event(t, workspace, "agents.gates.main", gates[0].ID+"/gateDelete", "gateDelete", nil)
	assert.Empty(t, f.gates(t, workspace))
}

func TestGateNamesFromMatch(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	for _, into := range []string{"any", "default", "base", "release/*"} {
		require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "into": into}, nil))
	}

	names := map[string]string{}
	for _, g := range f.gates(t, workspace) {
		names[g.Into] = g.Name
	}

	assert.Equal(t, map[string]string{"any": "Landing into any branch", "default": "Landing into main", "base": "Landing into the base", "release/*": "Landing into release/*"}, names)
}

func TestDockRepoRowOpensItsFirstGate(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	for _, into := range []string{"any", "default"} {
		require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "into": into}, nil))
	}
	gates := f.gates(t, workspace)
	first := gates[slices.IndexFunc(gates, func(g gateListed) bool { return g.Into == "default" })]

	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", "gate-repo:"+first.Repo)

	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(sheetNodes(n, "header"), func(h sdui.Node) bool { return h.Props["title"] == first.Name })
	})
}

func TestDockRepoRowOpensTheFirstToolCallGateByName(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.addPushGate(t, workspace, repo)
	f.addCommandGate(t, workspace, repo, "gh pr create")

	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", "gate-repo:"+f.gates(t, workspace)[0].Repo)

	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(sheetNodes(n, "header"), func(h sdui.Node) bool { return h.Props["title"] == "Tool call · gh pr create" })
	})
}

func TestSheetCountsTheRepoSessionsMidTurn(t *testing.T) {
	t.Parallel()
	f := start(t, callScript(openTurn("hold")))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	busy := f.newSession(t, workspace)
	f.newSession(t, workspace)
	f.prompt(t, workspace, busy, "hold")
	f.waitStatus(t, workspace, busy, "running")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	require.NoError(t, f.run(t, workspace, "agents.gates.new", map[string]any{}, nil))
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 1 })
	f.h.Event(t, workspace, "agents.sheet", "gateSheet/holds", "gateHolds", "tool")

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		return slices.ContainsFunc(sheetNodes(n, "text"), func(x sdui.Node) bool {
			return x.Props["text"] == "1 session mid-turn picks this gate up when its turn ends"
		})
	})
}
