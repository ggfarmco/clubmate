package testutil

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"clubmate/internal/bus"
	"clubmate/internal/core"
	"clubmate/internal/paths"
	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/session"
)

const testKillGrace = 300 * time.Millisecond

type builtBinary struct {
	once sync.Once
	path string
	err  error
}

var (
	buildsMu sync.Mutex
	builds   = map[string]*builtBinary{}
)

func ShortDirs(t *testing.T) paths.Dirs {
	t.Helper()
	runtime, err := os.MkdirTemp("/tmp", "cmt")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(runtime) })
	d := paths.Dirs{State: t.TempDir(), Runtime: runtime}
	require.NoError(t, d.Ensure())
	return d
}

func NewService(t *testing.T) *core.Service {
	t.Helper()
	return newService(t)
}

func NewServiceWithBus(t *testing.T, opts ...core.Option) (*core.Service, *bus.Bus) {
	t.Helper()
	return NewServiceWithBusIn(t, ShortDirs(t), opts...)
}

func NewServiceWithBusIn(t *testing.T, dirs paths.Dirs, opts ...core.Option) (*core.Service, *bus.Bus) {
	t.Helper()
	b, err := bus.Open(filepath.Join(t.TempDir(), "bus.db"))
	require.NoError(t, err)
	t.Cleanup(func() { b.Close() })
	return newServiceIn(t, dirs, append(opts, core.WithBus(b))...), b
}

func NewRPCServer(t *testing.T) (*rpc.Server, *bus.Bus) {
	t.Helper()
	return NewRPCServerIn(t, ShortDirs(t))
}

func NewRPCServerIn(t *testing.T, dirs paths.Dirs) (*rpc.Server, *bus.Bus) {
	t.Helper()
	svc, b := NewServiceWithBusIn(t, dirs)
	return rpc.NewServer(svc, b, rpc.WithDirs(dirs)), b
}

func newService(t *testing.T, opts ...core.Option) *core.Service {
	t.Helper()
	return newServiceIn(t, ShortDirs(t), opts...)
}

func newServiceIn(t *testing.T, dirs paths.Dirs, opts ...core.Option) *core.Service {
	t.Helper()
	bin := BuildBinary(t, "clubmate/cmd/clubmate-session")
	store, err := project.Open(dirs.DB())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	tracker := proc.NewTracker(proc.ReadEnv, os.Getpid())
	svc := core.NewService(store, session.NewManager(dirs, bin), tracker, proc.Snapshot, append(opts, core.WithKillGrace(testKillGrace))...)
	t.Cleanup(func() { killLeftovers(svc) })
	return svc
}

func BuildBinary(t *testing.T, pkg string) string {
	t.Helper()
	buildsMu.Lock()
	b, ok := builds[pkg]
	if !ok {
		b = &builtBinary{}
		builds[pkg] = b
	}
	buildsMu.Unlock()
	b.once.Do(func() { b.path, b.err = build(pkg) })
	require.NoError(t, b.err)
	return b.path
}

func EchoPluginDir(t *testing.T) string {
	t.Helper()
	bin := BuildBinary(t, "clubmate/internal/testutil/plugins/echo")
	_, self, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := t.TempDir()
	dir := filepath.Join(root, "echo")
	require.NoError(t, os.Mkdir(dir, 0o700))
	copyFile(t, bin, filepath.Join(dir, "echo"), 0o755)
	copyFile(t, filepath.Join(filepath.Dir(self), "plugins", "echo", "plugin.json"), filepath.Join(dir, "plugin.json"), 0o600)
	return root
}

func EchoPluginDirAs(t *testing.T, manifests ...string) string {
	t.Helper()
	bin := BuildBinary(t, "clubmate/internal/testutil/plugins/echo")
	root := t.TempDir()
	for _, manifest := range manifests {
		var m struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal([]byte(manifest), &m))
		dir := filepath.Join(root, m.ID)
		require.NoError(t, os.Mkdir(dir, 0o700))
		copyFile(t, bin, filepath.Join(dir, "echo"), 0o755)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o600))
	}
	return root
}

func BuildPluginDir(t *testing.T, pkg, manifestPath string) string {
	t.Helper()
	bin := BuildBinary(t, pkg)
	manifest := filepath.Join(moduleRoot(t), manifestPath)
	var m struct {
		ID string `json:"id"`
	}
	data, err := os.ReadFile(manifest)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &m))
	root := t.TempDir()
	dir := filepath.Join(root, m.ID)
	require.NoError(t, os.Mkdir(dir, 0o700))
	copyFile(t, bin, filepath.Join(dir, filepath.Base(pkg)), 0o755)
	copyFile(t, manifest, filepath.Join(dir, "plugin.json"), 0o600)
	return root
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	require.NoError(t, err)
	return filepath.Dir(strings.TrimSpace(string(gomod)))
}

func Eventually(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, cond, timeout, 20*time.Millisecond)
}

func Alive(pid int) bool {
	return unix.Kill(pid, 0) == nil
}

func copyFile(t *testing.T, src, dst string, perm os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, data, perm))
}

func StartOrphanSession(t *testing.T, svc *core.Service, projectID string) int {
	t.Helper()
	sess, err := svc.CreateSession(t.Context(), session.CreateRequest{ProjectID: projectID, Command: []string{"/bin/sh", "-c", "(sleep 60 &); sleep 300"}})
	require.NoError(t, err)
	orphan := 0
	Eventually(t, func() bool {
		nodes, err := svc.ProcessTree(t.Context(), core.TreeFilter{SessionID: sess.ID})
		if err != nil {
			return false
		}
		for _, n := range nodes {
			if n.Orphan && n.Process.Comm == "sleep" {
				orphan = n.Process.PID
				return true
			}
		}
		return false
	}, 5*time.Second)
	return orphan
}

func killLeftovers(svc *core.Service) {
	ctx := context.Background()
	sessions, _ := svc.ListSessions(ctx, session.ListFilter{})
	nodes, _ := svc.ProcessTree(ctx, core.TreeFilter{})
	for _, n := range nodes {
		_ = unix.Kill(n.Process.PID, unix.SIGKILL)
	}
	for _, s := range sessions {
		if s.State != session.StateRunning || s.PGID <= 1 || s.HostPID <= 1 {
			continue
		}
		_ = unix.Kill(-s.PGID, unix.SIGKILL)
		_ = unix.Kill(s.HostPID, unix.SIGKILL)
	}
}

func build(pkg string) (string, error) {
	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", err
	}
	root := filepath.Dir(strings.TrimSpace(string(gomod)))
	dir, err := os.MkdirTemp("", "cmbin")
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, filepath.Base(pkg))
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = root
	if msg, err := cmd.CombinedOutput(); err != nil {
		return "", &buildError{pkg: pkg, output: string(msg), err: err}
	}
	return out, nil
}

type buildError struct {
	pkg    string
	output string
	err    error
}

func (e *buildError) Error() string {
	return "build " + e.pkg + ": " + e.err.Error() + "\n" + e.output
}

func (e *buildError) Unwrap() error { return e.err }
