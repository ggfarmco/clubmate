package main

import (
	"fmt"
	"slices"

	"clubmate/pkg/contract"
	"clubmate/pkg/sdui"
)

type terminal struct {
	ID       string
	Title    string
	State    contract.SessionState
	ExitCode *int
}

func sidebarView(terminals []terminal, selected string) sdui.Node {
	items := make([]sdui.Item, 0, len(terminals))
	for _, t := range terminals {
		items = append(items, sdui.Item{ID: t.ID, Label: t.Title, Detail: detail(t), Badge: badge(t)})
	}
	var body sdui.Node
	if len(items) == 0 {
		body = sdui.Empty("No terminals")
	} else {
		body = sdui.List("terminals", items, selected)
	}
	return sdui.VStack(sdui.Button("new", "New terminal", "plus"), body)
}

func mainView(terminals []terminal, selected string) sdui.Node {
	for _, t := range terminals {
		if t.ID != selected {
			continue
		}
		if t.State == contract.SessionStateRunning {
			return sdui.Surface(t.ID)
		}
		return sdui.VStack(
			sdui.MutedText(exitedText(t)),
			sdui.HStack(
				sdui.Button("restart", "Restart", "arrow.clockwise"),
				sdui.Button("close", "Close", "xmark"),
			),
		)
	}
	return sdui.Empty("Create a terminal")
}

func resolveSelection(terminals []terminal, selected string) string {
	if slices.ContainsFunc(terminals, func(t terminal) bool { return t.ID == selected }) {
		return selected
	}
	if len(terminals) > 0 {
		return terminals[0].ID
	}
	return ""
}

func detail(t terminal) string {
	if t.State == contract.SessionStateRunning {
		return "running"
	}
	if t.State == contract.SessionStateExited && t.ExitCode != nil {
		return fmt.Sprintf("exited (%d)", *t.ExitCode)
	}
	return string(t.State)
}

func badge(t terminal) string {
	if t.State == contract.SessionStateRunning {
		return ""
	}
	return "exited"
}

func exitedText(t terminal) string {
	if t.ExitCode == nil {
		return "Terminal exited"
	}
	return fmt.Sprintf("Terminal exited (code %d)", *t.ExitCode)
}
