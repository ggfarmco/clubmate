package paths_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestResolveDefaults(t *testing.T) {
	t.Setenv("CLUBMATE_STATE_DIR", "")
	t.Setenv("CLUBMATE_RUNTIME_DIR", "")
	home, _ := os.UserHomeDir()
	d, err := paths.Resolve()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "Library/Application Support/clubmate"), d.State)
	assert.Equal(t, "/tmp/clubmate-"+strconv.Itoa(os.Getuid()), d.Runtime)
}

func TestEnsureRejectsLongRuntimeDir(t *testing.T) {
	d := paths.Dirs{State: t.TempDir(), Runtime: "/tmp/" + strings.Repeat("a", 100)}
	assert.ErrorIs(t, d.Ensure(), paths.ErrSocketPathTooLong)
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
