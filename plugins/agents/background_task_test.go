package main_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

func backgroundCall(task string) map[string]any {
	return map[string]any{"update": map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "bg",
		"_meta":         map[string]any{"claudeCode": map[string]any{"toolResponse": map[string]any{"backgroundTaskId": task}}},
	}}
}

func followUp(delay int) []map[string]any {
	return []map[string]any{
		{"sleep": delay},
		{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "CI is green"}}},
		{"sleep": 1500},
		{"update": map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}},
	}
}

func endedTurn(t *testing.T, f *fixture, workspace, session string) record {
	t.Helper()
	testutil.Eventually(t, func() bool {
		turns := f.transcript(t, workspace, session).Turns
		return len(turns) == 1 && turns[0].StopReason != ""
	}, wait)
	return f.session(t, workspace, session)
}

func TestBackgroundTaskKeepsSessionRunningUntilFollowUpEnds(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{
		"prompt": []map[string]any{backgroundCall("b1"), {"shell": "sleep 30; :"}},
		"after":  followUp(1500),
	})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "watch CI")

	assert.Equal(t, "running", endedTurn(t, f, workspace, session).Status)
	f.waitStatus(t, workspace, session, "done")
}

func TestAgentFollowUpWithoutPromptRuns(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"after": followUp(2000)})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")

	f.waitStatus(t, workspace, session, "done")
	f.waitStatus(t, workspace, session, "running")
	f.waitStatus(t, workspace, session, "done")
}

func TestFinishedBackgroundTaskEndsTurnDone(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{backgroundCall("b1")}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")

	assert.Equal(t, "done", endedTurn(t, f, workspace, session).Status)
}

func TestCancelStopsWaitingForBackgroundTask(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{backgroundCall("b1"), {"shell": "sleep 30; :"}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "watch CI")
	require.Equal(t, "running", endedTurn(t, f, workspace, session).Status)

	require.NoError(t, f.run(t, workspace, "agents.cancel", map[string]any{"session": session}, nil))
	f.waitStatus(t, workspace, session, "done")
}
