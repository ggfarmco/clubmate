package rpc_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
)

func TestWorktreeSelection(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t)
	a, b, a2, b2 := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{a, b})
	require.NoError(t, err)
	c := h.Client(t)
	set := func(repo, path string) (project.Project, error) {
		var out project.Project
		err := c.Call(t.Context(), "workspace.worktree.set", map[string]string{"id": p.ID, "repo": repo, "path": path}, &out)
		return out, err
	}
	cwd := func() string {
		var s session.Session
		require.NoError(t, c.Call(t.Context(), "session.create", map[string]any{"projectId": p.ID, "command": []string{"sh", "-c", "exec cat"}}, &s))
		t.Cleanup(func() { c.Call(context.Background(), "session.kill", map[string]string{"id": s.ID}, nil) })
		return s.Cwd
	}

	args := map[string]string{"id": p.ID, "repo": b, "path": b2}
	err = c.Call(t.Context(), "command.run", map[string]any{"id": "workspace.worktree.set", "agent": true, "args": args}, nil)
	require.ErrorIs(t, err, rpc.ErrNotFound)
	var got project.Project
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "workspace.worktree.set", "args": args}, &got))
	assert.Equal(t, []string{a, b2}, got.Roots)
	assert.Equal(t, []string{a, b}, got.Repos)
	evs, err := h.Bus.Read(t.Context(), "project.updated", 0, 0)
	require.NoError(t, err)
	require.NotEmpty(t, evs)
	var payload struct {
		Project project.Project `json:"project"`
	}
	require.NoError(t, json.Unmarshal(evs[len(evs)-1].Payload, &payload))
	assert.Equal(t, []string{a, b2}, payload.Project.Roots)
	assert.Equal(t, a, cwd())

	_, err = set(a, a2)
	require.NoError(t, err)
	assert.Equal(t, a2, cwd())

	got, err = set(a, "")
	require.NoError(t, err)
	assert.Equal(t, []string{a, b2}, got.Roots)

	_, err = set(t.TempDir(), b2)
	require.ErrorIs(t, err, rpc.ErrInvalidParams)
	assert.EqualError(t, err, "repo is not a folder of this workspace")
	file := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	_, err = set(b, file)
	require.ErrorIs(t, err, rpc.ErrInvalidParams)
	assert.EqualError(t, err, "path is not a directory")
}
