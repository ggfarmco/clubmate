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
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSleep$")
	cmd.Env = append(os.Environ(), "CLUBMATE_HELPER_SLEEP=1", "CLUBMATE_SESSION_ID=ses_probe")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	pid := cmd.Process.Pid
	snap, err := proc.Snapshot()
	require.NoError(t, err)
	p, ok := findPID(snap, pid)
	require.True(t, ok)
	assert.Positive(t, p.RSSKiB)
	env, err := proc.ReadEnv(pid)
	require.NoError(t, err)
	assert.Equal(t, filepath.Base(os.Args[0]), p.Comm)
	assert.Contains(t, env, "CLUBMATE_SESSION_ID=ses_probe")
}

func TestHelperSleep(t *testing.T) {
	if os.Getenv("CLUBMATE_HELPER_SLEEP") == "" {
		t.Skip()
	}
	time.Sleep(30 * time.Second)
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
