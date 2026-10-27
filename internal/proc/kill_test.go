package proc_test

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"clubmate/internal/proc"
	"clubmate/internal/testutil"
)

func TestKillTreeEscalates(t *testing.T) {
	gone := exec.Command("true")
	require.NoError(t, gone.Run())
	stubborn := startGroup(t, "sh", "-c", `trap "" TERM; sleep 30`)
	plain := startGroup(t, "sleep", "30")
	targets := []proc.Target{targetOf(t, stubborn), targetOf(t, plain), {PID: gone.Process.Pid, Start: time.Now()}}
	require.NoError(t, proc.KillTree(t.Context(), targets, 300*time.Millisecond))
	testutil.Eventually(t, func() bool { return !testutil.Alive(stubborn) && !testutil.Alive(plain) }, 2*time.Second)
}

func TestKillTreeRefusesUnsafePIDs(t *testing.T) {
	plain := startGroup(t, "sleep", "30")
	for _, unsafe := range []int{1, os.Getpid()} {
		targets := []proc.Target{targetOf(t, plain), {PID: unsafe}}
		assert.ErrorIs(t, proc.KillTree(t.Context(), targets, 0), proc.ErrUnsafePID)
	}
	assert.True(t, testutil.Alive(plain))
}

func TestKillTreeSkipsReusedPID(t *testing.T) {
	pid := startGroup(t, "sleep", "30")
	reused := proc.Target{PID: pid, Start: targetOf(t, pid).Start.Add(-time.Hour)}
	require.NoError(t, proc.KillTree(t.Context(), []proc.Target{reused}, 100*time.Millisecond))
	assert.True(t, testutil.Alive(pid))
}

func targetOf(t *testing.T, pid int) proc.Target {
	t.Helper()
	start, err := proc.StartTime(pid)
	require.NoError(t, err)
	return proc.Target{PID: pid, Start: start}
}

func startGroup(t *testing.T, name string, args ...string) int {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = unix.Kill(-pid, unix.SIGKILL)
		<-done
	})
	return pid
}
