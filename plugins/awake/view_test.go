package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

func find(n sdui.Node, match func(sdui.Node) bool) *sdui.Node {
	if match(n) {
		return &n
	}
	for _, c := range n.Children {
		if f := find(c, match); f != nil {
			return f
		}
	}
	return nil
}

func byID(id string) func(sdui.Node) bool {
	return func(n sdui.Node) bool { return n.ID == id }
}

func byText(text string) func(sdui.Node) bool {
	return func(n sdui.Node) bool { return n.Type == "text" && n.Props["text"] == text }
}

func TestSettingsPageOnALaptopAndADesktop(t *testing.T) {
	e := newEnv(t)
	_, err := e.run("awake.set", map[string]any{"mode": "hours", "hours": 4})
	require.NoError(t, err)
	e.a.mu.Lock()
	laptop := e.a.k.view()
	e.a.mu.Unlock()
	laptop.Laptop = true

	page := settingsPage(laptop, 4)
	require.NoError(t, internalsdui.Validate(page))
	assert.Equal(t, true, find(page, byID("choice-hours")).Props["on"])
	assert.Equal(t, false, find(page, byID("choice-off")).Props["on"])
	assert.Equal(t, "4", find(page, byID(actionHours)).Props["value"])
	assert.NotNil(t, find(page, byText(laptop.Ends)))
	assert.NotEmpty(t, laptop.Ends)
	assert.NotNil(t, find(page, byID("choice-agents")).Children, "the agents radio carries its caption and On battery too")
	assert.NotNil(t, find(page, byID(actionBattery)))
	assert.NotNil(t, find(page, byText(lidFootnote)))
	assert.NotNil(t, find(page, byText("You")), "the hold lines show in Now")

	desktop := laptop
	desktop.Laptop, desktop.Mode, desktop.Hours, desktop.On, desktop.Holds, desktop.Ends = false, modeOff, 0, false, nil, ""
	page = settingsPage(desktop, 2)
	assert.Equal(t, true, find(page, byID("choice-off")).Props["on"])
	assert.Equal(t, "2", find(page, byID(actionHours)).Props["value"], "the select keeps the last number used")
	assert.Nil(t, find(page, byID(actionBattery)))
	assert.Nil(t, find(page, byText(lidFootnote)))
}

func TestSettingsEventsChooseTheHoursTheSelectSends(t *testing.T) {
	e := newEnv(t)
	event := func(action string, value any) stateView {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		out, err := e.a.uiEvent(t.Context(), pluginsdk.UIEvent{Slot: settingsSlot, Action: action, Value: raw})
		require.NoError(t, err)
		return out.(stateView)
	}

	assert.Equal(t, 8, event(actionHours, "8").Hours)
	assert.Equal(t, modeOff, event(actionChoice, choiceOff).Mode)
	v := event(actionChoice, modeHours)
	assert.Equal(t, modeHours, v.Mode)
	assert.Equal(t, 8, v.Hours, "picking For again uses the last number")
}
