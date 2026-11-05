package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/sdui"
)

func TestReplayedEntriesCarryTheirLogTimes(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestOpenItemsCallLeavesTheRenderedTreeOnceItCompleted(t *testing.T) {
	t.Parallel()
	tool := func(id, title, status string) string {
		return `{"turn":1,"kind":"session/update","payload":{"sessionUpdate":"tool_call","toolCallId":"` + id + `","title":"` + title + `","kind":"other","status":"` + status + `"}}`
	}
	lines := []string{
		tool("search", "ToolSearch", "completed"),
		tool("failed", openItemsTool, "failed"),
		tool("posted", openItemsTool, "pending"),
		`{"turn":1,"kind":"session/update","payload":{"sessionUpdate":"tool_call_update","toolCallId":"posted","status":"completed"}}`,
		tool("running", openItemsTool, "in_progress"),
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	s := newSession(record{ID: "s"}, newEventLog(path), false, func(string, bool) {})

	view := s.view(0)

	var ids, hidden []string
	for _, en := range view.Entries {
		ids = append(ids, en.ID)
		if en.Hidden {
			hidden = append(hidden, en.ID)
		}
	}
	assert.Equal(t, []string{"search", "failed", "posted", "running"}, ids)
	assert.Equal(t, []string{"posted"}, hidden)
	var cards []string
	for _, n := range nodesOf(transcript(screen{View: view}), "toolCard") {
		cards = append(cards, n.ID)
	}
	assert.Equal(t, []string{"s/tool/search", "s/tool/failed", "s/tool/running"}, cards)
}

func TestDocLinksResolveWhenTheTurnEnds(t *testing.T) {
	wt, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	for _, p := range []string{filepath.Join(wt, "docs", "plan.md"), filepath.Join(wt, "out", "report.html"), filepath.Join(wt, "main.go"), filepath.Join(home, "notes.md")} {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
	}
	report := filepath.Join(wt, "out", "report.html")
	text := "See [the plan](docs/plan.md#goals), `docs/plan.md`, [report](" + report + "), [web](https://x.dev/a.md), [gone](docs/missing.md), `main.go`, `docs/`, `~/notes.md`.\n\n```\ndocs/plan.md\n```\n"
	chunk, err := json.Marshal(map[string]any{"turn": 1, "kind": "session/update", "payload": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": text}}})
	require.NoError(t, err)
	lines := []string{`{"turn":1,"kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"go"}]}}`, string(chunk)}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	s := newSession(record{Worktree: wt}, newEventLog(path), false, func(string, bool) {})

	before := s.view(0)
	require.Len(t, before.Entries, 2)
	assert.Nil(t, before.Entries[1].DocLinks)
	cursor, ok := parseCursor(before.Cursor)
	require.True(t, ok)

	s.mu.Lock()
	s.recordLocked(actorClubmate, kindTurnEnd, turnEndPayload{StopReason: "end_turn"})
	s.mu.Unlock()
	after := s.viewSince(0, cursor)
	require.Len(t, after.Entries, 1)
	assert.Equal(t, map[string]string{
		"docs/plan.md#goals": filepath.Join(wt, "docs", "plan.md") + "#goals",
		"docs/plan.md":       filepath.Join(wt, "docs", "plan.md"),
		report:               report,
		"~/notes.md":         filepath.Join(home, "notes.md"),
	}, after.Entries[0].DocLinks)
	node, _ := buildEntryNode(screen{View: after}, after.Entries[0], "")
	assert.Equal(t, after.Entries[0].DocLinks, node.Props["docLinks"])
	assert.Len(t, after.Entries[0].DocCards, 3)
	assert.Equal(t, after.Entries[0].DocCards, node.Props["docCards"])
}

func TestDocCardsListMentionedDocsInOrder(t *testing.T) {
	t.Parallel()
	wt, other := t.TempDir(), t.TempDir()
	files := map[string]string{
		filepath.Join(wt, "docs", "plan.md"):    "intro\n\n# Retry plan\n",
		filepath.Join(wt, "flow.mmd"):           "graph TD; A-->B\n",
		filepath.Join(wt, "out", "report.html"): "<title>Q3 report</title>",
		filepath.Join(other, "x.md"):            "no heading\n",
		filepath.Join(wt, "notes.md"):           "# Notes\n",
	}
	for p, src := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(src), 0o600))
	}
	outside := filepath.Join(other, "x.md")
	text := "[plan](docs/plan.md#goals), `docs/plan.md`, `flow.mmd`, [report](out/report.html), `" + outside + "`, `notes.md`."

	cards := docRefs(text, wt).cards

	assert.Equal(t, []sdui.DocCard{
		{Path: filepath.Join(wt, "docs", "plan.md"), Kind: "markdown", Title: "Retry plan", Rel: "docs/plan.md"},
		{Path: filepath.Join(wt, "flow.mmd"), Kind: "mermaid", Title: "flow", Rel: "flow.mmd"},
		{Path: filepath.Join(wt, "out", "report.html"), Kind: "artifact", Title: "Q3 report", Rel: "out/report.html"},
		{Path: outside, Kind: "markdown", Title: "x", Rel: outside},
		{Path: filepath.Join(wt, "notes.md"), Kind: "markdown", Title: "Notes", Rel: "notes.md"},
	}, cards)
}

func TestPresentedDocsAreMarkedAndAddedWhenTheTurnEnds(t *testing.T) {
	t.Parallel()
	wt := t.TempDir()
	for _, name := range []string{"spec.md", "notes.md", "report.html", "later.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(wt, name), []byte("# "+name+"\n"), 0o600))
	}
	canonical, err := filepath.EvalSymlinks(wt)
	require.NoError(t, err)
	s := newSession(record{Worktree: wt}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	say := func(turn int, text string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.apply(envelope{Turn: turn, Kind: kindUpdate, Payload: json.RawMessage(`{"sessionUpdate":"agent_message_chunk","messageId":"m` + strconv.Itoa(turn) + `","content":{"type":"text","text":` + strconv.Quote(text) + `}}`)})
	}
	record := func(turn int, kind string, payload any) {
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.apply(envelope{Turn: turn, Kind: kind, Payload: raw})
	}
	cards := func(turn int) []sdui.DocCard {
		for _, en := range s.view(0).Entries {
			if en.Kind == entryAgent && en.Turn == turn {
				return en.DocCards
			}
		}
		return nil
	}

	say(1, "Updated `notes.md` and `spec.md`.")
	record(1, kindPresented, presentedDoc{Path: filepath.Join(canonical, "spec.md")})
	record(1, kindPresented, presentedDoc{Path: filepath.Join(canonical, "report.html"), Title: "Q3 numbers"})
	record(1, kindTurnEnd, turnEndPayload{StopReason: "end_turn"})
	say(2, "Also `later.md`.")
	record(2, kindPresented, presentedDoc{Path: filepath.Join(canonical, "notes.md")})
	record(2, kindTurnEnd, turnEndPayload{StopReason: "end_turn"})
	record(2, kindPresented, presentedDoc{Path: filepath.Join(canonical, "later.md")})

	assert.Equal(t, []sdui.DocCard{
		{Path: filepath.Join(wt, "spec.md"), Kind: "markdown", Title: "spec.md", Rel: "spec.md", Presented: true},
		{Path: filepath.Join(canonical, "report.html"), Kind: "artifact", Title: "Q3 numbers", Rel: "report.html", Presented: true},
		{Path: filepath.Join(wt, "notes.md"), Kind: "markdown", Title: "notes.md", Rel: "notes.md"},
	}, cards(1))
	assert.Equal(t, []sdui.DocCard{
		{Path: filepath.Join(wt, "later.md"), Kind: "markdown", Title: "later.md", Rel: "later.md", Presented: true},
		{Path: filepath.Join(canonical, "notes.md"), Kind: "markdown", Title: "notes.md", Rel: "notes.md", Presented: true},
	}, cards(2))
}
