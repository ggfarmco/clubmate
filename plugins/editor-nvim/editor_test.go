package main_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"clubmate/internal/rpc"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	mainSlot      = "editor-nvim.main"
	statusSlot    = "editor-nvim.status"
	restartWait   = 15 * time.Second
	blockedBudget = 3 * time.Second
)

type editor struct {
	h         *testutil.PluginHarness
	projectID string
	repo      string
	sessionID string
	sock      string
}

func TestOpenFileAtLine(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, filepath.Join("-dir with space", "файл.go"))
	require.NoError(t, e.open(t, path, 3))
	v := dial(t, e.sock)
	assert.Equal(t, path, eval[string](t, v, "expand('%:p')"))
	assert.Equal(t, 3, eval[int](t, v, "line('.')"))
	testutil.Eventually(t, func() bool {
		events, err := e.h.Bus.Read(t.Context(), "editor.buffer", 0, 100)
		require.NoError(t, err)
		for _, ev := range events {
			var p struct {
				ProjectID string `json:"projectId"`
				Path      string `json:"path"`
			}
			require.NoError(t, json.Unmarshal(ev.Payload, &p))
			if p.ProjectID == e.projectID && p.Path == path {
				return true
			}
		}
		return false
	}, 5*time.Second)
}

func TestNvimInitsWithoutUI(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return eval[int](t, v, "v:vim_did_enter") == 1 }, 5*time.Second)
}

func TestBlockedNvimTimesOut(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, "a.go")
	require.NoError(t, e.open(t, path, 1))
	v := dial(t, e.sock)
	_, err := v.Input(`:echo "a\nb"<CR>`)
	require.NoError(t, err)
	testutil.Eventually(t, func() bool {
		m, err := v.Mode()
		require.NoError(t, err)
		return m.Blocking
	}, 5*time.Second)
	start := time.Now()
	require.ErrorContains(t, e.open(t, path, 2), "nvim rpc timed out")
	assert.Less(t, time.Since(start), blockedBudget)
	_, err = v.Input("<CR>")
	require.NoError(t, err)
	require.NoError(t, e.open(t, path, 2))
}

func TestPluginRestartReconnects(t *testing.T) {
	e := startEditor(t)
	first := e.file(t, "a.go")
	require.NoError(t, e.open(t, first, 1))
	pid := eval[int](t, dial(t, e.sock), "getpid()")
	plugins := e.h.Sup.PIDs()
	require.Len(t, plugins, 1)
	require.NoError(t, unix.Kill(plugins[0], unix.SIGKILL))
	second := e.file(t, "b.go")
	testutil.Eventually(t, func() bool {
		restarted := e.h.Sup.PIDs()
		return len(restarted) == 1 && restarted[0] != plugins[0] && e.open(t, second, 1) == nil
	}, restartWait)
	v := dial(t, e.sock)
	assert.Equal(t, second, eval[string](t, v, "expand('%:p')"))
	assert.Equal(t, pid, eval[int](t, v, "getpid()"))
	assert.Equal(t, []string{e.sessionID}, e.running(t))
}

func TestExitedShowsRestart(t *testing.T) {
	e := startEditor(t)
	require.NoError(t, e.h.Svc.CheckTransitions(t.Context()))
	v := dial(t, e.sock)
	_ = v.Command("qa!")
	testutil.Eventually(t, func() bool { return e.h.GetSession(t, e.sessionID).State != session.StateRunning }, 5*time.Second)
	require.NoError(t, e.h.Svc.CheckTransitions(t.Context()))
	e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool { return contains(n, "button") })
	e.h.WaitSlot(t, e.projectID, statusSlot, func(n sdui.Node) bool { return !contains(n, "badge") })
	e.h.Event(t, e.projectID, mainSlot, "restart", "restart", nil)
	main := e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	assert.Equal(t, []string{main.Props["sessionId"].(string)}, e.running(t))
}

func TestRestartRefusesUnsavedBuffers(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, "a.go")
	require.NoError(t, e.open(t, path, 1))
	require.NoError(t, dial(t, e.sock).Command("normal! ix"))
	restart := func(args any) error {
		return e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.restart", "projectId": e.projectID, "args": args}, nil)
	}
	require.ErrorContains(t, restart(nil), "unsaved buffers: a.go")
	assert.Equal(t, []string{e.sessionID}, e.running(t))
	require.NoError(t, restart(map[string]any{"force": true}))
	main := e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	assert.Equal(t, []string{main.Props["sessionId"].(string)}, e.running(t))
}

func TestEdgeFocusesSidebar(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	drain(e.h.Client(t))
	require.NoError(t, v.Command("lua require('clubmate').navigate('h')"))
	n, ok := nextFocus(e.h.Client(t), 5*time.Second)
	require.True(t, ok, "no ui.focus notification")
	var focus struct {
		Section string `json:"section"`
		Slot    string `json:"slot"`
	}
	require.NoError(t, json.Unmarshal(n.Params, &focus))
	assert.Equal(t, "editor-nvim", focus.Section)
	assert.Equal(t, "editor-nvim.sidebar", focus.Slot)
}

func TestEdgeAfterPluginRestart(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	plugins := e.h.Sup.PIDs()
	require.Len(t, plugins, 1)
	require.NoError(t, unix.Kill(plugins[0], unix.SIGKILL))
	c := e.h.Client(t)
	testutil.Eventually(t, func() bool {
		restarted := e.h.Sup.PIDs()
		if len(restarted) != 1 || restarted[0] == plugins[0] {
			return false
		}
		drain(c)
		if v.Command("lua require('clubmate').navigate('h')") != nil {
			return false
		}
		_, ok := nextFocus(c, 200*time.Millisecond)
		return ok
	}, restartWait)
}

func TestHookCreatesNoKeymaps(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	ref, err := nvim.NewChildProcess(nvim.ChildProcessArgs("--embed", "--headless"), nvim.ChildProcessLogf(func(string, ...any) {}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ref.Close() })
	for _, n := range []*nvim.Nvim{v, ref} {
		testutil.Eventually(t, func() bool { return eval[int](t, n, "v:vim_did_enter") == 1 }, 5*time.Second)
	}
	const lhs = "map(nvim_get_keymap('n'), {_, m -> m.lhs})"
	assert.Empty(t, eval[string](t, v, "maparg('<C-H>', 'n')"))
	assert.Equal(t, eval[string](t, ref, "maparg('<C-L>', 'n')"), eval[string](t, v, "maparg('<C-L>', 'n')"))
	assert.ElementsMatch(t, eval[[]string](t, ref, lhs), eval[[]string](t, v, lhs))
}

func TestDiffOpensSideBySide(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, "a.go")
	for _, args := range [][]string{{"init", "-q"}, {"add", "a.go"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "a"}} {
		out, err := exec.Command("git", append([]string{"-C", e.repo}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	require.NoError(t, os.WriteFile(path, []byte("changed\n"), 0o600))
	args := map[string]any{"path": path, "base": "index", "target": "worktree"}
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.diff", "projectId": e.projectID, "args": args}, nil))
	v := dial(t, e.sock)
	assert.Equal(t, 2, eval[int](t, v, "winnr('$')"))
	assert.Equal(t, []int{1, 1}, eval[[]int](t, v, "[getwinvar(1, '&diff'), getwinvar(2, '&diff')]"))
	assert.Equal(t, "clubmate://index/a.go", eval[string](t, v, "bufname(winbufnr(1))"))
	assert.Equal(t, path, eval[string](t, v, "fnamemodify(bufname(winbufnr(2)), ':p')"))
}

func TestRestartIgnoresScratchBuffers(t *testing.T) {
	e := startEditor(t)
	require.NoError(t, dial(t, e.sock).Command("setlocal buftype=nofile | normal! ix"))
	err := e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.restart", "projectId": e.projectID}, nil)
	require.NoError(t, err)
	e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
}

func TestMissingFirstRepoStartsInNext(t *testing.T) {
	e := startEditorIn(t, true)
	assert.Equal(t, e.repo, e.h.GetSession(t, e.sessionID).Cwd)
}

func startEditor(t *testing.T) *editor {
	t.Helper()
	return startEditorIn(t, false)
}

func startEditorIn(t *testing.T, missingFirst bool) *editor {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not in PATH")
	}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		t.Setenv(key, t.TempDir())
	}
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/editor-nvim", "plugins/editor-nvim/plugin.json"))
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repos := []string{repo}
	if missingFirst {
		repos = []string{t.TempDir(), repo}
	}
	p, err := h.Svc.CreateProject(t.Context(), "demo", repos)
	require.NoError(t, err)
	if missingFirst {
		require.NoError(t, os.RemoveAll(repos[0]))
	}
	e := &editor{h: h, projectID: p.ID, repo: repo}
	t.Cleanup(func() { e.killAll(t) })
	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &sub))
	main := h.WaitSlot(t, p.ID, mainSlot, func(n sdui.Node) bool { return n.Type == "surface" })
	h.WaitSlot(t, p.ID, statusSlot, func(n sdui.Node) bool { return contains(n, "badge") })
	e.sessionID = main.Props["sessionId"].(string)
	s := h.GetSession(t, e.sessionID)
	assert.Equal(t, "nvim", s.Title)
	assert.Equal(t, repo, s.Cwd)
	require.Len(t, s.Command, 3)
	e.sock = s.Command[2]
	return e
}

func (e *editor) file(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join(e.repo, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("line\n", 5)), 0o600))
	return path
}

func (e *editor) open(t *testing.T, path string, line int) error {
	t.Helper()
	args := map[string]any{"path": path, "line": line}
	return e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.open", "projectId": e.projectID, "args": args}, nil)
}

func (e *editor) running(t *testing.T) []string {
	t.Helper()
	sessions, err := e.h.Svc.ListSessions(t.Context(), session.ListFilter{ProjectID: e.projectID, Owner: "editor-nvim"})
	require.NoError(t, err)
	var ids []string
	for _, s := range sessions {
		if s.State == session.StateRunning {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

func (e *editor) killAll(t *testing.T) {
	ctx := context.Background()
	sessions, err := e.h.Svc.ListSessions(ctx, session.ListFilter{Owner: "editor-nvim"})
	require.NoError(t, err)
	for _, s := range sessions {
		if s.State == session.StateRunning {
			assert.NoError(t, e.h.Svc.KillSession(ctx, s.ID))
		}
	}
}

func dial(t *testing.T, sock string) *nvim.Nvim {
	t.Helper()
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(sock)
		return err == nil
	}, 5*time.Second)
	v, err := nvim.Dial(sock, nvim.DialLogf(func(string, ...any) {}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })
	return v
}

func eval[T any](t *testing.T, v *nvim.Nvim, expr string) T {
	t.Helper()
	var out T
	require.NoError(t, v.Eval(expr, &out))
	return out
}

func drain(c *rpc.Client) {
	for {
		select {
		case <-c.Notifications():
		default:
			return
		}
	}
}

func nextFocus(c *rpc.Client, wait time.Duration) (rpc.Notification, bool) {
	deadline := time.After(wait)
	for {
		select {
		case n := <-c.Notifications():
			if n.Method == "ui.focus" {
				return n, true
			}
		case <-deadline:
			return rpc.Notification{}, false
		}
	}
}

func contains(n sdui.Node, typ string) bool {
	if n.Type == typ {
		return true
	}
	for _, c := range n.Children {
		if contains(c, typ) {
			return true
		}
	}
	return false
}
