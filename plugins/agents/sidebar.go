package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"path"
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
	serviceKey   = "service:"
	moreID       = groupPrefix + "more:closed"
	closedShown  = 3
	closedBatch  = 25
	closedMore   = "Show %d more"
	closedLeft   = "Show %d more · %d left"
	serviceIcon  = "gearshape.2"
	runPrefix    = "run:"
	graphIcon    = "point.3.connected.trianglepath.dotted"
	graphRun     = "Graph run"

	noSessions    = "No agent sessions"
	deleteConfirm = "Delete this agent session, its transcript and its checkpoints?"
	deleteTree    = "Delete this agent session and its %d child sessions, their transcripts and checkpoints?"
	noneNeedYou   = "Nothing needs you"
	noneRunning   = "No running sessions"

	searchNode = "search"
	searchTree = "searchResults"
	hitPrefix  = "hit:"
	noMatches  = "No sessions match “%s”"

	filterNode    = "filter"
	filterAll     = "all"
	filterNeeds   = "needs"
	filterOpen    = "open"
	filterRunning = "running"

	statusAsks       = "asks"
	statusAsksParent = "asksParent"
	childrenRunning  = "childrenRunning"
	childrenWaiting  = "childrenWaiting"
	stoppedDetail    = "error"
	autoClosedLabel  = "auto"
)

var (
	statusIcons = map[string]string{
		statusRunning:    "play.circle",
		statusWaiting:    "exclamationmark.circle",
		statusAsks:       "exclamationmark.circle",
		statusAsksParent: "arrow.up.circle",
		statusDone:       "checkmark.circle",
		statusReview:     "eye.circle",
		statusIdle:       "circle",
		statusError:      "xmark.circle",
		statusClosed:     "archivebox",
		childrenRunning:  "play.circle",
		childrenWaiting:  "exclamationmark.circle",
	}
	statusTones = map[string]string{
		statusRunning:    "accent",
		statusWaiting:    "warning",
		statusAsks:       "warning",
		statusAsksParent: "accent",
		statusError:      "danger",
		statusReview:     "accent",
		childrenRunning:  "accent",
		childrenWaiting:  "warning",
	}
	statusLabels = map[string]string{
		statusRunning:    "Running",
		statusWaiting:    "waiting",
		statusAsks:       "asking",
		statusAsksParent: "asking parent",
		statusDone:       "Done",
		statusReview:     "review",
		statusIdle:       "idle",
		statusError:      "Stopped",
		statusClosed:     "Closed",
		childrenRunning:  "Waiting for child sessions",
		childrenWaiting:  "Child session waiting",
	}
	closedGlyphs = map[string]string{statusDone: statusDone, statusError: statusError}
	hitIcons     = map[string]string{hitPrompt: "person", hitReply: "text.bubble"}
	childDisplay = map[string]string{statusRunning: childrenRunning, statusWaiting: childrenWaiting}
	filterEmpty  = map[string]string{filterNeeds: noneNeedYou, filterOpen: noneOpen, filterRunning: noneRunning}
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
	CloseAsk string
	Branch   string
	Detached bool
	Asking   bool
	Since    time.Time
	Review   *reviewProgress
	RunNeed  *runNeed
	RunRow   *api.RunRow
	Nest     *reviewerNest
	Open     int
	Gate     string
}

type sidebarPlus struct {
	runs   []api.RunRow
	dock   []api.DockRow
	gates  []sdui.TreeItem
	graphs []sdui.TreeItem
	open   string
}

const (
	sessionsTree  = "sessions"
	removeKey     = "cmd+delete"
	actRemoveRun  = "removeRun"
	openRun       = "run"
	actNewGate    = "newGate"
	actNewGraph   = "newGraph"
	dockGateHelp  = "gate-help"
	dockGraphHelp = "graph-help"
	dockCap       = 6
)

func runMenu(r api.RunRow) []sdui.MenuEntry {
	if r.State == runRunning || r.State == runAsks {
		return nil
	}
	return []sdui.MenuEntry{{Action: actRemoveRun, Label: "Remove run…", Confirm: "Remove graph run “" + cmp.Or(r.Title, graphRun) + "” from the list? Its sessions stay.", Key: removeKey}}
}

func gateRemoveMenu(name string) []sdui.MenuEntry {
	return []sdui.MenuEntry{{Action: actGateDelete, Label: "Delete land gate…", Confirm: gateDeleteConfirm(name, ""), Key: removeKey}}
}

func openDock(current, id string, expand bool) string {
	if expand {
		return id
	}
	if current == id {
		return ""
	}
	return current
}

func (plus sidebarPlus) treeDock(service []sdui.TreeItem, query string) []sdui.TreeDock {
	rows := map[string][]sdui.TreeItem{dockService: service, dockGates: plus.gates, dockGraphs: plus.graphs}
	add := map[string]*sdui.TreeAdd{dockGates: {Action: actNewGate, Label: "New land gate…"}, dockGraphs: {Action: actNewGraph, Label: "New graph…"}}
	out := make([]sdui.TreeDock, 0, len(plus.dock))
	matched := false
	for _, d := range plus.dock {
		section := sdui.TreeDock{ID: d.ID, Label: d.Label, Icon: d.Icon, Detail: d.Detail, Attention: d.Attention, Rows: rows[d.ID], Add: add[d.ID], Cap: dockCap, Expanded: d.ID == plus.open}
		if query != "" {
			section.Rows = slices.DeleteFunc(slices.Clone(section.Rows), func(item sdui.TreeItem) bool {
				return !strings.Contains(strings.ToLower(item.Label), strings.ToLower(query))
			})
			section.Expanded = len(section.Rows) > 0 && !matched
			matched = matched || section.Expanded
		}
		out = append(out, section)
	}
	return out
}

type sidebarSearch struct {
	query   string
	results []searchResult
}

type notifyChoice struct {
	action string
	policy string
	label  string
}

func sidebarView(rows []sessionRow, selected string, collapsed map[string]bool, now time.Time, home string, branchOpen bool, filter string, search sidebarSearch, closedExtra int, plus sidebarPlus) sdui.Node {
	top := sdui.HStack(sdui.WithAction(sdui.Button("new", "New", "plus"), actNew), sdui.WithAction(sdui.Button("branch", "New in worktree…", "arrow.triangle.branch"), actShowBranch))
	if branchOpen {
		top = sdui.WithAction(sdui.Form(actNewBranch,
			sdui.TextField("branch", "", "branch…"),
			sdui.HStack(sdui.Button("start", "Start", "arrow.triangle.branch"), sdui.WithAction(sdui.Button("cancel", "Cancel", "xmark"), actHideBranch)),
		), actNewBranch)
	}
	byID := make(map[string]bool, len(rows))
	open := make(map[string]bool, len(rows))
	for _, row := range rows {
		byID[row.Rec.ID] = true
		open[row.Rec.ID] = !row.Rec.closed()
	}
	kids := map[string][]sessionRow{}
	var roots []sessionRow
	for _, row := range rows {
		parent := row.Rec.ParentSession
		if row.Nest != nil && open[row.Nest.Lander] {
			parent = row.Nest.Lander
		}
		if parent != "" && byID[parent] {
			kids[parent] = append(kids[parent], row)
			continue
		}
		roots = append(roots, row)
	}
	var serviceRows []sessionRow
	for _, row := range roots {
		if row.Rec.restricted() && !row.Rec.closed() {
			serviceRows = append(serviceRows, row)
		}
	}
	docked := func(tree sdui.Node) sdui.Node {
		service := runItems(serviceRows, serviceKey, kids, collapsed, now, map[string]bool{}, map[string]time.Time{})
		if search.query != "" {
			service = slices.DeleteFunc(service, func(item sdui.TreeItem) bool {
				return slices.ContainsFunc(search.results, func(r searchResult) bool { return r.Session == item.ID })
			})
		}
		return sdui.WithTreeDock(tree, plus.treeDock(service, search.query))
	}
	emptied := plus.emptiedRuns()
	dockOnly := func(nodes ...sdui.Node) sdui.Node {
		var groups []sdui.TreeItem
		if len(emptied) > 0 {
			folded, ok := collapsed[closedKey]
			group := groupItem(closedKey, "Closed", statusIcons[statusClosed], emptied, ok && !folded)
			group.Detail = strconv.Itoa(len(emptied))
			groups = append(groups, group)
		}
		if len(plus.dock) > 0 || len(groups) > 0 {
			nodes = append(nodes, docked(sdui.Tree(sessionsTree, groups, selected)))
		}
		return sdui.VStack(nodes...)
	}
	if len(rows) == 0 {
		return dockOnly(top, sdui.Empty(noSessions))
	}
	field := sdui.WithAction(sdui.WithLive(sdui.TextField(searchNode, search.query, "Search sessions")), actSearch)
	if search.query != "" {
		results := searchResults(search, selected, now)
		if results.Type != "tree" {
			return dockOnly(top, field, results)
		}
		return sdui.VStack(top, field, docked(results))
	}
	oldest := map[string]time.Time{}
	for _, row := range roots {
		oldestNeed(row, kids, oldest, map[string]bool{})
	}
	filter = cmp.Or(filter, filterAll)
	runs := map[string][]string{}
	for i, row := range roots {
		runs[row.Rec.ID] = runsNeeding(row, kids, map[string]bool{})
		roots[i].Open = openTotal(row, kids, map[string]bool{})
	}
	tabs := filterTabs(roots, filter, runs)
	roots = slices.DeleteFunc(roots, func(row sessionRow) bool {
		return !matchesFilter(row, filter) && (filter != filterNeeds || len(runs[row.Rec.ID]) == 0)
	})
	if filter == filterNeeds {
		slices.SortStableFunc(roots, func(x, y sessionRow) int { return compareSince(oldest[x.Rec.ID], oldest[y.Rec.ID]) })
	}
	if len(roots) == 0 {
		all := sdui.WithAction(sdui.Button("filterAll", "Show all", ""), actFilterAll)
		return dockOnly(top, field, tabs, sdui.EmptyState(filterEmpty[filter], sdui.EmptyProps{Action: &all}))
	}
	type bucket struct {
		label    string
		detached bool
		open     []sessionRow
	}
	var order []string
	var closed []sessionRow
	buckets := map[string]*bucket{}
	for _, row := range roots {
		switch {
		case row.Rec.closed():
			closed = append(closed, row)
			continue
		case row.Rec.restricted():
			continue
		}
		wt := row.Rec.Worktree
		b, ok := buckets[wt]
		if !ok {
			b = &bucket{label: cmp.Or(row.Branch, filepath.Base(wt)), detached: row.Detached}
			buckets[wt] = b
			order = append(order, wt)
		}
		b.open = append(b.open, row)
	}
	var groups []sdui.TreeItem
	for _, wt := range order {
		b := buckets[wt]
		children := runItems(b.open, wt, kids, collapsed, now, map[string]bool{}, oldest)
		if len(order) == 1 && wt == home {
			groups = children
			break
		}
		group := groupItem(wt, b.label, "arrow.triangle.branch", children, !collapsed[wt] || anyNeeds(b.open, oldest))
		group.Truncate = "middle"
		group.Faint = b.detached
		groups = append(groups, group)
	}
	if len(closed) > 0 || len(emptied) > 0 {
		folded, ok := collapsed[closedKey]
		items := closedItems(closed, selected, closedShown+closedExtra, kids, collapsed, now, oldest)
		group := groupItem(closedKey, "Closed", statusIcons[statusClosed], append(items, emptied...), ok && !folded)
		group.Detail = strconv.Itoa(len(closed) + len(emptied))
		groups = append(groups, group)
	}
	uniqueRunIDs(groups, map[string]int{})
	return sdui.VStack(top, field, tabs, docked(sdui.Tree(sessionsTree, groups, selected)))
}

func (plus sidebarPlus) emptiedRuns() []sdui.TreeItem {
	var out []sdui.TreeItem
	for _, r := range plus.runs {
		if len(r.Sessions) == 0 {
			out = append(out, sdui.TreeItem{ID: runPrefix + r.ID, Label: cmp.Or(r.Title, graphRun), Icon: graphIcon, Detail: r.Detail, Faint: true, Open: openRun, Menu: runMenu(r)})
		}
	}
	return out
}

func uniqueRunIDs(items []sdui.TreeItem, seen map[string]int) {
	for i := range items {
		if strings.HasPrefix(items[i].ID, runPrefix) {
			seen[items[i].ID]++
			if n := seen[items[i].ID]; n > 1 {
				items[i].ID += "@" + strconv.Itoa(n)
			}
		}
		uniqueRunIDs(items[i].Children, seen)
	}
}

func foldKey(id string) string {
	if wt, ok := strings.CutPrefix(id, groupPrefix); ok {
		return wt
	}
	if strings.HasPrefix(id, runPrefix) {
		return expandPrefix + runPrefix + runOf(id)
	}
	return expandPrefix + id
}

func runOf(id string) string {
	run, _, _ := strings.Cut(strings.TrimPrefix(id, runPrefix), "@")
	return run
}

func closedItems(rows []sessionRow, selected string, limit int, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, oldest map[string]time.Time) []sdui.TreeItem {
	slices.SortStableFunc(rows, func(x, y sessionRow) int { return y.Rec.UpdatedAt.Compare(x.Rec.UpdatedAt) })
	shown := rows[:min(len(rows), limit)]
	if i := slices.IndexFunc(rows, func(row sessionRow) bool { return row.Rec.ID == selected }); i >= limit {
		shown = append(slices.Clone(shown), rows[i])
	}
	items := runItems(shown, closedKey, kids, collapsed, now, map[string]bool{}, oldest)
	worktrees := map[string]bool{}
	for _, row := range rows {
		worktrees[row.Rec.Worktree] = true
	}
	if len(worktrees) > 1 {
		short := map[string]string{}
		for _, row := range shown {
			short[row.Rec.ID] = path.Base(cmp.Or(row.Branch, row.Rec.Worktree))
		}
		for i := range items {
			if name, ok := short[items[i].ID]; ok {
				items[i].Detail = name
			}
		}
	}
	if more := len(rows) - len(shown); more > 0 {
		label := fmt.Sprintf(closedMore, more)
		if more > closedBatch {
			label = fmt.Sprintf(closedLeft, closedBatch, more)
		}
		items = append(items, sdui.TreeItem{ID: moreID, Label: label, Faint: true, Action: true})
	}
	return items
}

func searchResults(search sidebarSearch, selected string, now time.Time) sdui.Node {
	if len(search.results) == 0 {
		return sdui.Empty(fmt.Sprintf(noMatches, search.query))
	}
	items := make([]sdui.TreeItem, 0, len(search.results))
	for _, res := range search.results {
		item := sdui.TreeItem{ID: res.Session, Label: cmp.Or(res.Title, "New session"), Detail: ago(res.UpdatedAt, now), Icon: statusIcons[res.Status], Tone: statusTones[res.Status]}
		for i, hit := range res.Hits {
			if hit.Kind == hitTitle {
				continue
			}
			item.Children = append(item.Children, sdui.TreeItem{ID: hitPrefix + res.Session + ":" + strconv.Itoa(i), Label: hit.Snippet, Icon: hitIcons[hit.Kind], Muted: true})
		}
		item.HasChildren = len(item.Children) > 0
		item.Expanded = item.HasChildren
		items = append(items, item)
	}
	return sdui.Tree(searchTree, items, selected)
}

func stepSession(sidebar []byte, by int) string {
	var root sdui.Node
	if by == 0 || json.Unmarshal(sidebar, &root) != nil {
		return ""
	}
	tree, ok := findTree(root)
	if !ok {
		return ""
	}
	raw, err := json.Marshal(tree.Props["items"])
	if err != nil {
		return ""
	}
	var items []sdui.TreeItem
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	ids := shownSessions(items)
	if len(ids) == 0 {
		return ""
	}
	selected, _ := tree.Props["selected"].(string)
	at := slices.Index(ids, selected)
	if at < 0 && by < 0 {
		at = 0
	}
	return ids[((at+by)%len(ids)+len(ids))%len(ids)]
}

func findTree(n sdui.Node) (sdui.Node, bool) {
	if n.Type == "tree" && (n.ID == "sessions" || n.ID == searchTree) {
		return n, true
	}
	for _, c := range n.Children {
		if tree, ok := findTree(c); ok {
			return tree, true
		}
	}
	return sdui.Node{}, false
}

func shownSessions(items []sdui.TreeItem) []string {
	var ids []string
	for _, item := range items {
		if !strings.HasPrefix(item.ID, groupPrefix) && !strings.HasPrefix(item.ID, hitPrefix) && !strings.HasPrefix(item.ID, repoPrefix) && !strings.HasPrefix(item.ID, runPrefix) {
			ids = append(ids, item.ID)
		}
		ids = append(ids, shownSessions(item.Children)...)
	}
	return ids
}

func hitSession(id string) (string, bool) {
	rest, ok := strings.CutPrefix(id, hitPrefix)
	if !ok {
		return "", false
	}
	session, _, _ := strings.Cut(rest, ":")
	return session, true
}

func filterTabs(roots []sessionRow, filter string, runs map[string][]string) sdui.Node {
	counts := map[string]int{}
	counted := map[string]bool{}
	for _, row := range roots {
		fresh := false
		for _, run := range runs[row.Rec.ID] {
			fresh = fresh || !counted[run]
			counted[run] = true
		}
		if fresh && !ownNeeds(row) {
			counts[filterNeeds]++
		}
		if row.Rec.closed() {
			continue
		}
		if row.Rec.restricted() {
			if matchesFilter(row, filterNeeds) {
				counts[filterNeeds]++
			}
			continue
		}
		for _, f := range []string{filterAll, filterNeeds, filterRunning} {
			if matchesFilter(row, f) {
				counts[f]++
			}
		}
		counts[filterOpen] += row.Open
	}
	needs := sdui.Item{ID: filterNeeds, Label: "Needs you", Icon: statusIcons[statusWaiting], Badge: strconv.Itoa(counts[filterNeeds])}
	if counts[filterNeeds] > 0 {
		needs.Tone = statusTones[statusWaiting]
	}
	return sdui.WithAction(sdui.Tabs(filterNode, []sdui.Item{
		{ID: filterAll, Label: "All", Badge: strconv.Itoa(counts[filterAll])},
		needs,
		{ID: filterOpen, Label: "Open", Icon: statusIcons[statusDone], Badge: strconv.Itoa(counts[filterOpen]), Shortcut: openFilterKey},
		{ID: filterRunning, Label: "Running", Icon: statusIcons[statusRunning], Badge: strconv.Itoa(counts[filterRunning])},
	}, filter), actFilter)
}

func ownNeeds(row sessionRow) bool {
	return needsYou(row.Rec) || row.Children == statusWaiting
}

func runsNeeding(row sessionRow, kids map[string][]sessionRow, seen map[string]bool) []string {
	seen[row.Rec.ID] = true
	var out []string
	if row.RunNeed != nil {
		out = append(out, row.RunNeed.Run)
	}
	for _, kid := range kids[row.Rec.ID] {
		if !seen[kid.Rec.ID] {
			out = append(out, runsNeeding(kid, kids, seen)...)
		}
	}
	return out
}

func matchesFilter(row sessionRow, filter string) bool {
	switch filter {
	case filterNeeds:
		return ownNeeds(row)
	case filterRunning:
		return !row.Rec.restricted() && (row.Rec.Status == statusRunning || row.Children == statusRunning)
	case filterOpen:
		return !row.Rec.restricted() && (row.Open > 0 || row.Rec.MayAsk)
	}
	return true
}

func openTotal(row sessionRow, kids map[string][]sessionRow, seen map[string]bool) int {
	seen[row.Rec.ID] = true
	total := row.Rec.OpenCount
	for _, kid := range kids[row.Rec.ID] {
		if !seen[kid.Rec.ID] {
			total += openTotal(kid, kids, seen)
		}
	}
	return total
}

func oldestNeed(row sessionRow, kids map[string][]sessionRow, oldest map[string]time.Time, seen map[string]bool) time.Time {
	seen[row.Rec.ID] = true
	var at time.Time
	if needsYou(row.Rec) {
		at = cmp.Or(row.Since, row.Rec.UpdatedAt)
	}
	if n := row.RunNeed; n != nil && (at.IsZero() || n.Since.Before(at)) {
		at = n.Since
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
	if row.Nest != nil {
		item.Label, item.Detail = row.Nest.Round.Label, row.Nest.Round.Verdict
	}
	if entries := row.Rec.repoEntries(); len(entries) > 0 {
		return taskParent(item, row, entries, children, kids, collapsed, now, path, oldest)
	}
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
		folded, ok := collapsed[expandPrefix+runPrefix+e.run.ID]
		run := sdui.TreeItem{ID: runPrefix + e.run.ID, Label: cmp.Or(e.run.Title, graphRun), Icon: graphIcon, HasChildren: true, Open: openRun}
		run.Expanded = !(ok && folded) || anyNeeds(e.rows, oldest)
		if run.Expanded {
			run.Children = nodes
		}
		run.Detail = runActivity(e.rows, now)
		if rr := e.rows[0].RunRow; rr != nil {
			run.Detail = rr.Detail
			run.Menu = runMenu(*rr)
			if rr.NeedsYou {
				run.Tone = statusTones[statusWaiting]
			}
		} else if n := e.rows[0].RunNeed; n != nil {
			run.Detail, run.Tone = n.Text, statusTones[statusWaiting]
		}
		items = append(items, run)
	}
	return items
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
	item := sdui.TreeItem{ID: groupPrefix + key, Label: label, Icon: icon, Action: true, HasChildren: true, Expanded: expanded}
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
		menu = append(menu, sdui.MenuEntry{Action: actClose, Label: "Close Session", Confirm: row.CloseAsk})
	}
	if canArchive(row) {
		menu = append(menu, sdui.MenuEntry{Action: actArchive, Label: archiveMenu})
	}
	if rec.closed() && ownsTask(rec) {
		menu = append(menu, sdui.MenuEntry{Action: actRestore, Label: restoreMenu})
	}
	menu = append(menu, sdui.MenuEntry{Action: actDelete, Label: "Delete Session", Confirm: confirm, Key: removeKey})
	display := displayStatus(rec, row.Children)
	icon := statusIcons[cmp.Or(closedGlyphs[rec.ClosedStatus], display)]
	if reviewRow(row) {
		icon = statusIcons[statusReview]
	}
	act := rowActivity(row, now)
	var badges []sdui.HeaderBadge
	if rec.OpenCount > 0 && (display == statusDone || display == statusReview) {
		badges = append(badges, openBadge(rec.OpenCount))
		if display == statusDone {
			act = ""
		}
	}
	mayAsk := rec.MayAsk && display == statusDone && rec.OpenCount == 0
	if mayAsk {
		act = mayAskDetail
	}
	var detailTone string
	if word, tone := rowGateChip(row); word != "" {
		act, detailTone, mayAsk = word, tone, false
	}
	return sdui.TreeItem{
		ID:          rec.ID,
		Label:       cmp.Or(rec.Title, "New session"),
		Detail:      strings.Join(nonEmpty(afkDetail(rec), notifyLabels[row.Notify], act), " · "),
		DetailTone:  detailTone,
		DetailFaint: mayAsk,
		Icon:        icon,
		Tone:        statusTones[display],
		Menu:        menu,
		Badges:      badges,
	}
}

func rowGateChip(row sessionRow) (string, string) {
	if turnActive(row.Rec) || needsYou(row.Rec) || row.Rec.Status == statusError {
		return "", ""
	}
	return gateChip(row.Gate)
}

func reviewRow(row sessionRow) bool {
	if row.Review == nil {
		return false
	}
	switch displayStatus(row.Rec, row.Children) {
	case statusDone, statusIdle, statusReview:
		return true
	}
	return false
}

func rowActivity(row sessionRow, now time.Time) string {
	if reviewRow(row) {
		return row.Review.Short
	}
	return activity(row, now)
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
	if rec.AsksParent {
		return statusAsksParent
	}
	if needsYou(rec) && rec.Status == statusDone {
		return statusAsks
	}
	if rec.Review && rec.Status == statusDone {
		return statusReview
	}
	return rec.Status
}

func needsYou(rec record) bool {
	return rec.Status == statusWaiting && !rec.AsksParent || rec.Status == statusDone && rec.Asks && rec.ParentSession == "" && rec.Night == "" || rec.connectWaits() || rec.taskWaits() || rec.gateWaits()
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
	case childrenRunning, childrenWaiting, statusAsks, statusAsksParent, statusIdle, statusReview:
		return label
	case statusRunning:
		var parts []string
		if !row.Started.IsZero() {
			parts = append(parts, elapsed(now.Sub(row.Started)))
		}
		return cmp.Or(strings.Join(nonEmpty(append(parts, row.Activity)...), " · "), label)
	case statusError:
		return failureOf(rec).Word
	case statusClosed:
		if rec.AutoClosed {
			return autoClosedLabel
		}
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
