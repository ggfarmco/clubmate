package main_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func TestDoneTreeClosesItselfUnlessItAsksOrOwnsJobs(t *testing.T) {
	t.Parallel()
	const after = 300 * time.Millisecond
	f := startEnv(t, []string{"CLUBMATE_AGENTS_CLOSE_AFTER=" + after.String()}, map[string]any{"prompt": []map[string]any{{"echo": true}}},
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	workspace := f.workspace(t, newRepo(t))
	root := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, root, map[string]any{"prompt": "one"}).Session
	asking := f.newSession(t, workspace)
	scheduled := f.newSession(t, workspace)
	require.NoError(t, f.runAs(t, workspace, scheduled, "cron.add", map[string]any{"name": "ci", "command": "agents.prompt", "args": `{"session":"` + scheduled + `","text":"Check CI"}`, "in": "10h"}, nil))
	f.prompt(t, workspace, root, "done")
	f.prompt(t, workspace, asking, "Ship it?")
	f.prompt(t, workspace, scheduled, "done")

	closed := f.waitStatus(t, workspace, root, "closed")
	assert.True(t, closed.AutoClosed)
	assert.Equal(t, "done", closed.ClosedStatus)
	assert.True(t, f.waitStatus(t, workspace, kid, "closed").AutoClosed)
	assert.Equal(t, "clubmate", closeActor(t, f.logLines(t, root)))
	time.Sleep(3 * after)
	assert.Equal(t, "done", f.session(t, workspace, asking).Status)
	assert.Equal(t, "done", f.session(t, workspace, scheduled).Status)

	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": root}, nil))
	resumed := f.session(t, workspace, root)
	assert.False(t, resumed.AutoClosed)
	assert.Empty(t, resumed.ClosedStatus)
}

func closeActor(t *testing.T, lines []map[string]json.RawMessage) string {
	t.Helper()
	var actor string
	for _, l := range lines {
		if string(l["kind"]) == `"session/close"` {
			require.NoError(t, json.Unmarshal(l["actor"], &actor))
		}
	}
	return actor
}

func TestChangedAskSessionWaitsForReviewBeforeClosing(t *testing.T) {
	t.Parallel()
	const after = 2 * time.Second
	repo := newRepo(t)
	f := startEnv(t, []string{"CLUBMATE_AGENTS_CLOSE_AFTER=" + after.String()}, map[string]any{"prompt": []map[string]any{{"echo": true}}},
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	workspace := f.workspace(t, repo)
	review := f.newSession(t, workspace)
	merge := f.newSession(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.config", map[string]any{"session": merge, "id": "finish", "value": "merge"}, nil))
	reviewed := func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "button"), func(b sdui.Node) bool { return b.ID == review+"/reviewed" })
	}
	f.prompt(t, workspace, review, "one")
	f.prompt(t, workspace, merge, "one")
	f.waitStatus(t, workspace, review, "done")
	f.waitStatus(t, workspace, merge, "done")

	write(t, filepath.Join(repo, "new.txt"), "x\n")
	f.prompt(t, workspace, review, "two")
	f.prompt(t, workspace, merge, "two")

	assert.True(t, f.waitStatus(t, workspace, merge, "closed").AutoClosed)
	assert.True(t, f.session(t, workspace, review).Review)
	f.h.Event(t, workspace, "agents.sidebar", review, "select", review)
	f.h.WaitSlot(t, workspace, "agents.main", reviewed)
	time.Sleep(after)
	assert.Equal(t, "done", f.session(t, workspace, review).Status)

	require.NoError(t, f.run(t, workspace, "agents.reviewed", map[string]any{"session": review}, nil))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return !reviewed(n) })
	assert.True(t, f.waitStatus(t, workspace, review, "closed").AutoClosed)
}
