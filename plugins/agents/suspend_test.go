package main_test

import (
	"encoding/json"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

const suspendAfter = 300 * time.Millisecond

func startSuspending(t *testing.T, script map[string]any) *fixture {
	t.Helper()
	return startEnv(t, []string{"CLUBMATE_AGENTS_SUSPEND_AFTER=" + suspendAfter.String()}, script)
}

func suspendedSession(t *testing.T, script map[string]any) (*fixture, string, string) {
	t.Helper()
	f := startSuspending(t, script)
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	f.waitStatus(t, workspace, session, "done")
	pid := f.calls(t, "start")[0].PID
	testutil.Eventually(t, func() bool { return errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH) }, wait)
	return f, workspace, session
}

func TestIdleSessionSuspendsAndResumesOnPrompt(t *testing.T) {
	t.Parallel()
	f, workspace, session := suspendedSession(t, map[string]any{"resume": true})
	got := f.session(t, workspace, session)
	assert.Equal(t, "done", got.Status)
	assert.True(t, got.Suspended)

	f.prompt(t, workspace, session, "two")

	resumed := f.waitCall(t, "session/resume", func(fakeCall) bool { return true })
	var params struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(t, json.Unmarshal(resumed.Params, &params))
	assert.Equal(t, got.AgentSessionID, params.SessionID)
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "two" })
	assert.False(t, f.waitStatus(t, workspace, session, "done").Suspended)
	assert.Len(t, f.transcript(t, workspace, session).Turns, 2)
}

func TestSuspendedSessionKeepsMode(t *testing.T) {
	t.Parallel()
	f, workspace, session := suspendedSession(t, map[string]any{
		"resume": true,
		"modes":  map[string]any{"currentModeId": "default", "availableModes": []map[string]string{{"id": "default", "name": "Manual"}, {"id": "plan", "name": "Plan"}}},
	})

	require.NoError(t, f.run(t, workspace, "agents.mode", map[string]any{"session": session, "mode": "plan"}, nil))
	f.waitCall(t, "session/set_mode", func(fakeCall) bool { return true })
	testutil.Eventually(t, func() bool { return f.session(t, workspace, session).Suspended }, wait)
	f.prompt(t, workspace, session, "two")

	f.waitStatus(t, workspace, session, "done")
	assert.Len(t, f.calls(t, "session/resume"), 2)
	assert.Len(t, f.calls(t, "session/set_mode"), 2)
	assert.Equal(t, "plan", f.transcript(t, workspace, session).Session.Mode)
}

func TestWaitOnSuspendedChild(t *testing.T) {
	t.Parallel()
	f := startSuspending(t, map[string]any{"resume": true})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "one"}).Session
	f.waitStatus(t, workspace, kid, "done")
	testutil.Eventually(t, func() bool { return f.session(t, workspace, kid).Suspended }, wait)

	assert.Equal(t, 1, f.wait(t, workspace, parent, kid)[0].Turn)

	require.NoError(t, f.runAs(t, workspace, parent, "agents.prompt", map[string]any{"session": kid, "text": "two"}, nil))

	testutil.Eventually(t, func() bool {
		got := f.wait(t, workspace, parent, kid)[0]
		return got.Status == "done" && got.Turn == 2
	}, wait)
	f.waitCall(t, "session/resume", func(fakeCall) bool { return true })
}

func TestBackgroundShellDelaysSuspend(t *testing.T) {
	t.Parallel()
	f := startSuspending(t, map[string]any{"resume": true, "prompt": []map[string]any{{"shell": "while :; do sleep 0.1; done"}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	f.waitStatus(t, workspace, session, "done")

	time.Sleep(4 * suspendAfter)
	require.False(t, f.session(t, workspace, session).Suspended)
	var shell int
	require.NoError(t, json.Unmarshal(f.calls(t, "shell")[0].Params, &shell))
	require.NoError(t, syscall.Kill(shell, syscall.SIGKILL))

	testutil.Eventually(t, func() bool { return f.session(t, workspace, session).Suspended }, wait)
}

func TestIdleSessionKeepsAgent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		script map[string]any
		status string
	}{
		{"pending permission", map[string]any{"resume": true, "after": []map[string]any{{"permission": permissionScript()}}}, "waiting"},
		{"agent without resume", map[string]any{}, "done"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := startSuspending(t, tt.script)
			workspace := f.workspace(t, newRepo(t))
			session := f.newSession(t, workspace)
			f.prompt(t, workspace, session, "one")
			f.waitStatus(t, workspace, session, tt.status)

			time.Sleep(4 * suspendAfter)

			assert.NoError(t, syscall.Kill(-f.calls(t, "start")[0].PID, 0))
			assert.False(t, f.session(t, workspace, session).Suspended)
		})
	}
}
