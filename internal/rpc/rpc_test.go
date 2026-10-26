package rpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/session"
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

func TestRPCEmptyListsEncodeAsArrays(t *testing.T) {
	c := startServer(t)
	for _, method := range []string{"project.list", "session.list", "process.tree"} {
		var raw json.RawMessage
		require.NoError(t, c.Call(t.Context(), method, struct{}{}, &raw))
		assert.JSONEq(t, `[]`, string(raw), method)
	}
	var p json.RawMessage
	require.NoError(t, c.Call(t.Context(), "project.create", map[string]any{"name": "bare"}, &p))
	assert.Contains(t, string(p), `"repos":[]`)
}

func TestRPCSessionCreateUsesCamelCaseParams(t *testing.T) {
	c := startServer(t)
	var p project.Project
	require.NoError(t, c.Call(t.Context(), "project.create", map[string]any{"name": "demo", "repos": []string{t.TempDir()}}, &p))
	var s session.Session
	require.NoError(t, c.Call(t.Context(), "session.create", map[string]any{"projectId": p.ID, "command": []string{"sh", "-c", "exec cat"}}, &s))
	t.Cleanup(func() { c.Call(context.Background(), "session.kill", map[string]string{"id": s.ID}, nil) })
	assert.Equal(t, p.ID, s.ProjectID)
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
	err := c.Call(t.Context(), "project.get", map[string]string{"id": "prj_0000000000000000"}, &p)
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

func createProject(t *testing.T, c *rpc.Client) project.Project {
	t.Helper()
	var p project.Project
	require.NoError(t, c.Call(t.Context(), "project.create", map[string]any{"name": "demo", "repos": []string{t.TempDir()}}, &p))
	return p
}

func TestRPCSessionRemove(t *testing.T) {
	c := startServer(t)
	p := createProject(t, c)
	var s session.Session
	require.NoError(t, c.Call(t.Context(), "session.create", map[string]any{"projectId": p.ID, "command": []string{"true"}}, &s))
	testutil.Eventually(t, func() bool {
		var got session.Session
		_ = c.Call(t.Context(), "session.get", map[string]string{"id": s.ID}, &got)
		return got.State == session.StateExited
	}, 3*time.Second)
	require.NoError(t, c.Call(t.Context(), "session.remove", map[string]string{"id": s.ID}, nil))
	err := c.Call(t.Context(), "session.get", map[string]string{"id": s.ID}, &s)
	assert.ErrorIs(t, err, rpc.ErrNotFound)
}

func TestRPCSessionRemoveRunningMapsToSentinel(t *testing.T) {
	c := startServer(t)
	p := createProject(t, c)
	var s session.Session
	require.NoError(t, c.Call(t.Context(), "session.create", map[string]any{"projectId": p.ID, "command": []string{"sh", "-c", "exec cat"}}, &s))
	t.Cleanup(func() { c.Call(context.Background(), "session.kill", map[string]string{"id": s.ID}, nil) })
	err := c.Call(t.Context(), "session.remove", map[string]string{"id": s.ID}, nil)
	assert.ErrorIs(t, err, rpc.ErrSessionRunning)
}

func TestRPCRejectsMalformedIDs(t *testing.T) {
	c := startServer(t)
	for _, m := range []string{"project.get", "project.delete", "project.addRepo", "session.get", "session.kill", "session.remove"} {
		err := c.Call(t.Context(), m, map[string]string{"id": "../etc"}, nil)
		assert.ErrorIs(t, err, rpc.ErrInvalidParams, m)
	}
	for _, tt := range []struct {
		method string
		params map[string]string
	}{
		{"session.list", map[string]string{"projectId": "../etc"}},
		{"session.create", map[string]string{"projectId": "../etc"}},
		{"process.tree", map[string]string{"sessionId": "../etc"}},
		{"process.tree", map[string]string{"projectId": "ses_0123456789abcdef"}},
		{"process.kill", map[string]string{"sessionId": "../etc"}},
	} {
		err := c.Call(t.Context(), tt.method, tt.params, nil)
		assert.ErrorIs(t, err, rpc.ErrInvalidParams, tt.method)
	}
}

type flakyListener struct {
	net.Listener
	errs []error
}

func (l *flakyListener) Accept() (net.Conn, error) {
	if len(l.errs) > 0 {
		err := l.errs[0]
		l.errs = l.errs[1:]
		return nil, err
	}
	return l.Listener.Accept()
}

func TestRPCServeRetriesTransientAcceptErrors(t *testing.T) {
	svc := testutil.NewService(t)
	dirs := testutil.ShortDirs(t)
	inner, err := net.Listen("unix", dirs.CoreSocket())
	require.NoError(t, err)
	l := &flakyListener{Listener: inner, errs: []error{syscall.EMFILE, os.ErrDeadlineExceeded}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rpc.Serve(ctx, l, svc) }()
	c, err := rpc.Dial(ctx, dirs.CoreSocket())
	require.NoError(t, err)
	require.NoError(t, c.Call(t.Context(), "core.ping", struct{}{}, nil))
	c.Close()
	cancel()
	require.NoError(t, <-done)
}

func TestRPCServeReturnsOnFatalAcceptError(t *testing.T) {
	svc := testutil.NewService(t)
	dirs := testutil.ShortDirs(t)
	inner, err := net.Listen("unix", dirs.CoreSocket())
	require.NoError(t, err)
	l := &flakyListener{Listener: inner, errs: []error{errors.New("boom")}}
	defer inner.Close()
	assert.ErrorContains(t, rpc.Serve(t.Context(), l, svc), "boom")
}
