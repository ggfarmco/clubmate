package plugin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
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
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDir(t))
	c := h.Client(t)
	testutil.Eventually(t, func() bool {
		var views []internalsdui.SlotView
		_ = c.Call(t.Context(), "ui.get", map[string]string{"projectId": p.ID}, &views)
		return len(views) == 1 && views[0].Tree.Type == "text"
	}, 5*time.Second)
	var res map[string]any
	require.NoError(t, c.Call(t.Context(), "ui.event", map[string]any{"projectId": p.ID, "slot": "echo.main", "nodeId": "x", "action": "say", "value": "hi"}, &res))
	assert.Equal(t, "hi", res["said"])
	var echoed map[string]any
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "echo.say", "projectId": p.ID, "args": map[string]any{"text": "hi", "n": 1}}, &echoed))
	assert.Equal(t, map[string]any{"plugin": "echo", "args": map[string]any{"text": "hi", "n": 1.0}}, echoed)
	err = h.PluginCall(t, "echo", "ui.render", map[string]any{"projectId": p.ID, "slot": "terminals.main", "tree": map[string]any{"type": "empty", "props": map[string]any{"text": "x"}}})
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
}

func TestClearOwnerOnPluginCrash(t *testing.T) {
	h := testutil.NewHarness(t, plugin.WithBackoff(10*time.Millisecond, 50*time.Millisecond))
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDir(t))
	c := h.Client(t)
	testutil.Eventually(t, func() bool {
		var views []internalsdui.SlotView
		_ = c.Call(t.Context(), "ui.get", map[string]string{"projectId": p.ID}, &views)
		return len(views) == 1 && len(h.Sup.PIDs()) == 1
	}, 5*time.Second)
	var sub subscription
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &sub))
	assert.Equal(t, "text", nextView(t, c, sub.Subscription).Tree.Type)
	require.NoError(t, syscall.Kill(h.Sup.PIDs()[0], syscall.SIGKILL))
	cleared := nextView(t, c, sub.Subscription)
	assert.Equal(t, "echo.main", cleared.Slot)
	assert.Equal(t, "empty", cleared.Tree.Type)
	assert.Equal(t, "text", nextView(t, c, sub.Subscription).Tree.Type)
}

func TestPluginErrorsAreRewrapped(t *testing.T) {
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDir(t))
	c := h.Client(t)
	testutil.Eventually(t, func() bool { return len(h.Sup.PIDs()) == 1 && h.Sup.Statuses()[0].State == plugin.StateRunning }, 5*time.Second)
	err = c.Call(t.Context(), "ui.event", map[string]any{"projectId": p.ID, "slot": "echo.main", "nodeId": "x", "action": "nope"}, nil)
	require.ErrorIs(t, err, rpc.ErrPluginError)
	assert.EqualError(t, err, `plugin echo ui.event: unknown action "nope"`)
}

func TestCallTimesOutWhenPluginStopsReading(t *testing.T) {
	coreSide, pluginSide := net.Pipe()
	conn := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), noopHandler{})
	t.Cleanup(func() { pluginSide.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := plugin.CallConn(ctx, conn, "wedged", "ui.event", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, rpc.ErrPluginUnavailable)
	conn.Close()
	testutil.Eventually(t, func() bool { return !strings.Contains(goroutines(), "plugin.callConn") }, 2*time.Second)
}

func TestRoutingToStoppedPluginIsUnavailable(t *testing.T) {
	t.Setenv("ECHO_CRASH", "1")
	h := testutil.NewHarness(t, plugin.WithBackoff(time.Minute, time.Minute))
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDir(t))
	c := h.Client(t)
	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].State == "backoff" }, 5*time.Second)
	err = c.Call(t.Context(), "command.run", map[string]any{"id": "echo.say", "projectId": p.ID, "args": map[string]any{"text": "hi"}}, nil)
	assert.ErrorIs(t, err, rpc.ErrPluginUnavailable)
}

func TestSubscriptionIDsAreScopedByKind(t *testing.T) {
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t)
	c := h.Client(t)
	var sub subscription
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &sub))
	assert.ErrorIs(t, c.Call(t.Context(), "bus.unsubscribe", sub, nil), rpc.ErrNotFound)
	assert.NoError(t, c.Call(t.Context(), "ui.unsubscribe", sub, nil))
}

func TestCapabilityRouting(t *testing.T) {
	t.Setenv("ECHO_CRASH_ID", "delta")
	h := testutil.NewHarness(t, plugin.WithBackoff(time.Minute, time.Minute))
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	sayer := func(id string, provides ...string) string {
		caps, err := json.Marshal(provides)
		require.NoError(t, err)
		return fmt.Sprintf(`{"id":%q,"name":%q,"version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"%s.say","title":"Say","provides":%s}]}}`, id, id, id, caps)
	}
	h.Start(t,
		testutil.EchoPluginDirAs(t, sayer("delta", "echo.down-cap")),
		testutil.EchoPluginDirAs(t, sayer("beta", "echo.say-cap")),
		testutil.EchoPluginDirAs(t, sayer("alpha", "echo.say-cap", "echo.down-cap", "beta.say")),
	)
	testutil.Eventually(t, func() bool {
		states := map[string]string{}
		for _, st := range h.Sup.Statuses() {
			states[st.ID] = st.State
		}
		return states["delta"] == plugin.StateBackoff && states["beta"] == plugin.StateRunning && states["alpha"] == plugin.StateRunning
	}, 5*time.Second)
	c := h.Client(t)
	run := func(id string) string {
		var out struct {
			Plugin string `json:"plugin"`
		}
		require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": id, "projectId": p.ID}, &out))
		return out.Plugin
	}
	assert.Equal(t, "beta", run("echo.say-cap"))
	assert.Equal(t, "alpha", run("alpha.say"))
	assert.Equal(t, "alpha", run("echo.down-cap"))
	assert.Equal(t, "beta", run("beta.say"))
	var caps struct {
		Capabilities map[string]string `json:"capabilities"`
	}
	require.NoError(t, c.Call(t.Context(), "command.capabilities", struct{}{}, &caps))
	assert.Equal(t, map[string]string{"echo.say-cap": "beta.say", "echo.down-cap": "alpha.say", "beta.say": "beta.say"}, caps.Capabilities)
}

type noopHandler struct{}

func (noopHandler) Handle(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) {}

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

func goroutines() string {
	buf := make([]byte, 1<<20)
	return string(buf[:runtime.Stack(buf, true)])
}
