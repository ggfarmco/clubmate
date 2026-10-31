package main

import (
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

func TestSwitchHoldsBusyGateWhileSettingWorktree(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	feat := filepath.Join(repo, ".worktrees", "feat")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
		{"worktree", "add", "-q", "-b", "feat", feat},
	} {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	var s state
	busyDuringSet := make(chan bool, 1)
	h := fakeCore(t, func(req *jsonrpc2.Request) (any, error) {
		p := pluginsdk.Project{ID: "p", Repos: []string{repo}, Roots: []string{repo}}
		if req.Method == "workspace.worktree.set" {
			busyDuringSet <- !s.claim()
		}
		return p, nil
	})

	require.NoError(t, switchWorktree(t.Context(), h, "p", &s, "", feat))
	assert.True(t, <-busyDuringSet)
	assert.True(t, s.claim())
}

func fakeCore(t *testing.T, handle func(*jsonrpc2.Request) (any, error)) *pluginsdk.Host {
	t.Helper()
	coreSide, pluginSide := net.Pipe()
	handler := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		return handle(req)
	})
	conn := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), handler)
	t.Cleanup(func() { conn.Close() })
	ready := make(chan *pluginsdk.Host, 1)
	init := func(_ context.Context, h *pluginsdk.Host) error {
		ready <- h
		return nil
	}
	go func() { _ = pluginsdk.RunConn(t.Context(), pluginSide, pluginsdk.Plugin{Init: init}) }()
	require.NoError(t, conn.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": pluginsdk.ProtocolVersion, "pluginId": "git"}, nil))
	select {
	case h := <-ready:
		return h
	case <-time.After(3 * time.Second):
		t.Fatal("Init not called")
		return nil
	}
}
