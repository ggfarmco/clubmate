package main

import (
	"context"
	"strconv"
	"strings"

	"clubmate/pkg/sdui"
)

const (
	actionChoice  = "choice"
	actionHours   = "hours"
	actionBattery = "battery"
	actionDisplay = "display"
	choiceOff     = "off"

	agentsCaption = "An agent running or waiting for you keeps the Mac awake."
	batteryHint   = "Off: agents keep it awake only on power."
	displayHint   = "Off: the screen can sleep while the Mac stays awake."
	lidFootnote   = "A closed lid still sleeps a laptop on battery. On power it stays awake closed only with an external display, keyboard and mouse."
	toggleKey     = "⌥⌘A"
)

func (a *awake) render(ctx context.Context, v stateView) error {
	a.mu.Lock()
	last := a.k.lastHours()
	a.mu.Unlock()
	_, err := a.h.Render(ctx, "", settingsSlot, settingsPage(v, last))
	return err
}

func settingsPage(v stateView, last int) sdui.Node {
	rows := []sdui.Node{
		group("Now", nowBlock(v)...),
		group("Keep awake", choices(v, last)...),
		group("Display", switchRow("Keep the screen on", displayHint, actionDisplay, v.Display)),
	}
	if v.Laptop {
		rows = append(rows, sdui.MutedText(lidFootnote))
	}
	return sdui.VStack(rows...)
}

func group(label string, rows ...sdui.Node) sdui.Node {
	return sdui.WithGap(sdui.VStack(append([]sdui.Node{sdui.MutedText(label)}, rows...)...), 6)
}

func switchRow(label, hint, action string, on bool) sdui.Node {
	return sdui.HStack(sdui.VStack(sdui.Text(label), sdui.MutedText(hint)), sdui.Spacer(), sdui.WithAction(sdui.Toggle(action, on), action))
}

func nowBlock(v stateView) []sdui.Node {
	icon := "clubmate.gourd"
	if v.On {
		icon = "clubmate.gourd.fill"
	}
	if !v.On || v.Text == "" {
		return append([]sdui.Node{line(sdui.Icon(icon), sdui.Text(v.Detail))}, holdRows(v)...)
	}
	return append([]sdui.Node{line(sdui.Icon(icon), strong(v.Text)), sdui.Text(v.Detail)}, holdRows(v)...)
}

func line(children ...sdui.Node) sdui.Node {
	return sdui.WithGap(sdui.HStack(children...), 6)
}

func holdRows(v stateView) []sdui.Node {
	rows := make([]sdui.Node, 0, len(v.Holds))
	for _, h := range v.Holds {
		rows = append(rows, line(sdui.MutedText(h.Title), sdui.Spacer(), sdui.Text(h.Trailing)))
	}
	return rows
}

func choices(v stateView, last int) []sdui.Node {
	hours := last
	if v.Mode == modeHours && validHours(v.Hours) {
		hours = v.Hours
	}
	options := make([]sdui.Option, 0, len(v.Labels.Hours.Options))
	for _, o := range v.Labels.Hours.Options {
		options = append(options, sdui.Option{Value: strconv.Itoa(o.Hours), Label: strings.ToLower(o.Title)})
	}
	hoursRow := []sdui.Node{
		radio(modeHours, "For", v.Mode == modeHours),
		sdui.WithAction(sdui.Select(actionHours, options, strconv.Itoa(hours)), actionHours),
	}
	if v.Ends != "" {
		hoursRow = append(hoursRow, sdui.MutedText(v.Ends))
	}
	var agents []sdui.Node
	agents = append(agents, sdui.MutedText(agentsCaption))
	if v.Laptop {
		agents = append(agents, switchRow("On battery too", batteryHint, actionBattery, v.Battery))
	}
	return []sdui.Node{
		radio(choiceOff, "Off", v.Mode == modeOff),
		line(hoursRow...),
		radio(modeAgents, "While agents work", v.Mode == modeAgents, agents...),
		sdui.HStack(radio(modeOn, "Until turned off", v.Mode == modeOn), sdui.Spacer(), sdui.MutedText(toggleKey)),
	}
}

func radio(value, label string, on bool, children ...sdui.Node) sdui.Node {
	return sdui.WithAction(sdui.Radio("choice-"+value, value, label, on, children...), actionChoice)
}

func strong(text string) sdui.Node {
	return sdui.Node{Type: "text", Props: map[string]any{"text": text, "style": "strong"}}
}
