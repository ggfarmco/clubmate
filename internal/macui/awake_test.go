package macui_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeepAwakeStatusMenu(t *testing.T) {
	in := start(t, "macui-awake", "")
	t.Cleanup(func() {
		_, _ = in.run("command", "run", "awake.set", "on=false")
		_, _ = in.run("command", "run", "awake.settings-set", "agents=false")
	})
	var state struct {
		Text   string `json:"text"`
		Manual bool   `json:"manual"`
		Agents bool   `json:"agents"`
	}
	read := func() {
		in.json(&state, "command", "run", "awake.state")
	}
	in.eventually("the status bar shows the keep-awake item without Turn Off", func() bool {
		item := in.snapshot().main().node("awake")
		return item != nil && len(item.Menu) > 0 && !slices.Contains(titles(item.Menu), "Turn Off")
	})

	in.pointer("menu", "awake", "Keep Awake Until Turned Off")
	in.eventually("the menu turned the hold on", func() bool {
		read()
		return state.Manual
	})
	assert.Equal(t, "Awake", state.Text)
	in.eventually("the item offers Turn Off", func() bool {
		item := in.snapshot().main().node("awake")
		return item != nil && slices.Contains(titles(item.Menu), "Turn Off")
	})

	in.pointer("menu", "awake", "Turn Off")
	in.eventually("Turn Off ended the hold", func() bool {
		read()
		return !state.Manual
	})

	in.pointer("menu", "awake", "While Agents Work")
	in.eventually("While Agents Work turned the rule on", func() bool {
		read()
		return state.Agents
	})
	in.eventually("the item shows the rule checked", func() bool {
		item := in.snapshot().main().node("awake")
		if item == nil {
			return false
		}
		i := slices.IndexFunc(item.Menu, func(m menuItem) bool { return m.Title == "While Agents Work" })
		return i >= 0 && item.Menu[i].Checked
	})

	in.pointer("menu", "awake", "Keep Awake Settings…")
	in.eventually("Settings opens on the plugin's page", func() bool {
		w := in.snapshot().kind("settings")
		return w != nil && w.has("display")
	})
}
