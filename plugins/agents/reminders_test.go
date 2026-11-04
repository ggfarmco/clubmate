package main_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

func TestDoneSessionWithOneShotReminderShowsRunning(t *testing.T) {
	t.Parallel()
	f := startEnv(t, nil, map[string]any{"prompt": []map[string]any{{"echo": true}}},
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"))
	workspace := f.workspace(t, newRepo(t))
	polling := f.newSession(t, workspace)
	reminded := f.newSession(t, workspace)
	f.prompt(t, workspace, polling, "done")
	f.prompt(t, workspace, reminded, "done")
	f.waitStatus(t, workspace, polling, "done")
	f.waitStatus(t, workspace, reminded, "done")

	selfPrompt := func(session, key, value string) string {
		var job struct {
			ID string `json:"id"`
		}
		require.NoError(t, f.runAs(t, workspace, session, "cron.add", map[string]any{"name": "ci", "command": "agents.prompt", "args": `{"session":"` + session + `","text":"Check CI"}`, key: value}, &job))
		return job.ID
	}
	selfPrompt(polling, "every", "1h")
	job := selfPrompt(reminded, "in", "10h")
	f.waitStatus(t, workspace, reminded, "running")
	assert.Equal(t, "done", f.session(t, workspace, polling).Status)
	var list []struct {
		Activity struct {
			Title string `json:"title"`
		} `json:"activity"`
		TurnStartedAt *string `json:"turnStartedAt"`
	}
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"session": reminded}, &list))
	require.Len(t, list, 1)
	assert.True(t, strings.HasPrefix(list[0].Activity.Title, "next check "))
	assert.Nil(t, list[0].TurnStartedAt)

	require.NoError(t, f.runAs(t, workspace, reminded, "cron.delete", map[string]any{"id": job}, nil))
	f.waitStatus(t, workspace, reminded, "done")
}
