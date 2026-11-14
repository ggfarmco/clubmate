package main

import (
	"context"
	"errors"
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
