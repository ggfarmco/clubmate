package plugin_test

import (
	"encoding/json"
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
	st := h.Sup.Statuses()
	require.Len(t, st, 1)
	assert.Equal(t, "running", st[0].State)
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
