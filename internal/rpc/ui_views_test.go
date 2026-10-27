package rpc_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/internal/rpc"
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
