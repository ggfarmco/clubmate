package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/cli"
	"clubmate/internal/paths"
	"clubmate/internal/plugin"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func startCore(t *testing.T) *bus.Bus {
	t.Helper()
	dirs := testutil.ShortDirs(t)
	srv, b := testutil.NewRPCServerIn(t, dirs)
	serveIn(t, srv, dirs)
	return b
}

func startCoreWithUI(t *testing.T) *internalsdui.Store {
	t.Helper()
	srv, _ := testutil.NewRPCServer(t)
	store := internalsdui.NewStore()
	sections := internalsdui.NewSections([]internalsdui.SectionDef{{ID: "echo", Title: "Echo", Plugin: "echo"}})
	sup := plugin.NewSupervisor(srv, testutil.ShortDirs(t), slog.New(slog.DiscardHandler))
	srv.SetUI(store, sections, plugin.NewRouter(sup))
	serve(t, srv)
	return store
}

func serve(t *testing.T, srv *rpc.Server) {
	t.Helper()
	serveIn(t, srv, testutil.ShortDirs(t))
}

func serveIn(t *testing.T, srv *rpc.Server, dirs paths.Dirs) {
	t.Helper()
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

func TestAttachExitsQuietlyOnSIGHUP(t *testing.T) {
	startCore(t)
	var p project.Project
	require.NoError(t, json.Unmarshal(run(t, "project", "create", "demo", "--json"), &p))
	sid := strings.TrimSpace(string(run(t, "session", "create", "--project", p.ID, "--", "sh", "-c", "exec cat")))
	dirs, err := paths.Resolve()
	require.NoError(t, err)
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(dirs.SessionSocket(sid))
		return err == nil
	}, 3*time.Second)
	cmd := exec.Command(testutil.BuildBinary(t, "clubmate/cmd/clubmate"), "attach", sid)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	_, err = stdin.Write([]byte("ping\n"))
	require.NoError(t, err)
	_, err = bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.NoError(t, cmd.Process.Signal(syscall.SIGHUP))
	require.NoError(t, cmd.Wait(), stderr.String())
	assert.Empty(t, stderr.String())
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

func TestUIDumpPrintsOutline(t *testing.T) {
	store := startCoreWithUI(t)
	var p project.Project
	require.NoError(t, json.Unmarshal(run(t, "project", "create", "demo", "--json"), &p))
	_, err := store.Render(t.Context(), p.ID, "echo.main", "echo", sdui.VStack(sdui.Text("hello"), sdui.Toggle("on", true)))
	require.NoError(t, err)
	want := "echo.main rev=1 owner=echo\n" +
		"  stack direction=vertical\n" +
		"    text text=hello\n" +
		"    toggle#on value=true\n"
	assert.Equal(t, want, string(run(t, "ui", "dump", "--project", p.ID)))
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

func coreCLI(t *testing.T) func(args ...string) (string, int) {
	t.Helper()
	bins := t.TempDir()
	for _, name := range []string{"clubmate", "clubmate-core", "clubmate-session"} {
		data, err := os.ReadFile(testutil.BuildBinary(t, "clubmate/cmd/"+name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(bins, name), data, 0o755))
	}
	dirs := testutil.ShortDirs(t)
	env := append(os.Environ(), "CLUBMATE_STATE_DIR="+dirs.State, "CLUBMATE_RUNTIME_DIR="+dirs.Runtime)
	clubmate := func(args ...string) (string, int) {
		cmd := exec.Command(filepath.Join(bins, "clubmate"), args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return string(out), exit.ExitCode()
		}
		require.NoError(t, err, string(out))
		return string(out), 0
	}
	t.Cleanup(func() {
		out, code := clubmate("core", "status")
		var pid int
		if code == 0 {
			_, _ = fmt.Sscanf(out, "running pid=%d", &pid)
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	})
	return clubmate
}

func TestCoreStartStatusStop(t *testing.T) {
	clubmate := coreCLI(t)

	out, code := clubmate("core", "status")
	assert.Equal(t, 3, code)
	assert.Equal(t, "not running\n", out)

	out, code = clubmate("core", "stop")
	assert.Equal(t, 3, code)
	assert.Equal(t, "not running\n", out)

	out, code = clubmate("core", "start")
	assert.Equal(t, 0, code)
	assert.Regexp(t, `^started pid=\d+\n$`, out)

	out, code = clubmate("core", "status")
	assert.Equal(t, 0, code)
	assert.Regexp(t, `^running pid=\d+ version=\S+\n$`, out)

	out, code = clubmate("core", "start")
	assert.Equal(t, 0, code)
	assert.Equal(t, "already running\n", out)

	_, code = clubmate("core", "stop")
	assert.Equal(t, 0, code)

	out, code = clubmate("core", "start")
	assert.Equal(t, 0, code)
	assert.Regexp(t, `^started pid=\d+\n$`, out)

	_, code = clubmate("core", "stop")
	assert.Equal(t, 0, code)

	_, code = clubmate("core", "status")
	assert.Equal(t, 3, code)
}

func TestSessionSurvivesCoreStop(t *testing.T) {
	clubmate := coreCLI(t)
	_, code := clubmate("core", "start")
	require.Equal(t, 0, code)
	var p project.Project
	out, _ := clubmate("project", "create", "demo", "--json")
	require.NoError(t, json.Unmarshal([]byte(out), &p))
	out, _ = clubmate("session", "create", "--project", p.ID, "--", "sh", "-c", "exec cat")
	sid := strings.TrimSpace(out)
	var before []session.Session
	out, _ = clubmate("session", "list", "--json")
	require.NoError(t, json.Unmarshal([]byte(out), &before))
	require.Len(t, before, 1)
	require.Equal(t, sid, before[0].ID)
	s := before[0]
	t.Cleanup(func() {
		_ = syscall.Kill(-s.PGID, syscall.SIGKILL)
		_ = syscall.Kill(s.HostPID, syscall.SIGKILL)
	})

	_, code = clubmate("core", "stop")
	require.Equal(t, 0, code)
	assert.True(t, testutil.Alive(s.HostPID))
	assert.True(t, testutil.Alive(s.ChildPID))

	_, code = clubmate("core", "start")
	require.Equal(t, 0, code)
	var list []session.Session
	out, _ = clubmate("session", "list", "--json")
	require.NoError(t, json.Unmarshal([]byte(out), &list))
	require.Len(t, list, 1)
	assert.Equal(t, session.StateRunning, list[0].State)
}

func TestProjectOpenLaunchesAppOnlyWithoutClients(t *testing.T) {
	startCore(t)
	var p project.Project
	require.NoError(t, json.Unmarshal(run(t, "project", "create", "demo", "--json"), &p))
	var launched []string
	open := func() {
		root := cli.NewRootWithLauncher(&bytes.Buffer{}, &bytes.Buffer{}, func(_ context.Context, id string) error {
			launched = append(launched, id)
			return nil
		})
		root.SetArgs([]string{"project", "open", p.ID})
		require.NoError(t, root.Execute())
	}
	open()
	assert.Equal(t, []string{p.ID}, launched)

	dirs, err := paths.Resolve()
	require.NoError(t, err)
	client, err := rpc.Dial(t.Context(), dirs.CoreSocket())
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })
	open()
	assert.Len(t, launched, 1)
	select {
	case n := <-client.Notifications():
		assert.Equal(t, "project.open", n.Method)
	case <-time.After(2 * time.Second):
		t.Fatal("no project.open notification")
	}
}
