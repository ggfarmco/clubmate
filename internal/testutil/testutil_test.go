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
