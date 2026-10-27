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

func startEditor(t *testing.T) *editor {
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
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
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
