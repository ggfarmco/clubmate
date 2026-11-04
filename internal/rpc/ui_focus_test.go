package rpc_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
	"clubmate/internal/workspace"
)

func TestUIFocusNotifiesWorkspaceSubscribers(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDirAs(t, manifestJSON("echo", `{"sections":[{"id":"echo","title":"Echo"},{"id":"echo.glob","title":"Glob","scope":"app"}]}`)))
	p := h.CreateWorkspace(t, "p")
	q := h.CreateWorkspace(t, "q")
	pc := h.Client(t)
	qc, err := rpc.Dial(t.Context(), h.Dirs.CoreSocket())
	require.NoError(t, err)
	t.Cleanup(func() { qc.Close() })
	var psub, qsub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, pc.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, &psub))
	require.NoError(t, qc.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": q.ID}, &qsub))
	ac, err := rpc.Dial(t.Context(), h.Dirs.CoreSocket())
	require.NoError(t, err)
	t.Cleanup(func() { ac.Close() })
	require.NoError(t, ac.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": ""}, nil))
	focus := func(workspaceID, slot string) (res struct {
		Delivered int `json:"delivered"`
	}, err error) {
		err = pc.Call(t.Context(), "ui.focus", map[string]string{"workspaceId": workspaceID, "section": "echo", "slot": slot}, &res)
		return res, err
	}
	res, err := focus(p.ID, "echo.sidebar")
	require.NoError(t, err)
	assert.Equal(t, 1, res.Delivered)
	n, ok := nextFocus(pc, 2*time.Second)
	require.True(t, ok, "no ui.focus notification")
	assert.JSONEq(t, fmt.Sprintf(`{"subscription":%q,"section":"echo","slot":"echo.sidebar"}`, psub.Subscription), string(n.Params))
	_, ok = nextFocus(qc, 100*time.Millisecond)
	assert.False(t, ok)
	_, ok = nextFocus(ac, 100*time.Millisecond)
	assert.False(t, ok)
	err = pc.Call(t.Context(), "ui.focus", map[string]string{"workspaceId": p.ID, "section": "echo.glob", "slot": ""}, nil)
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
	_, err = focus(p.ID, "echo.status")
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
	_, err = focus("prj_0123456789abcdef", "echo.main")
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
}

func TestUIFocusFieldTargetsSidebarField(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDirAs(t, manifestJSON("echo", `{"sections":[{"id":"echo","title":"Echo"}]}`)))
	p := h.CreateWorkspace(t, "p")
	c := h.Client(t)
	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, &sub))
	require.NoError(t, c.Call(t.Context(), "ui.focus", map[string]any{"workspaceId": p.ID, "section": "echo", "slot": "echo.sidebar", "field": true}, nil))
	n, ok := nextFocus(c, 2*time.Second)
	require.True(t, ok, "no ui.focus notification")
	assert.JSONEq(t, fmt.Sprintf(`{"subscription":%q,"section":"echo","slot":"echo.sidebar","field":true}`, sub.Subscription), string(n.Params))
	err := c.Call(t.Context(), "ui.focus", map[string]any{"workspaceId": p.ID, "section": "echo", "slot": "echo.main", "field": true}, nil)
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
}

func TestUIFocusNodeNamesItsTarget(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDirAs(t, manifestJSON("echo", `{"sections":[{"id":"echo","title":"Echo"}]}`)))
	p := h.CreateWorkspace(t, "p")
	c := h.Client(t)
	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, &sub))
	require.NoError(t, c.Call(t.Context(), "ui.focus", map[string]any{"workspaceId": p.ID, "section": "echo", "slot": "echo.main", "node": "s1/composer"}, nil))
	n, ok := nextFocus(c, 2*time.Second)
	require.True(t, ok, "no ui.focus notification")
	assert.JSONEq(t, fmt.Sprintf(`{"subscription":%q,"section":"echo","slot":"echo.main","node":"s1/composer"}`, sub.Subscription), string(n.Params))
	err := c.Call(t.Context(), "ui.focus", map[string]any{"workspaceId": p.ID, "section": "echo", "slot": "", "node": "s1/composer"}, nil)
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
}

func TestUIFocusRailOffSectionOnlyPopup(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDirAs(t, manifestJSON("echo", `{"sections":[{"id":"echo","title":"Echo"},{"id":"echo.docs","title":"Docs","rail":false,"slots":["popup"]}]}`)))
	p := h.CreateWorkspace(t, "p")
	c := h.Client(t)
	focus := func(section, slot string) error {
		return c.Call(t.Context(), "ui.focus", map[string]string{"workspaceId": p.ID, "section": section, "slot": slot}, nil)
	}
	require.NoError(t, focus("echo.docs", "echo.docs.popup"))
	assert.ErrorIs(t, focus("echo.docs", ""), rpc.ErrInvalidParams)
	assert.ErrorIs(t, focus("echo.docs", "echo.docs.main"), rpc.ErrInvalidParams)
	assert.ErrorIs(t, focus("echo", "echo.popup"), rpc.ErrInvalidParams)
}

func TestUIFocusDeclaredExtraSlot(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDirAs(t, manifestJSON("echo", `{"sections":[{"id":"echo","title":"Echo","slots":["review"]}]}`)))
	p := h.CreateWorkspace(t, "p")
	c := h.Client(t)
	focus := func(slot string) error {
		return c.Call(t.Context(), "ui.focus", map[string]string{"workspaceId": p.ID, "section": "echo", "slot": slot}, nil)
	}
	require.NoError(t, focus("echo.review"))
	assert.ErrorIs(t, focus("echo.other"), rpc.ErrInvalidParams)
}

func nextFocus(c *rpc.Client, wait time.Duration) (rpc.Notification, bool) {
	deadline := time.After(wait)
	for {
		select {
		case n := <-c.Notifications():
			if n.Method == "ui.focus" {
				return n, true
			}
		case <-deadline:
			return rpc.Notification{}, false
		}
	}
}

func TestWorkspaceOpenCountsRegisteredClients(t *testing.T) {
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
	var p workspace.Workspace
	require.NoError(t, caller.Call(t.Context(), "workspace.create", map[string]any{"name": "demo"}, &p))
	var res struct {
		Clients int `json:"clients"`
	}
	require.NoError(t, caller.Call(t.Context(), "workspace.show", map[string]string{"id": p.ID}, &res))
	assert.Equal(t, 0, res.Clients)
	select {
	case n := <-listener.Notifications():
		t.Fatalf("unexpected notification %s", n.Method)
	case <-time.After(100 * time.Millisecond):
	}
	err = listener.Call(t.Context(), "client.register", map[string]any{"handles": []string{"x"}}, nil)
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
	require.NoError(t, listener.Call(t.Context(), "client.register", map[string]any{"handles": []string{"workspace.show"}}, nil))
	require.NoError(t, caller.Call(t.Context(), "workspace.show", map[string]string{"id": p.ID}, &res))
	assert.Equal(t, 1, res.Clients)
	select {
	case n := <-listener.Notifications():
		assert.Equal(t, "workspace.show", n.Method)
		assert.JSONEq(t, fmt.Sprintf(`{"id":%q}`, p.ID), string(n.Params))
	case <-time.After(2 * time.Second):
		t.Fatal("no workspace.open notification")
	}
	err = caller.Call(t.Context(), "workspace.show", map[string]string{"id": "prj_0123456789abcdef"}, &res)
	assert.ErrorIs(t, err, rpc.ErrNotFound)
}
