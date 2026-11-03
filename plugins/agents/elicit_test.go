package main_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

func askScript(multi bool) map[string]any {
	options := []map[string]any{
		{"const": "Postgres", "title": "Postgres", "description": "Relational"},
		{"const": "Redis", "title": "Redis"},
	}
	question := map[string]any{"type": "string", "title": "Store", "oneOf": options}
	if multi {
		question = map[string]any{"type": "array", "title": "Store", "items": map[string]any{"anyOf": options}}
	}
	properties := map[string]any{
		"question_0":        question,
		"question_0_custom": map[string]any{"type": "string", "title": "Other"},
	}
	if multi {
		properties["question_1"] = map[string]any{"type": "string", "title": "Cache", "oneOf": options}
	}
	return map[string]any{
		"mode":            "form",
		"toolCallId":      "ask1",
		"message":         "Which store?",
		"requestedSchema": map[string]any{"type": "object", "properties": properties},
	}
}

func elicitationForm(t *testing.T, f *fixture, workspace string) sdui.Node {
	t.Helper()
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "form"), func(c sdui.Node) bool { return c.Props["action"] == "elicitation" })
	})
	forms := cardsOf(main, "form")
	return forms[slices.IndexFunc(forms, func(c sdui.Node) bool { return c.Props["action"] == "elicitation" })]
}

func TestElicitationOptionPick(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"elicitation": askScript(false)}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")

	waiting := f.waitStatus(t, workspace, session, "waiting")
	asks := ofKind(f.transcript(t, workspace, session).Entries, "elicitation")
	require.Len(t, asks, 1)
	assert.Equal(t, []string{asks[0].ID}, waiting.Pending)
	require.NotNil(t, waiting.PendingSummary)
	assert.Equal(t, "question", waiting.PendingSummary.Kind)
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool {
		return strings.Contains(string(rawTree(t, n)), `"detail":"asking"`)
	})
	init := f.waitCall(t, "initialize", func(fakeCall) bool { return true })
	assert.Contains(t, string(init.Params), `"elicitation":{"form":{},"url":{}}`)

	form := elicitationForm(t, f, workspace)
	require.Len(t, cardsOf(form, "card"), 1)
	assert.Equal(t, "warning", cardsOf(form, "card")[0].Props["tone"])
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "composer")) == 1 })
	assert.Equal(t, true, cardsOf(main, "composer")[0].Props["docked"])
	buttons := buttonsWith(form, "elicitationPick")
	i := slices.IndexFunc(buttons, func(b sdui.Node) bool { return b.Props["label"] == "Redis" })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, "opt+2", buttons[i].Props["shortcut"])
	f.h.Event(t, workspace, "agents.main", buttons[i].ID, "elicitationPick", nil)

	got := f.waitCall(t, "elicitation_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"action":"accept","content":{"question_0":"Redis"}}`, string(got.Params))
	f.waitStatus(t, workspace, session, "done")
}

func TestElicitationFormSubmit(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"elicitation": askScript(true)}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")

	form := elicitationForm(t, f, workspace)
	require.Len(t, cardsOf(form, "select"), 1)
	values := map[string]any{form.ID + "/f/0/0": true, form.ID + "/f/0/1": false, form.ID + "/f/1": "SQLite", form.ID + "/f/2": "Redis"}
	f.h.Event(t, workspace, "agents.main", form.ID, "elicitation", values)

	got := f.waitCall(t, "elicitation_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"action":"accept","content":{"question_0":["Postgres"],"question_0_custom":"SQLite","question_1":"Redis"}}`, string(got.Params))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return strings.Contains(string(rawTree(t, n)), "Postgres · SQLite · Redis · Which store?")
	})
}

func TestElicitationCancelledWithTurn(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"elicitation": askScript(false)}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "waiting")

	require.NoError(t, f.run(t, workspace, "agents.cancel", map[string]any{"session": session}, nil))

	got := f.waitCall(t, "elicitation_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"action":"cancel"}`, string(got.Params))
	f.waitStatus(t, workspace, session, "done")
}

func TestElicitationURLCompletes(t *testing.T) {
	t.Parallel()
	signIn := map[string]any{"mode": "url", "message": "Authenticate with MCP server linear", "url": "https://example.com/auth", "elicitationId": "e1"}
	f := start(t, map[string]any{"prompt": []map[string]any{{"elicitation": signIn, "complete": "e1"}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")

	got := f.waitCall(t, "elicitation_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"action":"accept"}`, string(got.Params))
	f.waitStatus(t, workspace, session, "done")
}

func TestElicitationRemoteDecline(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"elicitation": askScript(false)}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "waiting")
	request := ofKind(f.transcript(t, workspace, session).Entries, "elicitation")[0].ID
	params := map[string]any{"id": "agents.elicitation", "workspaceId": workspace, "args": map[string]any{"session": session, "request": request, "action": "accept", "answers": []map[string]any{{"key": "question_0", "value": "Mongo"}}}}

	assert.ErrorContains(t, f.h.PluginCall(t, "remote", "command.run", params), "unknown permission option")
	params["args"] = map[string]any{"session": session, "request": request, "action": "decline"}
	require.NoError(t, f.h.PluginCall(t, "remote", "command.run", params))

	got := f.waitCall(t, "elicitation_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"action":"decline"}`, string(got.Params))
	f.waitResolved(t, api.Changed{Workspace: workspace, Session: session, Kind: api.ChangeResolved, Request: request, RequestKind: api.RequestElicitation})
}
