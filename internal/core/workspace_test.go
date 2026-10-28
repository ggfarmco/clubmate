package core_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

func TestOpenFoldersDedupes(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	a, c := t.TempDir(), t.TempDir()
	p, created, err := svc.OpenFolders(t.Context(), []string{a, c})
	require.NoError(t, err)
	assert.True(t, created)
	same, created, err := svc.OpenFolders(t.Context(), []string{c + "/", a})
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, p.ID, same.ID)
	other, created, err := svc.OpenFolders(t.Context(), []string{a})
	require.NoError(t, err)
	assert.True(t, created)
	assert.NotEqual(t, p.ID, other.ID)
	evs, err := b.Read(t.Context(), "project.created", 0, 0)
	require.NoError(t, err)
	assert.Len(t, evs, 2)
}

func TestHomeWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	svc := testutil.NewService(t)
	p, err := svc.HomeWorkspace(t.Context())
	require.NoError(t, err)
	again, err := svc.HomeWorkspace(t.Context())
	require.NoError(t, err)
	assert.Equal(t, p.ID, again.ID)
	assert.True(t, again.Home)
	assert.Equal(t, "~", again.Name)
	assert.Equal(t, []string{home}, again.Repos)
	all, err := svc.ListProjects(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, 1)
}
