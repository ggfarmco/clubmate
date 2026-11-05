package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDonePostCountsOpenItems(t *testing.T) {
	t.Parallel()
	one := ending{workspace: "ws", session: "a", title: "A", kind: notifyDone, policy: notifyAll, open: 3}
	two := ending{workspace: "ws", session: "b", title: "B", kind: notifyDone, policy: notifyAll}

	single, _ := endPost([]ending{one})
	both, _ := endPost([]ending{one, two})

	assert.Equal(t, "Done · 3 open", single.Message)
	assert.Equal(t, notifyDone, single.Kind)
	assert.Equal(t, "Finished: A · 3 open, B", both.Message)
}

func TestEndQueueCoalescesWithinBudget(t *testing.T) {
	t.Parallel()
	q := newEndQueue(0)
	t0 := time.Now()
	end := func(session, kind, policy string) ending {
		return ending{workspace: "ws", session: session, title: "T" + session, kind: kind, policy: policy}
	}

	q.add(end("a", notifyDone, notifyAll), t0)
	q.add(end("b", notifyAsks, notifyNeedsInput), t0)
	q.add(end("c", notifyDone, notifyMuted), t0)
	posts, wait := q.take(t0)
	assert.Equal(t, []notifyPost{{WorkspaceID: "ws", Title: "2 agent sessions", Message: "Asks you a question: Tb\nFinished: Ta", Kind: notifyAsks}}, posts)
	assert.Zero(t, wait)

	q.add(end("d", notifyDone, notifyMuted), t0)
	posts, _ = q.take(t0.Add(time.Second))
	assert.Equal(t, []notifyPost{{WorkspaceID: "ws", Session: "d", Collapse: "d", Title: "Td", Message: "Finished", Kind: notifyDone}}, posts)

	q.add(end("e", notifyStuck, notifyAll), t0)
	posts, wait = q.take(t0.Add(2 * time.Second))
	assert.Empty(t, posts)
	assert.Equal(t, 8*time.Second, wait)

	posts, _ = q.take(t0.Add(endPostWindow))
	assert.Equal(t, []notifyPost{{WorkspaceID: "ws", Session: "e", Collapse: "e", Title: "Te", Message: "Stopped with an error", Kind: notifyStuck}}, posts)
}

func TestEndQueueSendsOnlyASessionsLastEnd(t *testing.T) {
	t.Parallel()
	q := newEndQueue(5 * time.Second)
	t0 := time.Now()
	end := func(message int) ending {
		return ending{workspace: "ws", session: "a", title: "Ta", kind: notifyDone, policy: notifyAll, open: message}
	}

	q.add(end(1), t0)
	posts, wait := q.take(t0)
	assert.Empty(t, posts)
	assert.Equal(t, 5*time.Second, wait)

	q.drop("a")
	posts, wait = q.take(t0.Add(5 * time.Second))
	assert.Empty(t, posts)
	assert.Zero(t, wait)

	q.add(end(1), t0)
	q.add(end(2), t0.Add(3*time.Second))
	posts, wait = q.take(t0.Add(5 * time.Second))
	assert.Empty(t, posts)
	assert.Equal(t, 3*time.Second, wait)

	posts, _ = q.take(t0.Add(8 * time.Second))
	assert.Equal(t, []notifyPost{{WorkspaceID: "ws", Session: "a", Collapse: "a", Title: "Ta", Message: "Done · 2 open", Kind: notifyDone}}, posts)
}
