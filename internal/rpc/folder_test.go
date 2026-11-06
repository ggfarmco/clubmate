package rpc_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
	"clubmate/internal/workspace"
)

func TestFolderMethods(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t)
	a, b := t.TempDir(), t.TempDir()
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", []string{a})
	require.NoError(t, err)
	c := h.Client(t)

	require.NoError(t, c.Call(t.Context(), "workspace.folder.add", map[string]any{"id": p.ID, "path": b, "addedBy": "Fix login", "addedAfk": true}, nil))
	var got workspace.Workspace
	require.NoError(t, c.Call(t.Context(), "workspace.get", map[string]string{"id": p.ID}, &got))
	require.Len(t, got.Folders, 2)
	assert.Equal(t, "Fix login", got.Folders[1].AddedBy)
	assert.True(t, got.Folders[1].AddedAfk)
	assert.False(t, got.Folders[1].Seen)
	assert.False(t, got.Folders[1].AddedAt.IsZero())

	require.NoError(t, c.Call(t.Context(), "workspace.folder.seen", map[string]string{"id": p.ID, "path": b}, &got))
	assert.True(t, got.Folders[1].Seen)

	require.NoError(t, c.Call(t.Context(), "workspace.folder.remove", map[string]string{"id": p.ID, "path": b}, &got))
	assert.Equal(t, []string{a}, got.Paths())
	assert.DirExists(t, b)

	err = c.Call(t.Context(), "workspace.folder.remove", map[string]string{"id": p.ID, "path": a}, nil)
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
	assert.ErrorContains(t, err, workspace.ErrLastFolder.Error())

	evs, err := h.Bus.Read(t.Context(), "workspace.updated", 0, 0)
	require.NoError(t, err)
	assert.Len(t, evs, 3)
}

func TestFolderMethodsAreNotCommands(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t)
	dir := t.TempDir()
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", []string{dir, t.TempDir()})
	require.NoError(t, err)
	c := h.Client(t)
	for _, id := range []string{"workspace.folder.remove", "workspace.folder.seen"} {
		args := map[string]string{"id": p.ID, "path": dir}
		err := c.Call(t.Context(), "command.run", map[string]any{"id": id, "agent": true, "args": args}, nil)
		assert.ErrorIs(t, err, rpc.ErrNotFound, id)
	}
}
