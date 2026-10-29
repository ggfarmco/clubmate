package session_test

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/id"
	"clubmate/internal/proc"
	"clubmate/internal/session"
	"clubmate/internal/sessionhost"
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
		"csi":        {"a\x1b[1;31mred\x1b[0m b", "ared b"},
		"osc bel":    {"\x1b]0;title\x07x", "x"},
		"osc st":     {"\x1b]8;;http://a\x1b\\link\x1b]8;;\x1b\\", "link"},
		"esc seq":    {"\x1b(Bx\x1b=y", "xy"},
		"crlf":       {"a\r\nb", "a\nb"},
		"lone cr":    {"x\nab\rc", "x\nc"},
		"pending cr": {"x\n50%\r", "x\n50%"},
		"cr newline": {"ab\r\r\ncd", "ab\ncd"},
		"backspace":  {"x\nab\b\bé\bc\b\bd", "x\nd"},
		"esc ctrl":   {"a\x1b\nb", "a\nb"},
		"controls":   {"a\x07b\tc", "ab\tc"},
		"split utf":  {"\xa4b", "b"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, session.Plain([]byte(tt.in)))
		})
	}
}

func TestSessionReadOldHost(t *testing.T) {
	dirs := testutil.ShortDirs(t)
	sid := id.New(session.IDPrefix)
	require.NoError(t, sessionhost.WriteMeta(dirs.SessionMeta(sid), sessionhost.Meta{ID: sid, HostPID: os.Getpid()}))
	ln, err := net.Listen("unix", dirs.SessionSocket(sid))
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go io.Copy(io.Discard, conn)
		}
	}()
	m := session.NewManager(dirs, "", session.WithSnapshotTimeout(100*time.Millisecond))
	_, err = m.Read(t.Context(), sid, 0, false)
	assert.ErrorIs(t, err, session.ErrNoSnapshot)
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
