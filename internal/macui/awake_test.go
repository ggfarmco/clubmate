package macui_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeepAwakeStatusMenu(t *testing.T) {
	in := start(t, "macui-awake", "")
	t.Cleanup(func() {
		_, _ = in.run("command", "run", "awake.set", "mode=off")
	})
	var state struct {
		Mode  string `json:"mode"`
		Hours int    `json:"hours"`
	}
	read := func() {
		in.json(&state, "command", "run", "awake.state")
	}
	menu := func() []menuItem {
		if item := in.snapshot().main().node("awake"); item != nil {
			return item.Menu
		}
		return nil
	}
	chosen := func(what, mode string, hours int) {
		t.Helper()
		in.eventually(what, func() bool {
			read()
			return state.Mode == mode && state.Hours == hours
		})
	}
	in.eventually("the status bar shows the keep-awake item without Turn Off", func() bool {
		items := menu()
		return len(items) > 0 && slices.Contains(titles(items), "For Hours") && !slices.Contains(titles(items), "Turn Off")
	})

	in.pointer("menu", "awake", "For Hours > 2 Hours")
	chosen("For Hours ▸ 2 Hours chose two hours", "hours", 2)
	in.eventually("the item names the hours, checked, with Turn Off", func() bool {
		items := menu()
		i := slices.IndexFunc(items, func(m menuItem) bool { return m.Title == "For 2 Hours" })
		return i >= 0 && items[i].Checked && slices.Contains(titles(items), "Turn Off")
	})

	in.pointer("menu", "awake", "While Agents Work")
	chosen("While Agents Work replaced the hours", "agents", 0)
	in.pointer("menu", "awake", "Until Turned Off")
	chosen("Until Turned Off replaced the agents choice", "on", 0)
	in.pointer("menu", "awake", "Turn Off")
	chosen("Turn Off ended the choice", "", 0)

	in.pointer("click", "awake", "--keys", "opt")
	chosen("⌥-click keeps it awake until turned off", "on", 0)
	routes := in.key("cmd+opt+a")
	assert.True(t, len(routes) == 1 && strings.HasPrefix(routes[0], "menu Turn Off Keep Awake"), "⌥⌘A routes to the menu item: %v", routes)
	chosen("⌥⌘A turns it off", "", 0)

	in.pointer("menu", "awake", "Keep Awake Settings…")
	in.eventually("Settings opens on the plugin's page", func() bool {
		w := in.snapshot().kind("settings")
		return w != nil && w.has("choice-agents")
	})
	in.pointer("click", "choice-agents", "--at", "20,12")
	chosen("the While agents work radio chose it", "agents", 0)
}
