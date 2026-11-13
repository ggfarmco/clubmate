package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTurnCutByAgentExitKeepsItsPrompt(t *testing.T) {
	t.Parallel()
	s := newSession(record{ID: "s"}, nil, true, func(string, bool) {})
	s.mu.Lock()
	_, epoch := s.beginTurnLocked()
	s.cutTurnLocked()
	s.mu.Unlock()

	assert.True(t, (&app{}).abandon(s, epoch, promptArgs{Text: "late"}))
	queue := s.queued()
	require.Len(t, queue, 1)
	assert.Equal(t, "late", queue[0].Text)
}
