package claudecli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAskReturnsWhileAChildHoldsStdout(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	script := fmt.Sprintf("#!/bin/sh\n/bin/sleep 20 &\n: > %q\nwait\n", ready)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		for {
			if _, err := os.Stat(ready); err == nil {
				cancel()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()

	start := time.Now()
	_, err := Ask(ctx, Haiku, "system", "input")

	require.Error(t, err)
	assert.Less(t, time.Since(start), 10*time.Second)
}

func TestFindSkipsHomePathsWithoutHome(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".local", "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".local", "bin", "claude"), []byte("#!/bin/sh\n"), 0o755))
	t.Chdir(dir)
	t.Setenv("HOME", "")
	t.Setenv("PATH", t.TempDir())

	path, err := Find()

	if err == nil {
		assert.True(t, filepath.IsAbs(path), path)
	}
}

func TestAskStartsNoClaudeInTestMode(t *testing.T) {
	dir := t.TempDir()
	ran := filepath.Join(dir, "ran")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(fmt.Sprintf("#!/bin/sh\n: > %q\necho summary\n", ran)), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLUBMATE_TEST_MODE", "1")

	for flavor, offline := range map[string]bool{"dev": true, "release": false} {
		t.Setenv("CLUBMATE_FLAVOR", flavor)
		os.Remove(ran)

		_, err := Ask(t.Context(), Haiku, "system", "input")

		if offline {
			assert.ErrorIs(t, err, ErrMissing, flavor)
			assert.NoFileExists(t, ran, flavor)
			continue
		}
		require.NoError(t, err, flavor)
		assert.FileExists(t, ran, flavor)
	}
}
