package main_test

import (
	"encoding/json"
	"testing"
	"time"

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

func TestTypedFailureInOrchestration(t *testing.T) {
	tests := []struct {
		name    string
		fail    string
		kind    string
		status  string
		prompts int
	}{
		{"rate limit backs off without turn retry", "Internal error: API Error: Request rejected (429)", "rate_limit", "done", 2},
		{"server error stops after Claude Code's retries", "Internal error: API Error: 500 Internal server error", "server_error", "error", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLUBMATE_AGENTS_NET_RETRY_AFTER", "10ms")
			f := start(t, map[string]any{"prompt": []map[string]any{{"match": "build it", "fail": tt.fail, "errorKind": tt.kind}}})
			workspace := f.workspace(t, newRepo(t))
			parent := f.newSession(t, workspace)
			kid := f.newChild(t, workspace, parent, map[string]any{"title": "Kid", "prompt": "build it"}).Session
			kidID := f.agentID(t, workspace, kid)

			f.waitStatus(t, workspace, kid, tt.status)
			time.Sleep(quiet)

			assert.Len(t, f.promptsTo(t, kidID, ""), tt.prompts)
			assert.Len(t, f.promptsTo(t, kidID, "Your turn ended with an error"), tt.prompts-1)
		})
	}
}
