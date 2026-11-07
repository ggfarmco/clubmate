package main

import (
	"context"
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
