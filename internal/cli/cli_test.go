package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/cli"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
)

func startCore(t *testing.T) {
	t.Helper()
	svc := testutil.NewService(t)
	dirs := testutil.ShortDirs(t)
	t.Setenv("CLUBMATE_STATE_DIR", dirs.State)
	t.Setenv("CLUBMATE_RUNTIME_DIR", dirs.Runtime)
	l, err := net.Listen("unix", dirs.CoreSocket())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rpc.Serve(ctx, l, svc) }()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})
}

func run(t *testing.T, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := cli.NewRoot(&stdout, &stderr)
	root.SetArgs(args)
	require.NoError(t, root.Execute(), stderr.String())
	return stdout.Bytes()
}

func TestProjectCreateAndListJSON(t *testing.T) {
	startCore(t)
	repo := t.TempDir()
	out := run(t, "project", "create", "demo", "--repo", repo, "--json")
	var p project.Project
	require.NoError(t, json.Unmarshal(out, &p))
	assert.Equal(t, "demo", p.Name)
	var list []project.Project
	require.NoError(t, json.Unmarshal(run(t, "project", "list", "--json"), &list))
	assert.Len(t, list, 1)
}

func TestAttachToExitedSessionReportsExitCode(t *testing.T) {
	startCore(t)
	var p project.Project
	require.NoError(t, json.Unmarshal(run(t, "project", "create", "demo", "--json"), &p))
	sid := strings.TrimSpace(string(run(t, "session", "create", "--project", p.ID, "--", "sh", "-c", "exit 3")))
	testutil.Eventually(t, func() bool {
		var list []session.Session
		return json.Unmarshal(run(t, "session", "list", "--json"), &list) == nil && len(list) == 1 && list[0].State == session.StateExited
	}, 3*time.Second)
	var stdout, stderr bytes.Buffer
	root := cli.NewRoot(&stdout, &stderr)
	root.SetArgs([]string{"attach", sid})
	var exit *cli.ExitError
	require.ErrorAs(t, root.Execute(), &exit)
	assert.Equal(t, 3, exit.Code)
	assert.Equal(t, "session "+sid+" exited (code 3)\n", stderr.String())
}

func TestSessionRemoveCommand(t *testing.T) {
	startCore(t)
	var p project.Project
	require.NoError(t, json.Unmarshal(run(t, "project", "create", "demo", "--json"), &p))
	sid := strings.TrimSpace(string(run(t, "session", "create", "--project", p.ID, "--", "true")))
	testutil.Eventually(t, func() bool {
		var list []session.Session
		return json.Unmarshal(run(t, "session", "list", "--json"), &list) == nil && len(list) == 1 && list[0].State == session.StateExited
	}, 3*time.Second)
	run(t, "session", "remove", sid)
	assert.JSONEq(t, `[]`, string(run(t, "session", "list", "--json")))
}
