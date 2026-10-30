package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

type match struct {
	Path      string `json:"path"`
	AbsPath   string `json:"absPath"`
	Positions []int  `json:"positions"`
	Line      int    `json:"line"`
}

type result struct {
	Matches   []match `json:"matches"`
	Truncated bool    `json:"truncated"`
	Capped    bool    `json:"capped"`
	Indexing  bool    `json:"indexing"`
}

func TestIndexRespectsIgnores(t *testing.T) {
	repo := tempDir(t)
	write(t, repo, ".gitignore", "node_modules/\n*.log\n")
	write(t, repo, "tracked.go", "")
	write(t, repo, "dir/untracked.go", "")
	write(t, repo, "debug.log", "")
	write(t, repo, "node_modules/pkg/index.js", "")
	write(t, repo, "sub/nested.go", "")
	git(t, repo, "init", "-q")
	git(t, repo, "add", ".gitignore", "tracked.go")
	git(t, filepath.Join(repo, "sub"), "init", "-q")
	h, id := start(t, repo)

	got := searchFiles(t, h, id, map[string]any{"query": "", "limit": 200})

	assert.ElementsMatch(t, []string{".gitignore", "tracked.go", "dir/untracked.go", "sub/nested.go"}, paths(got))
	assert.False(t, got.Truncated)
	assert.False(t, got.Capped)
}

func TestIndexWalksBrokenNestedRepo(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "a.go", "")
	write(t, dir, "broken/.git", "gitdir: /nonexistent\n")
	write(t, dir, "broken/b.go", "")
	h, id := start(t, dir)

	got := searchFiles(t, h, id, map[string]any{"query": ""})

	assert.ElementsMatch(t, []string{"a.go", "broken/b.go"}, paths(got))
}

func TestSearchFilesLabelsSameBasename(t *testing.T) {
	first, second := filepath.Join(tempDir(t), "app"), filepath.Join(tempDir(t), "app")
	write(t, first, "x.go", "")
	write(t, second, "y.go", "")
	h, id := start(t, first, second)

	got := searchFiles(t, h, id, map[string]any{"query": ""})

	assert.ElementsMatch(t, []match{
		{Path: "app/x.go", AbsPath: filepath.Join(first, "x.go"), Positions: []int{}},
		{Path: "app-2/y.go", AbsPath: filepath.Join(second, "y.go"), Positions: []int{}},
	}, got.Matches)
}

func TestSearchFilesLineSuffix(t *testing.T) {
	dir := filepath.Join(tempDir(t), "ws")
	write(t, dir, "main.go", "")
	write(t, dir, "other.txt", "")
	h, id := start(t, dir)
	real := filepath.Join(tempDir(t), "real")
	require.NoError(t, os.Rename(dir, real))
	require.NoError(t, os.Symlink(real, dir))

	got := searchFiles(t, h, id, map[string]any{"query": "main.go:12"})

	require.Len(t, got.Matches, 1)
	assert.Equal(t, match{Path: "main.go", AbsPath: filepath.Join(dir, "main.go"), Positions: []int{0, 1, 2, 3, 4, 5, 6}, Line: 12}, got.Matches[0])
}

func TestHomeWorkspaceRefused(t *testing.T) {
	h, _ := start(t, tempDir(t))
	home, err := h.Svc.HomeWorkspace(t.Context())
	require.NoError(t, err)

	err = h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "search.files", "projectId": home.ID, "args": map[string]any{"query": "a"}}, nil)

	assert.ErrorContains(t, err, "search unavailable in the home workspace")
}

func start(t *testing.T, folders ...string) (*testutil.PluginHarness, string) {
	t.Helper()
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/search", "plugins/search/plugin.json"))
	p, err := h.Svc.CreateProject(t.Context(), "demo", folders)
	require.NoError(t, err)
	testutil.Eventually(t, func() bool {
		_, ok := h.Sup.Conn("search")
		return ok
	}, 5*time.Second)
	return h, p.ID
}

func searchFiles(t *testing.T, h *testutil.PluginHarness, projectID string, args map[string]any) result {
	t.Helper()
	var out result
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "search.files", "projectId": projectID, "args": args}, &out))
	require.False(t, out.Indexing)
	return out
}

func paths(r result) []string {
	out := make([]string, 0, len(r.Matches))
	for _, m := range r.Matches {
		out = append(out, m.Path)
	}
	return out
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestSearchFollowsWorktree(t *testing.T) {
	repo := tempDir(t)
	write(t, repo, "a.txt", "")
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "add", ".")
	git(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")
	wt := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", wt, "-b", "feat")
	h, id := start(t, repo)
	absPaths := func() []string {
		var got []string
		for _, m := range searchFiles(t, h, id, map[string]any{"query": "a.txt"}).Matches {
			got = append(got, m.AbsPath)
		}
		return got
	}
	require.Contains(t, absPaths(), filepath.Join(repo, "a.txt"))

	_, err := h.Svc.SetWorktree(t.Context(), id, repo, wt)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(wt, "a.txt")}, absPaths())
}
