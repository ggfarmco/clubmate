package plugin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/core"
	"clubmate/internal/plugin"
	"clubmate/internal/proc"
	"clubmate/internal/testutil"
)

func TestSupervisorStartsPluginThatCallsCore(t *testing.T) {
	h := testutil.NewHarness(t)
	sub, err := h.Bus.Subscribe("echo.ready", 0)
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	h.Start(t, testutil.EchoPluginDir(t))
	select {
	case ev := <-sub.C():
		assert.Equal(t, "plugin:echo", ev.Source)
	case <-time.After(5 * time.Second):
		t.Fatal("echo plugin never reported ready")
	}
	require.Len(t, h.Sup.Statuses(), 1)
	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].State == "running" }, 5*time.Second)
}

func TestSupervisorCreatesPluginRuntimeDir(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDir(t))
	testutil.Eventually(t, func() bool { return len(h.Sup.PIDs()) == 1 }, 5*time.Second)
	info, err := os.Stat(h.Dirs.PluginRuntime("echo"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestSupervisorGivesUpOnCrashLoop(t *testing.T) {
	t.Setenv("ECHO_CRASH", "1")
	h := testutil.NewHarness(t, plugin.WithBackoff(10*time.Millisecond, 50*time.Millisecond), plugin.WithCrashLimit(5, time.Minute))
	h.Start(t, testutil.EchoPluginDir(t))
	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].State == "failed" }, 5*time.Second)
	assert.Equal(t, 5, h.Sup.Statuses()[0].Restarts)
}

func TestPluginsDoNotInheritMarkersAndAreNotInProcessTree(t *testing.T) {
	t.Setenv("CLUBMATE_SESSION_ID", "ses_0123456789abcdef")
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDir(t))
	testutil.Eventually(t, func() bool { return len(h.Sup.PIDs()) == 1 }, 5*time.Second)
	pid := h.Sup.PIDs()[0]
	env, err := proc.ReadEnv(pid)
	require.NoError(t, err)
	assert.NotContains(t, env, "CLUBMATE_SESSION_ID=ses_0123456789abcdef")
	nodes, err := h.Svc.ProcessTree(t.Context(), core.TreeFilter{})
	require.NoError(t, err)
	for _, n := range nodes {
		assert.NotEqual(t, pid, n.Process.PID)
	}
}

func TestPluginsGetResourcesDir(t *testing.T) {
	h := testutil.NewHarness(t, plugin.WithResourcesDir("/x/Resources"))
	h.Start(t, testutil.EchoPluginDir(t))
	testutil.Eventually(t, func() bool { return len(h.Sup.PIDs()) == 1 }, 5*time.Second)
	env, err := proc.ReadEnv(h.Sup.PIDs()[0])
	require.NoError(t, err)
	assert.Contains(t, env, "CLUBMATE_RESOURCES=/x/Resources")
}

func TestSupervisorKillsLeftoverProcessGroupWhenPluginExits(t *testing.T) {
	t.Setenv("ECHO_FORK_SLEEP", "1")
	h := testutil.NewHarness(t, plugin.WithBackoff(time.Minute, time.Minute))
	sub, err := h.Bus.Subscribe("echo.forked", 0)
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	h.Start(t, testutil.EchoPluginDir(t))
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
	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].State == "backoff" }, 5*time.Second)
	testutil.Eventually(t, func() bool { return !testutil.Alive(forked.PID) }, 5*time.Second)
}

func TestStopDoesNotHangOnWedgedPlugin(t *testing.T) {
	t.Setenv("ECHO_WEDGE", "1")
	h := testutil.NewHarness(t, plugin.WithBackoff(time.Minute, time.Minute))
	sub, err := h.Bus.Subscribe("echo.ready", 0)
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	h.Start(t, testutil.EchoPluginDir(t))
	select {
	case <-sub.C():
	case <-time.After(5 * time.Second):
		t.Fatal("echo plugin never reported ready")
	}
	require.Len(t, h.Sup.PIDs(), 1)
	pid := h.Sup.PIDs()[0]
	time.Sleep(500 * time.Millisecond)
	for i := range 40 {
		h.CreateProject(t, fmt.Sprintf("%d-%s", i, strings.Repeat("x", 4000)))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		h.Sup.Stop(ctx)
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(15 * time.Second):
		t.Fatal("Stop hung on wedged plugin")
	}
	assert.False(t, testutil.Alive(pid))
}
