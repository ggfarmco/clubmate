package main_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

const limitContinue = "Your usage limit has reset"

func limitError() string {
	return "You've hit your session limit · resets " + time.Now().UTC().Add(2*time.Hour).Format("3:04pm") + " (UTC)"
}

func (f *fixture) limitJobs(t *testing.T, workspace string) []string {
	t.Helper()
	var out struct {
		Jobs []struct {
			ID      string `json:"id"`
			Command string `json:"command"`
		} `json:"jobs"`
	}
	require.NoError(t, f.run(t, workspace, "cron.list", map[string]any{}, &out))
	var ids []string
	for _, j := range out.Jobs {
		if j.Command == "agents.limit-resume" {
			ids = append(ids, j.ID)
		}
	}
	return ids
}

func TestUsageLimitWaitsForTheReset(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		then func(f *fixture, workspace, session string)
	}{
		{"cancel drops the wait and its job", func(f *fixture, workspace, session string) {
			require.NoError(t, f.run(t, workspace, "agents.limit-cancel", map[string]any{"session": session}, nil))
			rec := f.waitRecord(t, workspace, session, "no wait", func(r record) bool { return r.LimitWait == nil })
			assert.NotContains(t, rec.Failure.Badge, "resumes")
			assert.Equal(t, "done", rec.Group)
			assert.False(t, rec.Paused)
			testutil.Eventually(t, func() bool { return len(f.limitJobs(t, workspace)) == 0 }, wait)
		}},
		{"a prompt by hand drops the wait", func(f *fixture, workspace, session string) {
			f.prompt(t, workspace, session, "hello")
			assert.Nil(t, f.waitStatus(t, workspace, session, "done").LimitWait)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := startEnv(t, nil, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "build it", "fail": limitError()}, {"echo": true}}},
				testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"))
			workspace := f.workspace(t, newRepo(t))
			session := f.newSession(t, workspace)
			f.prompt(t, workspace, session, "build it")

			rec := f.waitRecord(t, workspace, session, "a scheduled wait", func(r record) bool { return r.LimitWait != nil && r.LimitWait.Job != "" })
			assert.Equal(t, "error", rec.Status)
			assert.Contains(t, rec.Failure.Badge, "resumes")
			at, err := time.Parse(time.RFC3339Nano, rec.LimitWait.At)
			require.NoError(t, err)
			assert.Equal(t, "paused", rec.Group)
			assert.True(t, rec.Paused)
			assert.False(t, rec.NeedsYou)
			assert.Equal(t, at.Truncate(time.Minute).Unix(), *rec.PausedUntil)
			assert.Equal(t, []string{"retry", "cancelWait"}, []string{rec.Failure.Actions[0].ID, rec.Failure.Actions[1].ID})
			assert.Equal(t, "agents.limit-cancel", rec.Failure.Actions[1].Command)
			assert.Equal(t, []string{rec.LimitWait.Job}, f.limitJobs(t, workspace))

			f.restartAgents(t, workspace)
			assert.Equal(t, rec.LimitWait.Job, f.session(t, workspace, session).LimitWait.Job)
			tt.then(f, workspace, session)
		})
	}
}

func TestFailedTurnContinues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		steps   []map[string]any
		retry   bool
		want    string
		prompts int
		status  string
	}{
		{"limit wait continues once the reset is due", []map[string]any{{"match": "build it", "fail": limitError()}, {"echo": true}}, false, limitContinue, 1, "done"},
		{"limit wait gives up after three attempts", []map[string]any{{"match": "build it", "fail": limitError()}, {"match": limitContinue, "fail": limitError()}}, false, limitContinue, 3, "error"},
		{"retry continues a turn that failed", []map[string]any{{"match": "build it", "fail": "boom", "errorKind": "server_error"}, {"echo": true}}, true, "Your turn ended with an error", 1, "done"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := startEnv(t, []string{"CLUBMATE_AGENTS_LIMIT_MARGIN=-3h"}, map[string]any{"prompt": tt.steps})
			workspace := f.workspace(t, newRepo(t))
			session := f.newSession(t, workspace)
			f.prompt(t, workspace, session, "build it")
			agent := f.agentID(t, workspace, session)
			if tt.retry {
				f.waitStatus(t, workspace, session, "error")
				require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": session}, nil))
			}

			testutil.Eventually(t, func() bool { return len(f.promptsTo(t, agent, tt.want)) == tt.prompts }, wait)
			rec := f.waitRecord(t, workspace, session, tt.status+" with no wait", func(r record) bool { return r.Status == tt.status && r.LimitWait == nil })
			testutil.Never(t, func() bool { return len(f.promptsTo(t, agent, tt.want)) > tt.prompts }, quiet)
			if tt.status == "error" {
				require.NotNil(t, rec.Failure)
				assert.NotContains(t, rec.Failure.Badge, "resumes")
				assert.Equal(t, "done", rec.Group)
			}
		})
	}
}

func TestContinuedChildWakesItsParent(t *testing.T) {
	t.Parallel()
	steps := []map[string]any{{"match": "build it", "fail": limitError()}, {"match": limitContinue, "permission": permissionScript()}}
	f := startEnv(t, []string{"CLUBMATE_AGENTS_LIMIT_MARGIN=-3h"}, map[string]any{"prompt": steps})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	parentID := f.agentID(t, workspace, parent)
	kid := f.newChild(t, workspace, parent, map[string]any{"title": "Kid", "prompt": "build it"}).Session

	f.waitStatus(t, workspace, kid, "waiting")
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, "[Clubmate] Child")) > 0 }, wait)
	assert.Contains(t, f.promptsTo(t, parentID, "[Clubmate] Child")[0], "waiting")
}
