package plugin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
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
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDir(t))
	c := h.Client(t)
	testutil.Eventually(t, func() bool {
		var views []internalsdui.SlotView
		_ = c.Call(t.Context(), "ui.get", map[string]string{"workspaceId": p.ID}, &views)
		return len(views) == 1 && treeType(t, views[0]) == "text"
	}, 5*time.Second)
	var res map[string]any
	require.NoError(t, c.Call(t.Context(), "ui.event", map[string]any{"workspaceId": p.ID, "slot": "echo.main", "nodeId": "x", "action": "say", "value": "hi"}, &res))
	assert.Equal(t, "hi", res["said"])
	var echoed map[string]any
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "echo.say", "workspaceId": p.ID, "args": map[string]any{"text": "hi", "n": 1}}, &echoed))
	assert.Equal(t, map[string]any{"plugin": "echo", "args": map[string]any{"text": "hi", "n": 1.0}}, echoed)
	err = h.PluginCall(t, "echo", "ui.render", map[string]any{"workspaceId": p.ID, "slot": "terminals.main", "tree": map[string]any{"type": "empty", "props": map[string]any{"text": "x"}}})
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
}

func TestClearOwnerOnPluginCrash(t *testing.T) {
	h := testutil.NewHarness(t, plugin.WithBackoff(10*time.Millisecond, 50*time.Millisecond))
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDir(t))
	c := h.Client(t)
	testutil.Eventually(t, func() bool {
		var views []internalsdui.SlotView
		_ = c.Call(t.Context(), "ui.get", map[string]string{"workspaceId": p.ID}, &views)
		return len(views) == 1 && len(h.Sup.PIDs()) == 1
	}, 5*time.Second)
	var sub subscription
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, &sub))
	assert.Equal(t, "text", treeType(t, nextView(t, c, sub.Subscription)))
	require.NoError(t, syscall.Kill(h.Sup.PIDs()[0], syscall.SIGKILL))
	cleared := nextView(t, c, sub.Subscription)
	assert.Equal(t, "echo.main", cleared.Slot)
	assert.Equal(t, "empty", treeType(t, cleared))
	assert.Equal(t, "text", treeType(t, nextView(t, c, sub.Subscription)))
}

func TestPluginErrorsAreRewrapped(t *testing.T) {
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDir(t))
	c := h.Client(t)
	testutil.Eventually(t, func() bool { return len(h.Sup.PIDs()) == 1 && h.Sup.Statuses()[0].State == plugin.StateRunning }, 5*time.Second)
	err = c.Call(t.Context(), "ui.event", map[string]any{"workspaceId": p.ID, "slot": "echo.main", "nodeId": "x", "action": "nope"}, nil)
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
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDir(t))
	c := h.Client(t)
	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].State == "backoff" }, 5*time.Second)
	err = c.Call(t.Context(), "command.run", map[string]any{"id": "echo.say", "workspaceId": p.ID, "args": map[string]any{"text": "hi"}}, nil)
	assert.ErrorIs(t, err, rpc.ErrPluginUnavailable)
	var cmds []rpc.Command
	require.NoError(t, c.Call(t.Context(), "command.list", struct{}{}, &cmds))
	for _, cmd := range cmds {
		assert.Equal(t, cmd.Plugin == "core", cmd.Running, cmd.ID)
	}
}

func TestSubscriptionIDsAreScopedByKind(t *testing.T) {
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t)
	c := h.Client(t)
	var sub subscription
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, &sub))
	assert.ErrorIs(t, c.Call(t.Context(), "bus.unsubscribe", sub, nil), rpc.ErrNotFound)
	assert.NoError(t, c.Call(t.Context(), "ui.unsubscribe", sub, nil))
}

func TestCapabilityRouting(t *testing.T) {
	t.Setenv("ECHO_CRASH_ID", "delta")
	h := testutil.NewHarness(t, plugin.WithBackoff(time.Minute, time.Minute))
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
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
		require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": id, "workspaceId": p.ID}, &out))
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

func TestCommandTimeoutOutlivesRouteTimeout(t *testing.T) {
	h := testutil.NewPluginHarness(t, testutil.EchoPluginDirAs(t, `{"id":"slow","name":"Slow","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"slow.wait","title":"Wait","timeout":8}]},"echoResults":{"slow.wait":{"ok":true}},"echoDelays":{"slow.wait":6000}}`))
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	testutil.Eventually(t, func() bool { _, ok := h.Sup.Conn("slow"); return ok }, 5*time.Second)

	var out map[string]bool
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "slow.wait", "workspaceId": p.ID}, &out))

	assert.Equal(t, map[string]bool{"ok": true}, out)
}

func TestCallWaitsForStartingPlugin(t *testing.T) {
	t.Setenv("ECHO_START_DELAY", "300ms")
	h := testutil.NewHarness(t)
	p := h.CreateWorkspace(t, "demo")
	h.Start(t, testutil.EchoPluginDir(t))
	c := h.Client(t)

	var out map[string]any
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "echo.say", "workspaceId": p.ID, "args": map[string]any{"text": "hi"}}, &out))

	assert.Equal(t, "echo", out["plugin"])
}

func TestCallToPluginDyingAtStartFailsAtOnce(t *testing.T) {
	t.Setenv("ECHO_START_DELAY", "300ms")
	t.Setenv("ECHO_CRASH", "1")
	h := testutil.NewHarness(t, plugin.WithBackoff(time.Minute, time.Minute))
	p := h.CreateWorkspace(t, "demo")
	h.Start(t, testutil.EchoPluginDir(t))
	c := h.Client(t)

	started := time.Now()
	err := c.Call(t.Context(), "command.run", map[string]any{"id": "echo.say", "workspaceId": p.ID, "args": map[string]any{"text": "hi"}}, nil)

	assert.ErrorIs(t, err, rpc.ErrPluginUnavailable)
	assert.Less(t, time.Since(started), 4*time.Second)
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

func treeType(t *testing.T, v internalsdui.SlotView) string {
	t.Helper()
	n, err := v.Node()
	require.NoError(t, err)
	return n.Type
}

func goroutines() string {
	buf := make([]byte, 1<<20)
	return string(buf[:runtime.Stack(buf, true)])
}

func TestResolveFollowsPluginState(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDir(t))
	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].State == plugin.StateRunning }, 5*time.Second)
	router := plugin.NewRouter(h.Sup)
	running := func() bool {
		c, ok := router.Resolve("echo.say")
		require.True(t, ok)
		return c.Running
	}
	require.True(t, running())

	h.Sup.Stop(t.Context())
	assert.False(t, running())
}

func TestDebugPprofReachesPlugin(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDir(t))
	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].State == plugin.StateRunning }, 5*time.Second)
	var res struct {
		Data []byte `json:"data"`
	}
	require.NoError(t, h.Client(t).Call(t.Context(), "debug.pprof", map[string]any{"plugin": "echo", "profile": "goroutine", "debug": 1}, &res))
	assert.Contains(t, string(res.Data), "clubmate/pkg/pluginsdk.")
	assert.NotContains(t, string(res.Data), "clubmate/internal/rpc.")
}

const stuckManifest = `{"id":"stuck","name":"Stuck","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"stuck.wait","title":"Wait","timeout":1},{"id":"stuck.ok","title":"Ok"}]},"echoResults":{"stuck.wait":{},"stuck.ok":{}},"echoDelays":{"stuck.wait":60000}}`

func startStuck(t *testing.T, manifest string, opts ...plugin.SupervisorOption) (*testutil.PluginHarness, func(id string) error) {
	h := testutil.NewHarness(t, append([]plugin.SupervisorOption{plugin.WithBackoff(10*time.Millisecond, 10*time.Millisecond)}, opts...)...)
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDirAs(t, manifest))
	testutil.Eventually(t, func() bool { _, ok := h.Sup.Conn("stuck"); return ok }, 5*time.Second)
	c := h.Client(t)
	return h, func(id string) error {
		return c.Call(t.Context(), "command.run", map[string]any{"id": id, "workspaceId": p.ID}, nil)
	}
}

func TestRouteTimeoutsInARowRestartPluginWithoutFailingIt(t *testing.T) {
	h, run := startStuck(t, stuckManifest, plugin.WithCrashLimit(1, time.Minute))
	pid := h.Sup.PIDs()[0]
	for range 3 {
		require.Error(t, run("stuck.wait"))
	}
	testutil.Eventually(t, func() bool {
		st := h.Sup.Statuses()[0]
		return st.Restarts == 1 && st.State == plugin.StateRunning && st.PID != pid
	}, 5*time.Second)
	assert.Contains(t, h.Sup.Statuses()[0].LastError, "commands")
	assert.False(t, testutil.Alive(pid))
}

func TestOverlappingTimeoutsAndRepliesKeepPluginRunning(t *testing.T) {
	h, run := startStuck(t, stuckManifest)
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() { assert.Error(t, run("stuck.wait")) })
	}
	wg.Wait()
	require.Error(t, run("stuck.wait"))
	require.NoError(t, run("stuck.ok"))
	require.Error(t, run("stuck.wait"))
	require.Error(t, run("stuck.wait"))
	time.Sleep(200 * time.Millisecond)
	assert.Zero(t, h.Sup.Statuses()[0].Restarts)
}

const lagManifest = `{"id":"stuck","name":"Lag","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"stuck.wait","title":"Wait","timeout":1}]},"echoResults":{"stuck.wait":{}},"echoDelays":{"stuck.wait":1500}}`

func agentWaits(t *testing.T, h *testutil.PluginHarness) func(caller string) {
	r := plugin.NewRouter(h.Sup)
	c, ok := r.Resolve("stuck.wait")
	require.True(t, ok)
	return func(caller string) {
		_, err := r.Command(t.Context(), c, json.RawMessage(fmt.Sprintf(`{"id":"stuck.wait","agent":true,"caller":%q}`, caller)))
		require.Error(t, err)
	}
}

func TestOneAgentCannotRestartSlowButAlivePlugin(t *testing.T) {
	h, _ := startStuck(t, lagManifest, plugin.WithStuckSilence(2*time.Second))
	wait := agentWaits(t, h)
	pid := h.Sup.PIDs()[0]

	for range 3 {
		wait("agt_a")
	}
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, []int{pid}, h.Sup.PIDs())

	wait("agt_b")
	wait("agt_c")
	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].Restarts == 1 }, 5*time.Second)
	assert.False(t, testutil.Alive(pid))
}

func TestOneAgentRestartsSilentPlugin(t *testing.T) {
	h, _ := startStuck(t, stuckManifest, plugin.WithStuckSilence(2*time.Second))
	wait := agentWaits(t, h)
	pid := h.Sup.PIDs()[0]

	for range 3 {
		wait("agt_a")
	}

	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].Restarts == 1 }, 5*time.Second)
	assert.False(t, testutil.Alive(pid))
}

func TestWaitForStartAndCallShareOneTimeout(t *testing.T) {
	t.Setenv("ECHO_START_DELAY", "600ms")
	h := testutil.NewHarness(t)
	p := h.CreateWorkspace(t, "demo")
	c := h.Client(t)
	h.Start(t, testutil.EchoPluginDirAs(t, `{"id":"late","name":"Late","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"late.wait","title":"Wait","timeout":1}]},"echoResults":{"late.wait":{}},"echoDelays":{"late.wait":900}}`))

	err := c.Call(t.Context(), "command.run", map[string]any{"id": "late.wait", "workspaceId": p.ID}, nil)

	assert.ErrorIs(t, err, rpc.ErrPluginUnavailable)
}
