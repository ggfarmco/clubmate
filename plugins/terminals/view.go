package main

import (
	"fmt"
	"slices"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/sdui"
)

const idleAfter = time.Minute

type terminal struct {
	ID       string
	Title    string
	State    contract.SessionState
	ExitCode *int
	OutputAt *time.Time
	Bell     bool
}

func sidebarView(terminals []terminal, selected string, now time.Time) sdui.Node {
	items := make([]sdui.Item, 0, len(terminals))
	for _, t := range terminals {
		items = append(items, sdui.Item{ID: t.ID, Label: t.Title, Detail: detail(t, now), Badge: badge(t)})
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

func detail(t terminal, now time.Time) string {
	switch t.State {
	case contract.SessionStateRunning:
		if t.OutputAt == nil || now.Sub(*t.OutputAt) < idleAfter {
			return ""
		}
		return "idle " + elapsed(now.Sub(*t.OutputAt))
	case contract.SessionStateExited:
		if failed(t) {
			return ""
		}
		return "exited"
	}
	return ""
}

func badge(t terminal) string {
	switch {
	case t.State == contract.SessionStateLost:
		return "lost"
	case failed(t):
		return fmt.Sprintf("exit %d", *t.ExitCode)
	case t.State == contract.SessionStateRunning && t.Bell:
		return "bell"
	}
	return ""
}

func failed(t terminal) bool {
	return t.State == contract.SessionStateExited && t.ExitCode != nil && *t.ExitCode != 0
}

func elapsed(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

func exitedText(t terminal) string {
	if t.ExitCode == nil {
		return "Terminal exited"
	}
	return fmt.Sprintf("Terminal exited (code %d)", *t.ExitCode)
}
