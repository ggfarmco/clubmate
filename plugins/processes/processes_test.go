package main_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func TestAutoKillToggle(t *testing.T) {
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/processes", "plugins/processes/plugin.json"))
	p := h.CreateWorkspace(t, "demo")
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, nil))
	orphan := testutil.StartOrphanSession(t, h.Svc, p.ID)
	h.WaitSlot(t, p.ID, "processes.main", func(n sdui.Node) bool { return n.Type == "stack" })
	h.Event(t, p.ID, "processes.main", "autokill", "autokill", true)
	testutil.Eventually(t, func() bool { return !testutil.Alive(orphan) }, 12*time.Second)
	require.Eventually(t, func() bool {
		events, err := h.Bus.Read(t.Context(), "processes.killed", 0, 10)
		return err == nil && len(events) == 1 && strings.Contains(string(events[0].Payload), p.ID)
	}, 5*time.Second, 50*time.Millisecond)
}

func TestMainRendersOnlyWhileVisible(t *testing.T) {
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/processes", "plugins/processes/plugin.json"))
	p := h.CreateWorkspace(t, "demo")
	c := h.Client(t)
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, nil))
	slotRev := func(slot string) int64 {
		var views []internalsdui.SlotView
		require.NoError(t, c.Call(t.Context(), "ui.get", map[string]string{"workspaceId": p.ID}, &views))
		for _, v := range views {
			if v.Slot == slot {
				return v.Rev
			}
		}
		return 0
	}
	rev := func() int64 { return slotRev("processes.main") }
	section := func(id string) {
		require.NoError(t, c.Call(t.Context(), "ui.state.set", map[string]any{"workspaceId": p.ID, "state": map[string]string{"section": id}}, nil))
	}
	h.WaitSlot(t, p.ID, "processes.main", func(n sdui.Node) bool { return n.Type == "stack" })
	section("terminals")
	time.Sleep(1500 * time.Millisecond)
	hidden, sidebar := rev(), slotRev("processes.sidebar")
	time.Sleep(2500 * time.Millisecond)
	assert.Equal(t, hidden, rev())
	assert.Equal(t, sidebar, slotRev("processes.sidebar"))
	section("processes")
	testutil.Eventually(t, func() bool { return rev() > hidden+1 }, 5*time.Second)
}

func TestInitRendersOnlyOpenWorkspaces(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	h := testutil.NewHarness(t)
	p := h.CreateWorkspace(t, "demo")
	h.Start(t, testutil.BuildPluginDir(t, "clubmate/plugins/processes", "plugins/processes/plugin.json"))
	c := h.Client(t)
	testutil.Eventually(t, func() bool {
		events, err := h.Bus.Read(t.Context(), "processes.ready", 0, 1)
		return err == nil && len(events) == 1
	}, 5*time.Second)
	time.Sleep(time.Second)
	var views []internalsdui.SlotView
	require.NoError(t, c.Call(t.Context(), "ui.get", map[string]string{"workspaceId": p.ID}, &views))
	assert.Empty(t, views)
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, nil))
	h.WaitSlot(t, p.ID, "processes.main", func(n sdui.Node) bool { return n.Type == "stack" })
}

func TestSelectedSessionClearedWhenItEnds(t *testing.T) {
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/processes", "plugins/processes/plugin.json"))
	p := h.CreateWorkspace(t, "demo")
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, nil))
	sess, err := h.Svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: p.ID, Command: []string{"/bin/sh", "-c", "sleep 300"}})
	require.NoError(t, err)
	selected := func(id string) func(sdui.Node) bool {
		return func(n sdui.Node) bool { return n.Props["selected"] == id }
	}
	stored := func() bool {
		_, found, err := h.KV.Get(t.Context(), "processes", "selected/"+p.ID)
		require.NoError(t, err)
		return found
	}
	h.WaitSlot(t, p.ID, "processes.sidebar", func(n sdui.Node) bool { return strings.Contains(fmt.Sprint(n.Props["items"]), sess.ID) })
	h.Event(t, p.ID, "processes.sidebar", "sessions", "select", sess.ID)
	h.WaitSlot(t, p.ID, "processes.sidebar", selected(sess.ID))
	require.True(t, stored())

	require.NoError(t, h.Svc.KillSession(t.Context(), sess.ID))
	testutil.Eventually(t, func() bool { return h.Svc.RemoveSession(t.Context(), sess.ID) == nil }, 5*time.Second)
	h.WaitSlot(t, p.ID, "processes.sidebar", selected("all"))
	testutil.Eventually(t, func() bool { return !stored() }, 5*time.Second)
}
