package rpc_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/testutil"
)

func manifestJSON(id, contributes string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q,"version":"0.1.0","protocol":1,"command":["echo"],"contributes":%s}`, id, id, contributes)
}

func TestViewOwnsForeignSlot(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDirAs(t,
		manifestJSON("alpha", `{"sections":[{"id":"alpha","title":"Alpha"}]}`),
		manifestJSON("beta", `{"views":[{"slot":"alpha.sidebar"}]}`),
	))
	testutil.Eventually(t, func() bool {
		st := h.Sup.Statuses()
		return len(st) == 2 && st[0].State == plugin.StateRunning && st[1].State == plugin.StateRunning
	}, 5*time.Second)
	p := h.CreateProject(t, "demo")
	c := h.Client(t)
	tree := map[string]any{"type": "empty", "props": map[string]any{"text": "x"}}
	render := func(plugin, slot string) error {
		return h.PluginCall(t, plugin, "ui.render", map[string]any{"projectId": p.ID, "slot": slot, "tree": tree})
	}
	require.NoError(t, render("beta", "alpha.sidebar"))
	assert.ErrorIs(t, render("alpha", "alpha.sidebar"), rpc.ErrInvalidParams)
	require.NoError(t, render("alpha", "alpha.main"))
	require.NoError(t, render("alpha", "alpha.status"))
	var res map[string]any
	require.NoError(t, c.Call(t.Context(), "ui.event", map[string]any{"projectId": p.ID, "slot": "alpha.sidebar", "nodeId": "x", "action": "say", "value": "hi"}, &res))
	assert.Equal(t, "beta", res["plugin"])
}

func TestPresenceEvents(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t)
	p := h.CreateProject(t, "demo")
	c := h.Client(t)
	var subs [2]struct {
		Subscription string `json:"subscription"`
	}
	for i := range subs {
		require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &subs[i]))
	}
	var present []string
	require.NoError(t, c.Call(t.Context(), "ui.presence", struct{}{}, &present))
	assert.Equal(t, []string{p.ID}, present)
	for i := range subs {
		require.NoError(t, c.Call(t.Context(), "ui.unsubscribe", map[string]string{"subscription": subs[i].Subscription}, nil))
	}
	require.NoError(t, c.Call(t.Context(), "ui.presence", struct{}{}, &present))
	assert.Empty(t, present)
	events, err := h.Bus.Read(t.Context(), "ui.*", 0, 0)
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, "ui.opened", events[0].Topic)
	assert.Equal(t, "ui.idle", events[1].Topic)
	assert.JSONEq(t, fmt.Sprintf(`{"projectId":%q}`, p.ID), string(events[0].Payload))
}

func TestProjectDeleteDropsSlots(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDirAs(t, manifestJSON("alpha", `{"sections":[{"id":"alpha","title":"Alpha"}]}`)))
	testutil.Eventually(t, func() bool {
		st := h.Sup.Statuses()
		return len(st) == 1 && st[0].State == plugin.StateRunning
	}, 5*time.Second)
	p := h.CreateProject(t, "demo")
	c := h.Client(t)
	tree := map[string]any{"type": "empty", "props": map[string]any{"text": "x"}}
	require.NoError(t, h.PluginCall(t, "alpha", "ui.render", map[string]any{"projectId": p.ID, "slot": "alpha.main", "tree": tree}))
	require.NoError(t, c.Call(t.Context(), "project.delete", map[string]string{"id": p.ID}, nil))
	var views []internalsdui.SlotView
	require.NoError(t, c.Call(t.Context(), "ui.get", map[string]string{"projectId": p.ID}, &views))
	assert.Equal(t, []internalsdui.SlotView{}, views)
}

func TestHiddenCommandListed(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDirAs(t, manifestJSON("alpha", `{"commands":[{"id":"alpha.secret","title":"Secret","hidden":true},{"id":"alpha.open","title":"Open"}]}`)))
	var cmds []rpc.Command
	require.NoError(t, h.Client(t).Call(t.Context(), "command.list", struct{}{}, &cmds))
	assert.ElementsMatch(t, []rpc.Command{
		{ID: "alpha.secret", Title: "Secret", Plugin: "alpha", Hidden: true},
		{ID: "alpha.open", Title: "Open", Plugin: "alpha"},
	}, cmds)
}

func TestAppScopeSlots(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDirAs(t,
		manifestJSON("alpha", `{"sections":[{"id":"alpha","title":"Alpha","scope":"app"}]}`),
		manifestJSON("beta", `{"sections":[{"id":"beta","title":"Beta"}]}`),
	))
	testutil.Eventually(t, func() bool {
		st := h.Sup.Statuses()
		return len(st) == 2 && st[0].State == plugin.StateRunning && st[1].State == plugin.StateRunning
	}, 5*time.Second)
	p := h.CreateProject(t, "demo")
	c := h.Client(t)
	tree := map[string]any{"type": "empty", "props": map[string]any{"text": "x"}}
	render := func(plugin, projectID, slot string) error {
		return h.PluginCall(t, plugin, "ui.render", map[string]any{"projectId": projectID, "slot": slot, "tree": tree})
	}
	require.NoError(t, render("alpha", "", "alpha.main"))
	assert.ErrorIs(t, render("alpha", p.ID, "alpha.sidebar"), rpc.ErrInvalidParams)
	assert.ErrorIs(t, render("beta", "", "beta.main"), rpc.ErrInvalidParams)
	require.NoError(t, render("beta", p.ID, "beta.main"))

	var app, prj []internalsdui.SlotView
	require.NoError(t, c.Call(t.Context(), "ui.get", map[string]string{"projectId": ""}, &app))
	require.NoError(t, c.Call(t.Context(), "ui.get", map[string]string{"projectId": p.ID}, &prj))
	require.Len(t, app, 1)
	assert.Equal(t, "alpha.main", app[0].Slot)
	require.Len(t, prj, 1)
	assert.Equal(t, "beta.main", prj[0].Slot)

	event := func(projectID, slot string) error {
		return c.Call(t.Context(), "ui.event", map[string]any{"projectId": projectID, "slot": slot, "nodeId": "x", "action": "say", "value": "hi"}, nil)
	}
	assert.ErrorIs(t, event("", "beta.main"), rpc.ErrInvalidParams)
	assert.ErrorIs(t, event(p.ID, "alpha.main"), rpc.ErrInvalidParams)
	assert.NoError(t, event("", "alpha.main"))

	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": ""}, &sub))
	var present []string
	require.NoError(t, c.Call(t.Context(), "ui.presence", struct{}{}, &present))
	assert.Empty(t, present)
	require.NoError(t, c.Call(t.Context(), "ui.unsubscribe", map[string]string{"subscription": sub.Subscription}, nil))
	events, err := h.Bus.Read(t.Context(), "ui.*", 0, 0)
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestProjectOpenNotifiesClients(t *testing.T) {
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
