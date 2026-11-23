package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

func TestHeldCardIsAGateCardThatAsksAboutAPlan(t *testing.T) {
	t.Parallel()
	card := heldCard(record{ID: "agt_1"}, heldTurn{Request: "lg_1", Name: gateNamePlanSpecs, Last: gateChanges, Round: 2})
	require.NoError(t, internalsdui.Validate(sdui.VStack(card)))

	assert.Equal(t, true, card.Props["gate"])
	assert.Equal(t, "Accept the plan as done?", card.Props["question"])
	var shorts []string
	for _, o := range card.Props["options"].([]sdui.PermissionOption) {
		shorts = append(shorts, o.Short)
	}
	assert.Equal(t, []string{"accept", "one more round", "stop"}, shorts)
}

func TestEveryRoundsCardAsksItsQuestion(t *testing.T) {
	t.Parallel()
	questions := map[string]string{
		gateNameAnyTurn:       "Accept the turn as done?",
		gateNameAnyFile:       "Accept the changes as done?",
		gateNameDone + "*.go": "Accept the changes as done?",
	}
	for name, want := range questions {
		assert.Equal(t, want, heldCard(record{ID: "agt_1"}, heldTurn{Request: "lg_1", Name: name}).Props["question"], name)
	}
	landing := gateCard(record{ID: "agt_1"}, landReview{Request: "lg_2", Into: "main", Source: "feat/x", Graph: "Plan pipeline", Rounds: 2}).Props
	assert.Equal(t, []any{"Plan pipeline still asks for changes after 2 rounds", "Land feat/x without another review?"}, []any{landing["title"], landing["question"]})
}

func TestHeldRoundLineNamesEachRound(t *testing.T) {
	t.Parallel()
	b := heldBlock{Graph: "Plan pipeline", Round: 2, Rounds: []finishedRound{{Round: 1, Verdict: gateChanges}}}
	reviewing, waits := b, b
	reviewing.Reviewing, waits.Wait = true, "waits for trust"

	assert.Equal(t, "Plan pipeline · round 1 changes", heldRoundText(b))
	assert.Equal(t, "Plan pipeline · round 1 changes · round 2 reviewing", heldRoundText(reviewing))
	assert.Equal(t, "Plan pipeline · round 1 changes · round 2 waits for trust", heldRoundText(waits))
}

func TestTurnHeldOnlyWhileTheAgentIsNotWorking(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		held    *heldTurn
		working bool
		want    bool
	}{
		{"under review", &heldTurn{Round: 1}, false, true},
		{"card waits", &heldTurn{Last: gateChanges, Request: "lg_1"}, false, true},
		{"fixing the findings", &heldTurn{Last: gateChanges}, false, false},
		{"not held", nil, false, false},
		{"under review in a follow-up cycle", &heldTurn{Round: 1}, true, false},
		{"card waits in a follow-up cycle", &heldTurn{Last: gateChanges, Request: "lg_1"}, true, false},
	}
	for _, c := range cases {
		rec := record{ID: "agt_1", Status: statusRunning, Held: c.held}
		s := &session{rec: rec}
		s.cycle = c.working
		a := &app{registry: registry{sessions: map[string]*session{rec.ID: s}}}
		assert.Equal(t, c.want, a.turnHeld(rec), c.name)
	}
}

func TestDoneSentenceAndMatchNameWhoseTurn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		sessions       []string
		sentence, fact string
	}{
		{[]string{doneTop}, "Holds a top-level session's done turn in shop when it wrote a plan or a spec; Plan pipeline decides.", "top-level sessions · wrote plans and specs: **/plans/*.md or **/specs/*.md"},
		{[]string{doneChild}, "Holds a child session's done turn in shop when it wrote a plan or a spec; Plan pipeline decides.", "child sessions · wrote plans and specs: **/plans/*.md or **/specs/*.md"},
		{[]string{doneTop, doneChild}, "Holds any session's done turn in shop when it wrote a plan or a spec; Plan pipeline decides.", "all sessions · wrote plans and specs: **/plans/*.md or **/specs/*.md"},
		{[]string{doneTop, doneAFK}, "Holds a top-level AFK session's done turn in shop when it wrote a plan or a spec; Plan pipeline decides.", "top-level AFK sessions · wrote plans and specs: **/plans/*.md or **/specs/*.md"},
		{[]string{doneChild, doneAFK}, "Holds a child AFK session's done turn in shop when it wrote a plan or a spec; Plan pipeline decides.", "child AFK sessions · wrote plans and specs: **/plans/*.md or **/specs/*.md"},
		{[]string{doneTop, doneChild, doneAFK}, "Holds any AFK session's done turn in shop when it wrote a plan or a spec; Plan pipeline decides.", "AFK sessions · wrote plans and specs: **/plans/*.md or **/specs/*.md"},
		{[]string{doneAFK}, "Pick top-level, children or both.", ""},
	}
	for _, c := range cases {
		d := gateDraft{Point: gatePointTurn, Sessions: c.sessions, WroteMode: gateWrotePlans}
		assert.Equal(t, c.sentence, doneSentenceOf(d, "Plan pipeline", "shop"), c.sessions)
		if c.fact != "" {
			assert.Equal(t, c.fact, doneMatchWords(c.sessions, donePlansAndSpecs), c.sessions)
		}
	}
}

func TestHeldRoundIsNilForAGateThatIsGone(t *testing.T) {
	rec := record{ID: "agt_1", Status: statusRunning, Held: &heldTurn{Gate: "g_gone", Round: 1}}
	a := &app{registry: registry{sessions: map[string]*session{rec.ID: {rec: rec}}}}
	live := map[string]bool{"g_other": true}
	a.liveGates.Store(&live)

	assert.Nil(t, a.heldRound(rec))
}
