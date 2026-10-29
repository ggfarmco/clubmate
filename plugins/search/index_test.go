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

	got := searchFiles(t, repo, map[string]any{"query": "", "limit": 200})

	paths := make([]string, 0, len(got.Matches))
	for _, m := range got.Matches {
		paths = append(paths, m.Path)
	}
	assert.ElementsMatch(t, []string{".gitignore", "tracked.go", "dir/untracked.go", "sub/nested.go"}, paths)
	assert.False(t, got.Truncated)
}

func TestSearchFilesLineSuffix(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "main.go", "")
	write(t, dir, "other.txt", "")

	got := searchFiles(t, dir, map[string]any{"query": "main.go:12"})

	require.Len(t, got.Matches, 1)
	assert.Equal(t, match{Path: "main.go", AbsPath: filepath.Join(dir, "main.go"), Positions: []int{0, 1, 2, 3, 4, 5, 6}, Line: 12}, got.Matches[0])
}

func searchFiles(t *testing.T, folder string, args map[string]any) result {
	t.Helper()
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/search", "plugins/search/plugin.json"))
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{folder})
	require.NoError(t, err)
	testutil.Eventually(t, func() bool {
		_, ok := h.Sup.Conn("search")
		return ok
	}, 5*time.Second)
	var out result
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "search.files", "projectId": p.ID, "args": args}, &out))
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
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
}
