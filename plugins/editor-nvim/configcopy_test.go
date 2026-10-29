package main_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"

	main "clubmate/plugins/editor-nvim"
)

type xdg struct {
	config string
	data   string
}

type copier struct {
	h         *testutil.PluginHarness
	projectID string
	xdg       xdg
}

type copyStatus struct {
	Copied int64  `json:"copied"`
	Total  int64  `json:"total"`
	Done   bool   `json:"done"`
	Error  string `json:"error"`
}

type configInfo struct {
	Mode              string `json:"mode"`
	App               string `json:"app"`
	ConfigDir         string `json:"configDir"`
	DataDir           string `json:"dataDir"`
	ConfigExists      bool   `json:"configExists"`
	DataExists        bool   `json:"dataExists"`
	SourceConfigBytes int64  `json:"sourceConfigBytes"`
	SourceDataBytes   int64  `json:"sourceDataBytes"`
}

func TestCopyKeepsSymlinks(t *testing.T) {
	var stale []string
	c := startCopier(t, func(x xdg) {
		for _, parent := range []string{x.config, x.data} {
			dir := filepath.Join(parent, "clubmate-dev.clubmate-tmp-stale")
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "x"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "x", "y"), nil, 0o600))
			require.NoError(t, os.Chmod(filepath.Join(dir, "x"), 0o500))
			stale = append(stale, dir)
		}
	})
	src := filepath.Join(c.xdg.config, "nvim")
	require.NoError(t, os.MkdirAll(filepath.Join(src, ".git"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".git", "x"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a.lua"), []byte("vim.g.a = 1"), 0o751))
	require.NoError(t, os.Symlink("/", filepath.Join(src, "link")))
	require.NoError(t, os.Symlink(".", filepath.Join(src, "loop")))
	require.NoError(t, os.Symlink(filepath.Join(src, "a.lua"), filepath.Join(src, "abs")))
	dest := filepath.Join(c.xdg.config, "clubmate-dev")
	info := c.info(t)
	assert.Equal(t, configInfo{Mode: "shared", App: "clubmate-dev", ConfigDir: dest, DataDir: filepath.Join(c.xdg.data, "clubmate-dev"), SourceConfigBytes: 11}, info)
	st := c.copyAndWait(t, true)
	assert.Equal(t, copyStatus{Copied: 11, Total: 11, Done: true}, st)
	entries, err := os.ReadDir(dest)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Equal(t, []string{"a.lua", "abs", "link", "loop"}, names)
	a, err := os.Stat(filepath.Join(dest, "a.lua"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o751), a.Mode().Perm())
	for name, want := range map[string]string{"link": "/", "loop": ".", "abs": filepath.Join(dest, "a.lua")} {
		got, err := os.Readlink(filepath.Join(dest, name))
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	assert.NoDirExists(t, filepath.Join(c.xdg.data, "clubmate-dev"))
	for _, dir := range stale {
		assert.NoDirExists(t, dir)
	}
	assert.True(t, c.info(t).ConfigExists)
}

func TestCopyCancelLeavesNothing(t *testing.T) {
	src, parent := t.TempDir(), t.TempDir()
	for i := range 8 {
		require.NoError(t, os.WriteFile(filepath.Join(src, strings.Repeat("f", i+1)), make([]byte, 1<<20), 0o600))
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := main.CopyAll(ctx, [][2]string{{src, filepath.Join(parent, "clubmate-dev")}}, func(copied, total int64) {
		if copied >= total/2 {
			cancel()
		}
	})
	require.ErrorIs(t, err, context.Canceled)
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestCopyRollsBackWhenRenameFails(t *testing.T) {
	config, data, parent := t.TempDir(), t.TempDir(), t.TempDir()
	for _, dir := range []string{config, data} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o600))
	}
	configDest, dataDest := filepath.Join(parent, "config"), filepath.Join(parent, "data")
	err := main.CopyAll(t.Context(), [][2]string{{config, configDest}, {data, dataDest}}, func(int64, int64) {
		_ = os.Mkdir(dataDest, 0o700)
	})
	require.ErrorContains(t, err, dataDest+" already exists")
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "data", entries[0].Name())
}

func TestCopyCommitsConfigLast(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	for _, dir := range []string{filepath.Join(home, "config", "nvim"), filepath.Join(home, "data", "nvim")} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	pairs, err := main.ConfigPairs(true)
	require.NoError(t, err)
	var order []string
	t.Cleanup(main.OnCommitted(func(dst string) { order = append(order, dst) }))
	require.NoError(t, main.CopyAll(t.Context(), pairs, func(int64, int64) {}))
	assert.Equal(t, []string{filepath.Join(home, "data", "clubmate-dev"), filepath.Join(home, "config", "clubmate-dev")}, order)
}

func TestCopyRefusesExisting(t *testing.T) {
	c := startCopier(t, nil)
	for _, dir := range []string{filepath.Join(c.xdg.config, "nvim"), filepath.Join(c.xdg.data, "nvim")} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	dest := filepath.Join(c.xdg.data, "clubmate-dev")
	require.NoError(t, os.MkdirAll(dest, 0o700))
	err := c.run(t, "editor-nvim.config-copy", map[string]any{"data": true}, nil)
	require.ErrorContains(t, err, dest+" already exists")
	assert.NoDirExists(t, filepath.Join(c.xdg.config, "clubmate-dev"))
}

func TestAppNameEnv(t *testing.T) {
	t.Setenv("NVIM_APPNAME", "inherited")
	e := startEditor(t)
	v := dial(t, e.sock)
	assert.Equal(t, []any{int64(0), filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "nvim")}, eval[[]any](t, v, "[exists('$NVIM_APPNAME'), stdpath('config')]"))
	cl := e.h.Client(t)
	require.NoError(t, cl.Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.config", "projectId": e.projectID, "args": map[string]any{"mode": "clubmate"}}, nil))
	require.NoError(t, cl.Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.restart", "projectId": e.projectID, "args": map[string]any{"force": true}}, nil))
	surface := e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	sess := e.h.GetSession(t, surface.Props["sessionId"].(string))
	v = dial(t, sess.Command[2])
	assert.Equal(t, []any{"clubmate-dev", filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "clubmate-dev")}, eval[[]any](t, v, "[$NVIM_APPNAME, stdpath('config')]"))
}

func startCopier(t *testing.T, prepare func(xdg)) *copier {
	t.Helper()
	h := testutil.NewHarness(t)
	dir := testutil.BuildPluginDir(t, "clubmate/plugins/editor-nvim", "plugins/editor-nvim/plugin.json")
	c := &copier{h: h, xdg: xdg{config: t.TempDir(), data: t.TempDir()}}
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", c.xdg.config)
	t.Setenv("XDG_DATA_HOME", c.xdg.data)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if prepare != nil {
		prepare(c.xdg)
	}
	h.Start(t, dir)
	c.projectID = h.CreateProject(t, "demo").ID
	testutil.Eventually(t, func() bool {
		_, ok := h.Sup.Conn("editor-nvim")
		return ok
	}, 5*time.Second)
	return c
}

func (c *copier) run(t *testing.T, id string, args, out any) error {
	t.Helper()
	return c.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": id, "projectId": c.projectID, "args": args}, out)
}

func (c *copier) info(t *testing.T) configInfo {
	t.Helper()
	var info configInfo
	require.NoError(t, c.run(t, "editor-nvim.config", map[string]any{}, &info))
	return info
}

func (c *copier) copyAndWait(t *testing.T, data bool) copyStatus {
	t.Helper()
	var started struct {
		ID string `json:"id"`
	}
	require.NoError(t, c.run(t, "editor-nvim.config-copy", map[string]any{"data": data}, &started))
	deadline := time.Now().Add(10 * time.Second)
	for {
		var st copyStatus
		require.NoError(t, c.run(t, "editor-nvim.config-copy-status", map[string]any{"id": started.ID}, &st))
		if st.Done || time.Now().After(deadline) {
			return st
		}
	}
}
