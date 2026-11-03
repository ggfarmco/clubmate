package main_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendReviewRecipientFallback(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, repo)
	opener := f.newSession(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": opener}, nil))
	f.waitStatus(t, workspace, opener, "closed")
	args := map[string]any{"session": opener, "worktree": repo, "review": "rv_0000abcd", "base": "main", "head": "Working tree", "reviewed": 2, "total": 2, "comments": []map[string]any{{"id": 4, "file": "a.go", "side": "right", "lineStart": 1, "type": "issue", "text": "x"}, {"id": 7, "file": "b.go", "side": "left", "lineStart": 2, "type": "question", "text": "y"}}, "nrev": "/nrev"}

	assert.ErrorContains(t, f.run(t, workspace, "agents.send-review", args, nil), "only the editor-nvim plugin may send a review")
	send := func() {
		require.NoError(t, f.h.PluginCall(t, "editor-nvim", "command.run", map[string]any{"id": "agents.send-review", "workspaceId": workspace, "args": args}))
	}
	send()
	var list []record
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
	require.Len(t, list, 2)
	created := list[1]
	if created.ID == opener {
		created = list[0]
	}
	assert.Equal(t, repo, created.Worktree)
	f.waitStatus(t, workspace, created.ID, "done")

	send()
	f.waitStatus(t, workspace, created.ID, "done")
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
	assert.Len(t, list, 2)
	users := ofKind(f.transcript(t, workspace, created.ID).Entries, "user")
	require.Len(t, users, 2)
	assert.JSONEq(t, `{"id":"rv_0000abcd","worktree":"`+repo+`","base":"main","head":"Working tree","reviewed":2,"total":2,"comments":[{"id":4,"file":"a.go","side":"right","lineStart":1,"lineEnd":0,"type":"issue","text":"x"},{"id":7,"file":"b.go","side":"left","lineStart":2,"lineEnd":0,"type":"question","text":"y"}]}`, string(users[1].Review))
	assert.Contains(t, users[1].Text, "Review of Working tree against main: 2 comments")
}
