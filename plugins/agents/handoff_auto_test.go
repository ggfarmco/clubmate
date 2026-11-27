package main_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

func say(match, text string) map[string]any {
	return map[string]any{"match": match, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}}}
}

func autoHandedOff(t *testing.T, f *fixture, workspace, old string) (record, string) {
	t.Helper()
	closed := f.waitRecord(t, workspace, old, "a handoff", func(r record) bool { return r.HandoffTo != "" })
	next := f.waitStatus(t, workspace, closed.HandoffTo, "done")
	users := ofKind(f.transcript(t, workspace, next.ID).Entries, "user")
	require.NotEmpty(t, users)
	return next, users[0].Text
}

func noticeTitles(t *testing.T, f *fixture, workspace, session string) []string {
	t.Helper()
	var out []string
	for _, n := range ofKind(f.transcript(t, workspace, session).Entries, "notice") {
		var got struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		}
		require.NoError(t, json.Unmarshal(n.Notice, &got))
		out = append(out, got.Title+": "+got.Description)
	}
	return out
}

func TestAutoHandoffOverContextAsksForNotesOnce(t *testing.T) {
	t.Parallel()
	gate := filepath.Join(t.TempDir(), "notes")
	task := strings.Repeat("a", 2000) + " report usage"
	notesHead := "[Clubmate handoff notes]"
	f := start(t, map[string]any{"prompt": []map[string]any{
		{"match": "report usage", "update": map[string]any{"sessionUpdate": "usage_update", "used": 412000, "size": 1000000}},
		say("report usage", "built it"),
		{"match": notesHead, "wait": gate},
		say(notesHead, "plan: docs/plan.md, backlog #12"),
		say("extra", "did extra"),
	}})
	workspace := f.workspace(t, newRepo(t))
	old := f.newSession(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.config", map[string]any{"session": old, "id": "handoff", "value": "context"}, nil))
	notesPrompts := func() int {
		return len(slices.DeleteFunc(f.calls(t, "session/prompt"), func(c fakeCall) bool { return !strings.Contains(string(c.Params), notesHead) }))
	}

	f.prompt(t, workspace, old, task)
	testutil.Eventually(t, func() bool { return notesPrompts() == 1 }, wait)
	f.prompt(t, workspace, old, "extra")
	write(t, gate, "")
	next, brief := autoHandedOff(t, f, workspace, old)

	assert.Equal(t, 1, notesPrompts())
	assert.Equal(t, "context", next.Handoff)
	for _, want := range []string{"Task (turn 1):\naaa", "Last prompt (turn 3):\nextra", "Last reply (turn 3):\ndid extra", "context 412k", "plan: docs/plan.md, backlog #12"} {
		assert.Contains(t, brief, want)
	}
	assert.NotContains(t, brief, notesHead)
	testutil.Eventually(t, func() bool {
		return slices.Equal([]string{"Handed off to a new session: context 412k"}, noticeTitles(t, f, workspace, old)) &&
			slices.Equal([]string{"Continues a session handed off by itself: context 412k"}, noticeTitles(t, f, workspace, next.ID))
	}, wait)
}

func TestAutoHandoffAfterLandingIntoTheDefaultBranch(t *testing.T) {
	t.Parallel()
	f := startEnv(t, nil, map[string]any{"prompt": []map[string]any{say("[Clubmate handoff notes]", "nothing open")}}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	lander := f.newLander(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.config", map[string]any{"session": lander, "id": "handoff", "value": "landed"}, nil))
	feat := filepath.Join(repo, ".worktrees", "feat")
	side := filepath.Join(repo, ".worktrees", "side")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	git(t, repo, "worktree", "add", "-q", "-b", "side", side)
	write(t, filepath.Join(side, "s.go"), "side\n")
	git(t, side, "add", ".")
	git(t, side, "commit", "-qm", "side")
	write(t, filepath.Join(feat, "b.go"), "two\n")
	git(t, feat, "add", ".")
	git(t, feat, "commit", "-qm", "feat")

	require.NoError(t, f.runAs(t, workspace, lander, "git.land", map[string]any{"path": side, "into": "feat", "message": "feat: side"}, nil))
	f.prompt(t, workspace, lander, "next")
	f.waitStatus(t, workspace, lander, "done")
	testutil.Never(t, func() bool { return f.session(t, workspace, lander).HandoffTo != "" }, wait/10)

	require.NoError(t, f.runAs(t, workspace, lander, "git.land", map[string]any{"path": feat, "message": "feat: b"}, nil))
	f.prompt(t, workspace, lander, "next")
	_, brief := autoHandedOff(t, f, workspace, lander)

	assert.Contains(t, brief, "landed into main")
	assert.Contains(t, brief, "nothing open")
}
