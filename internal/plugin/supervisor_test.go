package plugin_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/core"
	"clubmate/internal/plugin"
	"clubmate/internal/proc"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

const stopTimeout = 15 * time.Second

type harness struct {
	svc *core.Service
	bus *bus.Bus
	srv *rpc.Server
	sup *plugin.Supervisor
}

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
	h := &harness{}
	h.svc, h.bus = testutil.NewServiceWithBus(t, core.WithExcludePIDs(func() []int { return h.sup.PIDs() }))
	h.srv = rpc.NewServer(h.svc, h.bus)
	h.sup = plugin.NewSupervisor(h.srv, testutil.ShortDirs(t), slog.New(slog.DiscardHandler), opts...)
	return h
}

func (h *harness) start(t *testing.T, searchPath ...string) {
	t.Helper()
	manifests, errs := plugin.Discover(searchPath)
	require.Empty(t, errs)
	h.sup.Start(context.Background(), manifests)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		defer cancel()
		h.sup.Stop(ctx)
	})
}
