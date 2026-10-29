package session_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/proc"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
)

func TestSessionSendRead(t *testing.T) {
	dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
	m := session.NewManager(dirs, bin)
	s := spawn(t, m, "sh", "-c", "exec cat")
	require.NoError(t, m.Send(t.Context(), s.ID, "hello", true))
	testutil.Eventually(t, func() bool {
		snap, err := m.Read(t.Context(), s.ID, 0, false)
		return err == nil && strings.Contains(snap.Text, "hello\nhello")
	}, 3*time.Second)
}

func TestSessionReadAltScreen(t *testing.T) {
	dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
	m := session.NewManager(dirs, bin)
	s := spawn(t, m, "sh", "-c", `printf 'before\033[?1049h'; exec cat`)
	testutil.Eventually(t, func() bool {
		snap, err := m.Read(t.Context(), s.ID, 0, false)
		return err == nil && snap == session.Snapshot{AltScreen: true}
	}, 3*time.Second)
}

func TestPlainText(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"csi":       {"a\x1b[1;31mred\x1b[0m b", "ared b"},
		"osc bel":   {"\x1b]0;title\x07x", "x"},
		"osc st":    {"\x1b]8;;http://a\x1b\\link\x1b]8;;\x1b\\", "link"},
		"esc seq":   {"\x1b(Bx\x1b=y", "xy"},
		"crlf":      {"a\r\nb", "a\nb"},
		"lone cr":   {"a\rb", "a\nb"},
		"controls":  {"a\x07\x08b\tc", "ab\tc"},
		"split utf": {"\xa4b", "b"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, session.Plain([]byte(tt.in)))
		})
	}
}

func TestSessionPathPrepended(t *testing.T) {
	binDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "clubmate"), []byte("#!/bin/sh\n"), 0o700))
	dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
	for dir, want := range map[string]bool{binDir: true, t.TempDir(): false} {
		s := spawn(t, session.NewManager(dirs, bin, session.WithPathDir(dir)), "sh", "-c", "exec cat")
		env, err := proc.ReadEnv(s.HostPID)
		require.NoError(t, err)
		i := slices.IndexFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "PATH=") })
		require.GreaterOrEqual(t, i, 0)
		assert.Equal(t, want, strings.HasPrefix(env[i], "PATH="+dir+":"), env[i])
	}
}
