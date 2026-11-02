package main_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

func TestDoneTreeClosesItselfUnlessItAsksOrOwnsJobs(t *testing.T) {
	const after = 300 * time.Millisecond
	t.Setenv("CLUBMATE_AGENTS_CLOSE_AFTER", after.String())
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}},
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

	assert.True(t, f.waitStatus(t, workspace, root, "closed").AutoClosed)
	assert.True(t, f.waitStatus(t, workspace, kid, "closed").AutoClosed)
	assert.Equal(t, "clubmate", closeActor(t, f.logLines(t, root)))
	time.Sleep(3 * after)
	assert.Equal(t, "done", f.session(t, workspace, asking).Status)
	assert.Equal(t, "done", f.session(t, workspace, scheduled).Status)

	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": root}, nil))
	assert.False(t, f.session(t, workspace, root).AutoClosed)
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
