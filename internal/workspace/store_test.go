package workspace_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/workspace"
)

func openStore(t *testing.T) *workspace.Store {
	t.Helper()
	s, err := workspace.Open(filepath.Join(t.TempDir(), "db"))
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
	assert.Equal(t, []string{a, b}, got.Paths())
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
		assert.ErrorIs(t, err, workspace.ErrInvalidFolder, repo)
	}
}

func TestCreateRejectsEmptyName(t *testing.T) {
	s := openStore(t)
	_, err := s.Create(t.Context(), "  ", nil)
	assert.ErrorIs(t, err, workspace.ErrEmptyName)
}

func TestAddRepo(t *testing.T) {
	s := openStore(t)
	a, b := t.TempDir(), t.TempDir()
	p, _ := s.Create(t.Context(), "demo", []string{a})
	p, err := s.AddRepo(t.Context(), p.ID, b, workspace.FolderMeta{})
	require.NoError(t, err)
	assert.Equal(t, []string{a, b}, p.Paths())
	_, err = s.AddRepo(t.Context(), p.ID, a, workspace.FolderMeta{})
	assert.ErrorIs(t, err, workspace.ErrDuplicateFolder)
}

func TestFolderRecords(t *testing.T) {
	s := openStore(t)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	p, err := s.Create(t.Context(), "demo", []string{a})
	require.NoError(t, err)
	before := time.Now()
	_, err = s.AddRepo(t.Context(), p.ID, b, workspace.FolderMeta{})
	require.NoError(t, err)
	_, err = s.AddRepo(t.Context(), p.ID, c, workspace.FolderMeta{AddedBy: "Fix login", AddedAfk: true})
	require.NoError(t, err)
	_, err = s.AddRepo(t.Context(), p.ID, t.TempDir(), workspace.FolderMeta{AddedAfk: true})
	require.NoError(t, err)
	got, err := s.Get(t.Context(), p.ID)
	require.NoError(t, err)

	created, user, agent, untitled := got.Folders[0], got.Folders[1], got.Folders[2], got.Folders[3]
	assert.True(t, p.Folders[0].Seen)
	assert.True(t, p.Folders[0].AddedAt.Equal(p.CreatedAt))
	assert.True(t, created.Seen)
	assert.True(t, created.AddedAt.Equal(p.CreatedAt))
	assert.Empty(t, user.AddedBy)
	assert.False(t, user.AddedAfk)
	assert.True(t, user.Seen)
	assert.False(t, user.AddedAt.Before(before))
	assert.Equal(t, "Fix login", agent.AddedBy)
	assert.True(t, agent.AddedAfk)
	assert.False(t, agent.Seen)
	assert.False(t, untitled.Seen)
}

func TestMarkFolderSeen(t *testing.T) {
	s := openStore(t)
	a, b := t.TempDir(), t.TempDir()
	p, err := s.Create(t.Context(), "demo", []string{a})
	require.NoError(t, err)
	_, err = s.AddRepo(t.Context(), p.ID, b, workspace.FolderMeta{AddedBy: "Fix login"})
	require.NoError(t, err)

	got, err := s.MarkFolderSeen(t.Context(), p.ID, b)
	require.NoError(t, err)
	assert.True(t, got.Folders[1].Seen)
	assert.Equal(t, "Fix login", got.Folders[1].AddedBy)
	_, err = s.MarkFolderSeen(t.Context(), p.ID, t.TempDir())
	assert.ErrorIs(t, err, workspace.ErrNotFolder)
}

func TestRemoveFolderKeepsFilesAndDropsSelection(t *testing.T) {
	s := openStore(t)
	a, b, b2 := t.TempDir(), t.TempDir(), t.TempDir()
	file := filepath.Join(b, "keep")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	p, err := s.Create(t.Context(), "demo", []string{a, b})
	require.NoError(t, err)
	_, err = s.SetWorktree(t.Context(), p.ID, b, b2)
	require.NoError(t, err)

	got, err := s.RemoveFolder(t.Context(), p.ID, b)
	require.NoError(t, err)
	assert.Equal(t, []string{a}, got.Paths())
	assert.FileExists(t, file)
	assert.DirExists(t, b2)

	got, err = s.AddRepo(t.Context(), p.ID, b, workspace.FolderMeta{})
	require.NoError(t, err)
	assert.Empty(t, got.Folders[1].Worktree)
}

func TestRemoveFolderRefuses(t *testing.T) {
	s := openStore(t)
	a, b := t.TempDir(), t.TempDir()
	p, err := s.Create(t.Context(), "demo", []string{a})
	require.NoError(t, err)

	_, err = s.RemoveFolder(t.Context(), p.ID, a)
	assert.ErrorIs(t, err, workspace.ErrLastFolder)
	_, err = s.RemoveFolder(t.Context(), p.ID, b)
	assert.ErrorIs(t, err, workspace.ErrNotFolder)
	_, err = s.RemoveFolder(t.Context(), "prj_missing", a)
	assert.ErrorIs(t, err, workspace.ErrNotFound)
	got, err := s.Get(t.Context(), p.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{a}, got.Paths())
}

func TestDelete(t *testing.T) {
	s := openStore(t)
	p, _ := s.Create(t.Context(), "demo", nil)
	require.NoError(t, s.Delete(t.Context(), p.ID))
	_, err := s.Get(t.Context(), p.ID)
	assert.ErrorIs(t, err, workspace.ErrNotFound)
}

func TestOpenPathWithSpecialChars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a #b?c%41 d")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	dbPath := filepath.Join(dir, "clubmate.db")
	s, err := workspace.Open(dbPath)
	require.NoError(t, err)
	_, err = s.Create(t.Context(), "demo", nil)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	assert.FileExists(t, dbPath)
}

func TestOpenRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	s, err := workspace.Open("rel.db")
	require.NoError(t, err)
	_, err = s.Create(t.Context(), "demo", nil)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	assert.FileExists(t, "rel.db")
}

func TestMigrationKeepsWorkspaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TABLE projects(id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at INTEGER NOT NULL);
		INSERT INTO projects(id, name, created_at) VALUES('prj_old', 'old', 1)`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	s, err := workspace.Open(path)
	require.NoError(t, err)
	all, err := s.List(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "prj_old", all[0].ID)
	assert.False(t, all[0].Home)
	assert.Nil(t, all[0].LastOpenedAt)
	require.NoError(t, s.Close())
	s, err = workspace.Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestMigrationKeepsFolders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	dir, selected := t.TempDir(), t.TempDir()
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TABLE projects(id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at INTEGER NOT NULL, home INTEGER NOT NULL DEFAULT 0, last_opened_at INTEGER NOT NULL DEFAULT 0);
		CREATE TABLE repos(project_id TEXT REFERENCES projects(id) ON DELETE CASCADE, position INTEGER, path TEXT, PRIMARY KEY(project_id, path));
		CREATE TABLE worktrees(project_id TEXT, repo TEXT, path TEXT NOT NULL, PRIMARY KEY(project_id, repo), FOREIGN KEY(project_id, repo) REFERENCES repos(project_id, path) ON DELETE CASCADE);
		INSERT INTO projects(id, name, created_at) VALUES('prj_old', 'old', 7000)`)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO repos(project_id, position, path) VALUES('prj_old', 0, ?)`, dir)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO worktrees(project_id, repo, path) VALUES('prj_old', ?, ?)`, dir, selected)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	for range 2 {
		s, err := workspace.Open(path)
		require.NoError(t, err)
		got, err := s.Get(t.Context(), "prj_old")
		require.NoError(t, err)
		require.NoError(t, s.Close())
		require.Len(t, got.Folders, 1)
		f := got.Folders[0]
		assert.Equal(t, dir, f.Path)
		assert.Equal(t, selected, f.Worktree)
		assert.True(t, f.Seen)
		assert.Empty(t, f.AddedBy)
		assert.False(t, f.AddedAfk)
		assert.True(t, f.AddedAt.Equal(time.Unix(0, 7000)))
	}
}

func TestRename(t *testing.T) {
	s := openStore(t)
	p, _ := s.Create(t.Context(), "demo", nil)
	require.NoError(t, s.Rename(t.Context(), p.ID, "renamed"))
	got, err := s.Get(t.Context(), p.ID)
	require.NoError(t, err)
	assert.Equal(t, "renamed", got.Name)
	assert.ErrorIs(t, s.Rename(t.Context(), p.ID, " "), workspace.ErrEmptyName)
}

func TestMissingSelectionFallsBack(t *testing.T) {
	s := openStore(t)
	a, b, b2 := t.TempDir(), t.TempDir(), t.TempDir()
	p, err := s.Create(t.Context(), "demo", []string{a, b})
	require.NoError(t, err)
	got, err := s.SetWorktree(t.Context(), p.ID, b, b2)
	require.NoError(t, err)
	assert.Equal(t, b2, got.Folders[1].Worktree)
	assert.False(t, got.Folders[1].Missing)
	require.NoError(t, os.RemoveAll(b2))
	got, err = s.Get(t.Context(), p.ID)
	require.NoError(t, err)
	assert.Equal(t, b2, got.Folders[1].Worktree)
	assert.True(t, got.Folders[1].Missing)
	assert.Equal(t, []string{a, b}, got.Roots())
}
