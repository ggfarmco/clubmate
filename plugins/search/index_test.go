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

func TestRefusals(t *testing.T) {
	base := tempDir(t)
	dir, outside := filepath.Join(base, "ws"), filepath.Join(base, "out")
	write(t, dir, "a.txt", "inside\n")
	write(t, outside, "secret.txt", "secret\n")
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt")))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "linkdir")))
	h, id := start(t, dir)
	home, err := h.Svc.HomeWorkspace(t.Context())
	require.NoError(t, err)
	run := func(projectID, cmd string, args map[string]any) error {
		return h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": cmd, "projectId": projectID, "args": args}, nil)
	}

	require.NoError(t, run(id, "search.preview", map[string]any{"path": filepath.Join(dir, "a.txt"), "line": 1}))
	for _, path := range []string{
		filepath.Join(dir, "link.txt"),
		filepath.Join(dir, "linkdir", "secret.txt"),
		filepath.Join(dir, "..", "out", "secret.txt"),
	} {
		assert.Error(t, run(id, "search.preview", map[string]any{"path": path, "line": 1}), path)
	}
	for cmd, args := range map[string]map[string]any{
		"search.files":   {"query": "a"},
		"search.start":   {"query": "a"},
		"search.preview": {"path": filepath.Join(dir, "a.txt"), "line": 1},
	} {
		assert.ErrorContains(t, run(home.ID, cmd, args), "search unavailable in the home workspace", cmd)
	}
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
