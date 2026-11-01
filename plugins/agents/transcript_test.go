package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplayedEntriesCarryTheirLogTimes(t *testing.T) {
	base := time.Date(2026, 10, 31, 9, 41, 0, 0, time.UTC)
	lines := []string{
		`{"turn":1,"ts":"2026-10-31T09:41:00Z","kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"go"}]}}`,
		`{"turn":1,"ts":"2026-10-31T09:41:01Z","kind":"session/update","payload":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"hm"}}}`,
		`{"turn":1,"ts":"2026-10-31T09:41:04Z","kind":"session/request_permission","payload":{"id":"permission-1","requestId":"1","params":{"toolCall":{"toolCallId":"t1"},"options":[]}}}`,
		`{"turn":1,"ts":"2026-10-31T09:42:00Z","kind":"permission","payload":{"request":"permission-1","outcome":{"outcome":"cancelled"}}}`,
		`{"turn":1,"kind":"session/update","payload":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ok"}}}`,
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	s := newSession(record{}, newEventLog(path), false, func(string, bool) {})

	entries := s.view(0).Entries
	require.Len(t, entries, 4)
	assert.Equal(t, []time.Time{base, base.Add(time.Second), base.Add(4 * time.Second), {}}, []time.Time{entries[0].At, entries[1].At, entries[2].At, entries[3].At})
	assert.Equal(t, base.Add(time.Minute), entries[2].AnsweredAt)
	raw, err := json.Marshal(entries[3])
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"at"`)
}

func TestViewSinceReturnsOnlyEntriesChangedAfterTheCursor(t *testing.T) {
	lines := []string{
		`{"turn":1,"kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"go"}]}}`,
		`{"turn":1,"kind":"session/update","payload":{"sessionUpdate":"tool_call","toolCallId":"t1","title":"Read","status":"pending"}}`,
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	s := newSession(record{}, newEventLog(path), false, func(string, bool) {})
	ids := func(v transcriptView) []string {
		var out []string
		for _, en := range v.Entries {
			out = append(out, en.Kind+"/"+en.ID)
		}
		return out
	}
	since := func(v transcriptView) int64 {
		n, ok := parseCursor(v.Cursor)
		require.True(t, ok)
		return n
	}

	full := s.view(0)
	require.Len(t, full.Entries, 2)
	assert.Empty(t, ids(s.viewSince(0, since(full))))

	s.update(json.RawMessage(`{"update":{"sessionUpdate":"tool_call_update","toolCallId":"t1","title":"Read a.go"}}`))
	s.update(json.RawMessage(`{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ok"}}}`))
	changed := s.viewSince(0, since(full))
	assert.Equal(t, []string{"tool/t1", "agent/agent-1"}, ids(changed))

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()
	reopened := s.viewSince(0, since(changed))
	assert.Equal(t, []string{"tool/t1"}, ids(reopened))
	assert.JSONEq(t, `"pending"`, string(reopened.Entries[0].Tool["status"]))

	_, ok := parseCursor("other" + cursorSep + "1")
	assert.False(t, ok)
}
