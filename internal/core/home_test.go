package core_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/core"
	"clubmate/internal/testutil"
	"clubmate/internal/workspace"
)

func homeDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	dir := filepath.Join(root, "home")
	t.Setenv("CLUBMATE_HOME_WORKSPACE_DIR", dir)
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func TestEnsureHomeCreatesRepo(t *testing.T) {
	dir := homeDir(t)
	got, err := core.HomeDir()
	require.NoError(t, err)
	assert.Equal(t, dir, got)
	p, err := testutil.NewService(t).EnsureHome(t.Context())
	require.NoError(t, err)
	assert.True(t, p.Home)
	assert.Equal(t, "Home", p.Name)
	assert.Equal(t, []string{dir}, p.Paths())
	git(t, dir, "rev-parse", "HEAD")
	assert.Equal(t, "README.md", git(t, dir, "ls-files"))
	assert.Empty(t, git(t, dir, "status", "--porcelain"))
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "Your personal workspace in Clubmate")
}

func TestEnsureHomeTwice(t *testing.T) {
	dir := homeDir(t)
	svc := testutil.NewService(t)
	first, err := svc.EnsureHome(t.Context())
	require.NoError(t, err)
	head := git(t, dir, "rev-parse", "HEAD")
	again, err := svc.EnsureHome(t.Context())
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)
	assert.Equal(t, first.Paths(), again.Paths())
	_, err = testutil.NewService(t).EnsureHome(t.Context())
	require.NoError(t, err)
	assert.Equal(t, head, git(t, dir, "rev-parse", "HEAD"))
	assert.Equal(t, "1", git(t, dir, "rev-list", "--count", "HEAD"))
}

func TestEnsureHomeKeepsFiles(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"plain folder":        func(*testing.T, string) {},
		"repo with no commit": func(t *testing.T, dir string) { git(t, dir, "init", "-q") },
		"repo with a commit": func(t *testing.T, dir string) {
			git(t, dir, "init", "-q")
			git(t, dir, "-c", "user.name=u", "-c", "user.email=u@x", "commit", "-q", "--allow-empty", "-m", "mine")
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			dir := homeDir(t)
			require.NoError(t, os.MkdirAll(dir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("mine"), 0o600))
			prepare(t, dir)
			_, err := testutil.NewService(t).EnsureHome(t.Context())
			require.NoError(t, err)
			notes, err := os.ReadFile(filepath.Join(dir, "notes.md"))
			require.NoError(t, err)
			assert.Equal(t, "mine", string(notes))
			assert.Equal(t, "1", git(t, dir, "rev-list", "--count", "HEAD"))
			assert.Equal(t, "?? notes.md", git(t, dir, "status", "--porcelain"))
		})
	}
}

func TestEnsureHomeMigratesOld(t *testing.T) {
	for _, tc := range []struct{ old, want string }{{old: "~", want: "Home"}, {old: "Me", want: "Me"}} {
		t.Run(tc.old, func(t *testing.T) {
			dir := homeDir(t)
			dirs := testutil.ShortDirs(t)
			store, err := workspace.Open(dirs.DB())
			require.NoError(t, err)
			t.Cleanup(func() { store.Close() })
			old, _, err := store.Home(t.Context(), t.TempDir())
			require.NoError(t, err)
			require.NoError(t, store.Rename(t.Context(), old.ID, tc.old))
			svc, _ := testutil.NewServiceWithBusIn(t, dirs)
			p, err := svc.EnsureHome(t.Context())
			require.NoError(t, err)
			assert.Equal(t, old.ID, p.ID)
			assert.Equal(t, tc.want, p.Name)
			assert.Equal(t, []string{dir}, p.Paths())
			all, err := svc.ListWorkspaces(t.Context())
			require.NoError(t, err)
			assert.Len(t, all, 1)
		})
	}
}

func TestEnsureHomeWithoutGit(t *testing.T) {
	dir := homeDir(t)
	svc := testutil.NewService(t)
	t.Setenv("PATH", t.TempDir())
	p, err := svc.EnsureHome(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{dir}, p.Paths())
	assert.NoDirExists(t, filepath.Join(dir, ".git"))
}

func TestDeleteHomeRefused(t *testing.T) {
	homeDir(t)
	svc := testutil.NewService(t)
	home, err := svc.EnsureHome(t.Context())
	require.NoError(t, err)
	require.ErrorIs(t, svc.DeleteWorkspace(t.Context(), home.ID), core.ErrHomeDelete)
	_, err = svc.GetWorkspace(t.Context(), home.ID)
	require.NoError(t, err)
}
