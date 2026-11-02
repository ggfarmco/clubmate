package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

func searchApp(t *testing.T, recs ...record) *app {
	t.Helper()
	a := &app{stateDir: t.TempDir(), registry: registry{sessions: map[string]*session{}}}
	require.NoError(t, os.MkdirAll(filepath.Join(a.stateDir, sessionsDir), 0o700))
	for _, rec := range recs {
		a.sessions[rec.ID] = newSession(rec, newEventLog(logPath(a.stateDir, rec.ID)), false, func(string, bool) {})
	}
	return a
}

func logLine(t *testing.T, kind string, turn int, payload any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	line, err := json.Marshal(envelope{Kind: kind, Turn: turn, Payload: raw})
	require.NoError(t, err)
	return string(line) + "\n"
}

func promptLine(t *testing.T, turn int, text string) string {
	return logLine(t, kindPrompt, turn, map[string]any{"prompt": []map[string]string{{"type": "text", "text": text}}})
}

func replyLine(t *testing.T, turn int, id, text string) string {
	return logLine(t, kindUpdate, turn, map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": id, "content": map[string]string{"type": "text", "text": text}})
}

func appendLog(t *testing.T, a *app, id string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(logPath(a.stateDir, id), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer f.Close()
	_, err = f.WriteString(strings.Join(lines, ""))
	require.NoError(t, err)
}

func TestSearchMatchesTitlesFirstThenConversationByRecency(t *testing.T) {
	now := time.Now()
	a := searchApp(t,
		record{ID: "agt_title", Workspace: "w", Title: "Refund flow", UpdatedAt: now.Add(-time.Hour)},
		record{ID: "agt_prompt", Workspace: "w", Title: "Payments", Status: statusClosed, UpdatedAt: now.Add(-time.Minute)},
		record{ID: "agt_reply", Workspace: "w", Title: "Billing", UpdatedAt: now},
		record{ID: "agt_thought", Workspace: "w", Title: "Other", UpdatedAt: now},
		record{ID: "agt_elsewhere", Workspace: "x", Title: "Refund elsewhere", UpdatedAt: now},
	)
	appendLog(t, a, "agt_prompt", promptLine(t, 1, "hello"), promptLine(t, 2, "Why does the REFUND fail?"))
	appendLog(t, a, "agt_reply", replyLine(t, 1, "m1", "The **ref"), logLine(t, kindUpdate, 1, map[string]any{"sessionUpdate": "tool_call", "title": "refund tool"}), replyLine(t, 1, "m1", "und** is *queued* ([why](docs/r.md))."))
	appendLog(t, a, "agt_thought", logLine(t, kindUpdate, 1, map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]string{"type": "text", "text": "refund"}}))

	got := a.search("w", "  refund ")
	require.Len(t, got, 3)
	assert.Equal(t, []string{"agt_title", "agt_reply", "agt_prompt"}, []string{got[0].Session, got[1].Session, got[2].Session})
	assert.Equal(t, []searchHit{{Kind: hitTitle, Snippet: "Refund flow"}}, got[0].Hits)
	assert.Equal(t, []searchHit{{Kind: hitReply, Turn: 1, Snippet: "The refund is queued (why)."}}, got[1].Hits)
	assert.Equal(t, []searchHit{{Kind: hitPrompt, Turn: 2, Snippet: "…does the REFUND fail?"}}, got[2].Hits)
	assert.Equal(t, statusClosed, got[2].Status)
}

func TestSearchPicksUpLinesAppendedAfterTheFirstSearch(t *testing.T) {
	a := searchApp(t, record{ID: "agt_1", Workspace: "w"})
	appendLog(t, a, "agt_1", promptLine(t, 1, "first"))
	assert.Empty(t, a.search("w", "second"))

	line := promptLine(t, 2, "the second prompt")
	appendLog(t, a, "agt_1", line[:20])
	assert.Empty(t, a.search("w", "second"))

	appendLog(t, a, "agt_1", line[20:])
	got := a.search("w", "second")
	require.Len(t, got, 1)
	assert.Equal(t, []searchHit{{Kind: hitPrompt, Turn: 2, Snippet: "the second prompt"}}, got[0].Hits)
}

func TestSnippetCutsAroundTheMatchOnItsLine(t *testing.T) {
	text := "first line\n" + strings.Repeat("а", 40) + "needle" + strings.Repeat("б", 200)
	got := snippet(text, strings.Index(text, "needle"))
	assert.Equal(t, "…"+strings.Repeat("а", snippetBefore)+"needle"+strings.Repeat("б", snippetRunes-snippetBefore-6)+"…", got)
	assert.Equal(t, "needle here", snippet("first line\nneedle here", len("first line\n")))
}

func TestSidebarShowsSearchResultsInPlaceOfTheTree(t *testing.T) {
	now := time.Now()
	rows := []sessionRow{{Rec: record{ID: "agt_1", Worktree: "/r/a", Title: "Refunds", Status: statusDone}}}
	search := sidebarSearch{query: "refund", results: []searchResult{
		{Session: "agt_1", Title: "Refunds", Status: statusClosed, UpdatedAt: now, Hits: []searchHit{{Kind: hitTitle, Snippet: "Refunds"}, {Kind: hitReply, Turn: 3, Snippet: "the refund is queued"}}},
		{Session: "agt_2", Title: "Payments", Status: statusDone, UpdatedAt: now, Hits: []searchHit{{Kind: hitTitle, Snippet: "Payments"}}},
	}}
	tree := sidebarView(rows, "agt_1", nil, now, false, "", search)
	require.NoError(t, internalsdui.Validate(tree))

	assert.Empty(t, nodesOf(tree, "tabs"))
	assert.Equal(t, "refund", nodesOf(tree, "textField")[0].Props["value"])
	items := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, items, 2)
	assert.Equal(t, []string{"agt_1", "agt_2"}, []string{items[0].ID, items[1].ID})
	assert.Equal(t, statusIcons[statusClosed], items[0].Icon)
	require.Len(t, items[0].Children, 1)
	assert.Equal(t, "the refund is queued", items[0].Children[0].Label)
	assert.True(t, items[0].Expanded)
	assert.Empty(t, items[1].Children)
	session, ok := hitSession(items[0].Children[0].ID)
	assert.True(t, ok)
	assert.Equal(t, "agt_1", session)

	none := sidebarView(rows, "", nil, now, false, "", sidebarSearch{query: "zzz"})
	assert.Equal(t, fmt.Sprintf(noMatches, "zzz"), nodesOf(none, "empty")[0].Props["text"])
}
