package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeldChildDoesNotUseUpTheChildLimitButStillRuns(t *testing.T) {
	t.Parallel()
	a := &app{sessions: map[string]*session{}}
	for _, rec := range []record{
		{ID: "agt_held", Workspace: "ws", ParentSession: "agt_p", Status: statusRunning, Held: &heldTurn{Gate: "gt_1"}},
		{ID: "agt_busy", Workspace: "ws", ParentSession: "agt_p", Status: statusRunning},
	} {
		a.sessions[rec.ID] = newSession(rec, nil, true, nil)
	}

	assert.Equal(t, 1, a.limitChildren("agt_p"))
	assert.Equal(t, 2, a.runningChildren("agt_p"))
}
