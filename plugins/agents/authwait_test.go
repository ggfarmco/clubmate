package main_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

const signedOut = "Failed to authenticate. API Error: 401 OAuth session expired and could not be refreshed"

func messageChunk(text string) map[string]any {
	return map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}}}
}

func signedOutChunk() map[string]any {
	return messageChunk(signedOut)
}

func TestSignedOutSessionContinuesAfterAnotherTurnSucceeds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fail   map[string]any
		reason string
	}{
		{name: "auth required", fail: map[string]any{"fail": "Authentication required", "code": -32000}, reason: "Authentication required"},
		{name: "authentication failed", fail: map[string]any{"fail": "Internal error: " + signedOut, "errorKind": "authentication_failed"}, reason: signedOut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fail := map[string]any{"match": "go"}
			maps.Copy(fail, tt.fail)
			f := start(t, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "go", "update": signedOutChunk()["update"]}, fail}})
			workspace := f.workspace(t, newRepo(t))
			stopped := f.newSession(t, workspace)
			f.prompt(t, workspace, stopped, "go")
			f.waitStatus(t, workspace, stopped, "error")
			f.restartPlugin(t, workspace)

			other := f.newSession(t, workspace)
			f.prompt(t, workspace, other, "hello")

			f.waitCall(t, "session/prompt", func(c fakeCall) bool {
				return strings.Contains(string(c.Params), "Your turn ended with an error ("+tt.reason+")")
			})
			f.waitStatus(t, workspace, stopped, "done")
		})
	}
}

func TestFollowUpSignInFailureStopsTheSession(t *testing.T) {
	t.Parallel()
	gate := filepath.Join(t.TempDir(), "go")
	f := start(t, map[string]any{"after": []map[string]any{
		{"await": gate},
		signedOutChunk(),
		{"update": map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}},
	}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "done")

	require.NoError(t, os.WriteFile(gate, nil, 0o600))
	got := f.waitStatus(t, workspace, session, "error")
	assert.Equal(t, "Sign in to continue using Claude.", got.Error)
	assert.Equal(t, "end_turn", f.transcript(t, workspace, session).Turns[0].StopReason)
}

func TestFollowUpMessageAboutAuthenticationIsNotSignedOut(t *testing.T) {
	t.Parallel()
	gate := filepath.Join(t.TempDir(), "go")
	f := start(t, map[string]any{"after": []map[string]any{
		{"await": gate},
		messageChunk("Failed to authenticate to the registry, so I skipped the publish step."),
		{"update": map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}},
	}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "done")

	require.NoError(t, os.WriteFile(gate, nil, 0o600))
	testutil.Never(t, func() bool { return f.session(t, workspace, session).Status == "error" }, time.Second)
}
