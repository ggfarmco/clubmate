package main_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
)

const askedText = "queued: approved; waits for the user's yes"

func (q queueSetup) askCard(t *testing.T, l queueLander) string {
	t.Helper()
	cards := q.f.session(t, q.workspace, l.id).CardRequests
	require.Len(t, cards, 1)
	return cards[0]
}

func TestAskLanderWaitsForUserYes(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	a := q.landerFinish(t, "A", "x.go", "ask")
	require.ErrorContains(t, q.land(t, a), "review pending")
	q.approve(t, a)
	tip := git(t, q.repo, "rev-parse", "main")

	err := q.land(t, a)

	require.ErrorContains(t, err, askedText)
	card := q.askCard(t, a)
	assert.True(t, strings.HasPrefix(card, "lq_"))
	testutil.WaitEvents(t, q.f.h.Bus, "notify.posted", func(events []bus.Event) bool {
		return slices.ContainsFunc(events, func(e bus.Event) bool {
			var p struct{ Request, Message string }
			require.NoError(t, json.Unmarshal(e.Payload, &p))
			return p.Request == card && p.Message == "Land into main? · 1 commit · review approved"
		})
	})
	props := q.f.phoneTranscript(t, q.workspace, a.id).Request.Props
	assert.Equal(t, "Land into main?", props["title"])
	assert.Equal(t, "1 commit · review approved", props["detail"])
	entry := q.queue(t).Entries[0]
	assert.Equal(t, "asks", entry.State)
	assert.True(t, entry.NeedsYou)
	assert.True(t, q.f.session(t, q.workspace, a.id).NeedsYou)
	assert.Equal(t, tip, git(t, q.repo, "rev-parse", "main"))
}

func TestUserYesPromptsLanderToLand(t *testing.T) {
	t.Parallel()
	q := startQueue(t, false)
	a := q.landerFinish(t, "A", "x.go", "ask")
	require.ErrorContains(t, q.land(t, a), askedText)
	agent := q.f.session(t, q.workspace, a.id).AgentSessionID

	require.NoError(t, q.f.run(t, q.workspace, "agents.land-answer", map[string]any{"request": q.askCard(t, a), "action": "merge"}, nil))

	testutil.Eventually(t, func() bool { return len(q.f.promptsTo(t, agent, "Call git_land again")) == 1 }, wait)
	q.f.restartPlugin(t, q.workspace)
	tip := git(t, q.repo, "rev-parse", "main")
	require.NoError(t, q.land(t, a))
	assert.NotEqual(t, tip, git(t, q.repo, "rev-parse", "main"))
	assert.Empty(t, q.f.session(t, q.workspace, a.id).CardRequests)
}

func TestAskHeadIsPassedByReadyEntry(t *testing.T) {
	t.Parallel()
	q := startQueue(t, false)
	a, b := q.landerFinish(t, "A", "x.go", "ask"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), askedText)

	require.NoError(t, q.land(t, b))

	q.waitTurn(t, a)
	assert.Equal(t, "head", q.queue(t).Entries[0].State)
	assert.Empty(t, q.f.session(t, q.workspace, a.id).CardRequests)
}

func TestAgentCannotAnswerQueueAsk(t *testing.T) {
	t.Parallel()
	q := startQueue(t, false)
	a := q.landerFinish(t, "A", "x.go", "ask")
	require.ErrorContains(t, q.land(t, a), askedText)

	err := q.f.runFrom(t, q.workspace, a.id, "agents.land-answer", map[string]any{"request": q.askCard(t, a), "action": "merge"}, nil)

	require.ErrorContains(t, err, "only the user")
	assert.Equal(t, "asks", q.queue(t).Entries[0].State)
}

func TestGateCardLandIsTheYes(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	require.NoError(t, g.f.run(t, g.workspace, "agents.config", map[string]any{"session": g.lander, "id": "finish", "value": "ask"}, nil))
	request, _ := g.cardAtCap(t)

	g.answer(t, request, "merge")

	require.NoError(t, g.land(t))
}
