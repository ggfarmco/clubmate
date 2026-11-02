package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sessionA = "agt_000000000000000a"
	sessionB = "agt_000000000000000b"
)

func TestResolveSessionWithPendingQuestion(t *testing.T) {
	_, h, _ := newTestVoice(t)
	stubAgents(h, []map[string]any{{"id": sessionA, "title": "Voice plugin", "status": "waiting", "needsYou": true}}, nil)
	h.commands["agents.transcript"] = func(json.RawMessage) (any, error) {
		return map[string]any{"entries": []map[string]any{{
			"kind": "elicitation",
			"id":   "q1",
			"elicitation": map[string]any{"message": "Which store?", "fields": []map[string]any{{
				"key": "store", "type": "choice", "options": []map[string]string{{"value": "kv", "label": "KV"}, {"value": "sql", "label": "SQLite"}},
			}}},
		}}}, nil
	}

	c, err := resolve(t.Context(), h, testWorkspace, Pointer{Device: "phone", Section: "agents", Selection: Selection{Session: sessionA}})

	require.NoError(t, err)
	require.NotNil(t, c.Session)
	require.NotNil(t, c.Session.Question)
	assert.Equal(t, "q1", c.Session.Question.Request)
	assert.Equal(t, []QuestionOption{{Value: "kv", Label: "KV"}, {Value: "sql", Label: "SQLite"}}, c.Session.Question.Fields[0].Options)
	assert.Contains(t, hintWords(c), "Voice plugin")
}

func TestResolveViewIsCut(t *testing.T) {
	_, h, _ := newTestVoice(t)
	stubAgents(h, nil, []map[string]any{{"slot": "cron.main", "tree": map[string]any{"type": "text", "props": map[string]string{"text": strings.Repeat("x", 20<<10)}}}})

	c, err := resolve(t.Context(), h, testWorkspace, Pointer{Device: "mac", Section: "cron"})

	require.NoError(t, err)
	assert.LessOrEqual(t, len(c.View), maxViewBytes)
	assert.True(t, json.Valid(c.View))
}

func TestResolveCutTaskKeepsIDTitleStatus(t *testing.T) {
	_, h, _ := newTestVoice(t)
	stubAgents(h, nil, nil)
	h.commands["backlog.get"] = func(json.RawMessage) (any, error) {
		return map[string]any{"id": 7, "title": "Voice polish", "status": "open", "description": strings.Repeat("x", 20<<10)}, nil
	}

	c, err := resolve(t.Context(), h, testWorkspace, Pointer{Device: "mac", Section: "backlog", Selection: Selection{Task: 7}})

	require.NoError(t, err)
	assert.JSONEq(t, `{"id":7,"title":"Voice polish","status":"open","truncated":true}`, string(c.Task))
}

func TestResolveFallsBackToSelectedSession(t *testing.T) {
	sessions := []map[string]any{
		{"id": sessionA, "title": "A", "status": "waiting", "needsYou": true, "needsYouSince": "2026-11-01T10:00:00Z"},
		{"id": sessionB, "title": "B", "status": "idle"},
	}
	sidebar := []map[string]any{{"slot": "agents.sidebar", "tree": map[string]any{"type": "vstack", "children": []map[string]any{
		{"type": "tree", "id": "sessions", "props": map[string]any{"selected": sessionB}},
	}}}}
	tests := []struct {
		name  string
		slots []map[string]any
		want  string
	}{
		{"sidebar selection", sidebar, sessionB},
		{"oldest needing you", nil, sessionA},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, h, _ := newTestVoice(t)
			stubAgents(h, sessions, tt.slots)
			h.commands["agents.transcript"] = func(json.RawMessage) (any, error) { return map[string]any{}, nil }

			c, err := resolve(t.Context(), h, testWorkspace, Pointer{Device: "phone", Section: "agents"})

			require.NoError(t, err)
			require.NotNil(t, c.Session)
			assert.Equal(t, tt.want, c.Session.ID)
		})
	}
}

func TestResolveKeepsCurrentAndNewestBranches(t *testing.T) {
	_, h, _ := newTestVoice(t)
	stubAgents(h, nil, nil)
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	worktrees := []map[string]any{{"branch": "main", "main": true, "committed": day}, {"branch": "feat/here", "current": true, "committed": day}}
	for i := range maxOtherBranches + 2 {
		worktrees = append(worktrees, map[string]any{"branch": "feat/b" + strconv.Itoa(i), "committed": day.Add(time.Duration(i) * time.Hour)})
	}
	h.commands["git.worktrees"] = func(json.RawMessage) (any, error) { return worktrees, nil }

	c, err := resolve(t.Context(), h, testWorkspace, Pointer{Device: "mac", Section: "cron"})

	require.NoError(t, err)
	want := []string{"main", "feat/here"}
	for i := maxOtherBranches + 1; i > 1; i-- {
		want = append(want, "feat/b"+strconv.Itoa(i))
	}
	assert.Equal(t, want, c.Workspace.Branches)
}

func stubAgents(h *fakeHost, sessions, slots []map[string]any) {
	h.methods["workspace.get"] = func(any) (any, error) { return map[string]any{"id": testWorkspace, "name": "clubmate"}, nil }
	h.methods["ui.get"] = func(any) (any, error) { return slots, nil }
	h.commands["git.worktrees"] = func(json.RawMessage) (any, error) { return []map[string]string{{"branch": "feat/voice"}}, nil }
	h.commands["agents.sessions"] = func(json.RawMessage) (any, error) { return sessions, nil }
}
