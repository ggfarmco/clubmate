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

	"clubmate/pkg/sdui"
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

func TestDocOnlyForCompletedDocTools(t *testing.T) {
	tool := func(update, id, status string, rest string) string {
		return `{"turn":1,"kind":"session/update","payload":{"sessionUpdate":"` + update + `","toolCallId":"` + id + `","kind":"edit","status":"` + status + `"` + rest + `}}`
	}
	diff := func(path string) string {
		return `,"content":[{"type":"diff","path":"` + path + `","newText":"x"}]`
	}
	lines := []string{
		tool("tool_call", "doc", "pending", diff("/r/docs/a.md")),
		tool("tool_call_update", "doc", "completed", ""),
		tool("tool_call", "code", "completed", diff("/r/main.go")),
		tool("tool_call", "failed", "failed", diff("/r/a.md")),
		tool("tool_call", "relative", "completed", `,"locations":[{"path":"main.go"},{"path":"docs/b.md"}]`),
		tool("tool_call", "artifact", "completed", diff("/r/out/report.html")),
		`{"turn":1,"kind":"session/update","payload":{"sessionUpdate":"tool_call","toolCallId":"link","kind":"other","status":"completed","content":[{"type":"content","content":{"type":"resource_link","uri":"file:///r/plan.mmd","name":"plan.mmd"}}]}}`,
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	s := newSession(record{Worktree: "/w"}, newEventLog(path), false, func(string, bool) {})

	docs := map[string]string{}
	for _, en := range s.view(0).Entries {
		docs[en.ID] = en.Doc
	}
	assert.Equal(t, map[string]string{"doc": "/r/docs/a.md", "code": "", "failed": "", "relative": "/w/docs/b.md", "artifact": "/r/out/report.html", "link": "/r/plan.mmd"}, docs)

	actions, _ := toolCard("c", "/r", s.view(0).Entries[0], false, false).Props["actions"].([]sdui.CardAction)
	require.Len(t, actions, 2)
	assert.Equal(t, sdui.CardAction{Action: actOpenDoc, Label: "Open", Icon: "doc.richtext", Value: "/r/docs/a.md"}, actions[0])
	assert.Equal(t, actOpenLocation, actions[1].Action)
}
