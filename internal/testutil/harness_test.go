package testutil

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const failingHarnessEnv = "TESTUTIL_FAILING_HARNESS"

func TestFailingHarnessReportsPluginStateAtFailure(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestFailingHarnessHelper$", "-test.v")
	cmd.Env = append(os.Environ(), failingHarnessEnv+"=1")
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	assert.Contains(t, string(out), "plugin echo: state running")
	assert.Contains(t, string(out), `msg="plugin running" id=echo`)
}

func TestFailingHarnessHelper(t *testing.T) {
	if os.Getenv(failingHarnessEnv) == "" {
		return
	}
	h := NewHarness(t)
	h.Start(t, EchoPluginDir(t))
	Eventually(t, func() bool { return h.Sup.Statuses()[0].State == "running" }, 5*time.Second)
	t.Fatal("failing on purpose")
}
