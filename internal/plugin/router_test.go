package plugin_test

import (
	"encoding/json"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/testutil"
)

type subscription struct {
	Subscription string `json:"subscription"`
}

type uiUpdated struct {
	Subscription string                `json:"subscription"`
	View         internalsdui.SlotView `json:"view"`
}

func TestUIRenderEventAndOwnership(t *testing.T) {
	h := newHarness(t)
	p, err := h.svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.start(t, testutil.EchoPluginDir(t))
	c := h.client(t)
	testutil.Eventually(t, func() bool {
		var views []internalsdui.SlotView
		_ = c.Call(t.Context(), "ui.get", map[string]string{"projectId": p.ID}, &views)
		return len(views) == 1 && views[0].Tree.Type == "text"
	}, 5*time.Second)
	var res map[string]any
	require.NoError(t, c.Call(t.Context(), "ui.event", map[string]any{"projectId": p.ID, "slot": "echo.main", "nodeId": "x", "action": "say", "value": "hi"}, &res))
	assert.Equal(t, "hi", res["said"])
	var echoed map[string]any
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "echo.say", "projectId": p.ID, "args": map[string]any{"n": 1}}, &echoed))
	assert.Equal(t, map[string]any{"n": 1.0}, echoed)
	err = h.pluginCall(t, "echo", "ui.render", map[string]any{"projectId": p.ID, "slot": "terminals.main", "tree": map[string]any{"type": "empty", "props": map[string]any{"text": "x"}}})
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
}

func TestClearOwnerOnPluginCrash(t *testing.T) {
	h := newHarness(t, plugin.WithBackoff(10*time.Millisecond, 50*time.Millisecond))
	p, err := h.svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.start(t, testutil.EchoPluginDir(t))
	c := h.client(t)
	testutil.Eventually(t, func() bool {
		var views []internalsdui.SlotView
		_ = c.Call(t.Context(), "ui.get", map[string]string{"projectId": p.ID}, &views)
		return len(views) == 1 && len(h.sup.PIDs()) == 1
	}, 5*time.Second)
	var sub subscription
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &sub))
	assert.Equal(t, "text", nextView(t, c, sub.Subscription).Tree.Type)
	require.NoError(t, syscall.Kill(h.sup.PIDs()[0], syscall.SIGKILL))
	cleared := nextView(t, c, sub.Subscription)
	assert.Equal(t, "echo.main", cleared.Slot)
	assert.Equal(t, "empty", cleared.Tree.Type)
	assert.Equal(t, "text", nextView(t, c, sub.Subscription).Tree.Type)
}

func TestRoutingToStoppedPluginIsUnavailable(t *testing.T) {
	t.Setenv("ECHO_CRASH", "1")
	h := newHarness(t, plugin.WithBackoff(time.Minute, time.Minute))
	p, err := h.svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.start(t, testutil.EchoPluginDir(t))
	c := h.client(t)
	testutil.Eventually(t, func() bool { return h.sup.Statuses()[0].State == "backoff" }, 5*time.Second)
	err = c.Call(t.Context(), "command.run", map[string]any{"id": "echo.say", "projectId": p.ID}, nil)
	assert.ErrorIs(t, err, rpc.ErrPluginUnavailable)
}

func TestSubscriptionIDsAreScopedByKind(t *testing.T) {
	h := newHarness(t)
	p, err := h.svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.start(t)
	c := h.client(t)
	var sub subscription
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &sub))
	assert.ErrorIs(t, c.Call(t.Context(), "bus.unsubscribe", sub, nil), rpc.ErrNotFound)
	assert.NoError(t, c.Call(t.Context(), "ui.unsubscribe", sub, nil))
}

func nextView(t *testing.T, c *rpc.Client, sid string) internalsdui.SlotView {
	t.Helper()
	select {
	case n := <-c.Notifications():
		require.Equal(t, "ui.updated", n.Method)
		var u uiUpdated
		require.NoError(t, json.Unmarshal(n.Params, &u))
		require.Equal(t, sid, u.Subscription)
		return u.View
	case <-time.After(5 * time.Second):
		t.Fatal("no ui.updated notification")
		return internalsdui.SlotView{}
	}
}
