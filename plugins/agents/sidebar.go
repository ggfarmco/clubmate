package main

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/sdui"
)

const (
	groupPrefix  = "wt:"
	expandPrefix = "s:"
	needsPrefix  = "needs:"
	needsKey     = "needs"
	closedKey    = "closed:"

	noSessions    = "No agent sessions"
	deleteConfirm = "Delete this agent session, its transcript and its checkpoints?"
	deleteTree    = "Delete this agent session and its %d child sessions, their transcripts and checkpoints?"
)

var (
	statusIcons = map[string]string{
		statusRunning: "circle.dotted",
		statusWaiting: "exclamationmark.circle.fill",
		statusDone:    "checkmark.circle",
		statusIdle:    "circle",
		statusError:   "xmark.octagon",
		statusClosed:  "archivebox",
	}
	statusTones = map[string]string{
		statusRunning: "accent",
		statusWaiting: "warning",
		statusDone:    "success",
		statusError:   "danger",
	}
	statusLabels = map[string]string{
		statusRunning: "Running",
		statusWaiting: "Waiting for permission",
		statusError:   "Error",
	}
	notifyLabels = map[string]string{notifyNeedsInput: "Input only", notifyMuted: "Muted"}
	notifyMenu   = []notifyChoice{
		{action: actNotifyAll, policy: notifyAll, label: "Notify About Everything"},
		{action: actNotifyNeeds, policy: notifyNeedsInput, label: "Notify Only When It Needs Input"},
		{action: actNotifyMuted, policy: notifyMuted, label: "Mute Notifications"},
	}
)

type sessionRow struct {
	Rec      record
	Activity string
	Started  time.Time
	Notify   string
}

type notifyChoice struct {
	action string
	policy string
	label  string
}

func sidebarView(rows []sessionRow, selected string, collapsed map[string]bool, now time.Time, branchOpen bool) sdui.Node {
	top := sdui.HStack(sdui.WithAction(sdui.Button("new", "New", "plus"), actNew), sdui.WithAction(sdui.Button("branch", "New in worktree…", "arrow.triangle.branch"), actShowBranch))
	if branchOpen {
		top = sdui.WithAction(sdui.Form(actNewBranch,
			sdui.TextField("branch", "", "branch…"),
			sdui.HStack(sdui.Button("start", "Start", "arrow.triangle.branch"), sdui.WithAction(sdui.Button("cancel", "Cancel", "xmark"), actHideBranch)),
		), actNewBranch)
	}
	if len(rows) == 0 {
		return sdui.VStack(top, sdui.Empty(noSessions))
	}
	byID := make(map[string]bool, len(rows))
	for _, row := range rows {
		byID[row.Rec.ID] = true
	}
	kids := map[string][]sessionRow{}
	var roots []sessionRow
	for _, row := range rows {
		if parent := row.Rec.ParentSession; parent != "" && byID[parent] {
			kids[parent] = append(kids[parent], row)
			continue
		}
		roots = append(roots, row)
	}
	type bucket struct{ open, closed []sdui.TreeItem }
	var order []string
	buckets := map[string]*bucket{}
	var needs []sdui.TreeItem
	for _, row := range roots {
		wt := row.Rec.Worktree
		b, ok := buckets[wt]
		if !ok {
			b = &bucket{}
			buckets[wt] = b
			order = append(order, wt)
		}
		item := nestedItem(row, kids, collapsed, now, map[string]bool{})
		child := row.Rec.ParentSession != ""
		if row.Rec.Closed && !child {
			b.closed = append(b.closed, item)
			continue
		}
		b.open = append(b.open, item)
		if !child && needsYou(row.Rec) {
			item = sessionItem(row, descendantCount(row.Rec.ID, kids, map[string]bool{}), now)
			item.ID = needsPrefix + item.ID
			needs = append(needs, item)
		}
	}
	var groups []sdui.TreeItem
	if len(needs) > 0 {
		groups = append(groups, groupItem(needsKey, fmt.Sprintf("Needs you · %d", len(needs)), "", "bell", needs, !collapsed[needsKey]))
	}
	for _, wt := range order {
		b := buckets[wt]
		children := b.open
		if len(b.closed) > 0 {
			key := closedKey + wt
			folded, ok := collapsed[key]
			children = append(children, groupItem(key, fmt.Sprintf("Closed · %d", len(b.closed)), "", statusIcons[statusClosed], b.closed, ok && !folded))
		}
		groups = append(groups, groupItem(wt, filepath.Base(wt), wt, "arrow.triangle.branch", children, !collapsed[wt]))
	}
	if !slices.ContainsFunc(needs, func(item sdui.TreeItem) bool { return item.ID == selected }) {
		selected = strings.TrimPrefix(selected, needsPrefix)
	}
	return sdui.VStack(top, sdui.Tree("sessions", groups, selected))
}

func nestedItem(row sessionRow, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, path map[string]bool) sdui.TreeItem {
	path[row.Rec.ID] = true
	defer delete(path, row.Rec.ID)
	var children []sessionRow
	for _, kid := range kids[row.Rec.ID] {
		if !path[kid.Rec.ID] {
			children = append(children, kid)
		}
	}
	item := sessionItem(row, descendantCount(row.Rec.ID, kids, map[string]bool{}), now)
	if len(children) == 0 {
		return item
	}
	item.HasChildren = true
	folded, ok := collapsed[expandPrefix+row.Rec.ID]
	item.Expanded = ok && !folded
	if item.Expanded {
		for _, kid := range children {
			item.Children = append(item.Children, nestedItem(kid, kids, collapsed, now, path))
		}
	}
	return item
}

func groupItem(key, label, detail, icon string, children []sdui.TreeItem, expanded bool) sdui.TreeItem {
	item := sdui.TreeItem{ID: groupPrefix + key, Label: label, Detail: detail, Icon: icon, HasChildren: true, Expanded: expanded}
	if expanded {
		item.Children = children
	}
	return item
}

func descendantCount(id string, kids map[string][]sessionRow, seen map[string]bool) int {
	seen[id] = true
	n := 0
	for _, kid := range kids[id] {
		if !seen[kid.Rec.ID] {
			n += 1 + descendantCount(kid.Rec.ID, kids, seen)
		}
	}
	return n
}

func sessionItem(row sessionRow, descendants int, now time.Time) sdui.TreeItem {
	rec := row.Rec
	confirm := deleteConfirm
	if descendants > 0 {
		confirm = fmt.Sprintf(deleteTree, descendants)
	}
	var menu []sdui.MenuEntry
	if !turnActive(rec) && rec.HandoffTo == "" {
		menu = append(menu, sdui.MenuEntry{Action: actHandoff, Label: "Hand Off to New Session"})
	}
	for _, n := range notifyMenu {
		if n.policy != cmp.Or(rec.Notify, notifyAll) {
			menu = append(menu, sdui.MenuEntry{Action: n.action, Label: n.label})
		}
	}
	if !rec.Closed {
		menu = append(menu, sdui.MenuEntry{Action: actClose, Label: "Close Session"})
	}
	menu = append(menu, sdui.MenuEntry{Action: actDelete, Label: "Delete Session", Confirm: confirm})
	return sdui.TreeItem{
		ID:     rec.ID,
		Label:  cmp.Or(rec.Title, "New session"),
		Detail: strings.Join(nonEmpty(notifyLabels[row.Notify], activity(row, now)), " · "),
		Icon:   statusIcons[rec.Status],
		Tone:   statusTones[rec.Status],
		Menu:   menu,
	}
}

func needsYou(rec record) bool {
	return rec.Status == statusWaiting || rec.Status == statusDone && rec.Asks
}

func activity(row sessionRow, now time.Time) string {
	rec := row.Rec
	label := cmp.Or(statusLabels[rec.Status], rec.Status)
	switch rec.Status {
	case statusRunning:
		var parts []string
		if !row.Started.IsZero() {
			parts = append(parts, elapsed(now.Sub(row.Started)))
		}
		return cmp.Or(strings.Join(nonEmpty(append(parts, row.Activity)...), " · "), label)
	case statusWaiting:
		return strings.Join(nonEmpty(label, row.Activity), " · ")
	case statusError:
		if line := firstLine(rec.Error); line != "" {
			return label + " · " + line
		}
		return label
	}
	return ago(rec.UpdatedAt, now)
}

func elapsed(d time.Duration) string {
	mins := int(d / time.Minute)
	switch {
	case mins < 1:
		return "<1 min"
	case mins < 60:
		return strconv.Itoa(mins) + " min"
	}
	return strconv.Itoa(mins/60) + " h " + strconv.Itoa(mins%60) + " min"
}

func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h ago"
	case d < 48*time.Hour:
		return "yesterday"
	}
	return t.Local().Format("Jan 2")
}
