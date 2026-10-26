package project_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/project"
)

func openStore(t *testing.T) *project.Store {
	t.Helper()
	s, err := project.Open(filepath.Join(t.TempDir(), "db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateGetList(t *testing.T) {
	s := openStore(t)
	a, b := t.TempDir(), t.TempDir()
	p, err := s.Create(t.Context(), "demo", []string{a, b})
	require.NoError(t, err)
	assert.Regexp(t, `^prj_`, p.ID)
	got, err := s.Get(t.Context(), p.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{a, b}, got.Repos)
	_, err = s.Create(t.Context(), "second", nil)
	require.NoError(t, err)
	all, err := s.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"demo", "second"}, []string{all[0].Name, all[1].Name})
}

func TestCreateRejectsInvalidRepo(t *testing.T) {
	s := openStore(t)
	for _, repo := range []string{"relative/path", "/definitely/missing/dir"} {
		_, err := s.Create(t.Context(), "x", []string{repo})
		assert.ErrorIs(t, err, project.ErrInvalidRepo, repo)
	}
}

func TestCreateRejectsEmptyName(t *testing.T) {
	s := openStore(t)
	_, err := s.Create(t.Context(), "  ", nil)
	assert.ErrorIs(t, err, project.ErrEmptyName)
}

func TestAddRepo(t *testing.T) {
	s := openStore(t)
	a, b := t.TempDir(), t.TempDir()
	p, _ := s.Create(t.Context(), "demo", []string{a})
	p, err := s.AddRepo(t.Context(), p.ID, b)
	require.NoError(t, err)
	assert.Equal(t, []string{a, b}, p.Repos)
	_, err = s.AddRepo(t.Context(), p.ID, a)
	assert.ErrorIs(t, err, project.ErrDuplicateRepo)
}

func TestDelete(t *testing.T) {
	s := openStore(t)
	p, _ := s.Create(t.Context(), "demo", nil)
	require.NoError(t, s.Delete(t.Context(), p.ID))
	_, err := s.Get(t.Context(), p.ID)
	assert.ErrorIs(t, err, project.ErrNotFound)
}
