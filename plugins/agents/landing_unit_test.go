package main

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeadContextLeavesALandTurnOpen(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tip := range []string{"", "tip"} {
		r := taskRepo{Path: t.TempDir(), State: landRunning, Tip: tip, Base: "base"}

		r.settleLandTurn(ctx, nil)

		assert.Equal(t, landRunning, r.State)
	}
}

func TestNoteLandingKeepsTheBranchWhenGitFails(t *testing.T) {
	t.Parallel()
	r := taskRepo{Path: t.TempDir(), Into: "trunk"}

	r.noteLanding(t.Context(), r.Path)

	assert.Equal(t, "trunk", r.Into)
}

func TestCoalescedRunsOnceMoreForCallsDuringARun(t *testing.T) {
	t.Parallel()
	var c coalesced
	var runs, running, overlap atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		c.run(func() {
			if running.Add(1) > 1 {
				overlap.Add(1)
			}
			if runs.Add(1) == 1 {
				close(started)
				<-release
			}
			running.Add(-1)
		})
		close(done)
	}()
	<-started

	for range 5 {
		c.run(func() { runs.Add(1) })
	}
	close(release)
	<-done

	assert.Equal(t, int32(2), runs.Load())
	assert.Zero(t, overlap.Load())
}
