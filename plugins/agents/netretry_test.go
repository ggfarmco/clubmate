package main_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/internal/testutil"
)

const noResponse = "API Error: No response from API (waited 3m, then 10m on the retry)."

func TestNetworkErrorRetriedInOrchestration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		after   string
		fail    []map[string]any
		prompts int
		wake    string
	}{
		{"retry succeeds without waking the parent", "1500ms", []map[string]any{{"match": "build it", "fail": noResponse, "errorKind": "server_error"}}, 2, "done"},
		{"parent is woken once retries run out", "10ms", []map[string]any{{"match": "build it", "fail": noResponse, "errorKind": "server_error"}, {"match": "Your turn ended with an error", "fail": noResponse, "errorKind": "server_error"}}, 9, "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := startEnv(t, []string{"CLUBMATE_AGENTS_NET_RETRY_AFTER=" + tt.after}, map[string]any{"prompt": tt.fail})
			workspace := f.workspace(t, newRepo(t))
			parent := f.newSession(t, workspace)
			parentID := f.agentID(t, workspace, parent)
			kid := f.newChild(t, workspace, parent, map[string]any{"title": "Kid", "prompt": "build it"}).Session
			kidID := f.agentID(t, workspace, kid)

			testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, "[Clubmate] Child")) > 0 }, wait)
			testutil.Never(t, func() bool {
				return len(f.promptsTo(t, parentID, "[Clubmate] Child")) > 1 || len(f.promptsTo(t, kidID, "")) > tt.prompts
			}, quiet)

			assert.Equal(t, []string{`[Clubmate] Child "Kid" (` + kid + ") is " + tt.wake + "."}, f.promptsTo(t, parentID, "[Clubmate] Child"))
			assert.Len(t, f.promptsTo(t, kidID, ""), tt.prompts)
		})
	}
}

func TestNetworkRetryWaitsForTheFollowUpCycleToEnd(t *testing.T) {
	t.Parallel()
	end := filepath.Join(t.TempDir(), "end")
	f := startEnv(t, []string{"CLUBMATE_AGENTS_NET_RETRY_AFTER=300ms"}, map[string]any{
		"prompt":    []map[string]any{{"match": "build it", "fail": noResponse, "errorKind": "server_error"}},
		"afterFail": true,
		"after": []map[string]any{
			{"match": "build it", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "still building"}}},
			{"match": "build it", "await": end},
			{"match": "build it", "update": map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}},
		},
	})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"title": "Kid", "prompt": "build it"}).Session
	kidID := f.agentID(t, workspace, kid)
	testutil.Never(t, func() bool { return len(f.promptsTo(t, kidID, "")) > 1 }, quiet)

	write(t, end, "")

	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, kidID, "Your turn ended with an error")) == 1 }, wait)
}
