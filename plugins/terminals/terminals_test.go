package main_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func TestTerminalsLifecycle(t *testing.T) {
	h := newHarness(t)
	p := h.CreateProject(t, "demo")
	sidebar := h.WaitSlot(t, p.ID, "terminals.sidebar", func(n sdui.Node) bool { return contains(n, "empty") })
	assert.Equal(t, "stack", sidebar.Type)
	h.Event(t, p.ID, "terminals.sidebar", "new", "new", nil)
	main := h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "surface" })
	sid := main.Props["sessionId"].(string)
	s := h.GetSession(t, sid)
	assert.Equal(t, "terminals", s.Owner)
	assert.Equal(t, "Terminal 1", s.Title)
	h.Event(t, p.ID, "terminals.main", "close", "close", nil)
	h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "empty" })
	h.WaitSessionGone(t, sid)
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
