package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

func TestOwnedSocketAcceptsDistroArgv(t *testing.T) {
	t.Setenv("CLUBMATE_PLUGIN_RUNTIME_DIR", "/rt")
	sock, err := socketPath(pluginsdk.RuntimeDir(), "p1", "/repo")
	require.NoError(t, err)
	for _, cmd := range [][]string{{"nvim", "--listen", sock}, {"nvim", "-u", "/res/nvim-distro/init.lua", "--listen", sock}} {
		got, ok := ownedSocket(pluginsdk.Session{State: contract.SessionStateRunning, Cwd: "/repo", Command: cmd}, "p1", "/repo")
		assert.True(t, ok, cmd)
		assert.Equal(t, sock, got)
	}
	_, ok := ownedSocket(pluginsdk.Session{State: contract.SessionStateRunning, Cwd: "/repo", Command: []string{"nvim", "-u", sock}}, "p1", "/repo")
	assert.False(t, ok)
}

func TestDialWaitsUntilSocketListens(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "cmdial")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "n.sock")
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = unix.Close(fd) })
	require.NoError(t, unix.Bind(fd, &unix.SockaddrUnix{Name: sock}))
	dialed := make(chan error, 1)
	go func() {
		v, err := dial(t.Context(), sock)
		if err == nil {
			_ = v.Close()
		}
		dialed <- err
	}()
	select {
	case err := <-dialed:
		require.Failf(t, "dial returned before listen", "%v", err)
	case <-time.After(4 * socketPoll):
	}
	require.NoError(t, unix.Listen(fd, 1))
	select {
	case err := <-dialed:
		require.NoError(t, err)
	case <-time.After(socketWait):
		require.Fail(t, "dial did not finish after listen")
	}
}
