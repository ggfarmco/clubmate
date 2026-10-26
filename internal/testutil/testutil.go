package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"clubmate/internal/paths"
)

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

func Eventually(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, cond, timeout, 20*time.Millisecond)
}

func Alive(pid int) bool {
	return unix.Kill(pid, 0) == nil
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
