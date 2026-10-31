package main_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func subagentStep(session string, update map[string]any) map[string]any {
	step := map[string]any{"update": update}
	if session != "" {
		step["session"] = session
	}
	return step
}

func announce(id, title, role string, state map[string]any) map[string]any {
	return map[string]any{"sessionUpdate": "subagent_update", "sessionId": id, "title": title, "description": role, "state": state}
}

func TestSubagentsNestUnderTheirCardAndSurviveRestart(t *testing.T) {
	running := map[string]any{"state": "running"}
	f := start(t, map[string]any{"resume": true, "prompt": []map[string]any{
		subagentStep("", announce("sub-1", "find auth", "Explore", running)),
		subagentStep("", map[string]any{"sessionUpdate": "session_message", "messageId": "toolu_1", "recipientSessionId": "sub-1", "content": []map[string]any{{"type": "text", "text": "Find the auth code"}}}),
		subagentStep("sub-1", map[string]any{"sessionUpdate": "tool_call", "toolCallId": "c1", "title": "Read auth.go", "kind": "read", "status": "completed"}),
		subagentStep("sub-1", announce("sub-2", "check tests", "", running)),
		subagentStep("sub-2", map[string]any{"sessionUpdate": "tool_call", "toolCallId": "c2", "title": "Grep TestAuth", "kind": "search", "status": "failed"}),
		subagentStep("sub-1", announce("sub-2", "check tests", "", map[string]any{"state": "idle", "stopReason": "error"})),
		subagentStep("sub-1", map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "Auth lives in auth.go"}}),
		subagentStep("", announce("sub-1", "find auth", "Explore", map[string]any{"state": "idle", "stopReason": "end_turn"})),
		subagentStep("", map[string]any{"sessionUpdate": "session_message", "messageId": "toolu_1:reply", "senderSessionId": "sub-1", "content": []map[string]any{{"type": "text", "text": "Auth lives in auth.go"}}}),
		subagentStep("", announce("sub-3", "", "Plan", running)),
		subagentStep("sub-3", map[string]any{"sessionUpdate": "tool_call", "toolCallId": "c3", "title": "Read plan.md", "kind": "read", "status": "in_progress"}),
	}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "done")

	assert.Contains(t, string(f.calls(t, "initialize")[0].Params), `"subagents":{}`)
	type card struct {
		ID, Title, Status, Body string
	}
	cards := func() []card {
		var out []card
		for _, en := range f.transcript(t, workspace, session).Entries {
			if en.Kind != "tool" {
				assert.NotEqual(t, "agent", en.Kind)
				continue
			}
			var c card
			var content []struct {
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			c.ID = en.ID
			require.NoError(t, json.Unmarshal(en.Tool["title"], &c.Title))
			require.NoError(t, json.Unmarshal(en.Tool["status"], &c.Status))
			require.NoError(t, json.Unmarshal(en.Tool["content"], &content))
			c.Body = content[0].Content.Text
			out = append(out, c)
		}
		return out
	}
	want := []card{
		{ID: "sub-1", Title: "Explore: find auth", Status: "completed", Body: "→ Find the auth code\n• Read auth.go\n↳ check tests — failed\nAuth lives in auth.go"},
		{ID: "sub-3", Title: "Plan · Read plan.md", Status: "in_progress", Body: "• Read plan.md"},
	}
	assert.Equal(t, want, cards())

	f.restartPlugin(t, workspace)
	want[1].Status = "cancelled"
	assert.Equal(t, want, cards())

	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": session}, nil))
	f.waitCall(t, "session/resume", func(fakeCall) bool { return true })
	want[1].Title = "Plan"
	testutil.Eventually(t, func() bool { return assert.ObjectsAreEqual(want, cards()) }, wait)
}

func TestStopSendsCancelToTheRunningSubagent(t *testing.T) {
	cancellable := announce("sub-1", "run tests", "", map[string]any{"state": "running"})
	cancellable["capabilities"] = map[string]any{"cancel": map[string]any{}}
	f := start(t, map[string]any{"prompt": []map[string]any{subagentStep("", cancellable)}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "toolCard")) == 1 })
	card := cardsOf(main, "toolCard")[0]
	assert.Equal(t, "agent", card.Props["kind"])

	f.h.Event(t, workspace, "agents.main", card.ID, "stopSubagent", nil)

	got := f.waitCall(t, "session/cancel", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"sessionId":"sub-1"}`, string(got.Params))
}

func TestSubagentPermissionAsksInTheParentSession(t *testing.T) {
	f := start(t, map[string]any{"after": []map[string]any{
		subagentStep("", announce("sub-1", "run tests", "", map[string]any{"state": "running"})),
		{"session": "sub-1", "permission": permissionScript()},
	}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "go")
	waiting := f.waitStatus(t, workspace, session, "waiting")
	require.Len(t, waiting.Pending, 1)
	perms := ofKind(f.transcript(t, workspace, session).Entries, "permission")
	require.Len(t, perms, 1)
	assert.Equal(t, "run tests", perms[0].Subagent)
	f.answerPermission(t, workspace, session, waiting.Pending[0])

	got := f.waitCall(t, "permission_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"outcome":{"outcome":"selected","optionId":"allow"}}`, string(got.Params))
}
