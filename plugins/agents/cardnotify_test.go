package main_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
)

func notesOf(notes []notePost, session string) []notePost {
	return slices.DeleteFunc(slices.Clone(notes), func(n notePost) bool { return n.Session != session })
}

func TestHeldCardPostsOneNeedsYouNotification(t *testing.T) {
	t.Parallel()
	d := startDone(t, map[string]any{"rounds": 1}, map[string]any{}, nil)

	d.cardAtCap(t)

	want := notePost{d.workspace, d.session, "Planner", "Opus review still asks for changes after 1 round · Accept the turn as done?", "needs-input"}
	d.f.waitNotes(t, func(n []notePost) bool { return slices.Contains(n, want) })
	testutil.Never(t, func() bool { return len(notesOf(d.f.notes(t), d.session)) > 1 }, quiet)
}

func TestMutedLanderGetsNoCardNotification(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	require.NoError(t, g.f.run(t, g.workspace, "agents.notify", map[string]any{"session": g.lander, "policy": "muted"}, nil))

	g.cardAtCap(t)

	testutil.Never(t, func() bool {
		return slices.ContainsFunc(notesOf(g.f.notes(t), g.lander), func(n notePost) bool { return n.Kind == "needs-input" })
	}, quiet)
}

func TestAnsweredLandCardWithdrawsItsNotification(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	req, _ := g.cardAtCap(t)
	want := notePost{g.workspace, g.lander, g.f.session(t, g.workspace, g.lander).Title, "Opus review still asks for changes after 1 round · Land feat without another review?", "needs-input"}
	g.f.waitNotes(t, func(n []notePost) bool { return slices.Contains(n, want) })

	g.answer(t, req, "leave")

	testutil.WaitEvents(t, g.f.h.Bus, "agents.changed", func(events []bus.Event) bool {
		return slices.ContainsFunc(events, func(e bus.Event) bool {
			var c struct{ Session, Kind, Request, RequestKind string }
			require.NoError(t, json.Unmarshal(e.Payload, &c))
			return c == struct{ Session, Kind, Request, RequestKind string }{g.lander, "resolved", req, "gate"}
		})
	})
}

func TestRestartWithAnOpenCardPostsNoSecondNotification(t *testing.T) {
	t.Parallel()
	d := startDone(t, map[string]any{"rounds": 1}, map[string]any{}, nil)
	req := d.cardAtCap(t)
	d.f.waitNotes(t, func(n []notePost) bool { return len(notesOf(n, d.session)) == 1 })

	d.f.restartAgents(t, d.workspace)

	d.f.postItems(t, d.workspace, d.session, decision("Lists"))
	testutil.Eventually(t, func() bool { return d.f.session(t, d.workspace, d.session).OpenCount == 1 && d.held(t).Request == req }, wait)
	testutil.Never(t, func() bool { return len(notesOf(d.f.notes(t), d.session)) > 1 }, quiet)
}

func TestLanderTurnEndingUnderItsCardPostsNoFinished(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	g.cardAtCap(t)
	card := func(n notePost) bool { return n.Kind == "needs-input" }
	g.f.waitNotes(t, func(n []notePost) bool { return slices.ContainsFunc(notesOf(n, g.lander), card) })
	turns := doneTurns(t, testutil.WaitEvents(t, g.f.h.Bus, "agents.turn", func([]bus.Event) bool { return true }), g.lander)

	g.f.prompt(t, g.workspace, g.lander, "go")

	testutil.WaitEvents(t, g.f.h.Bus, "agents.turn", func(events []bus.Event) bool { return doneTurns(t, events, g.lander) > turns })
	testutil.Never(t, func() bool {
		n := notesOf(g.f.notes(t), g.lander)
		return slices.ContainsFunc(n[slices.IndexFunc(n, card):], func(p notePost) bool { return p.Kind == "done" })
	}, quiet)
}
