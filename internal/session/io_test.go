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

	"clubmate/internal/proc"
	"clubmate/internal/session"
	"clubmate/internal/sessionhost"
	"clubmate/internal/testutil"
	"clubmate/pkg/id"
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

func TestSessionSendDeliversEveryEnterWithBigRing(t *testing.T) {
	dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
	m := session.NewManager(dirs, bin)
	file := filepath.Join(t.TempDir(), "typed")
	s := spawn(t, m, "sh", "-c", `head -c 250000 /dev/zero | tr '\0' x; stty raw; exec cat > '`+file+`'`)
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(file)
		return err == nil
	}, 5*time.Second)
	const sends = 30
	for range sends {
		require.NoError(t, m.Send(t.Context(), s.ID, "a", true))
	}
	testutil.Eventually(t, func() bool {
		data, _ := os.ReadFile(file)
		return string(data) == strings.Repeat("a\r", sends)
	}, 5*time.Second)
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
		"csi":            {"a\x1b[1;31mred\x1b[0m b", "ared b"},
		"osc bel":        {"\x1b]0;title\x07x", "x"},
		"osc st":         {"\x1b]8;;http://a\x1b\\link\x1b]8;;\x1b\\", "link"},
		"esc seq":        {"\x1b(Bx\x1b=y", "xy"},
		"crlf":           {"a\r\nb", "a\nb"},
		"cr overwrite":   {"x\n50%\r75%", "x\n75%"},
		"pending cr":     {"x\n50%\r", "x\n50%"},
		"cr newline":     {"ab\r\r\ncd", "ab\ncd"},
		"backspace":      {"x\nab\b\bé\bc\b\bd", "x\ndb"},
		"esc ctrl":       {"a\x1b\nb", "a\nb"},
		"controls":       {"a\x07b\tc", "ab\tc"},
		"split utf":      {"\xa4b", "b"},
		"cha erase":      {"abcdef\x1b[3GX\x1b[K", "abX"},
		"cub cuf":        {"abcd\x1b[3DX\x1b[1CY", "aXcY"},
		"zsh redraw":     {"% gi\x1b[2D\x1b[1m\x1b[32mgit\x1b[0m status\b \b\bus\r\n", "% git status\n"},
		"line cap":       {strings.Repeat("x", 5000) + "\x1b[9999Cy\nz", strings.Repeat("x", 4096) + "\nz"},
		"param overflow": {"ab\x1b[99999999999999999999Dc", "cb"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, session.Plain([]byte(tt.in), session.ReadMax))
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

func TestPlainOutputIsBounded(t *testing.T) {
	in := []byte(strings.Repeat("\x1b[4096Gx\n", session.ReadMax/9))
	assert.LessOrEqual(t, len(session.Plain(in, session.ReadMax)), session.ReadMax)
	assert.Equal(t, "cd", session.Plain([]byte("ab\ncd"), 2))
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
