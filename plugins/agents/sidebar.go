package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
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
	pausedKey    = "paused:"
	pausedTitle  = "Paused until %s"
	pausedIcon   = "pause.circle"
	moreID       = groupPrefix + "more:closed"
	olderKey     = "older:"
	olderMoreID  = groupPrefix + "more:older"
	olderTitle   = "Older"
	olderIcon    = "clock.arrow.circlepath"
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
	Rec       record
	Ungated   *ungatedView
	Activity  string
	Started   time.Time
	Notify    string
	Children  string
	CloseAsk  string
	Branch    string
	Detached  bool
	Asking    bool
	Since     time.Time
	Review    *reviewProgress
	RunNeed   *runNeed
	GateAsk   *runNeed
	RunRow    *api.RunRow
	Nest      *reviewerNest
	Open      int
	Landing   *landingView
	TreeNeeds bool
	Group     string
	Until     int64
	Older     bool
}

type sidebarPlus struct {
	runs     []api.RunRow
	dock     []api.DockRow
	gates    []sdui.TreeItem
	graphs   []sdui.TreeItem
	open     string
	needRuns []string
	older    olderSide
}

type olderSide struct {
	count int
	left  int
	mixed bool
	rank  map[string]int
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

func gateRemoveMenu(name, repo string) []sdui.MenuEntry {
	return []sdui.MenuEntry{{Action: actGateDelete, Label: "Delete gate…", Confirm: gateDeleteConfirm(name, repo), Key: removeKey}}
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
	add := map[string]*sdui.TreeAdd{dockGates: {Action: actNewGate, Label: "New gate…"}, dockGraphs: {Action: actNewGraph, Label: "New graph…"}}
	out := make([]sdui.TreeDock, 0, len(plus.dock))
	matched := false
	for _, d := range plus.dock {
		section := sdui.TreeDock{ID: d.ID, Label: d.Label, Icon: d.Icon, Detail: d.Detail, State: d.State, Attention: d.Attention, Rows: rows[d.ID], Add: add[d.ID], Cap: dockCap, Expanded: d.ID == plus.open}
		if query != "" {
			section.Rows = dockMatches(section.Rows, strings.ToLower(query))
			section.Expanded = len(section.Rows) > 0 && !matched
			matched = matched || section.Expanded
		}
		out = append(out, section)
	}
	return out
}

func dockMatches(items []sdui.TreeItem, query string) []sdui.TreeItem {
	var out []sdui.TreeItem
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Label), query) {
			out = append(out, item)
			continue
		}
		if children := dockMatches(item.Children, query); len(children) > 0 {
			item.Children = children
			out = append(out, item)
		}
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

func sidebarView(rows []sessionRow, selected string, collapsed map[string]bool, now time.Time, home string, worktreeOrder map[string]string, branchOpen bool, filter string, search sidebarSearch, closedExtra int, plus sidebarPlus, inbox []inboxEntry) sdui.Node {
	top := sdui.HStack(sdui.WithAction(sdui.Button("new", "New", "plus"), actNew), sdui.WithAction(sdui.Button("branch", "New in worktree…", "arrow.triangle.branch"), actShowBranch))
	if branchOpen {
		top = sdui.WithAction(sdui.Form(actNewBranch,
			sdui.TextField("branch", "", "branch…"),
			sdui.HStack(sdui.Button("start", "Start", "arrow.triangle.branch"), sdui.WithAction(sdui.Button("cancel", "Cancel", "xmark"), actHideBranch)),
		), actNewBranch)
	}
	roots, kids := sidebarTree(rows)
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
	if !slices.ContainsFunc(rows, func(row sessionRow) bool { return row.Nest == nil || row.Nest.Shown }) {
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
	filter = cmp.Or(filter, filterAll)
	runs, oldest := markRoots(roots, kids)
	tabs := filterTabs(roots, filter, runs, plus.needRuns)
	roots = slices.DeleteFunc(roots, func(row sessionRow) bool {
		return !matchesFilter(row, filter) && (filter != filterNeeds || len(runs[row.Rec.ID]) == 0)
	})
	if filter == filterNeeds {
		slices.SortStableFunc(roots, func(x, y sessionRow) int { return compareSince(oldest[x.Rec.ID], oldest[y.Rec.ID]) })
	}
	var gates []sdui.TreeItem
	if filter == filterNeeds {
		for _, entry := range inbox {
			if entry.Gate {
				gates = append(gates, gateInboxItem(entry))
			}
		}
	}
	if len(roots) == 0 && len(gates) == 0 {
		all := sdui.WithAction(sdui.Button("filterAll", "Show all", ""), actFilterAll)
		return dockOnly(top, field, tabs, sdui.EmptyState(filterEmpty[filter], sdui.EmptyProps{Action: &all}))
	}
	type bucket struct {
		label    string
		detached bool
		open     []sessionRow
	}
	var order []string
	var closed, older []sessionRow
	var resets []int64
	paused := map[int64][]sessionRow{}
	buckets := map[string]*bucket{}
	for _, row := range roots {
		switch {
		case row.Older:
			older = append(older, row)
			continue
		case row.Rec.closed():
			closed = append(closed, row)
			continue
		case row.Rec.restricted():
			continue
		case row.Group == groupPaused:
			if _, ok := paused[row.Until]; !ok {
				resets = append(resets, row.Until)
			}
			paused[row.Until] = append(paused[row.Until], row)
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
	if filter != filterNeeds {
		slices.SortStableFunc(order, func(x, y string) int { return cmp.Compare(worktreeOrder[x], worktreeOrder[y]) })
	}
	var mirrors []sdui.TreeItem
	if filter == filterAll {
		mirrors = inboxGroups(inbox, roots, kids, collapsed, now, oldest)
	}
	var groups []sdui.TreeItem
	for _, wt := range order {
		b := buckets[wt]
		children := runItems(b.open, wt, kids, collapsed, now, map[string]bool{}, oldest)
		if len(order) == 1 && wt == home && len(mirrors) == 0 {
			groups = children
			break
		}
		group := groupItem(wt, b.label, "arrow.triangle.branch", children, !collapsed[wt] || anyNeeds(b.open, oldest))
		group.Truncate = "middle"
		group.Faint = b.detached
		groups = append(groups, group)
	}
	groups = append(append(append(gates, mirrors...), pausedGroups(resets, paused, kids, collapsed, now, oldest)...), groups...)
	if len(closed) > 0 || len(emptied) > 0 {
		folded, ok := collapsed[closedKey]
		items := closedItems(closed, selected, closedShown+closedExtra, kids, collapsed, now, oldest)
		group := groupItem(closedKey, "Closed", statusIcons[statusClosed], append(items, emptied...), ok && !folded)
		group.Detail = strconv.Itoa(len(closed) + len(emptied))
		groups = append(groups, group)
	}
	if len(older) > 0 {
		slices.SortStableFunc(older, func(x, y sessionRow) int { return cmp.Compare(plus.older.at(x.Rec.ID), plus.older.at(y.Rec.ID)) })
		folded, ok := collapsed[olderKey]
		items := pageItems(older, plus.older.left, plus.older.mixed, olderKey, olderMoreID, kids, collapsed, now, oldest)
		group := groupItem(olderKey, olderTitle, olderIcon, items, ok && !folded)
		group.Detail = strconv.Itoa(plus.older.count)
		groups = append(groups, group)
	}
	uniqueRunIDs(groups, map[string]int{})
	return sdui.VStack(top, field, tabs, docked(sdui.WithTreeHold(sdui.Tree(sessionsTree, groups, selected), filter)))
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

func pausedGroups(resets []int64, paused map[int64][]sessionRow, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, oldest map[string]time.Time) []sdui.TreeItem {
	slices.Sort(resets)
	out := make([]sdui.TreeItem, 0, len(resets))
	for _, until := range resets {
		key := pausedKey + strconv.FormatInt(until, 10)
		rows := paused[until]
		group := groupItem(key, fmt.Sprintf(pausedTitle, limitClock(time.Unix(until, 0), now)), pausedIcon, runItems(rows, key, kids, collapsed, now, map[string]bool{}, oldest), !collapsed[pausedKey])
		group.Detail = strconv.Itoa(len(rows))
		out = append(out, group)
	}
	return out
}

func foldKey(id string) string {
	if wt, ok := strings.CutPrefix(id, groupPrefix); ok {
		if strings.HasPrefix(wt, pausedKey) {
			return pausedKey
		}
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
	worktrees := map[string]bool{}
	for _, row := range rows {
		worktrees[row.Rec.Worktree] = true
	}
	return pageItems(shown, len(rows)-len(shown), len(worktrees) > 1, closedKey, moreID, kids, collapsed, now, oldest)
}

func olderSidebar(units []olderUnit, selected string, limit int) (olderSide, map[string]bool) {
	side := olderSide{count: len(units), rank: make(map[string]int, min(len(units), limit+1))}
	show := map[string]bool{}
	worktrees := map[string]bool{}
	pick := -1
	for i, u := range units {
		for _, info := range u.rows {
			if info.ID == u.root {
				worktrees[info.Worktree] = true
			}
			if info.ID == selected && i >= limit {
				pick = i
			}
		}
	}
	add := func(i int) {
		side.rank[units[i].root] = i
		for _, info := range units[i].rows {
			show[info.ID] = true
		}
	}
	for i := range min(limit, len(units)) {
		add(i)
	}
	if pick >= 0 {
		add(pick)
	}
	side.left = len(units) - len(side.rank)
	side.mixed = len(worktrees) > 1
	return side, show
}

func (side olderSide) at(id string) int {
	if r, ok := side.rank[id]; ok {
		return r
	}
	return math.MaxInt
}

func pageItems(shown []sessionRow, left int, mixed bool, scope, more string, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, oldest map[string]time.Time) []sdui.TreeItem {
	items := runItems(shown, scope, kids, collapsed, now, map[string]bool{}, oldest)
	if mixed {
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
	if left > 0 {
		label := fmt.Sprintf(closedMore, left)
		if left > closedBatch {
			label = fmt.Sprintf(closedLeft, closedBatch, left)
		}
		items = append(items, sdui.TreeItem{ID: more, Label: label, Faint: true, Action: true})
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
		if !strings.HasPrefix(item.ID, groupPrefix) && !strings.HasPrefix(item.ID, mirrorPrefix) && !strings.HasPrefix(item.ID, hitPrefix) && !strings.HasPrefix(item.ID, repoPrefix) && !strings.HasPrefix(item.ID, runPrefix) {
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

func sidebarTree(rows []sessionRow) ([]sessionRow, map[string][]sessionRow) {
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
		if n := row.Nest; n != nil {
			if !n.Shown {
				continue
			}
			parent = ""
			if open[n.Lander] {
				parent = n.Lander
			}
		}
		if parent != "" && byID[parent] {
			kids[parent] = append(kids[parent], row)
			continue
		}
		roots = append(roots, row)
	}
	return roots, kids
}

type treeTally struct {
	kids   map[string][]sessionRow
	oldest map[string]time.Time
	seen   map[string]bool
	runs   []string
	open   int
	needs  bool
}

func markRoots(roots []sessionRow, kids map[string][]sessionRow) (map[string][]string, map[string]time.Time) {
	runs := map[string][]string{}
	t := treeTally{kids: kids, oldest: map[string]time.Time{}, seen: map[string]bool{}}
	for i, row := range roots {
		clear(t.seen)
		t.runs, t.open, t.needs = nil, 0, false
		t.walk(row, true)
		runs[row.Rec.ID], roots[i].Open, roots[i].TreeNeeds = t.runs, t.open, t.needs
	}
	return runs, t.oldest
}

func (t *treeTally) walk(row sessionRow, root bool) time.Time {
	t.seen[row.Rec.ID] = true
	t.open += row.Rec.OpenCount
	if !root && (needsYou(row.Rec) || row.GateAsk != nil) {
		t.needs = true
	}
	var at time.Time
	if needsYou(row.Rec) {
		at = cmp.Or(row.Since, row.Rec.UpdatedAt)
	}
	for _, n := range []*runNeed{row.RunNeed, row.GateAsk} {
		if n == nil {
			continue
		}
		t.runs = append(t.runs, n.Run)
		if at.IsZero() || n.Since.Before(at) {
			at = n.Since
		}
	}
	for _, kid := range t.kids[row.Rec.ID] {
		if t.seen[kid.Rec.ID] {
			continue
		}
		if k := t.walk(kid, false); !k.IsZero() && (at.IsZero() || k.Before(at)) {
			at = k
		}
	}
	if !at.IsZero() {
		t.oldest[row.Rec.ID] = at
	}
	return at
}

func tallyFilters(roots []sessionRow, runs map[string][]string, needRuns []string, add func(id, filter string, n int)) {
	counted := map[string]bool{}
	for _, row := range roots {
		var fresh string
		for _, run := range runs[row.Rec.ID] {
			if !counted[run] && fresh == "" {
				fresh = run
			}
			counted[run] = true
		}
		if fresh != "" && !ownNeeds(row) {
			add(fresh, filterNeeds, 1)
		}
		if row.Rec.closed() || row.Rec.restricted() {
			if matchesFilter(row, filterNeeds) {
				add(row.Rec.ID, filterNeeds, 1)
			}
			continue
		}
		for _, f := range []string{filterAll, filterNeeds, filterRunning} {
			if matchesFilter(row, f) {
				add(row.Rec.ID, f, 1)
			}
		}
		if row.Open > 0 {
			add(row.Rec.ID, filterOpen, row.Open)
		}
	}
	for _, run := range needRuns {
		if !counted[run] {
			add(run, filterNeeds, 1)
		}
	}
}

func filterTabs(roots []sessionRow, filter string, runs map[string][]string, needRuns []string) sdui.Node {
	counts := map[string]int{}
	tallyFilters(roots, runs, needRuns, func(_, f string, n int) { counts[f] += n })
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
	return needsYou(row.Rec) || row.Children == statusWaiting || row.TreeNeeds || row.GateAsk != nil
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
	var children, asides []sessionRow
	for _, kid := range kids[row.Rec.ID] {
		switch {
		case path[kid.Rec.ID]:
		case kid.Rec.isAside():
			asides = append(asides, kid)
		default:
			children = append(children, kid)
		}
	}
	item := sessionItem(row, descendantCount(row.Rec.ID, kids, map[string]bool{}), now)
	if row.Nest != nil {
		item = reviewItem(item, *row.Nest)
	}
	if row.Rec.isAside() {
		return asideItem(item, row.Rec)
	}
	if entries := row.Rec.repoEntries(); len(entries) > 0 {
		item = taskParent(item, row, entries, children, kids, collapsed, now, path, oldest)
		if len(asides) > 0 && (item.Expanded || anyNeeds(asides, oldest)) {
			item.Children = append(item.Children, asideItems(row.Rec.ID, asides, kids, collapsed, now, path, oldest)...)
		}
		return item
	}
	ask, asks := gateAskItem(row)
	if len(children) == 0 && len(asides) == 0 && !asks {
		return item
	}
	item.HasChildren = true
	folded, ok := collapsed[expandPrefix+row.Rec.ID]
	item.Expanded = ok && !folded || asks || anyNeeds(children, oldest) || anyNeeds(asides, oldest)
	if item.Expanded {
		item.Children = append(runItems(children, row.Rec.ID, kids, collapsed, now, path, oldest), asideItems(row.Rec.ID, asides, kids, collapsed, now, path, oldest)...)
		if asks {
			item.Children = append([]sdui.TreeItem{ask}, item.Children...)
		}
	}
	return item
}

func gateAskItem(row sessionRow) (sdui.TreeItem, bool) {
	l, n := row.Landing, row.GateAsk
	if l == nil || n == nil || l.Run != n.Run {
		return sdui.TreeItem{}, false
	}
	return sdui.TreeItem{ID: runPrefix + n.Run, Label: fmt.Sprintf(reviewRoundLabel, max(l.Round, 1)), Icon: landingIcon, Tone: toneWarning, Detail: n.Text, DetailTone: toneWarning}, true
}

func asideItems(parent string, rows []sessionRow, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, path map[string]bool, oldest map[string]time.Time) []sdui.TreeItem {
	rows = slices.Clone(rows)
	slices.SortStableFunc(rows, func(x, y sessionRow) int { return startedFirst(x.Rec, y.Rec) })
	items := make([]sdui.TreeItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, nestedItem(row, kids, collapsed, now, path, oldest))
	}
	if len(items) < asidesFoldAt {
		return items
	}
	key := asidesKey + parent
	folded, ok := collapsed[key]
	group := groupItem(key, fmt.Sprintf(asidesLabel, len(items)), asideIcon, items, ok && !folded || anyNeeds(rows, oldest))
	if anyNeeds(rows, oldest) {
		group.DetailTone = statusTones[statusWaiting]
	}
	return []sdui.TreeItem{group}
}

func asideItem(item sdui.TreeItem, rec record) sdui.TreeItem {
	state := asideState(rec)
	item.Icon, item.Detail, item.Tone, item.DetailTone, item.DetailFaint, item.Badges = asideIcon, asideDetails[state], "", "", false, nil
	if state == asideAsks {
		item.Tone, item.DetailTone = statusTones[statusWaiting], statusTones[statusWaiting]
	}
	item.Menu = slices.DeleteFunc(item.Menu, func(m sdui.MenuEntry) bool { return m.Action == actHandoff || m.Action == actArchive })
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
		slices.SortStableFunc(e.rows, func(x, y sessionRow) int { return startedFirst(x.Rec, y.Rec) })
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
		if !seen[kid.Rec.ID] && !kid.Rec.isAside() {
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
	tone := statusTones[display]
	if reviewRow(row) {
		icon = statusIcons[statusReview]
	}
	if display == statusError && rec.LimitWait.pausing() {
		icon, tone = pausedIcon, ""
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
		Tone:        tone,
		Menu:        menu,
		Badges:      badges,
	}
}

func rowGateChip(row sessionRow) (string, string) {
	l := row.Landing
	if l == nil || l.Asking && l.Ask == "" || needsYou(row.Rec) || row.Rec.Status == statusError {
		return "", ""
	}
	return l.Words, l.Tone
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
	if needsYou(rec) && rec.Status == statusDone || rec.heldWaits() || len(rec.callRequests()) > 0 {
		return statusAsks
	}
	if rec.Review && rec.Status == statusDone {
		return statusReview
	}
	return rec.Status
}

func needsYou(rec record) bool {
	if rec.isAside() {
		return asideState(rec) == asideAsks
	}
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
