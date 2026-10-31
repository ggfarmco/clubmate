package main_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLimitFailureEndsTurnWithTitle(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"fail": "Internal error: max_output_tokens", "errorKind": "max_output_tokens"}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "boom")

	got := f.waitStatus(t, workspace, session, "error")
	assert.Equal(t, "This Claude turn reached its configured limit.", got.Error)
	assert.Len(t, f.calls(t, "session/prompt"), 1)
	notices := ofKind(f.transcript(t, workspace, session).Entries, "notice")
	require.Len(t, notices, 1)
	var n struct {
		Description string `json:"description"`
	}
	require.NoError(t, json.Unmarshal(notices[0].Notice, &n))
	assert.Contains(t, n.Description, "Internal error: max_output_tokens")
}

func TestRetryableFailureBacksOffWithoutTurnRetry(t *testing.T) {
	t.Setenv("CLUBMATE_AGENTS_NET_RETRY_AFTER", "10ms")
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "build it", "fail": "Internal error: API Error: 529", "errorKind": "overloaded"}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"title": "Kid", "prompt": "build it"}).Session
	kidID := f.agentID(t, workspace, kid)

	f.waitStatus(t, workspace, kid, "done")

	assert.Len(t, f.promptsTo(t, kidID, "Your turn ended with an error (Internal error: API Error: 529)"), 1)
	assert.Empty(t, f.promptsTo(t, kidID, "Your previous reply ended with an error"))
}
