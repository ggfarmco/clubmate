package main_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlimTranscriptSummarisesToolCallsAndToolCallSendsOneInFull(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	path := filepath.Join(repo, "a.go")
	edit := map[string]any{
		"sessionUpdate": "tool_call", "toolCallId": "e1", "title": "Edit a.go", "kind": "edit", "status": "completed",
		"content":  []map[string]any{{"type": "diff", "path": path, "oldText": "one\ntwo\nthree\n", "newText": "one\n2\nthree\nfour\n"}},
		"rawInput": map[string]any{"file_path": path, "old_string": "two", "new_string": "2", "description": "Swap"},
	}
	read := map[string]any{
		"sessionUpdate": "tool_call", "toolCallId": "r1", "title": "Read a.go", "kind": "read", "status": "completed",
		"content": []map[string]any{{"type": "content", "content": map[string]any{"type": "text", "text": "package a"}}},
	}
	f := start(t, map[string]any{"prompt": []map[string]any{{"update": edit}, {"update": read}}})
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "done")

	var slim struct {
		Entries []struct {
			ID      string                     `json:"id"`
			Tool    map[string]json.RawMessage `json:"tool"`
			Summary json.RawMessage            `json:"toolSummary"`
		} `json:"entries"`
	}
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session, "compact": true, "slim": true}, &slim))
	tools := map[string]int{}
	for i, en := range slim.Entries {
		if en.Tool != nil {
			tools[en.ID] = i
			assert.NotContains(t, en.Tool, "content", en.ID)
		}
	}
	require.Contains(t, tools, "e1")
	require.Contains(t, tools, "r1")
	e1, r1 := slim.Entries[tools["e1"]], slim.Entries[tools["r1"]]
	assert.JSONEq(t, `{"diffs":[{"path":"`+path+`","added":2,"removed":1}]}`, string(e1.Summary))
	assert.JSONEq(t, `{"description":"Swap"}`, string(e1.Tool["rawInput"]))
	assert.JSONEq(t, `{"output":true}`, string(r1.Summary))

	var full struct {
		Tool map[string]json.RawMessage `json:"tool"`
	}
	require.NoError(t, f.run(t, workspace, "agents.tool-call", map[string]any{"session": session, "tool": "e1"}, &full))
	assert.Contains(t, string(full.Tool["content"]), `"newText":"one\n2\nthree\nfour\n"`)
	assert.Contains(t, string(full.Tool["rawInput"]), `"old_string":"two"`)
	assert.ErrorContains(t, f.run(t, workspace, "agents.tool-call", map[string]any{"session": session, "tool": "nope"}, nil), "no such tool call")
}

func TestAPollWithTheSameHeaderRevGetsNoHeader(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	f := start(t, map[string]any{})
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "done")

	header := []string{"modes", "configOptions", "commands", "turns"}
	fetch := func(args map[string]any) map[string]json.RawMessage {
		t.Helper()
		args["session"], args["compact"] = session, true
		var out map[string]json.RawMessage
		require.NoError(t, f.run(t, workspace, "agents.transcript", args, &out))
		return out
	}
	var rev, cursor string
	full := fetch(map[string]any{})
	require.NoError(t, json.Unmarshal(full["headerRev"], &rev))
	require.NoError(t, json.Unmarshal(full["cursor"], &cursor))
	require.NotEmpty(t, rev)

	kept := fetch(map[string]any{"since": cursor, "headerRev": rev})
	for _, key := range header {
		assert.NotContains(t, kept, key)
	}
	assert.JSONEq(t, `["modes","configOptions","commands","turns"]`, string(kept["kept"]))
	assert.JSONEq(t, string(full["headerRev"]), string(kept["headerRev"]))
	assert.Contains(t, kept, "session")

	for name, args := range map[string]map[string]any{
		"other rev":  {"since": cursor, "headerRev": rev + "x"},
		"bad cursor": {"since": "nope", "headerRev": rev},
		"last":       {"since": cursor, "headerRev": rev, "last": true},
		"fromTurn":   {"fromTurn": 1, "headerRev": rev},
	} {
		got := fetch(args)
		assert.NotContains(t, got, "kept", name)
		for _, key := range header {
			assert.Contains(t, got, key, name)
		}
	}
}
