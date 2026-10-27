package main_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func TestTerminalsLifecycle(t *testing.T) {
	h := newHarness(t)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{repo, t.TempDir()})
	require.NoError(t, err)
	sidebar := h.WaitSlot(t, p.ID, "terminals.sidebar", func(n sdui.Node) bool { return contains(n, "empty") })
	assert.Equal(t, "stack", sidebar.Type)
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "terminals.new", "projectId": p.ID}, nil))
	main := h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "surface" })
	sid := main.Props["sessionId"].(string)
	s := h.GetSession(t, sid)
	assert.Equal(t, "terminals", s.Owner)
	assert.Equal(t, "Terminal 1", s.Title)
	assert.Equal(t, repo, s.Cwd)
	h.Event(t, p.ID, "terminals.main", "close", "close", nil)
	h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "empty" })
	h.WaitSessionGone(t, sid)
	other := p.Repos[1]
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "terminals.new", "projectId": p.ID, "args": map[string]string{"cwd": other}}, nil))
	main = h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "surface" })
	assert.Equal(t, other, h.GetSession(t, main.Props["sessionId"].(string)).Cwd)
}

func TestExitedTerminalCanBeRestarted(t *testing.T) {
	h := newHarness(t)
	p := h.CreateProject(t, "demo")
	require.NoError(t, h.Svc.CheckTransitions(t.Context()))
	h.WaitSlot(t, p.ID, "terminals.sidebar", func(n sdui.Node) bool { return contains(n, "empty") })
	h.Event(t, p.ID, "terminals.sidebar", "new", "new", nil)
	assert.Same(t, h.Client(t), h.Client(t))
	main := h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "surface" })
	old := main.Props["sessionId"].(string)
	require.NoError(t, h.Svc.KillSession(t.Context(), old))
	testutil.Eventually(t, func() bool { return h.GetSession(t, old).State != session.StateRunning }, 5*time.Second)
	require.NoError(t, h.Svc.CheckTransitions(t.Context()))
	h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return contains(n, "button") })
	h.Event(t, p.ID, "terminals.main", "restart", "restart", nil)
	main = h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "surface" })
	fresh := main.Props["sessionId"].(string)
	assert.NotEqual(t, old, fresh)
	assert.Equal(t, "Terminal 1", h.GetSession(t, fresh).Title)
	h.WaitSessionGone(t, old)
}

func newHarness(t *testing.T) *testutil.PluginHarness {
	t.Helper()
	return testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/terminals", "plugins/terminals/plugin.json"))
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
