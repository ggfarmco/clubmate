package paths_test

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/buildinfo"
	"clubmate/internal/paths"
	"clubmate/internal/testutil"
)

func TestResolveUsesEnvOverrides(t *testing.T) {
	t.Setenv("CLUBMATE_STATE_DIR", "/x/state")
	t.Setenv("CLUBMATE_RUNTIME_DIR", "/tmp/cm-rt")
	d, err := paths.Resolve()
	require.NoError(t, err)
	assert.Equal(t, "/x/state/clubmate.db", d.DB())
	assert.Equal(t, "/tmp/cm-rt/core.sock", d.CoreSocket())
	assert.Equal(t, "/tmp/cm-rt/s/ses_1.sock", d.SessionSocket("ses_1"))
	assert.Equal(t, "/x/state/sessions/ses_1.json", d.SessionMeta("ses_1"))
}

func TestResolveFlavor(t *testing.T) {
	home, _ := os.UserHomeDir()
	uid := strconv.Itoa(os.Getuid())
	tests := map[string]struct {
		flavor      string
		wantState   string
		wantRuntime string
	}{
		"release": {flavor: "release", wantState: filepath.Join(home, "Library/Application Support/clubmate"), wantRuntime: "/tmp/clubmate-" + uid},
		"dev":     {flavor: "dev", wantState: filepath.Join(home, "Library/Application Support/clubmate-dev"), wantRuntime: "/tmp/clubmate-dev-" + uid},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			prev := buildinfo.Flavor
			buildinfo.Flavor = tt.flavor
			t.Cleanup(func() { buildinfo.Flavor = prev })
			t.Setenv("CLUBMATE_STATE_DIR", "")
			t.Setenv("CLUBMATE_RUNTIME_DIR", "")
			d, err := paths.Resolve()
			require.NoError(t, err)
			assert.Equal(t, tt.wantState, d.State)
			assert.Equal(t, tt.wantRuntime, d.Runtime)
		})
	}
}

func TestEnsureRejectsLongRuntimeDir(t *testing.T) {
	d := paths.Dirs{State: t.TempDir(), Runtime: "/tmp/" + strings.Repeat("a", 100)}
	assert.ErrorIs(t, d.Ensure(), paths.ErrSocketPathTooLong)
}

func TestEnsureRejectsInsecureRuntimeDir(t *testing.T) {
	tests := map[string]func(t *testing.T, base string) string{
		"loose mode": func(t *testing.T, base string) string {
			dir := filepath.Join(base, "rt")
			require.NoError(t, os.Mkdir(dir, 0o755))
			require.NoError(t, os.Chmod(dir, 0o755))
			return dir
		},
		"symlink": func(t *testing.T, base string) string {
			target := filepath.Join(base, "target")
			require.NoError(t, os.Mkdir(target, 0o700))
			link := filepath.Join(base, "link")
			require.NoError(t, os.Symlink(target, link))
			return link
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			base, err := os.MkdirTemp("/tmp", "cmt")
			require.NoError(t, err)
			t.Cleanup(func() { os.RemoveAll(base) })
			d := paths.Dirs{State: t.TempDir(), Runtime: setup(t, base)}
			assert.ErrorIs(t, d.Ensure(), paths.ErrInsecureRuntimeDir)
		})
	}
}

func TestLockCoreIsExclusive(t *testing.T) {
	d := testutil.ShortDirs(t)
	unlock, err := paths.LockCore(d)
	require.NoError(t, err)
	_, err = paths.LockCore(d)
	assert.ErrorIs(t, err, paths.ErrCoreRunning)
	unlock()
	unlock2, err := paths.LockCore(d)
	require.NoError(t, err)
	unlock2()
}

func TestCheckRuntimeRejectsInsecureDir(t *testing.T) {
	d := testutil.ShortDirs(t)
	require.NoError(t, d.CheckRuntime())
	require.NoError(t, os.Chmod(d.Runtime, 0o755))
	assert.ErrorIs(t, d.CheckRuntime(), paths.ErrInsecureRuntimeDir)
}

func TestVerifyPeerAcceptsSameUID(t *testing.T) {
	d := testutil.ShortDirs(t)
	l, err := net.Listen("unix", d.CoreSocket())
	require.NoError(t, err)
	defer l.Close()
	c, err := net.Dial("unix", d.CoreSocket())
	require.NoError(t, err)
	defer c.Close()
	assert.NoError(t, paths.VerifyPeer(c.(*net.UnixConn)))
}
