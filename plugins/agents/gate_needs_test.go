package main_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const gateLoop = `{"nodes":[{"id":"lint","prompt":"Lint. {{input}}","start":true},{"id":"check","run":"true"},{"id":"review","prompt":"Review. {{input}}","outcomes":["approved","changes"]}],"edges":[{"from":"lint","to":"check"},{"from":"check","to":"lint","maxFires":1},{"from":"check","to":"review","on":"error"}]}`

func (g gateSetup) waitAsking(t *testing.T) (lander, need record) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		var list []record
		if g.f.run(t, g.workspace, "agents.sessions", map[string]any{}, &list) != nil {
			return false
		}
		i := slices.IndexFunc(list, func(r record) bool { return r.ID == g.lander })
		j := slices.IndexFunc(list, func(r record) bool { return r.Kind == "run" && r.NeedsYou })
		if i < 0 || j < 0 || list[i].Landing == nil || list[i].Landing.Words != "asking" {
			return false
		}
		lander, need = list[i], list[j]
		assert.Equal(t, 1, len(slices.DeleteFunc(list, func(r record) bool { return !r.NeedsYou })))
		return true
	}, wait)
	return lander, need
}

func TestGateStepApprovalAsksThroughTheLander(t *testing.T) {
	t.Parallel()
	g := startGateWith(t, "where", gateWhere)
	write(t, g.release, "")
	require.ErrorContains(t, g.land(t), "review pending")

	lander, need := g.waitAsking(t)

	assert.Equal(t, []any{"where wants to run pwd", "warning", "reviewing"}, []any{lander.Landing.Ask, lander.Landing.Tone, lander.Landing.Verdict})
	assert.Equal(t, []any{g.lander, "needsYou"}, []any{need.RowUnder, lander.Group})
	var history gateHistory
	require.NoError(t, g.f.run(t, g.workspace, "agents.gate-verdicts", map[string]any{"id": g.gate}, &history))
	require.Len(t, history.Landings, 1)
	assert.Equal(t, "needs-you", history.Landings[0].State)

	g.f.h.Event(t, g.workspace, "agents.sidebar", "sessions", "select", "run:"+need.ID)
	card := need.ID + "/approve"
	g.f.h.WaitSlot(t, g.workspace, "agents.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "permissionCard"), func(c sdui.Node) bool { return c.ID == card })
	})
	approve := map[string]any{"request": "where", "option": "once"}
	assert.ErrorContains(t, g.f.eventFrom(t, map[string]any{"agent": true}, g.workspace, card, "approve", approve), "only the user presses this")
	gates := map[string]any{"workspaceId": g.workspace, "slot": "agents.gates.main", "nodeId": card, "action": "approve", "value": approve}
	assert.Error(t, g.f.h.Client(t).Call(t.Context(), "ui.event", map[string]any{"workspaceId": g.workspace, "slot": "agents.gates.main", "nodeId": card, "action": "approve", "value": approve, "agent": true}, nil))
	assert.Equal(t, "waiting", g.stepStatus(t, need.ID, "where"))
	require.NoError(t, g.f.h.Client(t).Call(t.Context(), "ui.event", gates, nil))
	testutil.Eventually(t, func() bool {
		l := g.f.session(t, g.workspace, g.lander).Landing
		return l != nil && l.Words != "asking"
	}, wait)
}

func TestGateHeldEdgeAsksThroughTheLander(t *testing.T) {
	t.Parallel()
	g := startGateEnv(t, allowHome(t, "Bash(true)"), "loop", gateLoop)
	write(t, g.release, "")
	require.ErrorContains(t, g.land(t), "review pending")

	lander, need := g.waitAsking(t)

	assert.Equal(t, []any{"check → lint ran 1 of 1 round", g.lander}, []any{lander.Landing.Ask, need.RowUnder})
	g.f.h.Event(t, g.workspace, "agents.sidebar", "sessions", "select", "run:"+need.ID)
	cont := need.ID + "/continue"
	g.f.h.WaitSlot(t, g.workspace, "agents.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "button"), func(c sdui.Node) bool { return c.ID == cont })
	})
	require.NoError(t, g.f.cardEvent(t, g.workspace, cont, "continue", nil))
	testutil.Eventually(t, func() bool {
		l := g.f.session(t, g.workspace, g.lander).Landing
		return l != nil && l.Ask != "check → lint ran 1 of 1 round"
	}, wait)
}
