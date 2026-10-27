package rpc_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

func TestUIFocusNotifiesProjectSubscribers(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDirAs(t, manifestJSON("echo", `{"sections":[{"id":"echo","title":"Echo"}]}`)))
	p := h.CreateProject(t, "p")
	q := h.CreateProject(t, "q")
	pc := h.Client(t)
	qc, err := rpc.Dial(t.Context(), h.Dirs.CoreSocket())
	require.NoError(t, err)
	t.Cleanup(func() { qc.Close() })
	var psub, qsub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, pc.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &psub))
	require.NoError(t, qc.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": q.ID}, &qsub))
	focus := func(projectID, slot string) (res struct {
		Delivered int `json:"delivered"`
	}, err error) {
		err = pc.Call(t.Context(), "ui.focus", map[string]string{"projectId": projectID, "section": "echo", "slot": slot}, &res)
		return res, err
	}
	res, err := focus(p.ID, "echo.sidebar")
	require.NoError(t, err)
	assert.Equal(t, 1, res.Delivered)
	select {
	case n := <-pc.Notifications():
		assert.Equal(t, "ui.focus", n.Method)
		assert.JSONEq(t, fmt.Sprintf(`{"subscription":%q,"section":"echo","slot":"echo.sidebar"}`, psub.Subscription), string(n.Params))
	case <-time.After(2 * time.Second):
		t.Fatal("no ui.focus notification")
	}
	select {
	case n := <-qc.Notifications():
		t.Fatalf("unexpected notification %s", n.Method)
	case <-time.After(100 * time.Millisecond):
	}
	_, err = focus(p.ID, "echo.status")
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
	_, err = focus("prj_0123456789abcdef", "echo.main")
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
}

func TestProjectOpenCountsRegisteredClients(t *testing.T) {
	srv, _ := testutil.NewRPCServer(t)
	dirs := testutil.ShortDirs(t)
	l, err := net.Listen("unix", dirs.CoreSocket())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, l) }()
	listener, err := rpc.Dial(t.Context(), dirs.CoreSocket())
	require.NoError(t, err)
	caller, err := rpc.Dial(t.Context(), dirs.CoreSocket())
	require.NoError(t, err)
	t.Cleanup(func() {
		listener.Close()
		caller.Close()
		cancel()
		<-done
	})
	var p project.Project
	require.NoError(t, caller.Call(t.Context(), "project.create", map[string]any{"name": "demo"}, &p))
	var res struct {
		Clients int `json:"clients"`
	}
	require.NoError(t, caller.Call(t.Context(), "project.open", map[string]string{"id": p.ID}, &res))
	assert.Equal(t, 0, res.Clients)
	select {
	case n := <-listener.Notifications():
		t.Fatalf("unexpected notification %s", n.Method)
	case <-time.After(100 * time.Millisecond):
	}
	err = listener.Call(t.Context(), "client.register", map[string]any{"handles": []string{"x"}}, nil)
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
	require.NoError(t, listener.Call(t.Context(), "client.register", map[string]any{"handles": []string{"project.open"}}, nil))
	require.NoError(t, caller.Call(t.Context(), "project.open", map[string]string{"id": p.ID}, &res))
	assert.Equal(t, 1, res.Clients)
	select {
	case n := <-listener.Notifications():
		assert.Equal(t, "project.open", n.Method)
		assert.JSONEq(t, fmt.Sprintf(`{"id":%q}`, p.ID), string(n.Params))
	case <-time.After(2 * time.Second):
		t.Fatal("no project.open notification")
	}
	err = caller.Call(t.Context(), "project.open", map[string]string{"id": "prj_0123456789abcdef"}, &res)
	assert.ErrorIs(t, err, rpc.ErrNotFound)
}
