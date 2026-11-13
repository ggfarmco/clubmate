package main_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/agents/api"
)

func TestCloseWhileAttachingStopsAgent(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"newSleep": 1500})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.waitCall(t, "session/new", func(fakeCall) bool { return true })

	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": session}, nil))

	starts := f.calls(t, "start")
	require.Len(t, starts, 1)
	testutil.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(starts[0].PID, 0), syscall.ESRCH)
	}, wait)
	assert.Equal(t, "closed", f.session(t, workspace, session).Status)
}

func TestHandoffMovesChildren(t *testing.T) {
	t.Parallel()
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "plan")
	f.waitStatus(t, workspace, parent, "done")
	kid := f.newChild(t, workspace, parent, map[string]any{"title": "Kid", "prompt": "work"}).Session

	var next struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.handoff", map[string]any{"session": parent}, &next))

	assert.Equal(t, next.Session, f.session(t, workspace, kid).ParentSession)
}

func TestAgentExitKeepsIdleSessionState(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "crash", "exit": 3}, {"echo": true}}})
	workspace := f.workspace(t, newRepo(t))
	idle, crashing := f.newSession(t, workspace), f.newSession(t, workspace)
	f.prompt(t, workspace, idle, "Ship it?")
	require.True(t, f.waitStatus(t, workspace, idle, "done").Asks)

	f.prompt(t, workspace, crashing, "crash")
	f.waitStatus(t, workspace, crashing, "error")

	got := f.session(t, workspace, idle)
	assert.Equal(t, "done", got.Status)
	assert.True(t, got.Asks)
	assert.Equal(t, []string{"done"}, turnStatuses(f.turns(t, idle)))
}

func TestOldTurnEndLeavesNewTurnRunning(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "slow", "sleep": 3000}, {"match": "late", "sleep": 5000}}})
	workspace := f.workspace(t, newRepo(t))
	keeper, session := f.newSession(t, workspace), f.newSession(t, workspace)
	f.prompt(t, workspace, keeper, "hi")
	f.waitStatus(t, workspace, keeper, "done")
	f.prompt(t, workspace, session, "slow")
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "slow" })

	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": session}, nil))
	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": session}, nil))
	f.waitStatus(t, workspace, session, "idle")
	f.prompt(t, workspace, session, "late")
	testutil.Eventually(t, func() bool { return len(f.calls(t, "prompt_end")) >= 2 }, wait)
	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, "running", f.session(t, workspace, session).Status)
	f.waitStatus(t, workspace, session, "done")
}

func TestQueuedPromptSurvivesRestart(t *testing.T) {
	t.Parallel()
	f := start(t, longTurn)
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "long")
	agentID := f.agentID(t, workspace, session)
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	f.prompt(t, workspace, session, "follow up")
	testutil.Eventually(t, func() bool {
		raw, _, err := f.h.KV.Get(t.Context(), "agents", "session/"+workspace+"/"+session)
		require.NoError(t, err)
		return strings.Contains(string(raw), "follow up")
	}, wait)

	f.restartPlugin(t, workspace)

	f.waitStatus(t, workspace, session, "done")
	got := f.promptsTo(t, agentID, cutOffText)
	require.Len(t, got, 1)
	assert.True(t, strings.HasPrefix(got[0], "[Clubmate] "+cutOffText), got[0])
	assert.True(t, strings.HasSuffix(got[0], "\n\nfollow up"), got[0])
}

func TestQueuedImageSurvivesRestart(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"resume": true, "image": true, "prompt": []map[string]any{{"match": "long", "sleep": 5000}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "long")
	agentID := f.agentID(t, workspace, session)
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	data := base64.StdEncoding.EncodeToString(make([]byte, 300<<10))
	image := map[string]any{"kind": "image", "mimeType": "image/png", "data": data}
	require.NoError(t, f.run(t, workspace, "agents.prompt", map[string]any{"session": session, "text": "see this", "attachments": []any{image}}, nil))
	testutil.Eventually(t, func() bool {
		raw, _, err := f.h.KV.Get(t.Context(), "agents", "session/"+workspace+"/"+session)
		require.NoError(t, err)
		return strings.Contains(string(raw), "see this")
	}, wait)

	f.restartPlugin(t, workspace)

	f.waitStatus(t, workspace, session, "done")
	var sent []acp.ContentBlock
	for _, c := range f.calls(t, "session/prompt") {
		var p acp.PromptRequest
		require.NoError(t, json.Unmarshal(c.Params, &p))
		if p.SessionID == agentID && strings.Contains(promptText(t, c), "see this") {
			sent = append(sent, p.Prompt...)
		}
	}
	assert.Contains(t, sent, acp.ContentBlock{Type: "image", Data: data, MimeType: "image/png"})
}

func turnStatuses(turns []api.Turn) []string {
	out := make([]string, 0, len(turns))
	for _, turn := range turns {
		out = append(out, turn.Status)
	}
	return out
}

func entryTexts(entries []entry) []string {
	out := make([]string, 0, len(entries))
	for _, en := range entries {
		out = append(out, en.Text)
	}
	return out
}

func TestDequeuedPromptIsNotSentAgainAfterRestart(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_SAVE_HOLD=2s"}, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "long", "sleep": 5000}, {"match": "follow up", "sleep": 5000}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "long")
	agentID := f.agentID(t, workspace, session)
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	f.prompt(t, workspace, session, "follow up")
	testutil.Eventually(t, func() bool {
		raw, _, err := f.h.KV.Get(t.Context(), "agents", "session/"+workspace+"/"+session)
		require.NoError(t, err)
		return strings.Contains(string(raw), "follow up")
	}, wait)
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "follow up" })

	f.restartPlugin(t, workspace)

	f.waitStatus(t, workspace, session, "done")
	assert.Len(t, f.promptsTo(t, agentID, "follow up"), 1)
}

func TestPromptCutByAgentExitBeforeSendIsQueued(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_SEND_HOLD=2s"}, map[string]any{"resume": true})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "first")
	agentID := f.waitStatus(t, workspace, session, "done").AgentSessionID
	pid := f.calls(t, "session/prompt")[0].PID
	f.prompt(t, workspace, session, "second")
	testutil.Eventually(t, func() bool { return len(f.transcript(t, workspace, session).Turns) == 2 }, wait)

	require.NoError(t, syscall.Kill(pid, syscall.SIGKILL))
	f.waitStatus(t, workspace, session, "error")
	f.prompt(t, workspace, session, "third")

	f.waitStatus(t, workspace, session, "done")
	assert.Equal(t, []string{"second\n\nthird"}, f.promptsTo(t, agentID, "second"))
}

func TestPromptCutByRestartBeforeSendIsSentOnce(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_SEND_HOLD=2s"}, map[string]any{"resume": true})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "first")
	agentID := f.waitStatus(t, workspace, session, "done").AgentSessionID
	f.prompt(t, workspace, session, "second")
	testutil.Eventually(t, func() bool { return len(f.transcript(t, workspace, session).Turns) == 2 }, wait)

	f.restartPlugin(t, workspace)

	f.waitStatus(t, workspace, session, "done")
	assert.Equal(t, []string{"second"}, f.promptsTo(t, agentID, "second"))
	assert.Empty(t, f.promptsTo(t, agentID, cutOffText))
}

func TestLongQueuedTextSurvivesRestart(t *testing.T) {
	t.Parallel()
	f := start(t, longTurn)
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "long")
	agentID := f.agentID(t, workspace, session)
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	big := strings.Repeat("x", 300<<10)
	f.prompt(t, workspace, session, big)
	testutil.Eventually(t, func() bool {
		raw, _, err := f.h.KV.Get(t.Context(), "agents", "session/"+workspace+"/"+session)
		require.NoError(t, err)
		return strings.Contains(string(raw), `"queueSpilled":true`)
	}, wait)

	f.restartPlugin(t, workspace)

	f.waitStatus(t, workspace, session, "done")
	assert.Len(t, f.promptsTo(t, agentID, big), 1)
}

func TestPromptHeldAtShutdownIsSentAfterRestart(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_SEND_HOLD=1s"}, map[string]any{"resume": true, "linger": true})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "first")
	agentID := f.waitStatus(t, workspace, session, "done").AgentSessionID
	f.prompt(t, workspace, session, "second")
	testutil.Eventually(t, func() bool { return len(f.transcript(t, workspace, session).Turns) == 2 }, wait)
	pids := f.h.Sup.PIDs()

	require.NoError(t, f.h.Sup.Restart("agents"))
	testutil.Eventually(t, func() bool {
		now := f.h.Sup.PIDs()
		return len(now) == 1 && now[0] != pids[0] && f.run(t, workspace, "agents.sessions", map[string]any{}, nil) == nil
	}, restartWait)

	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, agentID, "second")) > 0 }, wait)
	assert.Equal(t, []string{"second"}, f.promptsTo(t, agentID, "second"))
}
