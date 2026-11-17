package macui_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionsTreeHoldsNewRowsUnderThePointer(t *testing.T) {
	in := start(t, "macui-hold", permissionScript)
	in.focusSection("agents", "agents.sidebar")
	spawn := func(prompt, status string) string {
		var spawned struct {
			Session string `json:"session"`
		}
		in.json(&spawned, "agent", "spawn", prompt)
		require.NotEmpty(t, spawned.Session)
		in.eventually(prompt+" is "+status, func() bool {
			return slices.ContainsFunc(in.sessions(), func(s session) bool { return s.ID == spawned.Session && s.Status == status })
		})
		return spawned.Session
	}
	asker := spawn("Hello", "done")
	other := spawn("Hello again", "done")
	in.cm("ui", "event", "--workspace="+in.workspace, "--slot=agents.sidebar", "--node=sessions", "--action=select", `--value="`+other+`"`)
	in.eventually("the other session is picked", func() bool { return in.selected("sessions#" + other) })

	in.pointer("hover", "sessions#"+other)
	in.cm("command", "run", "agents.prompt", "session="+asker, "text=Wait now")
	in.eventually("the pill counts the held inbox copy", func() bool {
		pill := in.snapshot().main().node("sessions.hold")
		return pill != nil && pill.Label == "1 to show"
	})
	in.eventually("the session waits", func() bool {
		return slices.ContainsFunc(in.sessions(), func(s session) bool { return s.ID == asker && s.Status == "waiting" })
	})
	require.False(t, in.snapshot().main().has("sessions#mirror:"+asker))

	in.pointer("hover", in.rightOf("sessions"))
	in.eventually("the inbox copy lands once the pointer leaves", func() bool {
		w := in.snapshot().main()
		return w.has("sessions#mirror:"+asker) && !w.has("sessions.hold")
	})

	in.pointer("click", "sessions#mirror:"+asker)
	in.eventually("the inbox copy and its row are selected together", func() bool {
		return in.selected("sessions#"+asker) && in.selected("sessions#mirror:"+asker) && !in.selected("sessions#"+other)
	})
}

func (in *instance) rightOf(id string) string {
	in.t.Helper()
	w := in.snapshot().main()
	pane := w.node(id)
	require.NotNil(in.t, pane)
	edge := pane.Frame.X + pane.Frame.Width
	e := w.find(func(e element) bool { return e.ID != "" && e.Frame.X > edge+8 && e.Frame.Width > 0 })
	require.NotNil(in.t, e, "no element right of %s", id)
	return e.ID
}
