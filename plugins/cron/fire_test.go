package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/automation"
)

type memStore struct {
	mu   sync.Mutex
	jobs map[string]bool
}

func (m *memStore) Save(_ context.Context, j *Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[j.ID] = true
	return nil
}

func (m *memStore) Remove(_ context.Context, j *Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, j.ID)
	return nil
}

func (m *memStore) has(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

func testApp(now *time.Time, run func(context.Context, automation.CommandRequest) (json.RawMessage, error), jobs ...*Job) (*app, *memStore) {
	a := newApp()
	st := &memStore{jobs: map[string]bool{}}
	a.store = st
	a.now = func() time.Time { return *now }
	a.runCommand = run
	a.render = func(context.Context, string) error { return nil }
	a.registry.Notify = func(context.Context, automation.NotifyRequest) error { return nil }
	for _, j := range jobs {
		a.jobs[j.ID] = j
		st.jobs[j.ID] = true
	}
	return a, st
}

func blockingRun(calls *atomic.Int32, started, release chan struct{}) func(context.Context, automation.CommandRequest) (json.RawMessage, error) {
	return func(context.Context, automation.CommandRequest) (json.RawMessage, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return json.RawMessage(`{}`), nil
	}
}

func TestFireClassifiesErrors(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	long := `"` + strings.Repeat("é", 300) + `"`
	ping := Job{Command: "notify.post"}
	owned := Job{Command: "notify.post", Owner: "agt_1"}
	remind := Job{Command: "agents.prompt", Args: json.RawMessage(`{"session":"agt_1","text":"check"}`)}
	called := errors.New("called")
	tests := []struct {
		name     string
		job      Job
		sessions []agentSession
		raw      string
		err      error
		want     Run
		caller   string
	}{
		{"no app", ping, nil, "", errors.New("plugin core command.run: no app to approve"), Run{At: now, Status: "skipped", Reason: "no app"}, ""},
		{"error", ping, nil, "", errors.New("boom"), Run{At: now, Status: "error", Reason: "boom"}, ""},
		{"long result", ping, nil, long, nil, Run{At: now, Status: "ok", Result: `"` + strings.Repeat("é", 199)}, ""},
		{"target waiting", remind, []agentSession{{ID: "agt_1", Status: "waiting"}}, "", called, Run{At: now, Status: "skipped", Reason: "busy"}, ""},
		{"owner closed", owned, []agentSession{{ID: "agt_1", Status: "closed"}}, "", called, Run{At: now, Status: "skipped", Reason: "owner gone"}, ""},
		{"owner unknown", owned, nil, "", called, Run{At: now, Status: "skipped", Reason: "owner unknown"}, ""},
		{"owner alive", owned, []agentSession{{ID: "agt_1", Status: "done"}}, "{}", nil, Run{At: now, Status: "ok", Result: "{}"}, "agt_1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got automation.CommandRequest
			a, _ := testApp(&now, func(_ context.Context, req automation.CommandRequest) (json.RawMessage, error) {
				got = req
				return json.RawMessage(tt.raw), tt.err
			})
			a.sessions = func(context.Context, string) ([]agentSession, error) {
				if tt.sessions == nil {
					return nil, errors.New("agents down")
				}
				return tt.sessions, nil
			}
			assert.Equal(t, tt.want, a.fire(t.Context(), &tt.job))
			assert.Equal(t, tt.caller, got.Caller)
		})
	}
}

func TestInFlightJobIsSkipped(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	first := now
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	a, _ := testApp(&now, blockingRun(&calls, started, release),
		&Job{ID: "cj_1", Command: "notify.post", Enabled: true, Schedule: Schedule{Every: "1m"}, Missed: "skip", NextRun: now})

	a.tick(t.Context())
	<-started
	now = now.Add(time.Minute)
	a.tick(t.Context())
	close(release)
	a.runs.Wait()

	j := a.jobs["cj_1"]
	require.Len(t, j.History, 2)
	assert.Equal(t, Run{At: now, Status: "skipped", Reason: "previous run still running"}, j.History[0])
	assert.Equal(t, Run{At: first, Status: "ok", Result: "{}"}, j.History[1])
	assert.Equal(t, int32(1), calls.Load())
	assert.True(t, j.NextRun.Equal(now.Add(time.Minute)), j.NextRun)
}

func TestStartedOneShotLeavesStore(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	a, st := testApp(&now, blockingRun(&calls, started, release),
		&Job{ID: "cj_1", Command: "notify.post", Enabled: true, Schedule: Schedule{At: now}, Missed: "once", NextRun: now})

	a.tick(t.Context())
	<-started
	assert.False(t, st.has("cj_1"))
	close(release)
	a.runs.Wait()

	assert.NotContains(t, a.jobs, "cj_1")
	assert.Equal(t, int32(1), calls.Load())
}

func TestPruneDropsJobsOfDeletedWorkspaces(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	a, st := testApp(&now, nil,
		&Job{ID: "cj_live", Workspace: "prj_live"},
		&Job{ID: "cj_gone", Workspace: "prj_gone"},
		&Job{ID: "cj_app"})
	a.workspaces = func(context.Context) ([]pluginsdk.Workspace, error) {
		return []pluginsdk.Workspace{{ID: "prj_live"}}, nil
	}

	a.registry.Prune(t.Context())

	assert.ElementsMatch(t, []string{"cj_live", "cj_app"}, slices.Collect(maps.Keys(a.jobs)))
	assert.False(t, st.has("cj_gone"))
}

func TestReconcileDropsJobsOfGoneOwners(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	a, st := testApp(&now, nil,
		&Job{ID: "cj_closed", Workspace: "prj_1", Owner: "agt_closed"},
		&Job{ID: "cj_absent", Workspace: "prj_1", Owner: "agt_absent"},
		&Job{ID: "cj_live", Workspace: "prj_1", Owner: "agt_live"},
		&Job{ID: "cj_free", Workspace: "prj_1"})
	a.sessions = func(context.Context, string) ([]agentSession, error) {
		return []agentSession{{ID: "agt_closed", Status: "closed"}, {ID: "agt_live"}}, nil
	}

	a.registry.ReconcileAll(t.Context())

	assert.ElementsMatch(t, []string{"cj_live", "cj_free"}, slices.Collect(maps.Keys(a.jobs)))
	assert.False(t, st.has("cj_closed"))
	assert.False(t, st.has("cj_absent"))
}

func TestSkippedOneShotRetries(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	status := "running"
	var calls atomic.Int32
	a, st := testApp(&now, func(context.Context, automation.CommandRequest) (json.RawMessage, error) {
		calls.Add(1)
		return json.RawMessage(`{}`), nil
	}, &Job{ID: "cj_1", Command: "agents.prompt", Args: json.RawMessage(`{"session":"agt_1"}`), Enabled: true, Schedule: Schedule{At: now}, Missed: "skip", NextRun: now})
	a.sessions = func(context.Context, string) ([]agentSession, error) {
		return []agentSession{{ID: "agt_1", Status: status}}, nil
	}

	a.tick(t.Context())
	a.runs.Wait()

	require.Contains(t, a.jobs, "cj_1")
	assert.True(t, st.has("cj_1"))
	assert.Equal(t, []Run{{At: now, Status: "skipped", Reason: "busy"}}, a.jobs["cj_1"].History)
	assert.True(t, a.jobs["cj_1"].NextRun.Equal(now.Add(30*time.Second)), a.jobs["cj_1"].NextRun)

	status = "done"
	now = now.Add(30 * time.Second)
	a.tick(t.Context())
	a.runs.Wait()

	assert.NotContains(t, a.jobs, "cj_1")
	assert.False(t, st.has("cj_1"))
	assert.Equal(t, int32(1), calls.Load())
}

func TestAgentsCallDoesNotBlockScheduler(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	entered, release := make(chan struct{}), make(chan struct{})
	a, _ := testApp(&now, nil, &Job{ID: "cj_1", Workspace: "prj_1", Command: "notify.post", Enabled: true, Schedule: Schedule{Every: "1m"}, Owner: "agt_child", NextRun: now.Add(time.Minute)})
	a.sessions = func(context.Context, string) ([]agentSession, error) {
		close(entered)
		<-release
		return []agentSession{{ID: "agt_child", ParentSession: "agt_parent"}}, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := a.pause(t.Context(), pluginsdk.CommandRun{ID: "cron.pause", WorkspaceID: "prj_1", Args: json.RawMessage(`{"id":"cj_1"}`), Agent: true, Caller: "agt_parent"})
		done <- err
	}()
	<-entered

	ticked := make(chan struct{})
	go func() {
		a.tick(t.Context())
		close(ticked)
	}()
	select {
	case <-ticked:
	case <-time.After(5 * time.Second):
		t.Fatal("tick blocked behind agents.sessions")
	}
	close(release)

	require.NoError(t, <-done)
	assert.False(t, a.jobs["cj_1"].Enabled)
}

func TestOwnerlessJobPostsOnlyFailuresAndMissedRuns(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	results := []error{nil, errors.New("jsonrpc2: code -32000 message: GitHub answered 403"), errors.New("boom"), errors.New("boom")}
	a, _ := testApp(&now, func(context.Context, automation.CommandRequest) (json.RawMessage, error) {
		err := results[0]
		results = results[1:]
		return json.RawMessage(`null`), err
	}, &Job{ID: "cj_1", Name: "Poll CI", Workspace: "ws", Command: "notify.post", Enabled: true, Schedule: Schedule{Every: "1h"}, Missed: missedSkip, NextRun: now.Add(-5 * time.Minute)})
	var posts []string
	a.registry.Notify = func(_ context.Context, req automation.NotifyRequest) error {
		posts = append(posts, req.Kind+" "+req.Message)
		return nil
	}

	a.tick(t.Context())
	a.runs.Wait()
	for range 4 {
		_, err := a.runNow(t.Context(), pluginsdk.CommandRun{ID: "cron.run", WorkspaceID: "ws", Args: json.RawMessage(`{"id":"cj_1"}`)})
		require.NoError(t, err)
	}

	assert.Equal(t, []string{
		"info Cron job missed a run: Clubmate was closed or the Mac asleep; the policy skips it",
		"stuck Cron job failed: GitHub answered 403",
		"stuck Cron job failed: boom",
		"stuck Poll CI: boom",
	}, posts)
	j := a.jobs["cj_1"]
	assert.False(t, j.Enabled)
	assert.Equal(t, []automation.Span{{From: now, Reason: "boom"}}, j.Paused)
}
