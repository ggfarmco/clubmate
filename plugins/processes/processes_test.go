package main_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
