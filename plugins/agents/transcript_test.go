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
