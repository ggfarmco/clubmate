package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/cli"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
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
