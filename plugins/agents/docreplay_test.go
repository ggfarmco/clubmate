package main

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
)

func replayApp(t *testing.T, rec record, run func(context.Context) error) (*app, *session, *atomic.Int32) {
	t.Helper()
	a := &app{registry: newRegistry()}
	var calls atomic.Int32
	a.docsRun = func(ctx context.Context, _, _ string, _ map[string]any) error {
		calls.Add(1)
		return run(ctx)
	}
	var s *session
	s = newSession(rec, newEventLog(filepath.Join(t.TempDir(), rec.ID+".jsonl")), true, func(kind string, _ bool) {
		if kind == changeStatus {
			a.settleGatedDocs(s)
		}
	})
	a.sessions[rec.ID] = s
	return a, s, &calls
}

func queuedDocs(s *session) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rec.GatedDocs)
}

func replaying(s *session) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replayingDocs
}

func rpcError(code int64, reason string) error {
	e := &jsonrpc2.Error{Code: code, Message: "x"}
	if reason != "" {
		e.SetError(contract.ErrorReason{Reason: reason})
	}
	return e
}

func TestReplayErrorKeepsTheDocOnlyWhenDocsMayBeBack(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err  error
		kept bool
	}{
		"timeout":            {context.DeadlineExceeded, false},
		"outcome unknown":    {rpcError(contract.CodePluginUnavailable, contract.ReasonUnknown), false},
		"workspace gone":     {rpcError(codeNotFound, contract.ReasonStale), false},
		"command not loaded": {rpcError(codeNotFound, ""), true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := record{ID: "s1", Workspace: "ws", Status: statusDone, GatedDocs: []gatedDoc{{Workspace: "ws", For: "s1", Path: "/r/a.md", Mode: docGateOpen}}}
			a, s, calls := replayApp(t, rec, func(context.Context) error { return tc.err })

			a.settleGatedDocs(s)

			want := 0
			if tc.kept {
				want = 1
			}
			testutil.Eventually(t, func() bool { return calls.Load() == 1 && !replaying(s) && queuedDocs(s) == want }, 10*time.Second)
		})
	}
}

func TestDocQueuedAgainDuringItsReplayIsKept(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	rec := record{ID: "s1", Workspace: "ws", Status: statusRunning}
	a, s, calls := replayApp(t, rec, func(context.Context) error { <-release; return nil })
	doc := gatedDoc{Workspace: "ws", For: "s1", Path: "/r/a.md", Mode: docGateOpen}
	require.True(t, s.queueDoc(doc))
	s.mu.Lock()
	s.rec.Status = statusDone
	s.mu.Unlock()
	a.settleGatedDocs(s)
	testutil.Eventually(t, func() bool { return calls.Load() == 1 }, 10*time.Second)
	s.mu.Lock()
	s.rec.Status = statusRunning
	s.mu.Unlock()
	require.True(t, s.queueDoc(doc))

	close(release)

	testutil.Eventually(t, func() bool { return !replaying(s) && queuedDocs(s) == 1 }, 10*time.Second)
}

func TestLimitResumeThatCannotStartShowsQueuedDocs(t *testing.T) {
	t.Parallel()
	rec := record{ID: "s1", Workspace: "ws", Status: statusError, LimitWait: &limitWait{At: time.Now().Add(-time.Minute)}, GatedDocs: []gatedDoc{{Workspace: "ws", For: "s1", Path: "/r/a.md", Mode: docGateOpen}}}
	a, s, calls := replayApp(t, rec, func(context.Context) error { return nil })
	started := false
	inner := s.changed
	s.changed = func(kind string, save bool) {
		s.mu.Lock()
		if !started && s.rec.LimitWait != nil && s.rec.LimitWait.Fired {
			started, s.running = true, true
		}
		s.mu.Unlock()
		inner(kind, save)
	}

	assert.Zero(t, a.continueLimited("ws", time.Now()))

	testutil.Eventually(t, func() bool { return calls.Load() == 1 && queuedDocs(s) == 0 }, 10*time.Second)
}
