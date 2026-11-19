package testutil

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sessionEnvHelper = "TESTUTIL_SESSION_ENV_HELPER"

func TestAgentSessionEnvDoesNotReachTests(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSessionEnvHelper$", "-test.v")
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, hermeticEnv+"=") }),
		sessionEnvHelper+"=1",
		"CLUBMATE_AGENT_SESSION=agt_outer",
		"CLUBMATE_LAND_PROTECT=refs/heads/main",
		"CLUBMATE_TEST_KEEP=kept",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0=/outer/githooks",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Contains(t, string(out), "hooksPath=none caller= kept=kept")
}

func TestSessionEnvHelper(t *testing.T) {
	if os.Getenv(sessionEnvHelper) == "" {
		return
	}
	hooks, err := exec.CommandContext(t.Context(), "git", "config", "--get", "core.hooksPath").Output()
	if err != nil {
		hooks = []byte("none")
	}
	t.Logf("hooksPath=%s caller=%s kept=%s", strings.TrimSpace(string(hooks)), os.Getenv("CLUBMATE_AGENT_SESSION"), os.Getenv("CLUBMATE_TEST_KEEP"))
}
