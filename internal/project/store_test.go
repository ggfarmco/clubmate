package project_test

import (
	"database/sql"
	"os"
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
	assert.Equal(t, []string{"second", "demo"}, []string{all[0].Name, all[1].Name})
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

func TestOpenPathWithSpecialChars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a #b?c%41 d")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	dbPath := filepath.Join(dir, "clubmate.db")
	s, err := project.Open(dbPath)
	require.NoError(t, err)
	_, err = s.Create(t.Context(), "demo", nil)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	assert.FileExists(t, dbPath)
}

func TestOpenRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	s, err := project.Open("rel.db")
	require.NoError(t, err)
	_, err = s.Create(t.Context(), "demo", nil)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	assert.FileExists(t, "rel.db")
}

func TestMigrationKeepsProjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TABLE projects(id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at INTEGER NOT NULL);
		INSERT INTO projects(id, name, created_at) VALUES('prj_old', 'old', 1)`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	s, err := project.Open(path)
	require.NoError(t, err)
	all, err := s.List(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "prj_old", all[0].ID)
	assert.False(t, all[0].Home)
	assert.True(t, all[0].LastOpenedAt.IsZero())
	require.NoError(t, s.Close())
	s, err = project.Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestRename(t *testing.T) {
	s := openStore(t)
	p, _ := s.Create(t.Context(), "demo", nil)
	require.NoError(t, s.Rename(t.Context(), p.ID, "renamed"))
	got, err := s.Get(t.Context(), p.ID)
	require.NoError(t, err)
	assert.Equal(t, "renamed", got.Name)
	assert.ErrorIs(t, s.Rename(t.Context(), p.ID, " "), project.ErrEmptyName)
}
