package testutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneBinCacheDropsOnlyStaleEntries(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "stale")
	fresh := filepath.Join(dir, "fresh")
	require.NoError(t, os.Mkdir(stale, 0o700))
	require.NoError(t, os.Mkdir(fresh, 0o700))
	old := time.Now().Add(-binCacheTTL - time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))

	pruneBinCache(dir, time.Now().Add(-binCacheTTL))

	assert.NoDirExists(t, stale)
	assert.DirExists(t, fresh)
}

func TestPruneBinCacheDropsEntriesOfRemovedRoots(t *testing.T) {
	dir := t.TempDir()
	liveRoot := t.TempDir()
	gone := filepath.Join(dir, "gone")
	live := filepath.Join(dir, "live")
	empty := filepath.Join(dir, "empty")
	for entry, root := range map[string]string{gone: filepath.Join(liveRoot, "removed"), live: liveRoot, empty: ""} {
		require.NoError(t, os.Mkdir(entry, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(entry, binRootMarker), []byte(root), 0o600))
	}

	pruneBinCache(dir, time.Now().Add(-binCacheTTL))

	assert.NoDirExists(t, gone)
	assert.DirExists(t, live)
	assert.DirExists(t, empty)
}
