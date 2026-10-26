package plugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/core"
	"clubmate/internal/paths"
	"clubmate/internal/plugin"
	"clubmate/internal/proc"
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/testutil"
)

const stopTimeout = 15 * time.Second

type harness struct {
	svc   *core.Service
	bus   *bus.Bus
	srv   *rpc.Server
	sup   *plugin.Supervisor
	store *internalsdui.Store
	dirs  paths.Dirs
}

type noopHandler struct{}

func TestSupervisorStartsPluginThatCallsCore(t *testing.T) {
	h := newHarness(t)
	sub, err := h.bus.Subscribe("echo.ready", 0)
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	h.start(t, testutil.EchoPluginDir(t))
	select {
	case ev := <-sub.C():
		assert.Equal(t, "plugin:echo", ev.Source)
	case <-time.After(5 * time.Second):
		t.Fatal("echo plugin never reported ready")
	}
	st := h.sup.Statuses()
	require.Len(t, st, 1)
	assert.Equal(t, "running", st[0].State)
}

func TestSupervisorGivesUpOnCrashLoop(t *testing.T) {
	t.Setenv("ECHO_CRASH", "1")
	h := newHarness(t, plugin.WithBackoff(10*time.Millisecond, 50*time.Millisecond), plugin.WithCrashLimit(5, time.Minute))
	h.start(t, testutil.EchoPluginDir(t))
	testutil.Eventually(t, func() bool { return h.sup.Statuses()[0].State == "failed" }, 5*time.Second)
	assert.Equal(t, 5, h.sup.Statuses()[0].Restarts)
}

func TestPluginsDoNotInheritMarkersAndAreNotInProcessTree(t *testing.T) {
	t.Setenv("CLUBMATE_SESSION_ID", "ses_0123456789abcdef")
	h := newHarness(t)
	h.start(t, testutil.EchoPluginDir(t))
	testutil.Eventually(t, func() bool { return len(h.sup.PIDs()) == 1 }, 5*time.Second)
	pid := h.sup.PIDs()[0]
	env, err := proc.ReadEnv(pid)
	require.NoError(t, err)
	assert.NotContains(t, env, "CLUBMATE_SESSION_ID=ses_0123456789abcdef")
	nodes, err := h.svc.ProcessTree(t.Context(), core.TreeFilter{})
	require.NoError(t, err)
	for _, n := range nodes {
		assert.NotEqual(t, pid, n.Process.PID)
	}
}

func TestSupervisorKillsLeftoverProcessGroupWhenPluginExits(t *testing.T) {
	t.Setenv("ECHO_FORK_SLEEP", "1")
	h := newHarness(t, plugin.WithBackoff(time.Minute, time.Minute))
	sub, err := h.bus.Subscribe("echo.forked", 0)
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	h.start(t, testutil.EchoPluginDir(t))
	var forked struct {
		PID int `json:"pid"`
	}
	select {
	case ev := <-sub.C():
		require.NoError(t, json.Unmarshal(ev.Payload, &forked))
	case <-time.After(5 * time.Second):
		t.Fatal("echo plugin never forked")
	}
	t.Cleanup(func() { _ = syscall.Kill(forked.PID, syscall.SIGKILL) })
	testutil.Eventually(t, func() bool { return h.sup.Statuses()[0].State == "backoff" }, 5*time.Second)
	testutil.Eventually(t, func() bool { return !testutil.Alive(forked.PID) }, 5*time.Second)
}

func newHarness(t *testing.T, opts ...plugin.SupervisorOption) *harness {
	t.Helper()
	h := &harness{store: internalsdui.NewStore(), dirs: testutil.ShortDirs(t)}
	h.svc, h.bus = testutil.NewServiceWithBus(t, core.WithExcludePIDs(func() []int { return h.sup.PIDs() }))
	h.srv = rpc.NewServer(h.svc, h.bus)
	opts = append([]plugin.SupervisorOption{plugin.WithOnStop(h.store.ClearOwner)}, opts...)
	h.sup = plugin.NewSupervisor(h.srv, h.dirs, slog.New(slog.DiscardHandler), opts...)
	return h
}

func (h *harness) start(t *testing.T, searchPath ...string) {
	t.Helper()
	manifests, errs := plugin.Discover(searchPath)
	require.Empty(t, errs)
	h.srv.SetUI(h.store, internalsdui.NewSections(plugin.SectionDefs(manifests)), plugin.NewRouter(h.sup))
	h.sup.Start(context.Background(), manifests)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		defer cancel()
		h.sup.Stop(ctx)
	})
}

func (h *harness) client(t *testing.T) *rpc.Client {
	t.Helper()
	l, err := net.Listen("unix", h.dirs.CoreSocket())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.srv.Serve(ctx, l) }()
	c, err := rpc.Dial(t.Context(), h.dirs.CoreSocket())
	require.NoError(t, err)
	t.Cleanup(func() {
		c.Close()
		cancel()
		assert.NoError(t, <-done)
	})
	return c
}

func (h *harness) pluginCall(t *testing.T, pluginID, method string, params any) error {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server := h.srv.ServeConn(context.Background(), jsonrpc2.NewPlainObjectStream(serverSide), rpc.Caller{PluginID: pluginID})
	defer server.Close()
	client := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(clientSide), noopHandler{})
	defer client.Close()
	err := client.Call(t.Context(), method, params, nil)
	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) {
		return &rpc.RemoteError{Code: int(rpcErr.Code), Message: rpcErr.Message}
	}
	return err
}

func (noopHandler) Handle(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) {}
