package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailedReplayRetriesFromTheLogAlone(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(`{"turn":1,"kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"go"}]}}`+"\n"), 0o600))
	s := newSession(record{}, newEventLog(path), false, func(string, bool) {})
	s.update(json.RawMessage(`{"update":{"sessionUpdate":"tool_call","toolCallId":"t1","title":"Read","status":"completed"}}`))
	s.mu.Lock()
	s.unloadLocked()
	s.mu.Unlock()

	require.NoError(t, os.Chmod(path, 0))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	assert.Empty(t, s.view(0).Entries)
	s.update(json.RawMessage(`{"update":{"sessionUpdate":"tool_call","toolCallId":"t2","title":"Read","status":"completed"}}`))

	require.NoError(t, os.Chmod(path, 0o600))
	entries := s.view(0).Entries
	require.Len(t, entries, 3)
	assert.Equal(t, entryUser, entries[0].Kind)
	assert.Equal(t, []string{"t1", "t2"}, []string{entries[1].ID, entries[2].ID})
}
