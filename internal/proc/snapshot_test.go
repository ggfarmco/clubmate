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
	"clubmate/internal/testutil"
)

const deltaSampleGap = 300 * time.Millisecond

func TestSnapshotAndReadEnvOfChild(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperBusy$")
	cmd.Env = append(os.Environ(), "CLUBMATE_HELPER_BUSY=1", "CLUBMATE_SESSION_ID=ses_probe")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	pid := cmd.Process.Pid
	_, err := proc.Snapshot()
	require.NoError(t, err)
	primed := time.Now()
	var snap []proc.Process
	var p proc.Process
	testutil.Eventually(t, func() bool {
		var ok bool
		snap, err = proc.Snapshot()
		if err != nil {
			return false
		}
		p, ok = findPID(snap, pid)
		return ok && p.CPU > 0 && time.Since(primed) > deltaSampleGap
	}, 3*time.Second)
	self, ok := findPID(snap, os.Getpid())
	require.True(t, ok)
	assert.Equal(t, os.Getppid(), self.PPID)
	assert.Positive(t, self.RSSKiB)
	assert.Positive(t, p.RSSKiB)
	assert.Positive(t, p.CPU)
	assert.Less(t, p.CPU, 200.0)
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
