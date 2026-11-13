package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/editorroot"
)

func TestOwnedSocketAcceptsDistroArgv(t *testing.T) {
	t.Setenv("CLUBMATE_PLUGIN_RUNTIME_DIR", "/rt")
	sock, err := socketPath(pluginsdk.RuntimeDir(), "p1", "/repo")
	require.NoError(t, err)
	for _, cmd := range [][]string{{"nvim", "--listen", sock}, {"nvim", "-u", "/res/nvim-distro/init.lua", "--listen", sock}} {
		got, ok := ownedSocket(pluginsdk.Session{State: contract.SessionStateRunning, Cwd: "/repo", Command: cmd}, "p1", "/repo")
		assert.True(t, ok, cmd)
		assert.Equal(t, sock, got)
	}
	_, ok := ownedSocket(pluginsdk.Session{State: contract.SessionStateRunning, Cwd: "/repo", Command: []string{"nvim", "-u", sock}}, "p1", "/repo")
	assert.False(t, ok)
}

func TestStopRootOutlastsAStartingNeovim(t *testing.T) {
	type command struct {
		ID      string `json:"id"`
		Timeout int    `json:"timeout"`
	}
	var m struct {
		Contributes struct {
			Commands []command `json:"commands"`
		} `json:"contributes"`
	}
	raw, err := os.ReadFile("plugin.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &m))
	connect := socketWait + startupWait + rpcTimeout
	assert.GreaterOrEqual(t, rootBudget, 2*connect+2*rpcTimeout)
	for _, id := range []string{"editor-nvim.stop-root", "editor-nvim.release-root"} {
		i := slices.IndexFunc(m.Contributes.Commands, func(c command) bool { return c.ID == id })
		require.GreaterOrEqual(t, i, 0, id)
		assert.GreaterOrEqual(t, time.Duration(m.Contributes.Commands[i].Timeout)*time.Second, rootBudget+5*time.Second, id)
	}
}

func TestStopRootGivesUpWhileTheEditorIsBusy(t *testing.T) {
	a := &app{editors: map[editorKey]*editor{}, roots: editorroot.NewRoots(), stopping: map[editorKey]bool{}}
	e := a.editorAt("ws", "/repo/wt")
	e.life.Lock()
	defer e.life.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	err := a.stopEditor(ctx, nil, e, []string{"s1"}, true)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorIs(t, err, errEditorBusy)
	assert.False(t, a.isStopping("ws", "/repo/wt"))
	assert.Same(t, e, a.lookup("ws", "/repo/wt"))
}

func TestDialWaitsUntilSocketListens(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "cmdial")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "n.sock")
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = unix.Close(fd) })
	require.NoError(t, unix.Bind(fd, &unix.SockaddrUnix{Name: sock}))
	dialed := make(chan error, 1)
	go func() {
		v, err := dial(t.Context(), sock)
		if err == nil {
			_ = v.Close()
		}
		dialed <- err
	}()
	select {
	case err := <-dialed:
		require.Failf(t, "dial returned before listen", "%v", err)
	case <-time.After(4 * socketPoll):
	}
	require.NoError(t, unix.Listen(fd, 1))
	select {
	case err := <-dialed:
		require.NoError(t, err)
	case <-time.After(socketWait):
		require.Fail(t, "dial did not finish after listen")
	}
}

func TestConnectWaitsForStartupBeforeAUIAttaches(t *testing.T) {
	bin, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("nvim not in PATH")
	}
	dir, err := os.MkdirTemp("/tmp", "cmembed")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	initLua := filepath.Join(dir, "init.lua")
	require.NoError(t, os.WriteFile(initLua, []byte("vim.g.clubmate_test_init = 1\n"), 0o600))
	sock := filepath.Join(dir, "n.sock")
	cmd := exec.Command(bin, "--embed", "-i", "NONE", "-n", "-u", initLua, "--listen", sock)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })

	connected := make(chan *client, 1)
	go func() {
		c, err := connect(t.Context(), sock, func(nvimEvent) {})
		if err == nil {
			connected <- c
		}
		close(connected)
	}()
	select {
	case <-connected:
		require.Fail(t, "connect returned before nvim sourced its init")
	case <-time.After(10 * socketPoll):
	}

	ui, err := dial(t.Context(), sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ui.Close() })
	go func() { _ = ui.Serve() }()
	require.NoError(t, ui.AttachUI(80, 24, map[string]any{"ext_linegrid": true}))
	var c *client
	select {
	case c = <-connected:
	case <-time.After(socketWait):
	}
	require.NotNil(t, c, "connect did not return after a UI attached")
	t.Cleanup(c.close)
	var set int
	require.NoError(t, c.v.Var("clubmate_test_init", &set))
	assert.Equal(t, 1, set)
}

func TestReviewClientRoundTrip(t *testing.T) {
	c, events := distroClient(t)
	repo := reviewRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nvar x = 2\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a_test.go"), []byte("package a\n"), 0o600))
	tabs, err := c.reviewList()
	require.NoError(t, err)
	assert.Empty(t, tabs)
	host := reviewTabHost{ID: "rv_1", Worktree: repo, Base: "main"}
	tab, err := c.reviewOpen(reviewOpenArgs{Cwd: repo, Base: "HEAD", Left: "Before turn 3", Host: host})
	require.NoError(t, err)
	tabs, err = c.reviewList()
	require.NoError(t, err)
	assert.Equal(t, []reviewTab{{Tab: tab, Layout: layoutSplit, Host: host}}, tabs)
	info, err := c.reviewInfo(tab)
	require.NoError(t, err)
	assert.Equal(t, reviewLabels{Left: "Before turn 3", Right: "Working tree"}, info.Labels)
	assert.True(t, info.Editable)
	var entries []reviewEntry
	testutil.Eventually(t, func() bool {
		entries, err = c.reviewEntries(tab)
		require.NoError(t, err)
		return len(entries) == 1
	}, 10*time.Second)
	assert.Equal(t, "a.go", entries[0].Path)
	assert.Equal(t, "modified", entries[0].Kind)
	assert.False(t, entries[0].Reviewed)
	require.NoError(t, reviewDo(c, "set_reviewed", "a.go", true, tab))
	entries, err = c.reviewEntries(tab)
	require.NoError(t, err)
	assert.True(t, entries[0].Reviewed)
	testutil.Eventually(t, func() bool { return events.has(nvimEvent{Event: "review", Kind: "reviewed", Path: "a.go", Tab: tab}) }, 5*time.Second)
	filters, err := c.reviewFilters(tab)
	require.NoError(t, err)
	assert.Equal(t, []any{true, false, 1}, []any{filters.Code.Shown, filters.Tests.Shown, filters.Tests.Count})
	require.NoError(t, reviewDo(c, "set_filter", "tests", true, tab))
	filters, err = c.reviewFilters(tab)
	require.NoError(t, err)
	assert.True(t, filters.Tests.Shown)
	require.NoError(t, c.reviewMeta(tab, reviewMeta{Subtitle: "Claude · Fix it", Recipient: "Fix it", Sent: 2}))
	entries, err = c.reviewEntries(tab)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
	newer, err := c.reviewOpen(reviewOpenArgs{Cwd: repo, Base: "HEAD"})
	require.NoError(t, err)
	require.NotEqual(t, tab, newer)
	require.NoError(t, c.v.ExecLua("vim.cmd.tabclose(vim.api.nvim_tabpage_get_number(...))", nil, tab))
	testutil.Eventually(t, func() bool { return events.has(nvimEvent{Event: "review", Kind: "closed", Tab: tab}) }, 5*time.Second)
	assert.False(t, events.has(nvimEvent{Event: "review", Kind: "closed", Tab: newer}))
}

type eventLog struct {
	mu     sync.Mutex
	events []nvimEvent
}

func (l *eventLog) add(ev nvimEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, ev)
}

func (l *eventLog) has(want nvimEvent) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.ContainsFunc(l.events, func(ev nvimEvent) bool {
		return ev.Event == want.Event && ev.Kind == want.Kind && ev.Path == want.Path && ev.Tab == want.Tab
	})
}

func distroClient(t *testing.T) (*client, *eventLog) {
	t.Helper()
	res := os.Getenv("CLUBMATE_TEST_RESOURCES")
	if res == "" {
		t.Skip("CLUBMATE_TEST_RESOURCES is not set; run make nvim-test")
	}
	home, err := os.MkdirTemp("/tmp", "cmrev")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	sock := filepath.Join(home, "n.sock")
	env := []string{"HOME=" + home, "NVIM_APPNAME=clubmate-test", "PATH=" + strings.Join([]string{filepath.Join(res, "tools"), "/usr/bin", "/bin"}, ":"), "TMPDIR=" + os.TempDir(), "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1"}
	for k, rel := range map[string]string{"XDG_CONFIG_HOME": ".config", "XDG_DATA_HOME": ".local/share", "XDG_STATE_HOME": ".local/state", "XDG_CACHE_HOME": ".cache"} {
		dir := filepath.Join(home, rel)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		env = append(env, k+"="+dir)
	}
	cmd := exec.Command(filepath.Join(res, "nvim", "bin", "nvim"), "--headless", "-u", filepath.Join(res, "nvim-distro", "init.lua"), "--listen", sock)
	cmd.Env, cmd.Dir = env, home
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	events := &eventLog{}
	c, err := connect(t.Context(), sock, events.add)
	require.NoError(t, err)
	t.Cleanup(c.close)
	return c, events
}

func reviewRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	git("init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nvar x = 1\n"), 0o600))
	git("add", ".")
	git("commit", "-qm", "init")
	return repo
}
