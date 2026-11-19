package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/toolgate"
)

func TestGateNodeAndNightSessionAreNeverHeld(t *testing.T) {
	t.Parallel()
	req := acp.RequestPermissionRequest{ToolCall: []byte(`{"toolCallId":"c","name":"Bash","rawInput":{"command":"git push"}}`)}
	for _, rec := range []record{{ID: "node", GateFor: &gateFor{Gate: "g"}}, {ID: "night", Night: "n"}} {
		s := newSession(rec, newEventLog(filepath.Join(t.TempDir(), rec.ID+".jsonl")), true, func(string, bool) {})
		_, held := (&app{}).holdCall(t.Context(), s, nil, "1", req)
		assert.False(t, held, rec.ID)
	}
}

func TestHeldCallIsAnsweredOnlyWithOnceOptions(t *testing.T) {
	t.Parallel()
	options := []acp.PermissionOption{{OptionID: "always", Kind: "allow_always"}, {OptionID: "once", Kind: "allow_once"}, {OptionID: "no", Kind: "reject_once"}}

	assert.Equal(t, acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "once"}, callOutcome(options, callAllow))
	assert.Equal(t, acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "no"}, callOutcome(options, callRefuse))
	assert.Equal(t, acp.OutcomeCancelled, callOutcome(options[:1], callAllow).Outcome)
	assert.Equal(t, acp.OutcomeCancelled, callOutcome(options, callCancelled).Outcome)
}

func TestCallHoldIsAHoldOfItsGate(t *testing.T) {
	t.Parallel()
	done, push, fetch := &heldTurn{Gate: "d"}, &heldTurn{Gate: "p", Round: 1}, &heldTurn{Gate: "f", Round: 2}
	rec := record{ID: "s", Held: done, Calls: []*callHold{{Hold: push, Call: "c1"}, {Hold: fetch}}}
	s := newSession(rec, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})

	assert.Equal(t, map[string]heldTurn{"": *done, "p": *push, "f": *fetch}, heldCopies(rec))
	assert.Len(t, s.holds(), 3)
	r := holdRef{s: s, gate: "p"}
	assert.Equal(t, gatePointTool, r.point())
	next := &heldTurn{Gate: "p", Round: 2}
	require.True(t, r.cas(push, next))
	assert.False(t, r.cas(push, next))
	assert.Same(t, next, r.get())
	assert.Equal(t, "c1", s.snapshot().call("p").Call)
	assert.Same(t, push, rec.Calls[0].Hold)
	require.True(t, r.cas(next, nil))
	assert.Nil(t, s.snapshot().call("p"))
	assert.Same(t, done, s.doneHold().get())
}

func TestNoLaterCallOvertakesOnAGate(t *testing.T) {
	t.Parallel()
	on := func(id string, gates ...string) *heldCall {
		c := &heldCall{id: id}
		for _, g := range gates {
			c.hits = append(c.hits, toolgate.Hit{Rule: toolgate.Rule{ID: g}})
		}
		return c
	}
	a, b, c, d := on("a", "x"), on("b", "x", "y"), on("c", "y"), on("d", "z")

	assert.Equal(t, []*heldCall{a, d}, nextCalls([]*heldCall{a, b, c, d}))
	a.taken = true
	assert.Empty(t, nextCalls([]*heldCall{a, b, c}))
	assert.Equal(t, []*heldCall{b}, nextCalls([]*heldCall{b, c}))
}

func TestGateTakenOffACallNotYetFilledInLeavesItToItsTake(t *testing.T) {
	t.Parallel()
	s := newSession(record{ID: "s"}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	c := &heldCall{id: "c", taken: true, reply: make(chan callAnswer, 1)}
	for _, g := range []string{"g", "h"} {
		c.hits = append(c.hits, toolgate.Hit{Rule: toolgate.Rule{ID: g}})
	}
	s.calls = []*heldCall{c}

	(&app{}).regateCalls(t.Context(), s, nil, "g", byGateDeleted)

	assert.Equal(t, []toolgate.Hit{{Rule: toolgate.Rule{ID: "h"}}}, c.hits)
	assert.Equal(t, []*heldCall{c}, s.calls)
	assert.Empty(t, c.reply)
}

func TestHeldCallKeepsTheSessionFromDetaching(t *testing.T) {
	t.Parallel()
	s := newSession(record{ID: "s", Status: statusDone}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	s.hookOwed, s.cycle = true, true
	free := []bool{s.idleLocked(0), s.hookCycleReadyLocked()}
	s.cycle = false
	free = append(free, s.quietLocked())
	require.Equal(t, []bool{true, true, true}, free)

	s.calls = []*heldCall{{id: "c"}}
	assert.False(t, s.idleLocked(0))
	assert.False(t, s.quietLocked())
	s.cycle = true
	assert.False(t, s.hookCycleReadyLocked())
}

func TestNoTurnRefusalReadsRefusedInTheHistory(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "refused: no turn", verdictLook(gateVerdict{Outcome: gateRefused, By: byNoTurn}, gatePointTool).Word)
}
