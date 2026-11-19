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

	"clubmate/pkg/pluginsdk"
)

var docsBase = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

func TestAgentDocsUnique(t *testing.T) {
	t.Parallel()
	var lines []string
	for i := range 30 {
		lines = append(lines, docLine(i, "", fmt.Sprintf("a%d", i), "/r/a.md"))
	}
	lines = append(lines, docLine(10, "", "b", "/r/out/b.html"))
	a := docsApp(t, docsSession(t, "s1", "ws", lines...))

	got := runDocs(t, a, "ws", "")

	assert.Equal(t, []docRef{
		{Path: "/r/a.md", Session: "s1", At: docsBase.Add(29 * time.Minute)},
		{Path: "/r/out/b.html", Session: "s1", At: docsBase.Add(10 * time.Minute)},
	}, got)
}

func TestAgentDocsPerSession(t *testing.T) {
	t.Parallel()
	s1 := docsSession(t, "s1", "ws",
		docLine(1, "", "a", "/r/a.md"),
		`{"turn":1,"ts":"`+docsBase.Add(2*time.Minute).Format(time.RFC3339)+`","kind":"session/update","payload":{"sessionUpdate":"subagent_update","sessionId":"sub-1","title":"plan","state":{"state":"running"}}}`,
		docLine(3, "sub-1", "c", "/r/sub.md"),
	)
	s2 := docsSession(t, "s2", "ws", docLine(4, "", "b", "/r/b.md"))
	other := docsSession(t, "s3", "other", docLine(5, "", "x", "/r/x.md"))
	a := docsApp(t, s1, s2, other)

	assert.Equal(t, []string{"/r/sub.md", "/r/a.md"}, docPaths(runDocs(t, a, "ws", "s1")))
	assert.Equal(t, []string{"/r/b.md", "/r/sub.md", "/r/a.md"}, docPaths(runDocs(t, a, "ws", "")))
	_, err := a.docsCommand(t.Context(), pluginsdk.CommandRun{WorkspaceID: "ws"}, docsArgs{Session: "s3"})
	assert.ErrorIs(t, err, errNoSession)
}

func TestAgentDocsComeFromTheIndexWithoutLoadingASession(t *testing.T) {
	t.Parallel()
	sessions := []*session{
		docsSession(t, "s1", "ws",
			docLine(1, "", "a", "/r/a.md"),
			`{"turn":1,"ts":"`+docsBase.Add(2*time.Minute).Format(time.RFC3339)+`","kind":"session/update","payload":{"sessionUpdate":"subagent_update","sessionId":"sub-1","title":"plan","state":{"state":"running"}}}`,
			docLine(3, "sub-1", "c", "/r/sub.md"),
			docLine(6, "", "a2", "/r/a.md"),
		),
		docsSession(t, "s2", "ws", `{"turn":1,"ts":"`+docsBase.Format(time.RFC3339)+`","kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"go"}]}}`),
		docsSession(t, "s3", "ws", docLine(4, "", "b", "/r/b.md"), docLine(5, "", "a", "/r/a.md")),
	}
	a := docsApp(t, sessions...)
	want := []docRef{
		{Path: "/r/a.md", Session: "s1", At: docsBase.Add(6 * time.Minute)},
		{Path: "/r/b.md", Session: "s3", At: docsBase.Add(4 * time.Minute)},
		{Path: "/r/sub.md", Session: "s1", At: docsBase.Add(3 * time.Minute)},
	}

	require.Equal(t, want, runDocs(t, a, "ws", ""))
	for _, s := range sessions {
		require.NoError(t, os.Chmod(s.log.path, 0))
		t.Cleanup(func() { _ = os.Chmod(s.log.path, 0o600) })
	}

	assert.Equal(t, want, runDocs(t, a, "ws", ""))
	assert.Equal(t, []string{"/r/a.md", "/r/sub.md"}, docPaths(runDocs(t, a, "ws", "s1")))
	restarted := make([]*session, 0, len(sessions))
	for _, s := range sessions {
		raw, err := json.Marshal(s.snapshot())
		require.NoError(t, err)
		var rec record
		require.NoError(t, json.Unmarshal(raw, &rec))
		restarted = append(restarted, newSession(rec, newEventLog(s.log.path), false, func(string, bool) {}))
	}
	assert.Equal(t, want, runDocs(t, docsApp(t, restarted...), "ws", ""))
	for _, s := range append(sessions, restarted...) {
		assert.False(t, s.loaded, s.rec.ID)
	}
}

func TestAgentDocsOfALiveSessionAreIndexedWhenItUnloads(t *testing.T) {
	t.Parallel()
	s := docsSession(t, "s1", "ws", docLine(1, "", "a", "/r/a.md"))
	a := docsApp(t, s)
	s.update(json.RawMessage(`{"update":{"sessionUpdate":"tool_call","toolCallId":"n","kind":"edit","status":"completed","content":[{"type":"diff","path":"/r/new.md","newText":"x"}]}}`))
	var saves []string
	s.changed = func(kind string, save bool) {
		if save {
			saves = append(saves, kind)
		}
	}

	live := runDocs(t, a, "ws", "")
	require.Equal(t, []string{"/r/new.md", "/r/a.md"}, docPaths(live))
	a.unloadIdle(0)
	require.NoError(t, os.Chmod(s.log.path, 0))
	t.Cleanup(func() { _ = os.Chmod(s.log.path, 0o600) })

	assert.Equal(t, []string{changeUpdate}, saves)
	assert.Equal(t, live, runDocs(t, a, "ws", ""))
	assert.False(t, s.loaded)
}

func docLine(minute int, sub, id, path string) string {
	agent := ""
	if sub != "" {
		agent = `"agentSession":"` + sub + `",`
	}
	ts := docsBase.Add(time.Duration(minute) * time.Minute).Format(time.RFC3339)
	return `{"turn":1,"ts":"` + ts + `",` + agent + `"kind":"session/update","payload":{"sessionUpdate":"tool_call","toolCallId":"` + id + `","kind":"edit","status":"completed","content":[{"type":"diff","path":"` + path + `","newText":"x"}]}}`
}

func docsSession(t *testing.T, id, workspace string, lines ...string) *session {
	t.Helper()
	path := filepath.Join(t.TempDir(), id+".jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return newSession(record{ID: id, Workspace: workspace, Worktree: "/r"}, newEventLog(path), false, func(string, bool) {})
}

func docsApp(t *testing.T, sessions ...*session) *app {
	t.Helper()
	a := &app{registry: registry{sessions: map[string]*session{}}}
	for _, s := range sessions {
		a.sessions[s.rec.ID] = s
	}
	return a
}

func runDocs(t *testing.T, a *app, workspace, session string) []docRef {
	t.Helper()
	res, err := a.docsCommand(t.Context(), pluginsdk.CommandRun{WorkspaceID: workspace}, docsArgs{Session: session})
	require.NoError(t, err)
	raw, err := json.Marshal(res)
	require.NoError(t, err)
	var out struct {
		Docs []docRef `json:"docs"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out.Docs
}

func docPaths(docs []docRef) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.Path
	}
	return out
}
