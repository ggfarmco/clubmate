package main_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func TestAutoKillToggle(t *testing.T) {
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/processes", "plugins/processes/plugin.json"))
	p := h.CreateProject(t, "demo")
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
	p := h.CreateProject(t, "demo")
	c := h.Client(t)
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, nil))
	rev := func() int64 {
		var views []internalsdui.SlotView
		require.NoError(t, c.Call(t.Context(), "ui.get", map[string]string{"projectId": p.ID}, &views))
		for _, v := range views {
			if v.Slot == "processes.main" {
				return v.Rev
			}
		}
		return 0
	}
	section := func(id string) {
		require.NoError(t, c.Call(t.Context(), "ui.state.set", map[string]any{"projectId": p.ID, "state": map[string]string{"section": id}}, nil))
	}
	h.WaitSlot(t, p.ID, "processes.main", func(n sdui.Node) bool { return n.Type == "stack" })
	section("terminals")
	time.Sleep(1500 * time.Millisecond)
	hidden := rev()
	time.Sleep(2500 * time.Millisecond)
	assert.Equal(t, hidden, rev())
	section("processes")
	testutil.Eventually(t, func() bool { return rev() > hidden }, 5*time.Second)
}
