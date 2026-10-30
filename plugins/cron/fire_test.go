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
)

type memStore struct {
	mu   sync.Mutex
	jobs map[string]bool
}

func (m *memStore) save(_ context.Context, j *Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[j.ID] = true
	return nil
}

func (m *memStore) remove(_ context.Context, j *Job) error {
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

func testApp(now *time.Time, run func(context.Context, commandRequest) (json.RawMessage, error), jobs ...*Job) (*app, *memStore) {
	a := newApp()
	st := &memStore{jobs: map[string]bool{}}
	a.store = st
	a.now = func() time.Time { return *now }
	a.runCommand = run
	a.render = func(context.Context, string) error { return nil }
	for _, j := range jobs {
		a.jobs[j.ID] = j
		st.jobs[j.ID] = true
	}
	return a, st
}

func blockingRun(calls *atomic.Int32, started, release chan struct{}) func(context.Context, commandRequest) (json.RawMessage, error) {
	return func(context.Context, commandRequest) (json.RawMessage, error) {
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
		{"owner closed", owned, []agentSession{{ID: "agt_1", Closed: true}}, "", called, Run{At: now, Status: "skipped", Reason: "owner gone"}, ""},
		{"owner unknown", owned, nil, "", called, Run{At: now, Status: "skipped", Reason: "owner unknown"}, ""},
		{"owner alive", owned, []agentSession{{ID: "agt_1", Status: "done"}}, "{}", nil, Run{At: now, Status: "ok", Result: "{}"}, "agt_1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got commandRequest
			a, _ := testApp(&now, func(_ context.Context, req commandRequest) (json.RawMessage, error) {
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
	a.projects = func(context.Context) ([]pluginsdk.Project, error) {
		return []pluginsdk.Project{{ID: "prj_live"}}, nil
	}

	a.prune(t.Context())

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
		return []agentSession{{ID: "agt_closed", Closed: true}, {ID: "agt_live"}}, nil
	}

	a.reconcileAll(t.Context())

	assert.ElementsMatch(t, []string{"cj_live", "cj_free"}, slices.Collect(maps.Keys(a.jobs)))
	assert.False(t, st.has("cj_closed"))
	assert.False(t, st.has("cj_absent"))
}
