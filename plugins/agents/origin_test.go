package main_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

func TestSessionAnAgentStartsElsewhereNeverGetsUserAFK(t *testing.T) {
	t.Parallel()
	f := start(t, nil)
	home, other := f.workspace(t, newRepo(t)), f.workspace(t, newRepo(t))
	require.NoError(t, f.run(t, home, "agents.afk-default", map[string]any{"on": true}, nil))
	caller := f.newSession(t, home)

	plain := f.session(t, other, f.newChild(t, other, caller, map[string]any{}).Session)
	assert.Empty(t, plain.ParentSession)
	assert.False(t, plain.AFK)
	assert.Equal(t, caller, plain.StartedBy)
	afk := f.session(t, other, f.newChild(t, other, caller, map[string]any{"afk": true}).Session)
	assert.Equal(t, caller, afk.AFKBy)
	nameless := f.session(t, home, f.newChild(t, home, "", map[string]any{}).Session)
	assert.False(t, nameless.AFK)
	assert.Equal(t, "unknown", nameless.StartedBy)

	for range 6 {
		f.newChild(t, other, caller, map[string]any{})
	}
	assert.ErrorContains(t, f.runAs(t, other, caller, "agents.new", map[string]any{}, nil), "at most 8 sessions open outside its own tree")
	f.close(t, other, plain.ID)
	f.newChild(t, other, caller, map[string]any{})
}

func TestCronSessionGetsUserAFKOnlyFromTheUsersJob(t *testing.T) {
	t.Parallel()
	f := start(t, nil, testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"))
	workspace := f.workspace(t, newRepo(t))
	require.NoError(t, f.run(t, workspace, "agents.afk-default", map[string]any{"on": true}, nil))
	agent := f.newSession(t, workspace)
	fire := func(caller string) record {
		var job struct {
			ID string `json:"id"`
		}
		args := map[string]any{"name": "start", "command": "agents.new", "in": "10h", "owner": "none"}
		if caller == "" {
			require.NoError(t, f.run(t, workspace, "cron.add", args, &job))
		} else {
			require.NoError(t, f.runAs(t, workspace, caller, "cron.add", args, &job))
		}
		require.NoError(t, f.run(t, workspace, "cron.run", map[string]any{"id": job.ID}, nil))
		var got record
		testutil.Eventually(t, func() bool {
			var list []record
			require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
			for _, r := range list {
				if r.CronJob == job.ID {
					got = r
					return true
				}
			}
			return false
		}, wait)
		return got
	}

	mine, theirs := fire(""), fire(agent)
	assert.True(t, mine.AFK)
	assert.Empty(t, mine.StartedBy)
	assert.False(t, theirs.AFK)
	assert.Equal(t, agent, theirs.StartedBy)
}
