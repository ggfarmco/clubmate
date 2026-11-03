package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEndQueueCoalescesWithinBudget(t *testing.T) {
	t.Parallel()
	q := newEndQueue()
	t0 := time.Now()
	end := func(session, kind, policy string) ending {
		return ending{workspace: "ws", session: session, title: "T" + session, kind: kind, policy: policy}
	}

	q.add(end("a", notifyDone, notifyAll))
	q.add(end("b", notifyAsks, notifyNeedsInput))
	q.add(end("c", notifyDone, notifyMuted))
	posts, wait := q.take(t0)
	assert.Equal(t, []notifyPost{{WorkspaceID: "ws", Title: "2 agent sessions", Message: "Asks you a question: Tb\nFinished: Ta", Kind: notifyAsks}}, posts)
	assert.Zero(t, wait)

	q.add(end("d", notifyDone, notifyMuted))
	posts, _ = q.take(t0.Add(time.Second))
	assert.Equal(t, []notifyPost{{WorkspaceID: "ws", Session: "d", Title: "Td", Message: "Finished", Kind: notifyDone}}, posts)

	q.add(end("e", notifyStuck, notifyAll))
	posts, wait = q.take(t0.Add(2 * time.Second))
	assert.Empty(t, posts)
	assert.Equal(t, 8*time.Second, wait)

	posts, _ = q.take(t0.Add(endPostWindow))
	assert.Equal(t, []notifyPost{{WorkspaceID: "ws", Session: "e", Title: "Te", Message: "Stopped with an error", Kind: notifyStuck}}, posts)
}
