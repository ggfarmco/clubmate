package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnloadedTranscriptReadsBackAndResendsEveryEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(`{"turn":1,"kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"go"}]}}`+"\n"), 0o600))
	s := newSession(record{}, newEventLog(path), false, func(string, bool) {})
	s.update(json.RawMessage(`{"update":{"sessionUpdate":"tool_call","toolCallId":"t1","title":"Read","status":"completed"}}`))
	s.update(json.RawMessage(`{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ok"}}}`))
	before := s.view(0)
	n, ok := parseCursor(before.Cursor)
	require.True(t, ok)

	s.mu.Lock()
	require.True(t, s.unloadableLocked(0))
	s.unloadLocked()
	s.mu.Unlock()
	assert.Empty(t, s.entries)

	resent := s.viewSince(0, n)
	require.Len(t, resent.Entries, len(before.Entries))
	for i, en := range resent.Entries {
		assert.Equal(t, before.Entries[i].Kind+"/"+before.Entries[i].ID, en.Kind+"/"+en.ID)
		assert.Equal(t, before.Entries[i].Text, en.Text)
		assert.Equal(t, before.Entries[i].Tool, en.Tool)
	}
	assert.Greater(t, resent.Entries[0].rev, n)
}
