package main_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/internal/testutil"
)

const noResponse = "API Error: No response from API (waited 3m, then 10m on the retry)."

func TestNetworkErrorRetriedInOrchestration(t *testing.T) {
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
			t.Setenv("CLUBMATE_AGENTS_NET_RETRY_AFTER", tt.after)
			f := start(t, map[string]any{"prompt": tt.fail})
			workspace := f.workspace(t, newRepo(t))
			parent := f.newSession(t, workspace)
			parentID := f.agentID(t, workspace, parent)
			kid := f.newChild(t, workspace, parent, map[string]any{"title": "Kid", "prompt": "build it"}).Session
			kidID := f.agentID(t, workspace, kid)

			testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, "[Clubmate] Child")) > 0 }, wait)
			time.Sleep(quiet)

			assert.Equal(t, []string{`[Clubmate] Child "Kid" (` + kid + ") is " + tt.wake + "."}, f.promptsTo(t, parentID, "[Clubmate] Child"))
			assert.Len(t, f.promptsTo(t, kidID, ""), tt.prompts)
		})
	}
}
