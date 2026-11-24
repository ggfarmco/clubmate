package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/plugins/agents/graphs"
)

func TestGateTreeSetupRunsWithoutGateMuAndDropsATreeTheLandingMovedPast(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	var removed []string
	a := &app{}
	a.gateTreeRun = func(_ context.Context, _ string, args map[string]any, out any) error {
		if args["remove"] == true {
			removed = append(removed, args["key"].(string))
			return nil
		}
		close(started)
		<-release
		return json.Unmarshal([]byte(`{"path":"/r/.worktrees/gate"}`), out)
	}
	a.keepGates([]gateConfig{{ID: "gt_1"}})
	lander := newSession(record{ID: "agt_l", Workspace: "ws"}, nil, true, nil)
	lander.putLandReview(landReview{Into: "main", Source: "feat", Gate: "gt_1", Run: "gr_1"})
	held := lander.landReview("main", "feat")
	done := make(chan error, 1)

	a.gateMu.Lock()
	go func() {
		_, _, err := a.unlockedGateTree(t.Context(), "ws", lander, gateConfig{ID: "gt_1", Repo: "/r"}, "lt_new", landGateArgs{Into: "main", Source: "feat"}, held)
		a.gateMu.Unlock()
		done <- err
	}()
	<-started
	require.True(t, a.gateMu.TryLock())
	assert.True(t, a.gatePreparing(lander.snapshot().ID, "main", "feat"))
	lander.putLandReview(landReview{Into: "main", Source: "feat", Gate: "gt_1", Run: "gr_2"})
	a.gateMu.Unlock()
	close(release)

	require.ErrorIs(t, <-done, errLandingMoved)
	assert.Equal(t, []string{"lt_new"}, removed)
	assert.False(t, a.gatePreparing("agt_l", "main", "feat"))
}

func TestFailedGateTreePromptsTheLanderItToldToWait(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	a := &app{}
	a.gateTreeRun = func(context.Context, string, map[string]any, any) error {
		close(started)
		<-release
		return errors.New("setup.sh failed")
	}
	a.keepGates([]gateConfig{{ID: "gt_1"}})
	lander := newSession(record{ID: "agt_l", Workspace: "ws"}, nil, true, func(string, bool) {})
	lander.running = true
	held := lander.landReview("main", "feat")
	done := make(chan error, 1)

	a.gateMu.Lock()
	go func() {
		_, _, err := a.unlockedGateTree(t.Context(), "ws", lander, gateConfig{ID: "gt_1", Repo: "/r"}, "lt_1", landGateArgs{Into: "main", Source: "feat"}, held)
		a.gateMu.Unlock()
		done <- err
	}()
	<-started
	a.gateMu.Lock()
	require.True(t, a.waitPreparing("agt_l", "main", "feat"))
	a.gateMu.Unlock()
	close(release)

	require.ErrorContains(t, <-done, "setup.sh failed")
	lander.mu.Lock()
	defer lander.mu.Unlock()
	require.Len(t, lander.queue, 1)
	assert.Contains(t, lander.queue[0].Text, "did not start: prepare the gate's worktree: setup.sh failed. Call git_land again.")
}

func TestClosedLanderWaitsForTheLandingBeingStartedAndForgetsItsRun(t *testing.T) {
	t.Parallel()
	a := landingApp(t)
	removed := make(chan string, 1)
	a.gateTreeRun = func(_ context.Context, _ string, args map[string]any, _ any) error {
		removed <- args["key"].(string)
		return nil
	}
	lander := newSession(record{ID: "agt_l", Workspace: "ws", Status: statusClosed}, nil, true, func(string, bool) {})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})

	a.gateMu.Lock()
	go func() {
		a.landerClosed(ctx, lander)
		close(done)
	}()
	testutil.Never(t, func() bool { return isClosed(done) }, time.Second)
	lander.putLandReview(landReview{Into: "main", Source: "feat", Gate: "gt_1", Run: "gr_1", TreeKey: "lt_1"})
	a.gateMu.Unlock()
	<-done

	assert.Equal(t, landReview{Into: "main", Source: "feat", Gate: "gt_1"}, lander.landReview("main", "feat"))
	require.Len(t, removed, 1)
	assert.Equal(t, "lt_1", <-removed)
}

func TestClosedLanderKeepsTheTreeKeyUntilTheRemoveSucceeds(t *testing.T) {
	t.Parallel()
	a := landingApp(t)
	var fail atomic.Bool
	fail.Store(true)
	var removed atomic.Int32
	a.gateTreeRun = func(_ context.Context, _ string, args map[string]any, _ any) error {
		if fail.Load() {
			return errors.New("git plugin is restarting")
		}
		removed.Add(1)
		return nil
	}
	lander := newSession(record{ID: "agt_l", Workspace: "ws", Status: statusClosed}, nil, true, func(string, bool) {})
	lander.putLandReview(landReview{Into: "main", Source: "feat", Gate: "gt_1", TreeKey: "lt_1"})
	a.sessions = map[string]*session{"agt_l": lander}

	a.landerClosed(t.Context(), lander)
	assert.Equal(t, landReview{Into: "main", Source: "feat", Gate: "gt_1", TreeKey: "lt_1"}, lander.landReview("main", "feat"))

	fail.Store(false)
	a.dropClosedGateTrees()
	assert.Equal(t, landReview{Into: "main", Source: "feat", Gate: "gt_1"}, lander.landReview("main", "feat"))
	assert.EqualValues(t, 1, removed.Load())
}

func TestClosedLanderDropsTheTreeKeyWhenTheRepoIsGone(t *testing.T) {
	t.Parallel()
	a := landingApp(t)
	a.gateTreeRun = func(context.Context, string, map[string]any, any) error {
		return errors.New("not a git repository")
	}
	lander := newSession(record{ID: "agt_l", Workspace: "ws", Status: statusClosed}, nil, true, func(string, bool) {})
	lander.putLandReview(landReview{Into: "main", Source: "feat", Repo: filepath.Join(t.TempDir(), "gone"), Gate: "gt_1", TreeKey: "lt_1"})
	a.sessions = map[string]*session{"agt_l": lander}

	a.dropClosedGateTrees()

	assert.Empty(t, lander.landReview("main", "feat").TreeKey)
}

func TestClosingAGateNodeDoesNotWaitForTheLandingBeingStarted(t *testing.T) {
	t.Parallel()
	a := landingApp(t)
	node := newSession(record{ID: "agt_n", Workspace: "ws", Status: statusClosed, GateFor: &gateFor{Lander: "agt_l"}}, nil, true, func(string, bool) {})
	done := make(chan struct{})

	a.gateMu.Lock()
	defer a.gateMu.Unlock()
	go func() {
		a.landerClosed(t.Context(), node)
		close(done)
	}()

	testutil.Eventually(t, func() bool { return isClosed(done) }, 10*time.Second)
}

func TestLanderClosedWhileItsRunStartsKeepsNoRun(t *testing.T) {
	t.Parallel()
	a := landingApp(t)
	lander := newSession(record{ID: "agt_l", Workspace: "ws"}, nil, true, func(string, bool) {})
	a.gateStart = func(context.Context, graphs.GateRun) (string, string, error) {
		lander.mu.Lock()
		lander.rec.Status = statusClosed
		lander.mu.Unlock()
		return "gr_1", "agt_v", nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := a.reviewLanding(ctx, "ws", lander, gateConfig{ID: "gt_1", Graph: "solo", Rounds: 2}, landGateArgs{Into: "main", Source: "feat", Old: "a", Tree: "b"}, gateGraphCheck{ok: true})

	require.ErrorIs(t, err, errClosed)
	assert.Empty(t, lander.landReview("main", "feat").Run)
	_, err = a.reviewLanding(ctx, "ws", lander, gateConfig{ID: "gt_1", Graph: "solo", Rounds: 2}, landGateArgs{Into: "main", Source: "feat", Old: "a", Tree: "c"}, gateGraphCheck{ok: true})
	require.ErrorIs(t, err, errClosed)
	assert.Empty(t, lander.landReview("main", "feat").Tree)
}

func TestFailedRunStartPromptsTheLanderItToldToWait(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".clubmate", "graphs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".clubmate", "graphs", "checked.json"), []byte(`{"nodes":[{"id":"lint","prompt":"Lint. {{input}}","start":true},{"id":"check","run":"true"},{"id":"review","prompt":"Review. {{input}}","outcomes":["approved","changes"]}],"edges":[{"from":"lint","to":"check"},{"from":"check","to":"review"}]}`), 0o600))
	started, release := make(chan struct{}), make(chan struct{})
	a := landingApp(t)
	a.keepGates([]gateConfig{{ID: "gt_1"}})
	a.gateTreeRun = func(_ context.Context, _ string, args map[string]any, out any) error {
		if args["remove"] == true {
			return nil
		}
		close(started)
		<-release
		return json.Unmarshal([]byte(`{"path":"/r/.worktrees/gate"}`), out)
	}
	a.gateStart = func(context.Context, graphs.GateRun) (string, string, error) {
		return "", "", errors.New("the review node did not start")
	}
	lander := newSession(record{ID: "agt_l", Workspace: "ws"}, nil, true, func(string, bool) {})
	lander.running = true
	done := make(chan error, 1)

	go func() {
		_, err := a.reviewLanding(t.Context(), "ws", lander, gateConfig{ID: "gt_1", Repo: repo, Graph: "checked", Rounds: 2}, landGateArgs{Into: "main", Source: "feat", Old: "a", Tree: "b"}, gateGraphCheck{ok: true})
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		require.FailNow(t, "the review ended before its worktree was prepared", "%v", err)
	}
	a.gateMu.Lock()
	require.True(t, a.waitPreparing("agt_l", "main", "feat"))
	a.gateMu.Unlock()
	close(release)

	require.ErrorContains(t, <-done, "the review node did not start")
	lander.mu.Lock()
	defer lander.mu.Unlock()
	require.Len(t, lander.queue, 1)
	assert.Contains(t, lander.queue[0].Text, "did not start: the review node did not start. Call git_land again.")
}

func landingApp(t *testing.T) *app {
	t.Helper()
	h := scriptedHost(t, func(string, string) any { return nil })
	return &app{host: h, registry: newRegistry(), graphs: graphs.New(graphs.Deps{}), notes: newNotifier(h, t.TempDir())}
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
