package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		_, err := a.unlockedGateTree(t.Context(), "ws", lander, gateConfig{ID: "gt_1", Repo: "/r"}, "lt_new", landGateArgs{Into: "main", Source: "feat"}, held)
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
