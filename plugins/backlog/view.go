package main

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"maps"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/docrender"
	"clubmate/pkg/sdui"
)

const (
	sidebarSlot = "backlog.sidebar"
	mainSlot    = "backlog.main"
	sectionID   = "backlog"
	groupPrefix = "group:"
	tagsNode    = "tags"
	tagsField   = "tagsText"
	tagAll      = "all"
	tagPrefix   = "tag:"
	actFilter   = "filter"
	noTasks     = "No tasks yet — add one on the left"
	formLines   = 8
	actReveal   = "reveal"
	actDescribe = "describe"
)

var (
	groupOrder   = []string{statusInProgress, statusTodo, statusIdea, statusNotNow, statusDone}
	statusLabels = map[string]string{statusInProgress: "In progress", statusTodo: "Todo", statusIdea: "Idea", statusNotNow: "Not now", statusDone: "Done"}
	statusIcons  = map[string]string{statusInProgress: "play.circle", statusTodo: "circle", statusIdea: "lightbulb", statusNotNow: "moon.zzz", statusDone: "checkmark.circle"}
	priorityIcon = map[string]string{priorityHigh: "exclamationmark.2", priorityLow: "arrow.down"}
	priorityText = map[string]string{priorityHigh: "!! High", priorityNormal: "Normal", priorityLow: "↓ Low"}
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

func tagCounts(tasks []Task) map[string]int {
	counts := map[string]int{}
	for _, t := range tasks {
		for _, tag := range t.Tags {
			counts[tag]++
		}
	}
	return counts
}

func tagTabs(tasks []Task, counts map[string]int, filter string) sdui.Node {
	items := []sdui.Item{{ID: tagAll, Label: "All", Badge: strconv.Itoa(len(tasks))}}
	tags := slices.Collect(maps.Keys(counts))
	slices.SortFunc(tags, func(a, b string) int { return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b)) })
	for _, tag := range tags {
		items = append(items, sdui.Item{ID: tagPrefix + tag, Label: tag, Badge: strconv.Itoa(counts[tag])})
	}
	selected := tagAll
	if filter != "" {
		selected = tagPrefix + filter
	}
	return sdui.WithAction(sdui.Tabs(tagsNode, items, selected), actFilter)
}

func sidebarView(tasks []Task, selected string, collapsed map[string]bool, formRev int, filter string) sdui.Node {
	add := sdui.WithAction(sdui.Form("new", sdui.TextField("title", "", "New task…"), sdui.Button("add", "Add", "plus")), "add")
	form := sdui.VStack(add)
	form.ID = fmt.Sprintf("new-%d", formRev)
	if len(tasks) == 0 {
		return sdui.VStack(form, sdui.Empty("No tasks yet"))
	}
	top := []sdui.Node{form}
	if counts := tagCounts(tasks); len(counts) > 0 {
		top = append(top, tagTabs(tasks, counts, filter))
	}
	byStatus := map[string][]sdui.TreeItem{}
	for _, t := range tasks {
		if filter == "" || slices.Contains(t.Tags, filter) {
			byStatus[t.Status] = append(byStatus[t.Status], taskItem(t))
		}
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
	return sdui.VStack(append(top, sdui.Scroll(sdui.Tree("tasks", groups, selected)))...)
}

func taskItem(t Task) sdui.TreeItem {
	return sdui.TreeItem{ID: strconv.Itoa(t.ID), Label: t.Title, Detail: fmt.Sprintf("#%d", t.ID), Icon: priorityIcon[t.Priority], Menu: taskMenu(t)}
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

func mainView(t *Task, owners map[string]string, hasTasks bool, errs map[string]string, editing bool) sdui.Node {
	switch {
	case t == nil && hasTasks:
		return sdui.Empty("Select a task")
	case t == nil:
		return sdui.Empty(noTasks)
	}
	meta := []string{fmt.Sprintf("#%d", t.ID)}
	if t.Status == statusInProgress {
		meta[0] += " · In progress by " + labelOf(owners, t.ClaimedBy)
		if !t.ClaimedAt.IsZero() {
			meta[0] += " since " + t.ClaimedAt.Local().Format("Jan 2 15:04")
		}
	}
	if t.LastClaimedBy != "" {
		meta = append(meta, "Last claimed by "+labelOf(owners, t.LastClaimedBy))
	}
	if t.Note != "" {
		meta = append(meta, t.Note)
	}
	saved := sdui.Button("save", "Saved", "checkmark")
	if len(errs) > 0 {
		saved = sdui.Button("save", "Not saved", "")
	}
	header := sdui.Header(t.Title, sdui.HeaderProps{Icon: statusIcons[t.Status], Meta: strings.Join(meta, "\n"), Field: "title", Error: errs["title"]}, actions(t)...)
	header.ID = "heading"
	fields := []sdui.Node{header}
	described := t.Description != "" && !editing
	if described {
		fields = append(fields, sdui.Document("doc", sdui.DocumentProps{HTML: describe(t.Description), Rev: descriptionRev(t.Description), Size: sdui.SizeFit}))
	}
	if t.Description != "" {
		label := "Done editing"
		if described {
			label = "Edit description"
		}
		fields = append(fields, sdui.HStack(sdui.WithVariant(sdui.WithAction(sdui.Button(actDescribe, label, "pencil"), actDescribe), sdui.VariantGhost)))
	}
	if t.Status != statusInProgress {
		fields = append(fields, sdui.HStack(sdui.MutedText("Status"), sdui.Select("status", options(editStatuses, statusLabels), t.Status)))
	}
	fields = append(fields, sdui.HStack(sdui.MutedText("Priority"), sdui.Select("priority", options(priorities, priorityText), t.Priority)))
	tags := sdui.TextField(tagsField, strings.Join(t.Tags, ", "), "voice, phone")
	if errs[tagsField] != "" {
		tags = sdui.WithError(tags, errs[tagsField])
	}
	fields = append(fields, sdui.HStack(sdui.MutedText("Tags"), tags))
	if !described {
		fields = append(fields, sdui.HStack(sdui.MutedText("Description"), sdui.WithLines(sdui.TextField("description", t.Description, "Markdown details an agent needs"), formLines)))
	}
	fields = append(fields, sdui.WithShortcut(sdui.WithVariant(saved, sdui.VariantGhost), "cmd+s"))
	form := sdui.VStack(sdui.WithAutosave(sdui.WithAction(sdui.Form("edit", fields...), "save."+strconv.Itoa(t.ID))))
	form.ID = fmt.Sprintf("task-%d", t.ID)
	return sdui.Scroll(form)
}

func describe(desc string) string {
	if desc == "" {
		return ""
	}
	return docrender.Render([]byte(desc), docrender.Options{}).HTML
}

func descriptionRev(desc string) int {
	h := fnv.New32a()
	h.Write([]byte(desc))
	return int(h.Sum32()&0x7fffffff) | 1
}

func actions(t *Task) []sdui.Node {
	button := func(id, label, icon, variant string) sdui.Node {
		return sdui.WithVariant(sdui.WithAction(sdui.Button(id, label, icon), id), variant)
	}
	var buttons []sdui.Node
	switch t.Status {
	case statusTodo, statusIdea:
		buttons = append(buttons, sdui.WithShortcut(button("start", "Start agent", "play.fill", sdui.VariantPrimary), "cmd+return"))
	case statusInProgress:
		buttons = append(buttons, button("release", "Release", "arrow.uturn.backward", sdui.VariantSecondary))
		if strings.HasPrefix(t.ClaimedBy, sessionPfx) {
			buttons = append(buttons, sdui.WithVariant(sdui.WithAction(sdui.Button("claimer", "Show Session", "sparkles"), actReveal), sdui.VariantGhost))
		}
	}
	if t.Status != statusDone {
		buttons = append(buttons, sdui.WithShortcut(button("done", "Done", "checkmark", sdui.VariantSecondary), "d"))
	}
	buttons = append(buttons, sdui.WithShortcut(sdui.WithConfirm(button("delete", "Delete", "trash", sdui.VariantDanger), deleteConfirm(t.ID)), "cmd+delete"))
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
