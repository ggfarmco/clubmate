package core_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
)

func TestOpenFoldersDedupes(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	a, c := t.TempDir(), t.TempDir()
	p, created, err := svc.OpenFolders(t.Context(), []string{a, c})
	require.NoError(t, err)
	assert.True(t, created)
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(c, link))
	same, created, err := svc.OpenFolders(t.Context(), []string{a + "/", link})
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, p.ID, same.ID)
	other, created, err := svc.OpenFolders(t.Context(), []string{link})
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "link", other.Name)
	assert.NotEqual(t, p.ID, other.ID)
	evs, err := b.Read(t.Context(), "workspace.created", 0, 0)
	require.NoError(t, err)
	assert.Len(t, evs, 2)
}

func TestOpenFoldersFirstFolderFallback(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	first, added := t.TempDir(), t.TempDir()
	grown, _, err := svc.OpenFolders(t.Context(), []string{first})
	require.NoError(t, err)
	_, err = svc.AddRepo(t.Context(), grown.ID, added, contract.FolderMeta{AddedBy: "Fix login"})
	require.NoError(t, err)

	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(first, link))
	got, created, err := svc.OpenFolders(t.Context(), []string{link})
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, grown.ID, got.ID)

	_, created, err = svc.OpenFolders(t.Context(), []string{first, t.TempDir()})
	require.NoError(t, err)
	assert.True(t, created, "several folders only match exactly")

	got, created, err = svc.OpenFolders(t.Context(), []string{added})
	require.NoError(t, err)
	assert.True(t, created, "a folder that is not first never matches")
	assert.NotEqual(t, grown.ID, got.ID)

	evs, err := b.Read(t.Context(), "workspace.created", 0, 0)
	require.NoError(t, err)
	assert.Len(t, evs, 3)
}

func TestOpenFoldersExactBeatsFirstFolder(t *testing.T) {
	svc := testutil.NewService(t)
	dir := t.TempDir()
	exact, err := svc.CreateWorkspace(t.Context(), "exact", []string{dir})
	require.NoError(t, err)
	grown, err := svc.CreateWorkspace(t.Context(), "grown", []string{dir, t.TempDir()})
	require.NoError(t, err)
	require.NoError(t, svc.TouchWorkspace(t.Context(), grown.ID))

	got, created, err := svc.OpenFolders(t.Context(), []string{dir})
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, exact.ID, got.ID)
}

func TestHomeWorkspace(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HOME", home)
	svc := testutil.NewService(t)
	p, err := svc.HomeWorkspace(t.Context())
	require.NoError(t, err)
	again, err := svc.HomeWorkspace(t.Context())
	require.NoError(t, err)
	assert.Equal(t, p.ID, again.ID)
	assert.True(t, again.Home)
	assert.Equal(t, "~", again.Name)
	assert.Equal(t, []string{home}, again.Paths())
	all, err := svc.ListWorkspaces(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, 1)
}
