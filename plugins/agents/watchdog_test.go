package main_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

const (
	cutOffText = "Your turn was cut off by a restart"
	quiet      = 2 * time.Second
)

var longTurn = map[string]any{"resume": true, "prompt": []map[string]any{{"match": "long", "sleep": 5000}}}

func (f *fixture) promptsTo(t *testing.T, agentSession, substr string) []string {
	t.Helper()
	var out []string
	for _, c := range f.calls(t, "session/prompt") {
		var p struct {
			SessionID string `json:"sessionId"`
		}
		require.NoError(t, json.Unmarshal(c.Params, &p))
		if text := promptText(t, c); p.SessionID == agentSession && strings.Contains(text, substr) {
			out = append(out, text)
		}
	}
	return out
}

func (f *fixture) agentID(t *testing.T, workspace, session string) string {
	t.Helper()
	var id string
	testutil.Eventually(t, func() bool {
		id = f.session(t, workspace, session).AgentSessionID
		return id != ""
	}, wait)
	return id
}

func TestTurnCutOffByRestartContinues(t *testing.T) {
	f := start(t, longTurn)
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "long")
	agentID := f.agentID(t, workspace, session)
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	f.restartPlugin(t, workspace)

	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(promptText(t, c), cutOffText) })
	f.waitStatus(t, workspace, session, "done")
	assert.Len(t, f.promptsTo(t, agentID, cutOffText), 1)
}

func TestChildDoneWakesSuspendedParent(t *testing.T) {
	f := startSuspending(t, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "work", "sleep": 500}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "plan")
	parentID := f.waitStatus(t, workspace, parent, "done").AgentSessionID
	testutil.Eventually(t, func() bool { return f.session(t, workspace, parent).Suspended }, wait)

	kid := f.newChild(t, workspace, parent, map[string]any{"title": "Kid", "prompt": "work"}).Session

	f.waitCall(t, "session/resume", func(fakeCall) bool { return true })
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, "[Clubmate] Child")) > 0 }, wait)
	time.Sleep(quiet)
	assert.Equal(t, []string{`[Clubmate] Child "Kid" (` + kid + ") is done."}, f.promptsTo(t, parentID, "[Clubmate] Child"))
}

func TestChildrenDoneWakeParentOnce(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "plan", "sleep": 2000}}})
	workspace := f.workspace(t, newRepo(t))
	var lead struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"title": "Lead", "prompt": "plan"}, &lead))
	parent := lead.Session
	parentID := f.agentID(t, workspace, parent)
	first := f.newChild(t, workspace, parent, map[string]any{"title": "One", "prompt": "work"}).Session
	second := f.newChild(t, workspace, parent, map[string]any{"title": "Two", "prompt": "work"}).Session
	f.waitStatus(t, workspace, first, "done")
	f.waitStatus(t, workspace, second, "done")

	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, "[Clubmate] Child")) > 0 }, wait)
	time.Sleep(quiet)
	got := f.promptsTo(t, parentID, "[Clubmate] Child")
	require.Len(t, got, 1)
	assert.Contains(t, got[0], `Child "One" (`+first+") is done.")
	assert.Contains(t, got[0], `Child "Two" (`+second+") is done.")
}

func TestParentWaitingOnChildIsNotWoken(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"sleep": 500}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	parentID := f.agentID(t, workspace, parent)
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "work"}).Session

	assert.Equal(t, "done", f.wait(t, workspace, parent, kid)[0].Status)

	time.Sleep(quiet)
	assert.Empty(t, f.promptsTo(t, parentID, "[Clubmate] Child"))
}

func TestChildQuestionGoesToParent(t *testing.T) {
	ask := map[string]any{"match": "pick a store", "elicitation": askScript(false)}
	f := start(t, map[string]any{"prompt": []map[string]any{ask, ask}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	parentID := f.agentID(t, workspace, parent)
	kid := f.newChild(t, workspace, parent, map[string]any{"title": "Kid", "prompt": "pick a store"}).Session

	asking := f.waitStatus(t, workspace, kid, "waiting")
	assert.True(t, asking.AsksParent)
	assert.False(t, asking.NeedsYou)
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, "[Clubmate] Child")) > 0 }, wait)
	assert.Contains(t, f.promptsTo(t, parentID, "[Clubmate] Child")[0], `Child "Kid" (`+kid+`) asks you, not the user: "Which store?" (agents_elicitation request `+asking.Pending[0]+")")
	assert.NotEqual(t, "waiting", f.session(t, workspace, parent).ChildStatus)
	events, err := f.h.Bus.Read(t.Context(), "notify.posted", 0, 100)
	require.NoError(t, err)
	assert.Empty(t, events)

	answer := map[string]any{"session": kid, "request": asking.Pending[0], "action": "accept", "answers": []map[string]any{{"key": "question_0", "value": "Redis"}}}
	require.NoError(t, f.runAs(t, workspace, parent, "agents.elicitation", answer, nil))
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, "[Clubmate] Child")) == 2 }, wait)
	again := f.session(t, workspace, kid)
	require.Len(t, again.Pending, 1)
	assert.NotEqual(t, asking.Pending[0], again.Pending[0])
	assert.Contains(t, f.promptsTo(t, parentID, "[Clubmate] Child")[1], "agents_elicitation request "+again.Pending[0])
}

func TestChildSignInStaysWithUser(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "sign in", "elicitation": map[string]any{"mode": "url", "elicitationId": "e1", "url": "https://example.com/login", "message": "Sign in"}}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "sign in"}).Session

	waiting := f.waitStatus(t, workspace, kid, "waiting")
	assert.False(t, waiting.AsksParent)
	assert.True(t, waiting.NeedsYou)
}
