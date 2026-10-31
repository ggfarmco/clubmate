package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

func TestOwnedSocketAcceptsDistroArgv(t *testing.T) {
	t.Setenv("CLUBMATE_PLUGIN_RUNTIME_DIR", "/rt")
	sock, err := socketPath(pluginsdk.RuntimeDir(), "p1", "/repo")
	require.NoError(t, err)
	for _, cmd := range [][]string{{"nvim", "--listen", sock}, {"nvim", "-u", "/res/nvim-distro/init.lua", "--listen", sock}} {
		got, ok := ownedSocket(pluginsdk.Session{State: stateRunning, Cwd: "/repo", Command: cmd}, "p1", "/repo")
		assert.True(t, ok, cmd)
		assert.Equal(t, sock, got)
	}
	_, ok := ownedSocket(pluginsdk.Session{State: stateRunning, Cwd: "/repo", Command: []string{"nvim", "-u", sock}}, "p1", "/repo")
	assert.False(t, ok)
}
