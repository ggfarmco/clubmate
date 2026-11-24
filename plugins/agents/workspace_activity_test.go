package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

func TestActivityCountsEachWorkspaceOnce(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry(), uiState: newUIState(), reviews: newReviewBoard(), watchdog: newWatchdog()}
	dir := t.TempDir()
	at := time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)
	add := func(rec record, age time.Duration) {
		rec.CreatedAt, rec.UpdatedAt = at, at.Add(-age)
		a.sessions[rec.ID] = newSession(rec, newEventLog(filepath.Join(dir, rec.ID+".jsonl")), true, func(string, bool) {})
	}
	add(record{ID: "boss", Workspace: "shop", Status: statusDone}, 3*time.Minute)
	add(record{ID: "kid", Workspace: "shop", Status: statusRunning, lineage: lineage{ParentSession: "boss"}}, time.Minute)
	add(record{ID: "solo", Workspace: "shop", Status: statusRunning}, 4*time.Minute)
	add(record{ID: "asks", Workspace: "shop", Status: statusWaiting}, 5*time.Minute)
	add(record{ID: "broke", Workspace: "shop", Status: statusError}, 6*time.Minute)
	add(record{ID: "limit", Workspace: "shop", Status: statusError, LimitWait: &limitWait{}}, 7*time.Minute)
	add(record{ID: "voice", Workspace: "shop", Status: statusWaiting, SystemPrompt: "voice"}, 0)
	add(record{ID: "quiet", Workspace: "docs", Status: statusDone}, time.Hour)
	add(record{ID: "listen", Workspace: "voice-only", Status: statusWaiting, SystemPrompt: "voice"}, 0)
	a.runNeeds.set("graphs", []runNeed{{Run: "gr_1", Since: at}})

	out, err := a.activityCommand(t.Context(), pluginsdk.CommandRun{})
	require.NoError(t, err)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"shop": {"needsYou": 1, "running": 2, "failed": 1, "lastActiveAt": "2026-11-23T07:59:00Z"},
		"docs": {"lastActiveAt": "2026-11-23T07:00:00Z"},
		"graphs": {"needsYou": 1}
	}`, string(raw))
	for _, ws := range []string{"shop", "docs", "voice-only", "graphs"} {
		assert.Equal(t, activityOf(a.infos(ws)), out.(map[string]workspaceActivity)[ws], ws)
	}
}
