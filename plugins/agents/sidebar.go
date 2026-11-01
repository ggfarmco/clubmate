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
	"clubmate/plugins/agents/api"
)

const (
	groupPrefix  = "wt:"
	expandPrefix = "s:"
	closedKey    = "closed:"
	runPrefix    = "run:"
	graphIcon    = "point.3.connected.trianglepath.dotted"
	graphRun     = "Graph run"

	noSessions    = "No agent sessions"
	deleteConfirm = "Delete this agent session, its transcript and its checkpoints?"
	deleteTree    = "Delete this agent session and its %d child sessions, their transcripts and checkpoints?"
	noneNeedYou   = "Nothing needs you"
	noneRunning   = "No running sessions"

	filterNode    = "filter"
	filterAll     = "all"
	filterNeeds   = "needs"
	filterRunning = "running"

	statusAsks      = "asks"
	childrenRunning = "childrenRunning"
	childrenWaiting = "childrenWaiting"
	stoppedDetail   = "error"
)

var (
	statusIcons = map[string]string{
		statusRunning:   "play.circle",
		statusWaiting:   "exclamationmark.circle",
		statusAsks:      "exclamationmark.circle",
		statusDone:      "checkmark.circle",
		statusIdle:      "circle",
		statusError:     "xmark.circle",
		statusClosed:    "archivebox",
		childrenRunning: "play.circle",
		childrenWaiting: "exclamationmark.circle",
	}
	statusTones = map[string]string{
		statusRunning:   "accent",
		statusWaiting:   "warning",
		statusAsks:      "warning",
		statusError:     "danger",
		childrenRunning: "accent",
		childrenWaiting: "warning",
	}
	statusLabels = map[string]string{
		statusRunning:   "Running",
		statusWaiting:   "waiting",
		statusAsks:      "asking",
		statusDone:      "Done",
		statusIdle:      "idle",
		statusError:     "Stopped",
		statusClosed:    "Closed",
		childrenRunning: "Waiting for child sessions",
		childrenWaiting: "Child session waiting",
	}
	childDisplay = map[string]string{statusRunning: childrenRunning, statusWaiting: childrenWaiting}
	filterEmpty  = map[string]string{filterNeeds: noneNeedYou, filterRunning: noneRunning}
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
	Children string
	Branch   string
	Asking   bool
	Since    time.Time
}

type notifyChoice struct {
	action string
	policy string
	label  string
}

func sidebarView(rows []sessionRow, selected string, collapsed map[string]bool, now time.Time, branchOpen bool, filter string) sdui.Node {
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
	oldest := map[string]time.Time{}
	for _, row := range roots {
		oldestNeed(row, kids, oldest, map[string]bool{})
	}
	filter = cmp.Or(filter, filterAll)
	tabs := filterTabs(roots, filter)
	roots = slices.DeleteFunc(roots, func(row sessionRow) bool { return !matchesFilter(row, filter) })
	if filter == filterNeeds {
		slices.SortStableFunc(roots, func(x, y sessionRow) int { return compareSince(oldest[x.Rec.ID], oldest[y.Rec.ID]) })
	}
	if len(roots) == 0 {
		all := sdui.WithAction(sdui.Button("filterAll", "Show all", ""), actFilterAll)
		return sdui.VStack(top, tabs, sdui.EmptyState(filterEmpty[filter], sdui.EmptyProps{Action: &all}))
	}
	type bucket struct {
		label        string
		open, closed []sessionRow
	}
	var order []string
	buckets := map[string]*bucket{}
	for _, row := range roots {
		wt := row.Rec.Worktree
		b, ok := buckets[wt]
		if !ok {
			b = &bucket{label: cmp.Or(row.Branch, filepath.Base(wt))}
			buckets[wt] = b
			order = append(order, wt)
		}
		if row.Rec.closed() && row.Rec.ParentSession == "" {
			b.closed = append(b.closed, row)
			continue
		}
		b.open = append(b.open, row)
	}
	var groups []sdui.TreeItem
	for _, wt := range order {
		b := buckets[wt]
		children := runItems(b.open, wt, kids, collapsed, now, map[string]bool{}, oldest)
		if len(b.closed) > 0 {
			key := closedKey + wt
			folded, ok := collapsed[key]
			closed := runItems(b.closed, key, kids, collapsed, now, map[string]bool{}, oldest)
			children = append(children, groupItem(key, fmt.Sprintf("Closed · %d", len(b.closed)), statusIcons[statusClosed], closed, ok && !folded))
		}
		group := groupItem(wt, b.label, "arrow.triangle.branch", children, !collapsed[wt] || anyNeeds(b.open, oldest))
		group.Truncate = "middle"
		groups = append(groups, group)
	}
	return sdui.VStack(top, tabs, sdui.Tree("sessions", groups, selected))
}

func filterTabs(roots []sessionRow, filter string) sdui.Node {
	counts := map[string]int{}
	for _, row := range roots {
		if row.Rec.closed() {
			continue
		}
		for _, f := range []string{filterAll, filterNeeds, filterRunning} {
			if matchesFilter(row, f) {
				counts[f]++
			}
		}
	}
	needs := sdui.Item{ID: filterNeeds, Label: "Needs you", Icon: statusIcons[statusWaiting], Badge: strconv.Itoa(counts[filterNeeds])}
	if counts[filterNeeds] > 0 {
		needs.Tone = statusTones[statusWaiting]
	}
	return sdui.WithAction(sdui.Tabs(filterNode, []sdui.Item{
		{ID: filterAll, Label: "All", Badge: strconv.Itoa(counts[filterAll])},
		needs,
		{ID: filterRunning, Label: "Running", Icon: statusIcons[statusRunning], Badge: strconv.Itoa(counts[filterRunning])},
	}, filter), actFilter)
}

func matchesFilter(row sessionRow, filter string) bool {
	switch filter {
	case filterNeeds:
		return needsYou(row.Rec) || row.Children == statusWaiting
	case filterRunning:
		return row.Rec.Status == statusRunning || row.Children == statusRunning
	}
	return true
}

func oldestNeed(row sessionRow, kids map[string][]sessionRow, oldest map[string]time.Time, seen map[string]bool) time.Time {
	seen[row.Rec.ID] = true
	var at time.Time
	if needsYou(row.Rec) {
		at = cmp.Or(row.Since, row.Rec.UpdatedAt)
	}
	for _, kid := range kids[row.Rec.ID] {
		if seen[kid.Rec.ID] {
			continue
		}
		if k := oldestNeed(kid, kids, oldest, seen); !k.IsZero() && (at.IsZero() || k.Before(at)) {
			at = k
		}
	}
	if !at.IsZero() {
		oldest[row.Rec.ID] = at
	}
	return at
}

func compareSince(x, y time.Time) int {
	switch {
	case x.IsZero() || y.IsZero():
		return cmp.Compare(boolInt(x.IsZero()), boolInt(y.IsZero()))
	}
	return x.Compare(y)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func anyNeeds(rows []sessionRow, oldest map[string]time.Time) bool {
	return slices.ContainsFunc(rows, func(row sessionRow) bool { return !oldest[row.Rec.ID].IsZero() })
}

func nestedItem(row sessionRow, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, path map[string]bool, oldest map[string]time.Time) sdui.TreeItem {
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
	item.Expanded = ok && !folded || anyNeeds(children, oldest)
	if item.Expanded {
		item.Children = runItems(children, row.Rec.ID, kids, collapsed, now, path, oldest)
	}
	return item
}

func runItems(rows []sessionRow, scope string, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, path map[string]bool, oldest map[string]time.Time) []sdui.TreeItem {
	type entry struct {
		row  sessionRow
		run  *api.Run
		rows []sessionRow
	}
	var entries []*entry
	byRun := map[string]*entry{}
	for _, row := range rows {
		r := row.Rec.Run
		if r == nil {
			entries = append(entries, &entry{row: row})
			continue
		}
		e, ok := byRun[r.ID]
		if !ok {
			e = &entry{run: r}
			byRun[r.ID] = e
			entries = append(entries, e)
		}
		e.rows = append(e.rows, row)
	}
	items := make([]sdui.TreeItem, 0, len(entries))
	for _, e := range entries {
		if e.run == nil {
			items = append(items, nestedItem(e.row, kids, collapsed, now, path, oldest))
			continue
		}
		slices.SortStableFunc(e.rows, func(x, y sessionRow) int { return x.Rec.CreatedAt.Compare(y.Rec.CreatedAt) })
		nodes := make([]sdui.TreeItem, 0, len(e.rows))
		for _, row := range e.rows {
			item := nestedItem(row, kids, collapsed, now, path, oldest)
			item.Label = nodeLabel(row.Rec)
			nodes = append(nodes, item)
		}
		key := runGroupKey(scope, e.run.ID)
		run := groupItem(key, cmp.Or(e.run.Title, graphRun), graphIcon, nodes, !collapsed[key] || anyNeeds(e.rows, oldest))
		run.Detail = runActivity(e.rows, now)
		items = append(items, run)
	}
	return items
}

func runGroupKey(scope, run string) string {
	return runPrefix + scope + "/" + run
}

func nodeLabel(rec record) string {
	label := cmp.Or(rec.Title, "New session")
	if rec.Run == nil || rec.Run.Title == "" {
		return label
	}
	return cmp.Or(strings.TrimPrefix(label, rec.Run.Title+" · "), label)
}

func runActivity(rows []sessionRow, now time.Time) string {
	var running bool
	var latest time.Time
	for _, row := range rows {
		if needsYou(row.Rec) {
			return nodeLabel(row.Rec) + " needs you"
		}
		running = running || row.Rec.Status == statusRunning || row.Children != ""
		if row.Rec.UpdatedAt.After(latest) {
			latest = row.Rec.UpdatedAt
		}
	}
	if running {
		return statusLabels[statusRunning]
	}
	return ago(latest, now)
}

func groupItem(key, label, icon string, children []sdui.TreeItem, expanded bool) sdui.TreeItem {
	item := sdui.TreeItem{ID: groupPrefix + key, Label: label, Icon: icon, HasChildren: true, Expanded: expanded}
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
	if !rec.closed() {
		menu = append(menu, sdui.MenuEntry{Action: actClose, Label: "Close Session"})
	}
	menu = append(menu, sdui.MenuEntry{Action: actDelete, Label: "Delete Session", Confirm: confirm})
	display := displayStatus(rec, row.Children)
	return sdui.TreeItem{
		ID:     rec.ID,
		Label:  cmp.Or(rec.Title, "New session"),
		Detail: strings.Join(nonEmpty(afkDetail(rec), notifyLabels[row.Notify], activity(row, now)), " · "),
		Icon:   statusIcons[display],
		Tone:   statusTones[display],
		Menu:   menu,
	}
}

func afkDetail(rec record) string {
	if rec.AFK {
		return afkLabel
	}
	return ""
}

func displayStatus(rec record, children string) string {
	if display, ok := childDisplay[children]; ok {
		return display
	}
	if needsYou(rec) && rec.Status == statusDone {
		return statusAsks
	}
	return rec.Status
}

func needsYou(rec record) bool {
	return rec.Status == statusWaiting || rec.Status == statusDone && rec.Asks && rec.ParentSession == ""
}

func activity(row sessionRow, now time.Time) string {
	rec := row.Rec
	display := displayStatus(rec, row.Children)
	label := cmp.Or(statusLabels[display], rec.Status)
	switch display {
	case statusWaiting:
		if row.Asking {
			return statusLabels[statusAsks]
		}
		return label
	case childrenRunning, childrenWaiting, statusAsks, statusIdle:
		return label
	case statusRunning:
		var parts []string
		if !row.Started.IsZero() {
			parts = append(parts, elapsed(now.Sub(row.Started)))
		}
		return cmp.Or(strings.Join(nonEmpty(append(parts, row.Activity)...), " · "), label)
	case statusError:
		return label + " · " + cmp.Or(firstLine(rec.Error), stoppedDetail)
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
