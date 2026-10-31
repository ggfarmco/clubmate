package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/agents/api"
)

func TestRecordJSONContract(t *testing.T) {
	const want = `{"id":"agt_1","workspace":"ws","worktree":"/r","agent":"claude","agentSessionId":"acp-1",` +
		`"parentSession":"agt_p","advisor":"opus","fork":"abc","handoffFrom":"agt_f","handoffTo":"agt_t",` +
		`"importedFrom":"agt_i","title":"T","titleSet":true,"status":"closed","mode":"plan","finish":"merge",` +
		`"afk":true,"createdAt":"2026-10-31T12:00:00Z","updatedAt":"2026-10-31T12:00:00Z","closed":true,` +
		`"asks":true,"notify":"muted","error":"boom"}`
	var rec record
	require.NoError(t, json.Unmarshal([]byte(want), &rec))
	assert.Equal(t, "agt_p", rec.ParentSession)
	assert.Equal(t, "agt_i", rec.ImportedFrom)
	raw, err := json.Marshal(rec)
	require.NoError(t, err)
	assert.Equal(t, want, string(raw))

	info, err := json.Marshal(sessionInfo{record: record{ID: "agt_2", lineage: lineage{ParentSession: "agt_p"}}, NotifyEffective: notifyAll})
	require.NoError(t, err)
	assert.Equal(t, `{"id":"agt_2","workspace":"","worktree":"","agent":"","agentSessionId":"","parentSession":"agt_p","title":"","status":"","mode":"",`+
		`"createdAt":"0001-01-01T00:00:00Z","updatedAt":"0001-01-01T00:00:00Z","closed":false,"asks":false,"notifyEffective":"all"}`, string(info))
}

func TestSessionInfoDecodesAsAPISession(t *testing.T) {
	info := sessionInfo{record: record{
		ID:        "agt_1",
		Workspace: "ws",
		Worktree:  "/r",
		Title:     "T",
		lineage:   lineage{ParentSession: "agt_p", HandoffFrom: "agt_f"},
		Status:    statusClosed,
		Closed:    true,
	}}
	raw, err := json.Marshal(info)
	require.NoError(t, err)
	var got api.Session
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, api.Session{
		ID:            "agt_1",
		Workspace:     "ws",
		Worktree:      "/r",
		Title:         "T",
		ParentSession: "agt_p",
		HandoffFrom:   "agt_f",
		Status:        statusClosed,
		Closed:        true,
	}, got)
}
