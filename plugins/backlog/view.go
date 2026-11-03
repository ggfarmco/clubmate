package main

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

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
	actStart    = "start"
	actTakeBack = "takeback"
	actLink     = "link"
	actUnlink   = "unlink"
	actAttach   = "attach"
	actDetach   = "detach"
	actOpenDoc  = "openDoc"
	linksNode   = "links"
	linkField   = "linkText"
	attachNode  = "attachments"
	attachField = "attachText"
	maxOpenName = 32
	emptyText   = "Tasks are ideas and todos, for you or for an agent. Start agent hands a task to a session; it owns the task until it ends or you take it back."
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
	linkWords    = map[string]string{linkBlockedBy: "blocked by", linkBlocks: "blocks", linkRelates: "relates to"}
	attachIcons  = map[string]string{attachFile: "doc", attachImage: "photo", attachArtifact: "square.on.square", attachURL: "link"}
	attachWords  = map[string]string{attachFile: "worktree", attachImage: "image", attachArtifact: "artifact", attachURL: "link"}
	attachCounts = map[string][2]string{attachFile: {"1 file", "%d files"}, attachImage: {"1 image", "%d images"}, attachArtifact: {"1 artifact", "%d artifacts"}, attachURL: {"1 URL", "%d URLs"}}
)

type owner struct {
	label    string
	session  bool
	stale    bool
	run      string
	runTitle string
}

type detail struct {
	owners   map[string]owner
	linked   map[int]Task
	hasTasks bool
	errs     map[string]string
	editing  bool
	rev      int
}

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

func labelOf(owners map[string]owner, id string) string {
	if o, ok := owners[id]; ok {
		return o.label
	}
	return ownerLabel(id, "", false)
}

func tagCounts(tasks []Task) map[string]int {
	counts := map[string]int{}
	for _, t := range tasks {
		if t.Status == statusDone {
			continue
		}
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

func mainView(t *Task, d detail) sdui.Node {
	switch {
	case t == nil && d.hasTasks:
		return sdui.Empty("Select a task")
	case t == nil:
		return emptyView(d.rev)
	}
	errs := d.errs
	meta := []string{fmt.Sprintf("#%d", t.ID)}
	if t.Status == statusInProgress {
		meta[0] += " · In progress by " + labelOf(d.owners, t.ClaimedBy)
		if !t.ClaimedAt.IsZero() {
			meta[0] += " since " + t.ClaimedAt.Local().Format("Jan 2 15:04")
		}
	}
	if t.LastClaimedBy != "" {
		meta = append(meta, "Last owner "+labelOf(d.owners, t.LastClaimedBy))
	}
	if t.Note != "" {
		meta = append(meta, t.Note)
	}
	saved := sdui.Button("save", "Saved", "checkmark")
	if len(errs) > 0 {
		saved = sdui.Button("save", "Not saved", "")
	}
	header := sdui.Header(t.Title, sdui.HeaderProps{Icon: statusIcons[t.Status], Meta: strings.Join(meta, "\n"), Field: "title", Error: errs["title"]}, actions(t, d.owners[t.ClaimedBy])...)
	header.ID = "heading"
	fields := []sdui.Node{header}
	described := t.Description != "" && !d.editing
	if described {
		fields = append(fields, sdui.Document("doc", sdui.DocumentProps{HTML: describe(t.Description, d.linked), Rev: descriptionRev(t.Description), Size: sdui.SizeFit}))
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
	tags := sdui.TextField(tagsField, strings.Join(t.Tags, ", "), "Tags — comma-separated words")
	if errs[tagsField] != "" {
		tags = sdui.WithError(tags, errs[tagsField])
	}
	fields = append(fields, sdui.HStack(sdui.MutedText("Tags"), tags))
	if !described {
		fields = append(fields, sdui.HStack(sdui.MutedText("Description"), sdui.WithLines(sdui.TextField("description", t.Description, "Description — markdown"), formLines)))
	}
	fields = append(fields, sdui.WithShortcut(sdui.WithVariant(saved, sdui.VariantGhost), "cmd+s"))
	form := sdui.VStack(sdui.WithAutosave(sdui.WithAction(sdui.Form("edit", fields...), "save."+strconv.Itoa(t.ID))), linksView(t, d), attachmentsView(t, d))
	form.ID = fmt.Sprintf("task-%d", t.ID)
	return sdui.Scroll(form)
}

func emptyView(rev int) sdui.Node {
	add := sdui.WithAction(sdui.Form("new", sdui.HStack(sdui.TextField("title", "", "New task…"), sdui.WithVariant(sdui.Button("add", "Add", "plus"), sdui.VariantPrimary))), "add")
	view := sdui.VStack(sdui.EmptyState(emptyText, sdui.EmptyProps{Title: "Nothing in the backlog", Icon: "checklist"}), add)
	view.ID = fmt.Sprintf("empty-%d", rev)
	return sdui.Scroll(view)
}

func linksView(t *Task, d detail) sdui.Node {
	items := make([]sdui.Item, 0, len(t.Links))
	for _, l := range t.Links {
		o, ok := d.linked[l.Task]
		if !ok {
			continue
		}
		items = append(items, sdui.Item{
			ID:     strconv.Itoa(l.Task),
			Label:  fmt.Sprintf("#%d %s", o.ID, o.Title),
			Detail: linkWords[l.Kind] + " · " + strings.ToLower(statusLabels[o.Status]),
			Icon:   statusIcons[o.Status],
			Menu:   []sdui.MenuEntry{{Action: actUnlink, Label: "Remove link"}},
		})
	}
	field := sdui.WithAction(sdui.TextField(linkField, "", "Link a task — blocks #3, blocked by #5, relates #7"), actLink)
	return section(fmt.Sprintf("links-%d-%d", t.ID, d.rev), "Links", sdui.List(linksNode, items, ""), len(items), field, d.errs[linkField])
}

func attachmentsView(t *Task, d detail) sdui.Node {
	items := make([]sdui.Item, 0, len(t.Attachments))
	for i, a := range t.Attachments {
		item := sdui.Item{ID: strconv.Itoa(i), Label: cmp.Or(a.Title, a.Ref), Detail: attachWords[a.Kind], Icon: attachIcons[a.Kind], Menu: []sdui.MenuEntry{{Action: actDetach, Label: "Remove attachment"}}}
		if a.Kind == attachFile || a.Kind == attachImage {
			item.Truncate = "middle"
		}
		items = append(items, item)
	}
	field := sdui.WithAction(sdui.TextField(attachField, "", "Attach — a worktree path or a URL"), actAttach)
	return section(fmt.Sprintf("attachments-%d-%d", t.ID, d.rev), "Attachments", sdui.List(attachNode, items, ""), len(items), field, d.errs[attachField])
}

func section(id, title string, list sdui.Node, n int, field sdui.Node, err string) sdui.Node {
	if n > 0 {
		title = fmt.Sprintf("%s · %d", title, n)
	}
	if err != "" {
		field = sdui.WithError(field, err)
	}
	children := []sdui.Node{sdui.MutedText(title)}
	if n > 0 {
		children = append(children, list)
	}
	v := sdui.VStack(append(children, field)...)
	v.ID = id
	return v
}

func describe(desc string, tasks map[int]Task) string {
	if desc == "" {
		return ""
	}
	opts := docrender.Options{}
	if tasks != nil {
		opts.Task = func(id int) (string, bool) {
			t, ok := tasks[id]
			return t.Title, ok
		}
	}
	return docrender.Render([]byte(desc), opts).HTML
}

func descriptionRev(desc string) int {
	h := fnv.New32a()
	h.Write([]byte(desc))
	return int(h.Sum32()&0x7fffffff) | 1
}

func actions(t *Task, cur owner) []sdui.Node {
	button := func(id, label, icon, variant string) sdui.Node {
		return sdui.WithVariant(sdui.WithAction(sdui.Button(id, label, icon), id), variant)
	}
	start := sdui.WithShortcut(button(actStart, startLabel(t), "play.fill", sdui.VariantPrimary), "cmd+return")
	takeBack := button(actTakeBack, "Take back", "person", sdui.VariantGhost)
	var buttons []sdui.Node
	switch {
	case t.Status == statusTodo || t.Status == statusIdea:
		buttons = append(buttons, start)
	case t.Status != statusInProgress:
	case t.ClaimedBy == userOwner:
		buttons = append(buttons, start, button("release", "Release", "arrow.uturn.backward", sdui.VariantSecondary))
	case cur.session && !cur.stale:
		name := cmp.Or(cur.runTitle, cur.label)
		icon := "sparkles"
		if cur.run != "" {
			icon = "point.3.connected.trianglepath.dotted"
		}
		open := sdui.WithVariant(sdui.WithAction(sdui.Button("open", "Open "+clip(name, maxOpenName), icon), actReveal), sdui.VariantPrimary)
		buttons = append(buttons, sdui.WithShortcut(open, "cmd+return"), takeBack)
	case cur.session:
		buttons = append(buttons, start, takeBack)
	default:
		buttons = append(buttons, takeBack)
	}
	if t.Status != statusDone {
		buttons = append(buttons, sdui.WithShortcut(button("done", "Done", "checkmark", sdui.VariantSecondary), "d"))
	}
	buttons = append(buttons, sdui.WithShortcut(sdui.WithConfirm(button("delete", "Delete", "trash", sdui.VariantDanger), deleteConfirm(t.ID)), "cmd+delete"))
	return buttons
}

func startLabel(t *Task) string {
	var parts []string
	if strings.TrimSpace(t.Description) != "" {
		parts = append(parts, "description")
	}
	if n := len(t.Links); n > 0 {
		parts = append(parts, plural(n, "1 link", "%d links"))
	}
	if n := len(t.Attachments); n > 0 {
		kind := t.Attachments[0].Kind
		words := attachCounts[kind]
		if slices.ContainsFunc(t.Attachments, func(a Attachment) bool { return a.Kind != kind }) {
			words = [2]string{"1 attachment", "%d attachments"}
		}
		parts = append(parts, plural(n, words[0], words[1]))
	}
	if len(parts) == 0 {
		return "Start agent"
	}
	return "Start agent · " + strings.Join(parts, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return fmt.Sprintf(many, n)
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
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
