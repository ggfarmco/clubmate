package graphs

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

func TestPruneDeletesRunsEndedBeforeCutoff(t *testing.T) {
	a := testApp(t, &fakeCore{})
	a.stateDir = t.TempDir()
	start := a.now()
	stop := func(r *Run) {
		_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "agents.graph-stop", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
		require.NoError(t, err)
	}
	old, recent, running := runGraph(t, a), runGraph(t, a), runGraph(t, a)
	for _, r := range []*Run{old, recent, running} {
		require.NoError(t, os.MkdirAll(a.outputDir(r.ID), 0o700))
	}
	stop(old)
	a.now = func() time.Time { return start.Add(8 * 24 * time.Hour) }
	stop(recent)
	var changed []string
	a.changed = func(ws string) { changed = append(changed, ws) }

	a.prune(t.Context(), 7*24*time.Hour)

	var ids []string
	for _, r := range a.workspaceRuns("ws1", true) {
		ids = append(ids, r.ID)
	}
	assert.ElementsMatch(t, []string{recent.ID, running.ID}, ids)
	assert.NoDirExists(t, a.outputDir(old.ID))
	assert.DirExists(t, a.outputDir(recent.ID))
	assert.DirExists(t, a.outputDir(running.ID))
	assert.True(t, slices.Contains(changed, "ws1"))
}
