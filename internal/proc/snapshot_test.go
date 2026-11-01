package proc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/proc"
)

func TestSnapshotAndReadEnvOfChild(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperBusy$")
	cmd.Env = append(os.Environ(), "CLUBMATE_HELPER_BUSY=1", "CLUBMATE_SESSION_ID=ses_probe")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	pid := cmd.Process.Pid
	_, err := proc.Snapshot()
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	snap, err := proc.Snapshot()
	require.NoError(t, err)
	self, ok := findPID(snap, os.Getpid())
	require.True(t, ok)
	assert.Equal(t, os.Getppid(), self.PPID)
	assert.Positive(t, self.RSSKiB)
	p, ok := findPID(snap, pid)
	require.True(t, ok)
	assert.Positive(t, p.RSSKiB)
	assert.Greater(t, p.CPU, 20.0)
	env, err := proc.ReadEnv(pid)
	require.NoError(t, err)
	assert.Equal(t, filepath.Base(os.Args[0]), p.Comm)
	assert.Contains(t, env, "CLUBMATE_SESSION_ID=ses_probe")
}

func TestProcessArgs(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	assert.Equal(t, []string{"sleep", "30"}, proc.ReadArgs(cmd.Process.Pid))
	assert.Equal(t, []string{}, proc.ReadArgs(1))
}

func TestHelperBusy(t *testing.T) {
	if os.Getenv("CLUBMATE_HELPER_BUSY") == "" {
		t.Skip()
	}
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
	}
}

func pids(nodes []proc.Node) []int {
	out := make([]int, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Process.PID)
	}
	return out
}

func node(nodes []proc.Node, pid int) proc.Node {
	for _, n := range nodes {
		if n.Process.PID == pid {
			return n
		}
	}
	return proc.Node{}
}

func findPID(snap []proc.Process, pid int) (proc.Process, bool) {
	for _, p := range snap {
		if p.PID == pid {
			return p, true
		}
	}
	return proc.Process{}, false
}
