package main_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
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
	testutil.Eventually(t, func() bool { return len(f.turns(t, idle)) > 0 && len(f.turns(t, crashing)) > 0 }, wait)

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
	testutil.Never(t, func() bool { return f.session(t, workspace, session).Status != "running" }, 500*time.Millisecond)
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

func TestStopSavesEndedTurn(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_SAVE_HOLD=2s"}, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "long", "sleep": 4000}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "long")
	agentID := f.agentID(t, workspace, session)
	stored := func() string {
		raw, _, err := f.h.KV.Get(t.Context(), "agents", "session/"+workspace+"/"+session)
		require.NoError(t, err)
		var got storedState
		_ = json.Unmarshal(raw, &got)
		return got.Status
	}
	testutil.Eventually(t, func() bool { return stored() == "running" }, wait)
	f.waitStatus(t, workspace, session, "done")

	require.NoError(t, f.h.Sup.SetEnabled(t.Context(), "agents", false))
	assert.Equal(t, "done", stored())
	require.NoError(t, f.h.Sup.SetEnabled(t.Context(), "agents", true))

	f.waitStatus(t, workspace, session, "done")
	testutil.Never(t, func() bool { return len(f.promptsTo(t, agentID, cutOffText)) > 0 }, 2*time.Second)
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

func TestPromptShowsAsSendingUntilItIsSent(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_SEND_HOLD=4s"}, map[string]any{})
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	msg := session + "/msg/user/prompt-2"

	f.h.Event(t, workspace, "agents.main", session+"/composer/0", "send", map[string]any{"text": "second"})

	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		m := nodesWithID(n, msg)
		return len(m) == 1 && m[0].Props["sending"] == true && m[0].Props["text"] == "second"
	})
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		m := nodesWithID(n, msg)
		return len(m) == 1 && m[0].Props["sending"] == nil && m[0].Props["text"] == "second"
	})
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

func TestComposerAttachesADroppedImageByItsBytes(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"image": true, "prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	shot := filepath.Join(t.TempDir(), "Screenshot 2026-11-16 at 17.14.03.png")
	var img bytes.Buffer
	require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 3136, 1000))))
	require.NoError(t, os.WriteFile(shot, img.Bytes(), 0o600))
	logo := filepath.Join(t.TempDir(), "logo.png")
	require.NoError(t, os.WriteFile(logo, img.Bytes(), 0o600))
	notes := filepath.Join(t.TempDir(), "notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("hi"), 0o600))
	composer := session + "/composer/0"

	for _, path := range []string{shot, logo, notes} {
		f.h.Event(t, workspace, "agents.main", composer, "attachFile", map[string]any{"path": path})
	}
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		c := nodesWithID(n, composer)
		if len(c) != 1 {
			return false
		}
		raw, err := json.Marshal(c[0].Props["attachments"])
		require.NoError(t, err)
		return strings.Contains(string(raw), `"kind":"image","path":"Screenshot 2026-11-16 at 17.14.03.png"`)
	})
	require.NoError(t, os.Remove(shot))
	f.h.Event(t, workspace, "agents.main", composer, "send", map[string]any{"text": "see"})

	call := f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), `"type":"image"`) })
	var p struct {
		Prompt []struct {
			Type     string `json:"type"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
			URI      string `json:"uri"`
		} `json:"prompt"`
	}
	require.NoError(t, json.Unmarshal(call.Params, &p))
	var blocks []string
	for _, b := range p.Prompt {
		if b.URI != "" {
			b.Type += " " + filepath.Base(b.URI)
		}
		blocks = append(blocks, b.Type)
	}
	assert.Equal(t, []string{"text", "image", "image", "resource_link logo.png", "resource_link notes.txt", "text"}, blocks)
	assert.Equal(t, "image/png", p.Prompt[1].MimeType)
	data, err := base64.StdEncoding.DecodeString(p.Prompt[1].Data)
	require.NoError(t, err)
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, [2]int{1568, 500}, [2]int{cfg.Width, cfg.Height})
}
