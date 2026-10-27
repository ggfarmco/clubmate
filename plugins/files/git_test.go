package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/gitrepo"
)

func TestStatusMap(t *testing.T) {
	entries := []gitrepo.Entry{
		{Path: "/r/a.go", X: ' ', Y: 'M'},
		{Path: "/r/new", X: '?', Y: '?'},
		{Path: "/r/e.go", Orig: "/r/d.go", X: 'R', Y: ' '},
		{Path: "/r/x", X: 'A', Y: ' '},
		{Path: "/r/gone", X: ' ', Y: 'D'},
		{Path: "/r/c", X: 'U', Y: 'U'},
	}
	assert.Equal(t, map[string]string{
		"/r/a.go": "M",
		"/r/new":  "U",
		"/r/e.go": "R",
		"/r/x":    "A",
		"/r/gone": "D",
		"/r/c":    "U",
	}, statusMap(entries))
}

func TestCollectIgnoredSkipsSubmodule(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	git := func(dir string, args ...string) {
		out, err := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "protocol.file.allow=always", "-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	lib := filepath.Join(base, "lib")
	repo := filepath.Join(base, "repo")
	require.NoError(t, os.MkdirAll(lib, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "bin"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(lib, "f"), nil, 0o600))
	git(lib, "init", "-q")
	git(lib, "add", ".")
	git(lib, "commit", "-q", "-m", "i")
	git(repo, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("bin/\n"), 0o600))
	git(repo, "submodule", "add", "-q", lib, "sub")

	got, err := collectIgnored(t.Context(), repo, map[string]bool{repo: true, filepath.Join(repo, "sub"): true})
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{filepath.Join(repo, "bin"): true}, got)
}
