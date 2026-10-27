package shellenv_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"clubmate/internal/shellenv"
)

func fakeShell(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shell")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	return path
}

func TestResolveMergesLoginShell(t *testing.T) {
	shell := fakeShell(t, `echo noise
printf '\n__CLUBMATE_ENV__\n'
printf 'PATH=/fake/bin\0GHOSTTY_RESOURCES_DIR=/x\0CLUBMATE_STATE_DIR=/evil\0'`)

	resolved, err := shellenv.Resolve(context.Background(), shell)
	require.NoError(t, err)

	merged := shellenv.Merge([]string{"CLUBMATE_STATE_DIR=/good", "PATH=/usr/bin"}, resolved)

	require.Contains(t, merged, "PATH=/fake/bin")
	require.Contains(t, merged, "CLUBMATE_STATE_DIR=/good")
	require.NotContains(t, merged, "GHOSTTY_RESOURCES_DIR=/x")
	require.NotContains(t, merged, "CLUBMATE_STATE_DIR=/evil")
}

func TestResolveTimesOut(t *testing.T) {
	shell := fakeShell(t, "sleep 5")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := shellenv.Resolve(ctx, shell)

	require.Error(t, err)
	require.Less(t, time.Since(start), time.Second)
}
