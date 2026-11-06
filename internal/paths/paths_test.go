package paths_test

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/buildinfo"
	"clubmate/internal/bus"
	"clubmate/internal/kv"
	"clubmate/internal/paths"
	"clubmate/internal/testutil"
	"clubmate/internal/workspace"
)

func TestStoresKeepTheirDatabasePrivate(t *testing.T) {
	stores := map[string]func(string) (io.Closer, error){
		"kv":        func(p string) (io.Closer, error) { return kv.Open(p) },
		"bus":       func(p string) (io.Closer, error) { return bus.Open(p) },
		"workspace": func(p string) (io.Closer, error) { return workspace.Open(p) },
	}
	files := []string{"", "-wal", "-shm"}
	for name, open := range stores {
		for _, existing := range []bool{false, true} {
			t.Run(name+"/existing="+strconv.FormatBool(existing), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "x.db")
				if existing {
					require.NoError(t, os.WriteFile(path, nil, 0o644))
				}

				store, err := open(path)
				require.NoError(t, err)
				t.Cleanup(func() { store.Close() })

				for _, suffix := range files {
					fi, err := os.Stat(path + suffix)
					require.NoError(t, err)
					assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), suffix)
				}
			})
		}
	}
}

func TestPrivateDBTightensAWALLeftByACrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		require.NoError(t, os.WriteFile(path+suffix, []byte("x"), 0o644))
	}

	require.NoError(t, paths.PrivateDB(path))

	for _, suffix := range []string{"", "-wal", "-shm"} {
		fi, err := os.Stat(path + suffix)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), suffix)
	}
}

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
