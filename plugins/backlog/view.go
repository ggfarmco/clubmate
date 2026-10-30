package main

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"

	"clubmate/pkg/sdui"
)

const (
	sidebarSlot = "backlog.sidebar"
	mainSlot    = "backlog.main"
	sectionID   = "backlog"
	groupPrefix = "group:"
	noTasks     = "No tasks yet — add one on the left"
	formLines   = 8
)

var (
	groupOrder   = []string{statusInProgress, statusTodo, statusIdea, statusNotNow, statusDone}
	statusLabels = map[string]string{statusInProgress: "In progress", statusTodo: "Todo", statusIdea: "Idea", statusNotNow: "Not now", statusDone: "Done"}
	statusIcons  = map[string]string{statusInProgress: "play.circle", statusTodo: "circle", statusIdea: "lightbulb", statusNotNow: "moon.zzz", statusDone: "checkmark.circle"}
	statusTones  = map[string]string{statusInProgress: "accent", statusDone: "success"}
	priorityTone = map[string]string{priorityHigh: "warning"}
	moveTargets  = []string{statusTodo, statusIdea, statusNotNow}
	priorities   = []string{priorityHigh, priorityNormal, priorityLow}
	editStatuses = []string{statusTodo, statusIdea, statusNotNow, statusDone}
)

func ownerLabel(owner, title string, stale bool) string {
	switch {
	case owner == userOwner:
		return "you"
	case strings.HasPrefix(owner, sessionPfx):
		label := cmp.Or(title, owner)
		if stale {
			label += " (closed)"
		}
		return label
	}
	return owner
}

func labelOf(owners map[string]string, owner string) string {
	if label, ok := owners[owner]; ok {
		return label
	}
	return ownerLabel(owner, "", false)
}

func sidebarView(tasks []Task, owners map[string]string, selected string, collapsed map[string]bool, formRev int) sdui.Node {
	add := sdui.WithAction(sdui.Form("new", sdui.TextField("title", "", "New task…"), sdui.Button("add", "Add", "plus")), "add")
	form := sdui.VStack(add)
	form.ID = fmt.Sprintf("new-%d", formRev)
	if len(tasks) == 0 {
		return sdui.VStack(form, sdui.Empty("No tasks yet"))
	}
	byStatus := map[string][]sdui.TreeItem{}
	for _, t := range tasks {
		byStatus[t.Status] = append(byStatus[t.Status], taskItem(t, owners))
	}
	groups := make([]sdui.TreeItem, 0, len(groupOrder))
	for _, status := range groupOrder {
		rows := byStatus[status]
		if len(rows) == 0 {
			continue
		}
		g := sdui.TreeItem{
			ID:          groupPrefix + status,
			Label:       fmt.Sprintf("%s · %d", statusLabels[status], len(rows)),
			Icon:        statusIcons[status],
			HasChildren: true,
			Expanded:    !collapsed[status],
		}
		if g.Expanded {
			g.Children = rows
		}
		groups = append(groups, g)
	}
	return sdui.VStack(form, sdui.Scroll(sdui.Tree("tasks", groups, selected)))
}

func taskItem(t Task, owners map[string]string) sdui.TreeItem {
	detail, _, _ := strings.Cut(t.Note, "\n")
	if t.Status == statusInProgress {
		detail = labelOf(owners, t.ClaimedBy)
	}
	item := sdui.TreeItem{ID: strconv.Itoa(t.ID), Label: fmt.Sprintf("#%d %s", t.ID, t.Title), Detail: detail, Menu: taskMenu(t)}
	if t.Priority != priorityNormal {
		item.Badge = t.Priority
	}
	return item
}

func taskMenu(t Task) []sdui.MenuEntry {
	var menu []sdui.MenuEntry
	for _, s := range moveTargets {
		if s != t.Status {
			menu = append(menu, sdui.MenuEntry{Action: actionFor("status", s), Label: "Move to " + statusLabels[s]})
		}
	}
	if t.Status != statusDone {
		menu = append(menu, sdui.MenuEntry{Action: "done", Label: "Mark Done"})
	}
	if t.Status == statusInProgress {
		menu = append(menu, sdui.MenuEntry{Action: "release", Label: "Release"})
	}
	for _, p := range priorities {
		if p != t.Priority {
			menu = append(menu, sdui.MenuEntry{Action: actionFor("priority", p), Label: "Priority " + titleCase(p)})
		}
	}
	return append(menu, sdui.MenuEntry{Action: "delete", Label: "Delete", Confirm: deleteConfirm(t.ID)})
}

func deleteConfirm(id int) string {
	return fmt.Sprintf("Delete task #%d?", id)
}

func actionFor(verb, value string) string {
	return verb + "." + strings.ReplaceAll(value, "_", "-")
}

func parseAction(action string) (string, string) {
	verb, value, _ := strings.Cut(action, ".")
	return verb, strings.ReplaceAll(value, "-", "_")
}

func mainView(t *Task, owners map[string]string, hasTasks bool) sdui.Node {
	switch {
	case t == nil && hasTasks:
		return sdui.Empty("Select a task")
	case t == nil:
		return sdui.Empty(noTasks)
	}
	nodes := []sdui.Node{sdui.HStack(
		sdui.Text(fmt.Sprintf("#%d", t.ID)),
		sdui.Badge(statusLabels[t.Status], cmp.Or(statusTones[t.Status], "neutral")),
		sdui.Badge(t.Priority, cmp.Or(priorityTone[t.Priority], "neutral")),
	)}
	if t.Status == statusInProgress {
		line := "In progress by " + labelOf(owners, t.ClaimedBy)
		if !t.ClaimedAt.IsZero() {
			line += " since " + t.ClaimedAt.Local().Format("Jan 2 15:04")
		}
		nodes = append(nodes, sdui.MutedText(line))
	}
	if t.LastClaimedBy != "" {
		nodes = append(nodes, sdui.MutedText("Last claimed by "+labelOf(owners, t.LastClaimedBy)))
	}
	if t.Note != "" {
		nodes = append(nodes, sdui.MutedText(t.Note))
	}
	fields := []sdui.Node{
		sdui.TextField("title", t.Title, "Title"),
		sdui.WithLines(sdui.TextField("description", t.Description, "Description"), formLines),
	}
	if t.Status != statusInProgress {
		fields = append(fields, sdui.HStack(sdui.MutedText("Status"), sdui.Select("status", options(editStatuses, statusLabels), t.Status)))
	}
	fields = append(fields, sdui.HStack(sdui.MutedText("Priority"), sdui.Select("priority", options(priorities, nil), t.Priority)), sdui.Button("save", "Save", ""))
	form := sdui.VStack(sdui.WithAction(sdui.Form("edit", fields...), "save."+strconv.Itoa(t.ID)))
	form.ID = fmt.Sprintf("task-%d", t.ID)
	nodes = append(nodes, form)
	if buttons := actions(t); len(buttons) > 0 {
		nodes = append(nodes, sdui.HStack(buttons...))
	}
	return sdui.Scroll(sdui.VStack(nodes...))
}

func actions(t *Task) []sdui.Node {
	var buttons []sdui.Node
	switch t.Status {
	case statusTodo, statusIdea:
		buttons = append(buttons, sdui.Button("start", "Start", "play"))
	case statusInProgress:
		buttons = append(buttons, sdui.Button("release", "Release", "arrow.uturn.backward"))
	}
	if t.Status != statusDone {
		buttons = append(buttons, sdui.Button("done", "Done", "checkmark"))
	}
	buttons = append(buttons, sdui.WithConfirm(sdui.Button("delete", "Delete", "trash"), deleteConfirm(t.ID)))
	return buttons
}

func options(values []string, labels map[string]string) []sdui.Option {
	out := make([]sdui.Option, 0, len(values))
	for _, v := range values {
		out = append(out, sdui.Option{Value: v, Label: cmp.Or(labels[v], titleCase(v))})
	}
	return out
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
