package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type storeThatFails struct {
	memStore
}

func (storeThatFails) StorageSet(context.Context, string, any) error {
	return errors.New("core going down")
}

func TestQueuedPromptSentAfterAFailedSaveIsNotRestored(t *testing.T) {
	t.Parallel()
	dir, kv := t.TempDir(), memStore{}
	rec := record{ID: "agt_1", Workspace: "ws_1"}
	s := newSession(rec, newEventLog(logPath(dir, rec.ID)), false, func(string, bool) {})
	s.mu.Lock()
	s.enqueueLocked(promptArgs{Text: "follow up"})
	s.mu.Unlock()
	(&notifier{kv: kv, dir: dir}).persist(t.Context(), s)

	s.mu.Lock()
	_, ok := s.dequeueLocked()
	s.mu.Unlock()
	require.True(t, ok)
	(&notifier{kv: storeThatFails{kv}, dir: dir}).persist(t.Context(), s)
	s.mu.Lock()
	s.recordSentLocked(promptPayload{})
	s.mu.Unlock()

	var stored record
	found, err := kv.StorageGet(t.Context(), sessionStorageKey(rec), &stored)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, stored.Queue, 1)
	queue, unsent := restoredQueue(dir, rec.ID, stored.Queue)
	assert.Empty(t, queue)
	assert.False(t, unsent)
}

func TestGateMessageSentBeforeACrashIsNotQueuedAgain(t *testing.T) {
	t.Parallel()
	dir, kv := t.TempDir(), memStore{}
	msg := gateMessage{Call: "c1", Tail: "again"}
	rec := record{ID: "agt_1", Workspace: "ws_1", Calls: []*callHold{{Hold: &heldTurn{Gate: "g", Last: gateChanges}, Sent: "c1", Message: &msg}}}
	s := newSession(rec, newEventLog(logPath(dir, rec.ID)), false, func(string, bool) {})
	s.mu.Lock()
	s.enqueueLocked(promptArgs{Text: msg.text(), Queued: true, Gate: true, Message: &msg})
	_, ok := s.dequeueLocked()
	s.mu.Unlock()
	require.True(t, ok)
	(&notifier{kv: kv, dir: dir}).persist(t.Context(), s)
	s.mu.Lock()
	s.recordSentLocked(promptPayload{})
	s.mu.Unlock()
	(&notifier{kv: storeThatFails{kv}, dir: dir}).persist(t.Context(), s)

	var stored record
	found, err := kv.StorageGet(t.Context(), sessionStorageKey(rec), &stored)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, stored.Queue, 1)
	booted := newSession(stored, newEventLog(logPath(dir, rec.ID)), false, func(string, bool) {})
	before := slices.Clone(stored.Queue)
	queue, _ := restoredQueue(dir, rec.ID, stored.Queue)
	booted.restoreQueue(queue)

	booted.settleDelivered(before, queue)
	(&app{}).tellFindings(booted, false)

	assert.Empty(t, queuedCalls(booted))
	assert.True(t, booted.snapshot().Calls[0].Told)
	assert.Empty(t, booted.snapshot().Calls[0].Sent)
}
