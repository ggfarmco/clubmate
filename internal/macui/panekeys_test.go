package macui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (in *instance) statusBar(id string) string {
	in.t.Helper()
	if e := in.snapshot().main().node(id); e != nil {
		return e.Label
	}
	return ""
}

func (in *instance) mode(want string) {
	in.t.Helper()
	in.eventually("the status bar names "+want, func() bool { return in.statusBar("statusbar.mode") == want })
}

func (in *instance) stepOut(keys ...string) {
	in.t.Helper()
	for _, route := range in.key(keys...) {
		require.False(in.t, strings.HasPrefix(route, "responder"), "esc reached the window: %v", route)
	}
}

func TestAgentsPaneKeys(t *testing.T) {
	release := filepath.Join(root, "build", "mac-ui", "macui-pane-keys-release")
	require.NoError(t, os.RemoveAll(release))
	in := start(t, "macui-pane-keys", fmt.Sprintf(`{"prompt": [{"match": "Hold", "wait": %q}]}`, release))
	in.focusSection("agents", "agents.sidebar")
	idle := in.idleSession("Hello")
	in.pointer("click", idle+"/composer/0")
	in.focusIn(idle)
	in.mode("TYPE")

	t.Run("esc steps out composer, transcript, tree and back", func(t *testing.T) {
		in := in.at(t)
		in.stepOut("esc")
		in.mode("READ")
		in.stepOut("ctrl+[")
		in.mode("TREE")
		in.stepOut("esc")
		in.focusIn(idle)
	})
	t.Run("opt+cmd arrows go straight to a pane", func(t *testing.T) {
		in := in.at(t)
		in.stepOut("opt+cmd+left")
		in.mode("TREE")
		in.stepOut("opt+cmd+right")
		in.mode("TYPE")
		in.stepOut("opt+cmd+up")
		in.mode("READ")
		in.stepOut("opt+cmd+down")
		in.mode("TYPE")
	})
	t.Run("i in the transcript goes to the composer", func(t *testing.T) {
		in := in.at(t)
		in.stepOut("esc")
		in.mode("READ")
		in.key("i")
		in.focusIn(idle)
	})
	t.Run("a row click while typing gives the tree key focus", func(t *testing.T) {
		in := in.at(t)
		in.pointer("click", "sessions#"+idle)
		in.mode("TREE")
		in.stepOut("esc")
		in.focusIn(idle)
	})
	t.Run("esc in the search field clears it, then goes to the tree", func(t *testing.T) {
		in := in.at(t)
		row := "sessions#" + idle
		in.stepOut("opt+cmd+left")
		in.mode("TREE")
		in.key("g", "c", "z", "z")
		in.eventually("the search hides the row", func() bool { return !in.snapshot().main().has(row) })
		in.stepOut("esc")
		in.eventually("the cleared search shows the row, the field keeps the caret", func() bool {
			return in.snapshot().main().has(row) && strings.Contains(in.snapshot().key().Responder, "FieldEditor")
		})
		in.stepOut("esc")
		in.eventually("the tree has the keys", func() bool { return !strings.Contains(in.snapshot().key().Responder, "FieldEditor") })
		in.mode("TREE")
	})
	t.Run("a transcript click gives the transcript focus", func(t *testing.T) {
		in := in.at(t)
		in.pointer("click", idle+"/transcript")
		in.mode("READ")
		in.stepOut("esc")
		in.mode("TREE")
	})

	before := in.sessions()
	in.cm("command", "run", "agents.new", "prompt=Hold")
	busy := in.added(before).ID
	in.focusIn(busy)
	statusOf := func() string {
		list := in.sessions()
		if i := slices.IndexFunc(list, func(s session) bool { return s.ID == busy }); i >= 0 {
			return list[i].Status
		}
		return ""
	}
	in.eventually("the turn runs", func() bool { return statusOf() == "running" })

	t.Run("esc leaves a running prompt with the hint and never stops", func(t *testing.T) {
		in := in.at(t)
		in.stepOut("esc")
		in.mode("READ")
		in.eventually("the hint names cmd+.", func() bool { return strings.Contains(in.statusBar("statusbar.hints"), "⌘. stops") })
		in.stepOut("esc", "esc", "esc", "esc", "esc", "esc")
		assert.Equal(t, "running", statusOf())
	})
	t.Run("cmd+. stops the turn from the composer", func(t *testing.T) {
		in := in.at(t)
		in.pointer("click", busy+"/composer/0")
		in.focusIn(busy)
		assert.Equal(t, []string{"monitor"}, in.key("cmd+."))
		in.eventually("the turn stops", func() bool { return statusOf() != "running" })
	})
}
