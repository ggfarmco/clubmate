package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClosedSessionForgetsPromptInFlight(t *testing.T) {
	t.Parallel()
	s := newSession(record{ID: "s"}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	s.mu.Lock()
	s.enqueueLocked(promptArgs{Text: "late"})
	s.dequeueLocked()
	s.beginTurnLocked()
	s.running = false
	s.mu.Unlock()

	(&app{}).closeSession(t.Context(), s, false)

	s.mu.Lock()
	s.setStatusLocked(statusIdle, "")
	s.cutTurnLocked()
	assert.Empty(t, s.storedQueueLocked())
	s.mu.Unlock()
}
