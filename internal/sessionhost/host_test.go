package sessionhost_test

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/proc"
	"clubmate/internal/sessionhost"
	"clubmate/internal/testutil"
	"clubmate/internal/wire"
)

const waitTimeout = 3 * time.Second

var bgPID = regexp.MustCompile(`bg:(\d+)\r?\n`)

type testClient struct {
	conn net.Conn
	mu   sync.Mutex
	out  bytes.Buffer
	exit chan int
}

func TestSessionHostStreamsAndReplays(t *testing.T) {
	cfg := testConfig(t, "sh", "-c", `printf "ready:$CLUBMATE_SESSION_ID"; exec cat`)
	startHost(t, cfg)
	a := dial(t, cfg.Socket)
	a.waitOutput(t, "ready:ses_test")
	b := dial(t, cfg.Socket)
	b.waitOutput(t, "ready:ses_test")
	b.send(t, "ping\n")
	a.waitOutput(t, "ping")
	m, err := sessionhost.ReadMeta(cfg.MetaPath)
	require.NoError(t, err)
	assert.Positive(t, m.ChildPID)
	assert.Equal(t, m.ChildPID, m.PGID)
}

func TestSessionHostReplacesInheritedMarkers(t *testing.T) {
	t.Setenv("CLUBMATE_SESSION_ID", "ses_outer")
	cfg := testConfig(t, os.Args[0], "-test.run=^TestHelperSleep$")
	cfg.Env = []string{"CLUBMATE_HELPER_SLEEP=1", "CLUBMATE_PROJECT_ID=prj_outer"}
	startHost(t, cfg)
	m, err := sessionhost.ReadMeta(cfg.MetaPath)
	require.NoError(t, err)
	env, err := proc.ReadEnv(m.ChildPID)
	require.NoError(t, err)
	markers := slices.DeleteFunc(env, func(kv string) bool {
		return !strings.HasPrefix(kv, "CLUBMATE_") || strings.HasPrefix(kv, "CLUBMATE_HELPER")
	})
	assert.Equal(t, []string{"CLUBMATE_SESSION_ID=ses_test", "CLUBMATE_PROJECT_ID=prj_test"}, markers)
}

func TestHelperSleep(t *testing.T) {
	if os.Getenv("CLUBMATE_HELPER_SLEEP") == "" {
		t.Skip()
	}
	time.Sleep(30 * time.Second)
}

func TestSessionHostResize(t *testing.T) {
	cfg := testConfig(t, "sh", "-c", `read x; stty size; exec cat`)
	startHost(t, cfg)
	c := dial(t, cfg.Socket)
	c.resize(t, 100, 40)
	c.send(t, "go\n")
	c.waitOutput(t, "40 100")
}

func TestSessionHostExit(t *testing.T) {
	cfg := testConfig(t, "sh", "-c", `read x; exit 7`)
	done := startHost(t, cfg)
	c := dial(t, cfg.Socket)
	c.send(t, "go\n")
	assert.Equal(t, 7, c.waitExit(t))
	require.NoError(t, <-done)
	m, err := sessionhost.ReadMeta(cfg.MetaPath)
	require.NoError(t, err)
	require.NotNil(t, m.ExitCode)
	assert.Equal(t, 7, *m.ExitCode)
	assert.NoFileExists(t, cfg.Socket)
}

func TestSessionHostExitsWhileBackgroundJobHoldsPTY(t *testing.T) {
	cfg := testConfig(t, "sh", "-c", `set -m; sleep 30 & echo bg:$!; read x; exit 3`)
	done := startHost(t, cfg)
	c := dial(t, cfg.Socket)
	testutil.Eventually(t, func() bool { return bgPID.MatchString(c.output()) }, waitTimeout)
	pid, err := strconv.Atoi(bgPID.FindStringSubmatch(c.output())[1])
	require.NoError(t, err)
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	c.send(t, "go\n")
	assert.Equal(t, 3, c.waitExit(t))
	require.NoError(t, <-done)
	assert.NoFileExists(t, cfg.Socket)
}

func TestSessionHostListenFailureRemovesMeta(t *testing.T) {
	cfg := testConfig(t, "sh", "-c", "exec cat")
	cfg.Socket = filepath.Join(filepath.Dir(cfg.Socket), "missing", "x.sock")
	require.Error(t, sessionhost.Run(t.Context(), cfg))
	assert.NoFileExists(t, cfg.MetaPath)
}

func testConfig(t *testing.T, command ...string) sessionhost.Config {
	t.Helper()
	d := testutil.ShortDirs(t)
	return sessionhost.Config{
		ID:        "ses_test",
		ProjectID: "prj_test",
		Command:   command,
		Socket:    d.SessionSocket("ses_test"),
		MetaPath:  d.SessionMeta("ses_test"),
	}
}

func startHost(t *testing.T, cfg sessionhost.Config) <-chan error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sessionhost.Run(ctx, cfg)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(waitTimeout):
			t.Error("session host did not stop")
		}
	})
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(cfg.Socket)
		return err == nil
	}, waitTimeout)
	return done
}

func dial(t *testing.T, socket string) *testClient {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	require.NoError(t, err)
	c := &testClient{conn: conn, exit: make(chan int, 1)}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		c.readLoop()
	}()
	t.Cleanup(func() {
		conn.Close()
		<-stopped
	})
	return c
}

func (c *testClient) waitOutput(t *testing.T, substr string) {
	t.Helper()
	testutil.Eventually(t, func() bool { return strings.Contains(c.output(), substr) }, waitTimeout)
}

func (c *testClient) output() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.out.String()
}

func (c *testClient) resize(t *testing.T, cols, rows uint16) {
	t.Helper()
	require.NoError(t, wire.WriteFrame(c.conn, wire.Frame{Type: wire.FrameResize, Payload: wire.ResizePayload(cols, rows)}))
}

func (c *testClient) send(t *testing.T, s string) {
	t.Helper()
	require.NoError(t, wire.WriteFrame(c.conn, wire.Frame{Type: wire.FrameData, Payload: []byte(s)}))
}

func (c *testClient) waitExit(t *testing.T) int {
	t.Helper()
	select {
	case code := <-c.exit:
		return code
	case <-time.After(waitTimeout):
		require.FailNow(t, "no exit frame")
		return 0
	}
}

func (c *testClient) readLoop() {
	for {
		f, err := wire.ReadFrame(c.conn)
		if err != nil {
			return
		}
		switch f.Type {
		case wire.FrameData:
			c.mu.Lock()
			c.out.Write(f.Payload)
			c.mu.Unlock()
		case wire.FrameExit:
			code, err := wire.ParseExit(f.Payload)
			if err != nil {
				return
			}
			c.exit <- code
		}
	}
}
