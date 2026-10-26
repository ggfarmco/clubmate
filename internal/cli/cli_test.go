package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/cli"
	"clubmate/internal/project"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
)

func startCore(t *testing.T) *bus.Bus {
	t.Helper()
	srv, b := testutil.NewRPCServer(t)
	dirs := testutil.ShortDirs(t)
	t.Setenv("CLUBMATE_STATE_DIR", dirs.State)
	t.Setenv("CLUBMATE_RUNTIME_DIR", dirs.Runtime)
	l, err := net.Listen("unix", dirs.CoreSocket())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, l) }()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})
	return b
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

func TestBusPublishAndRead(t *testing.T) {
	startCore(t)
	run(t, "bus", "publish", "demo.x", `{"a":1}`, "--json")
	out := strings.TrimSpace(string(run(t, "bus", "read", "demo.*", "--json")))
	require.NotContains(t, out, "\n")
	assert.Contains(t, out, `"demo.x"`)
}

func TestBusReadFollowStreamsNewEvents(t *testing.T) {
	startCore(t)
	run(t, "bus", "publish", "demo.old")
	var stdout syncBuffer
	root := cli.NewRoot(&stdout, &stdout)
	root.SetArgs([]string{"bus", "read", "demo.*", "--follow"})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	testutil.Eventually(t, func() bool { return strings.Contains(stdout.String(), "demo.old") }, 3*time.Second)
	run(t, "bus", "publish", "demo.new", `{"n":2}`)
	testutil.Eventually(t, func() bool { return strings.Contains(stdout.String(), `demo.new`) }, 3*time.Second)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, 2, strings.Count(stdout.String(), " cli "))
}

func TestBusReadFollowPagesBacklogBeyondLimit(t *testing.T) {
	b := startCore(t)
	for range 300 {
		_, err := b.Publish(t.Context(), "demo.x", "test", nil)
		require.NoError(t, err)
	}
	stdout := syncBuffer{delay: time.Millisecond}
	root := cli.NewRoot(&stdout, &stdout)
	root.SetArgs([]string{"bus", "read", "demo.*", "--limit", "100", "--follow", "--json"})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	testutil.Eventually(t, func() bool { return strings.Count(stdout.String(), "\n") >= 300 }, 3*time.Second)
	time.Sleep(100 * time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	var seqs []int64
	for line := range strings.Lines(stdout.String()) {
		var e bus.Event
		require.NoError(t, json.Unmarshal([]byte(line), &e))
		seqs = append(seqs, e.Seq)
	}
	require.Len(t, seqs, 300)
	for i, seq := range seqs {
		assert.Equal(t, int64(i+1), seq)
	}
}

type syncBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	delay time.Duration
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	time.Sleep(b.delay)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
