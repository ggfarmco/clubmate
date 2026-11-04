package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

func TestRunNeedsOnlyFromGraphs(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry(), uiState: newUIState()}
	need := runNeedsArgs{Runs: []runNeed{{Run: "gr_1", Title: "Review gate", Number: 4, Kind: needStep, Request: "step-tests", Node: "tests", Role: "Tests", Text: "Tests wants to run make test", Command: "make test", Since: time.Unix(100, 0)}}}
	for _, cmd := range []pluginsdk.CommandRun{{ID: "agents.run-needs", WorkspaceID: "ws", Via: graphsPlugin, Agent: true}, {ID: "agents.run-needs", WorkspaceID: "ws", Via: "remote"}} {
		_, err := a.runNeedsCommand(t.Context(), cmd, need)
		assert.ErrorIs(t, err, errUnknownCmd)
	}
	_, err := a.runNeedsCommand(t.Context(), pluginsdk.CommandRun{ID: "agents.run-needs", WorkspaceID: "ws", Via: graphsPlugin}, need)
	require.NoError(t, err)
	infos := a.infos("ws")
	require.Len(t, infos, 1)
	got := infos[0]
	assert.Equal(t, []any{"gr_1", kindRun, statusWaiting, true, groupNeedsYou, []string{"step-tests"}, false}, []any{got.ID, got.Kind, got.Status, got.NeedsYou, got.Group, got.Pending, got.Active})
	assert.Equal(t, pendingSummary{Kind: entryPermission, Text: "make test", Tool: toolExecute, Role: "Tests", At: time.Unix(100, 0)}, *got.PendingSummary)
	assert.Equal(t, api.Run{ID: "gr_1", Title: "Review gate", Number: 4}, *got.Run)

	full := a.sessionRevs.page("ws", infos, "")
	_, err = a.runNeedsCommand(t.Context(), pluginsdk.CommandRun{ID: "agents.run-needs", WorkspaceID: "ws", Via: graphsPlugin}, runNeedsArgs{})
	require.NoError(t, err)
	assert.Equal(t, []string{"gr_1"}, a.sessionRevs.page("ws", a.infos("ws"), full.Cursor).Removed)
}

func TestRunNeedChangesResolveWhatLeft(t *testing.T) {
	t.Parallel()
	step := runNeed{Run: "gr_1", Kind: needStep, Request: "step-tests"}
	capped := runNeed{Run: "gr_1", Kind: "cap", Request: "cap-1"}
	assert.Equal(t, []api.Changed{
		{Workspace: "ws", Session: "gr_1", Kind: api.ChangeResolved, Request: "step-tests", RequestKind: api.RequestPermission},
		{Workspace: "ws", Session: "gr_1", Kind: api.ChangeStatus, Status: statusWaiting, Request: "cap-1", RequestKind: api.RequestPermission},
	}, runNeedChanges("ws", []runNeed{step}, []runNeed{capped}))
}

func TestSidebarRunNeedsYou(t *testing.T) {
	t.Parallel()
	now := time.Now()
	run := &api.Run{ID: "gr_1", Title: "Review gate"}
	need := &runNeed{Run: "gr_1", Text: "Tests wants to run make test", Since: now}
	rows := []sessionRow{
		{Rec: record{ID: "agt_c", Worktree: "/r/a", Run: run, Title: "Review gate · Coder", Status: statusDone, CreatedAt: now, UpdatedAt: now}, RunNeed: need},
		{Rec: record{ID: "agt_r", Worktree: "/r/a", Run: run, Title: "Review gate · Reviewer", Status: statusIdle, CreatedAt: now, UpdatedAt: now}, RunNeed: need},
		{Rec: record{ID: "agt_x", Worktree: "/r/a", Title: "Solo", Status: statusDone, UpdatedAt: now}},
	}
	tree := sidebarView(rows, "", map[string]bool{runGroupKey("/r/a", "gr_1"): true}, now, "/r/a", false, filterNeeds, sidebarSearch{}, 0)
	require.NoError(t, internalsdui.Validate(tree))
	tabs := nodesOf(tree, "tabs")[0].Props["items"].([]sdui.Item)
	assert.Equal(t, "1", tabs[1].Badge)
	items := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, items, 1)
	assert.Equal(t, []any{"Review gate", "Tests wants to run make test", "warning", true}, []any{items[0].Label, items[0].Detail, items[0].Tone, items[0].Expanded})
}
