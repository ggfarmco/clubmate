package rpc_test

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

func startServer(t *testing.T) *rpc.Client {
	t.Helper()
	svc := testutil.NewService(t)
	dirs := testutil.ShortDirs(t)
	l, err := net.Listen("unix", dirs.CoreSocket())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rpc.Serve(ctx, l, svc) }()
	c, err := rpc.Dial(ctx, dirs.CoreSocket())
	require.NoError(t, err)
	t.Cleanup(func() {
		c.Close()
		cancel()
		assert.NoError(t, <-done)
	})
	return c
}

func TestRPCProjectRoundTrip(t *testing.T) {
	c := startServer(t)
	var p project.Project
	require.NoError(t, c.Call(t.Context(), "project.create", map[string]any{"name": "demo", "repos": []string{t.TempDir()}}, &p))
	var list []project.Project
	require.NoError(t, c.Call(t.Context(), "project.list", struct{}{}, &list))
	require.Len(t, list, 1)
	assert.Equal(t, p.ID, list[0].ID)
}

func TestRPCPing(t *testing.T) {
	c := startServer(t)
	var out struct {
		Version string `json:"version"`
	}
	require.NoError(t, c.Call(t.Context(), "core.ping", struct{}{}, &out))
	assert.Equal(t, rpc.Version, out.Version)
}

func TestRPCNotFoundMapsToSentinel(t *testing.T) {
	c := startServer(t)
	var p project.Project
	err := c.Call(t.Context(), "project.get", map[string]string{"id": "prj_missing"}, &p)
	assert.ErrorIs(t, err, rpc.ErrNotFound)
}

func TestRPCErrorCodes(t *testing.T) {
	c := startServer(t)
	tests := []struct {
		name   string
		method string
		params any
		want   error
	}{
		{"not tracked", "process.kill", map[string]any{"pids": []int{1}}, rpc.ErrNotTracked},
		{"domain validation", "project.create", map[string]any{"name": ""}, rpc.ErrInvalidParams},
		{"bad params", "project.get", "oops", rpc.ErrInvalidParams},
		{"unknown method", "nope.nope", struct{}{}, rpc.ErrMethodNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Call(t.Context(), tt.method, tt.params, nil)
			assert.ErrorIs(t, err, tt.want)
		})
	}
}

func TestRPCServeStopsOnContextCancel(t *testing.T) {
	svc := testutil.NewService(t)
	dirs := testutil.ShortDirs(t)
	l, err := net.Listen("unix", dirs.CoreSocket())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rpc.Serve(ctx, l, svc) }()
	c, err := rpc.Dial(ctx, dirs.CoreSocket())
	require.NoError(t, err)
	defer c.Close()
	cancel()
	require.NoError(t, <-done)
	assert.Error(t, c.Call(context.Background(), "core.ping", struct{}{}, nil))
}
