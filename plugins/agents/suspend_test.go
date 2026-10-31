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
	t.Setenv("CLUBMATE_AGENTS_SUSPEND_AFTER", suspendAfter.String())
	return start(t, script)
}

func suspendedSession(t *testing.T, script map[string]any) (*fixture, string, string) {
	t.Helper()
	f := startSuspending(t, script)
	project := f.project(t, newRepo(t))
	session := f.newSession(t, project)
	f.prompt(t, project, session, "one")
	f.waitStatus(t, project, session, "done")
	pid := f.calls(t, "start")[0].PID
	testutil.Eventually(t, func() bool { return errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH) }, wait)
	return f, project, session
}

func TestIdleSessionSuspendsAndResumesOnPrompt(t *testing.T) {
	f, project, session := suspendedSession(t, map[string]any{"resume": true})
	got := f.session(t, project, session)
	assert.Equal(t, "done", got.Status)
	assert.True(t, got.Suspended)

	f.prompt(t, project, session, "two")

	resumed := f.waitCall(t, "session/resume", func(fakeCall) bool { return true })
	var params struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(t, json.Unmarshal(resumed.Params, &params))
	assert.Equal(t, got.AgentSessionID, params.SessionID)
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "two" })
	assert.False(t, f.waitStatus(t, project, session, "done").Suspended)
	assert.Len(t, f.transcript(t, project, session).Turns, 2)
}

func TestSuspendedSessionKeepsMode(t *testing.T) {
	f, project, session := suspendedSession(t, map[string]any{
		"resume": true,
		"modes":  map[string]any{"currentModeId": "default", "availableModes": []map[string]string{{"id": "default", "name": "Manual"}, {"id": "plan", "name": "Plan"}}},
	})

	require.NoError(t, f.run(t, project, "agents.mode", map[string]any{"session": session, "mode": "plan"}, nil))
	f.waitCall(t, "session/set_mode", func(fakeCall) bool { return true })
	testutil.Eventually(t, func() bool { return f.session(t, project, session).Suspended }, wait)
	f.prompt(t, project, session, "two")

	f.waitStatus(t, project, session, "done")
	assert.Len(t, f.calls(t, "session/resume"), 2)
	assert.Len(t, f.calls(t, "session/set_mode"), 2)
	assert.Equal(t, "plan", f.transcript(t, project, session).Session.Mode)
}

func TestWaitOnSuspendedChild(t *testing.T) {
	f := startSuspending(t, map[string]any{"resume": true})
	project := f.project(t, newRepo(t))
	parent := f.newSession(t, project)
	kid := f.newChild(t, project, parent, map[string]any{"prompt": "one"}).Session
	f.waitStatus(t, project, kid, "done")
	testutil.Eventually(t, func() bool { return f.session(t, project, kid).Suspended }, wait)

	assert.Equal(t, 1, f.wait(t, project, parent, kid)[0].Turn)

	require.NoError(t, f.runAs(t, project, parent, "agents.prompt", map[string]any{"session": kid, "text": "two"}, nil))

	testutil.Eventually(t, func() bool {
		got := f.wait(t, project, parent, kid)[0]
		return got.Status == "done" && got.Turn == 2
	}, wait)
	f.waitCall(t, "session/resume", func(fakeCall) bool { return true })
}

func TestBackgroundShellDelaysSuspend(t *testing.T) {
	f := startSuspending(t, map[string]any{"resume": true, "prompt": []map[string]any{{"shell": "while :; do sleep 0.1; done"}}})
	project := f.project(t, newRepo(t))
	session := f.newSession(t, project)
	f.prompt(t, project, session, "one")
	f.waitStatus(t, project, session, "done")

	time.Sleep(4 * suspendAfter)
	require.False(t, f.session(t, project, session).Suspended)
	var shell int
	require.NoError(t, json.Unmarshal(f.calls(t, "shell")[0].Params, &shell))
	require.NoError(t, syscall.Kill(shell, syscall.SIGKILL))

	testutil.Eventually(t, func() bool { return f.session(t, project, session).Suspended }, wait)
}

func TestIdleSessionKeepsAgent(t *testing.T) {
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
			f := startSuspending(t, tt.script)
			project := f.project(t, newRepo(t))
			session := f.newSession(t, project)
			f.prompt(t, project, session, "one")
			f.waitStatus(t, project, session, tt.status)

			time.Sleep(4 * suspendAfter)

			assert.NoError(t, syscall.Kill(-f.calls(t, "start")[0].PID, 0))
			assert.False(t, f.session(t, project, session).Suspended)
		})
	}
}
