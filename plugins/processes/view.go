package main

import (
	"cmp"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	allID        = "all"
	sessionsID   = "sessions"
	tableID      = "processes"
	moreID       = "more"
	selectPID    = "select-pid"
	killAction   = "kill"
	orphanKill   = "kill-orphans"
	autoKill     = "autokill"
	optionsID    = "options"
	groupPrefix  = "g:"
	otherGroup   = "other"
	kibPerMiB    = 1024
	mibPerGiB    = 1024
	maxRows      = 1000
	maxDepth     = 32
	pidWidth     = 64
	indent       = 12
	stripHeight  = 16
	cellRunes    = 512
	stripRunes   = 64
	groupRunes   = 40
	killKey      = "shift+k"
	revealKey    = "cmd+shift+r"
	filterID     = "filter"
	filterAction = "filter"
	copyID       = "copy"
	revealAction = "reveal"

	colPID     = "pid"
	colCPU     = "cpu"
	colMem     = "mem"
	colTime    = "time"
	colHistory = "history"
	colCommand = "command"
)

var (
	defaultSort = sdui.Sort{Column: colCPU, Desc: true}
	columnIDs   = []string{colPID, colCPU, colMem, colTime, colHistory, colCommand}
	ownerLabels = map[string]string{"terminals": "Terminals", "agents": "Agents", "editor-nvim": "Code"}
	ownerIcons  = map[string]string{
		coreID:        "cpu",
		"terminals":   "terminal",
		"agents":      "sparkles",
		"editor-nvim": "chevron.left.forwardslash.chevron.right",
		"cron":        "clock",
		"triggers":    "bolt",
		"graphs":      "point.3.connected.trianglepath.dotted",
	}
)

type viewModel struct {
	nodes     []pluginsdk.ProcessNode
	groups    []group
	selected  int
	filter    string
	coreOpen  bool
	auto      bool
	options   bool
	sort      sdui.Sort
	collapsed map[int]bool
	folded    map[string]bool
	scope     string
	totalKey  seriesKey
	hist      *history
	memKiB    int64
	pidDepth  int
	now       time.Time
}

type treeEntry struct {
	proc        *procRow
	depth       int
	hasChildren bool
	expanded    bool
}

type procRow struct {
	node    pluginsdk.ProcessNode
	id      string
	command string
	recent  []float64
	avg     float64
}

type ownerGroup struct {
	key    string
	label  string
	nodes  []pluginsdk.ProcessNode
	titles map[string]string
}

func sidebarView(sessions []group, cpu map[string]string, total int, selected string) sdui.Node {
	items := []sdui.Item{{ID: allID, Label: "All", Detail: strconv.Itoa(total)}}
	for _, s := range sessions {
		items = append(items, sdui.Item{ID: s.ID, Label: s.Title, Icon: cmp.Or(ownerIcons[s.Owner], "gearshape"), Detail: cpu[s.ID], Truncate: "tail"})
	}
	if selected == "" {
		selected = allID
	}
	return sdui.List(sessionsID, items, selected)
}

func sessionCPU(sessions []group, nodes []pluginsdk.ProcessNode) map[string]string {
	sums := make(map[string]float64, len(sessions))
	for _, n := range nodes {
		sums[n.SessionID] += n.Process.CPU
	}
	out := make(map[string]string, len(sessions))
	for _, s := range sessions {
		out[s.ID] = strconv.FormatFloat(sums[s.ID], 'f', 1, 64) + "%"
	}
	return out
}

func mainView(m viewModel) (sdui.Node, int) {
	shown := m
	shown.nodes = filterNodes(m.nodes, m.groups, m.filter)
	rows, depth := processRows(shown)
	depth = max(depth, m.pidDepth)
	selectedID := ""
	if m.selected != 0 {
		selectedID = strconv.Itoa(m.selected)
	}
	children := []sdui.Node{headerView(m)}
	if m.options {
		children = append(children, optionsView(m))
	}
	children = append(children, totalsView(m))
	if len(rows) == 0 && m.filter != "" {
		children = append(children, withID(sdui.Empty(fmt.Sprintf("No processes match “%s”.", m.filter)), "no-match"))
	} else {
		table := sdui.WithAction(sdui.Table(tableID, tableColumns(depth), rows, selectedID, m.sort), selectPID)
		children = append(children, withID(sdui.Scroll(table), "table-scroll"))
	}
	if strip, ok := actionStrip(shown); ok {
		children = append(children, strip)
	}
	return withID(sdui.VStack(children...), "main"), depth
}

func headerView(m viewModel) sdui.Node {
	orphans := 0
	for _, n := range m.nodes {
		if n.Orphan {
			orphans++
		}
	}
	meta := fmt.Sprintf("%d running · %d orphans", len(m.nodes), orphans)
	if m.auto {
		meta += " · auto-kill on"
	}
	title := m.scope
	if title == "All" {
		title = "All processes"
	}
	kill := sdui.WithVariant(sdui.WithConfirm(sdui.Button(orphanKill, "Kill orphans", "trash"), "Kill all orphan processes?"), sdui.VariantDanger)
	if orphans == 0 {
		kill = sdui.WithDisabled(kill)
	}
	optionsLabel := "Options"
	if m.options {
		optionsLabel = "Hide options"
	}
	options := sdui.WithIconOnly(sdui.WithVariant(sdui.Button(optionsID, optionsLabel, "slider.horizontal.3"), sdui.VariantGhost))
	filter := sdui.WithAction(sdui.WithLive(sdui.TextField(filterID, m.filter, "Filter")), filterAction)
	return withID(sdui.Header(title, sdui.HeaderProps{Meta: meta}, filter, kill, options), "header")
}

func optionsView(m viewModel) sdui.Node {
	return withID(sdui.HStack(
		sdui.WithAction(sdui.Toggle(autoKill, m.auto), autoKill),
		sdui.Text("Auto-kill orphans"),
		sdui.MutedText(fmt.Sprintf("every %d s", int(autoKillEvery/time.Second))),
	), "options-row")
}

func totals(nodes []pluginsdk.ProcessNode) (float64, int64) {
	cpu, rss := 0.0, int64(0)
	for _, n := range nodes {
		cpu += n.Process.CPU
		rss += n.Process.RSSKiB
	}
	return cpu, rss
}

func totalsView(m viewModel) sdui.Node {
	cpu, rss := totals(m.nodes)
	cpuHist, memHist := m.hist.cpu(m.totalKey, historyLen), m.hist.mem(m.totalKey, historyLen)
	cpuPeak, _ := peakAvg(cpuHist)
	memPeak, memAvg := peakAvg(memHist)
	return withID(sdui.HStack(
		sdui.VStack(sdui.MutedText(fmt.Sprintf("CPU · peak %.0f%%", cpuPeak)), sdui.Text(strconv.FormatFloat(cpu, 'f', 1, 64)+"%")),
		sdui.SparklineSized("total-cpu", cpuHist, cpuScale(cpuHist), stripHeight),
		sdui.VStack(sdui.MutedText("MEM · avg "+formatMB(memAvg)), sdui.Text(formatMB(float64(rss)/kibPerMiB))),
		sdui.SparklineSized("total-mem", memHist, max(memPeak, 1), stripHeight),
	), "totals")
}

func actionStrip(m viewModel) (sdui.Node, bool) {
	n, ok := m.selectedNode()
	if !ok {
		return sdui.Node{}, false
	}
	p := n.Process
	var facts []string
	if slices.ContainsFunc(m.nodes, func(o pluginsdk.ProcessNode) bool { return o.Process.PID == p.PPID && p.PPID != p.PID }) {
		facts = append(facts, "child of "+strconv.Itoa(p.PPID))
	}
	if !p.StartTime.IsZero() {
		facts = append(facts, "started "+startedAt(p.StartTime, m.now))
	}
	copyCommand := sdui.WithCopy(sdui.WithVariant(sdui.Button(copyID, "Copy command", "doc.on.doc"), sdui.VariantGhost), shellCommand(n))
	reveal := sdui.WithShortcut(sdui.WithVariant(sdui.Button(revealAction, "Reveal", "folder"), sdui.VariantGhost), revealKey)
	if executable(n) == "" {
		reveal = sdui.WithDisabled(reveal)
	}
	confirm := fmt.Sprintf("Kill %s (%d) and its children?", p.Comm, p.PID)
	kill := sdui.WithShortcut(sdui.WithVariant(sdui.WithConfirm(sdui.Button(killAction, "Kill", "xmark.circle"), confirm), sdui.VariantDanger), killKey)
	if n.SessionID == coreID {
		kill = sdui.WithDisabled(kill)
	}
	return withID(sdui.HStack(
		sdui.VStack(titleText(strconv.Itoa(p.PID)+"  "+middle(shortCommand(n), cellRunes)), sdui.MutedText(strings.Join(facts, " · "))),
		copyCommand,
		reveal,
		kill,
	), "action-strip"), true
}

func tableColumns(depth int) []sdui.Column {
	return []sdui.Column{
		{ID: colPID, Title: "PID", Width: pidWidth + indent*float64(depth)},
		{ID: colCPU, Title: "CPU%", Align: "right", Width: 64},
		{ID: colMem, Title: "MEM", Align: "right", Width: 72, Priority: 1},
		{ID: colTime, Title: "TIME", Align: "right", Width: 72, Priority: 2},
		{ID: colHistory, Title: "LAST MIN", Width: 72, Priority: 3},
		{ID: colCommand, Title: "COMMAND", Truncate: "middle"},
	}
}

func processRows(m viewModel) ([]sdui.TableRow, int) {
	groups := ownerGroups(m.groups, m.nodes)
	var all []sdui.TableRow
	depth := 0
	if len(groups) <= 1 {
		var titles map[string]string
		if len(groups) == 1 {
			titles = groups[0].titles
		}
		all, depth = m.treeRows(m.nodes, titles, 0)
	} else {
		for _, g := range groups {
			id := groupPrefix + g.key
			open := !m.folded[g.key]
			cells := make([]sdui.Cell, len(columnIDs))
			cells[0].Text = g.label
			if !open {
				cpu, rss := totals(g.nodes)
				cells[0].Text = fmt.Sprintf("%s · %s%% · %s", middle(g.label, groupRunes), strconv.FormatFloat(cpu, 'f', 1, 64), formatMB(float64(rss)/kibPerMiB))
			}
			all = append(all, sdui.TableRow{ID: id, Group: true, HasChildren: true, Expanded: open, Cells: cells})
			if !open {
				continue
			}
			rows, d := m.treeRows(g.nodes, g.titles, 1)
			all = append(all, rows...)
			depth = max(depth, d)
		}
	}
	if len(all) <= maxRows {
		return all, depth
	}
	hidden := 0
	for _, r := range all[maxRows-1:] {
		if !r.Group {
			hidden++
		}
	}
	cells := make([]sdui.Cell, len(columnIDs))
	cells[len(cells)-1].Text = fmt.Sprintf("… %d more", hidden)
	return append(all[:maxRows-1:maxRows-1], sdui.TableRow{ID: moreID, Muted: true, Cells: cells}), depth
}

func (m viewModel) treeRows(nodes []pluginsdk.ProcessNode, titles map[string]string, base int) ([]sdui.TableRow, int) {
	entries := treeEntries(m, nodes, titles, base)
	rows := make([]sdui.TableRow, 0, len(entries))
	depth := 0
	for _, e := range entries {
		depth = max(depth, e.depth)
		rows = append(rows, sdui.TableRow{
			ID:          e.proc.id,
			Depth:       e.depth,
			HasChildren: e.hasChildren,
			Expanded:    e.expanded,
			Cells:       m.cells(e.proc),
		})
	}
	return rows, depth
}

func ownerGroups(sessions []group, nodes []pluginsdk.ProcessNode) []ownerGroup {
	perOwner := make(map[string]int, len(sessions))
	for _, s := range sessions {
		perOwner[s.Owner]++
	}
	keyOf := make(map[string]string, len(sessions))
	var out []ownerGroup
	index := map[string]int{}
	add := func(key, label string) int {
		i, ok := index[key]
		if !ok {
			i = len(out)
			index[key] = i
			out = append(out, ownerGroup{key: key, label: label, titles: map[string]string{}})
		}
		return i
	}
	for _, s := range sessions {
		key, label := s.ID, s.Title
		if perOwner[s.Owner] > 1 {
			key, label = s.Owner, ownerLabel(s.Owner)
			out[add(key, label)].titles[s.ID] = s.Title
		} else {
			add(key, label)
		}
		keyOf[s.ID] = key
	}
	for _, n := range nodes {
		key, known := keyOf[n.SessionID]
		i := index[key]
		if !known {
			i = add(otherGroup, "Other")
		}
		out[i].nodes = append(out[i].nodes, n)
	}
	if i, ok := index[coreID]; ok && i > 0 {
		core := out[i]
		out = slices.Insert(slices.Delete(out, i, i+1), 0, core)
	}
	return slices.DeleteFunc(out, func(g ownerGroup) bool { return len(g.nodes) == 0 })
}

func filterNodes(nodes []pluginsdk.ProcessNode, groups []group, query string) []pluginsdk.ProcessNode {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nodes
	}
	labels := make(map[string]string, len(groups))
	for _, g := range groups {
		labels[g.ID] = strings.ToLower(g.Title + " " + ownerLabel(g.Owner))
	}
	byPID := make(map[int]pluginsdk.ProcessNode, len(nodes))
	for _, n := range nodes {
		byPID[n.Process.PID] = n
	}
	keep := make(map[int]bool, len(nodes))
	for _, n := range nodes {
		text := strings.ToLower(strconv.Itoa(n.Process.PID) + " " + command(n) + " " + labels[n.SessionID])
		if !strings.Contains(text, query) {
			continue
		}
		for p, ok := n.Process, true; ok && !keep[p.PID]; {
			keep[p.PID] = true
			parent, found := byPID[p.PPID]
			p, ok = parent.Process, found && p.PPID != p.PID
		}
	}
	return slices.DeleteFunc(slices.Clone(nodes), func(n pluginsdk.ProcessNode) bool { return !keep[n.Process.PID] })
}

func ownerLabel(owner string) string {
	if label, ok := ownerLabels[owner]; ok {
		return label
	}
	if owner == "" {
		return "Other"
	}
	return strings.ToUpper(owner[:1]) + owner[1:]
}

func treeEntries(m viewModel, nodes []pluginsdk.ProcessNode, titles map[string]string, base int) []treeEntry {
	procs := make([]procRow, len(nodes))
	present := make(map[int]bool, len(nodes))
	for i, n := range nodes {
		procs[i] = m.procRow(n)
		present[n.Process.PID] = true
	}
	children := make(map[int][]*procRow, len(nodes))
	var roots []*procRow
	for i := range procs {
		p := &procs[i].node.Process
		if present[p.PPID] && p.PPID != p.PID {
			children[p.PPID] = append(children[p.PPID], &procs[i])
			continue
		}
		if title := titles[procs[i].node.SessionID]; title != "" {
			procs[i].command += " · " + title
		}
		roots = append(roots, &procs[i])
	}
	compare := m.comparator()
	entries := make([]treeEntry, 0, len(nodes))
	var walk func(siblings []*procRow, depth int)
	walk = func(siblings []*procRow, depth int) {
		slices.SortFunc(siblings, compare)
		for _, p := range siblings {
			pid := p.node.Process.PID
			kids := children[pid]
			open := !m.collapsed[pid]
			if depth == base && p.node.SessionID == coreID {
				open = m.coreOpen
			}
			expanded := len(kids) > 0 && open
			entries = append(entries, treeEntry{proc: p, depth: min(depth, maxDepth), hasChildren: len(kids) > 0, expanded: expanded})
			if expanded {
				walk(kids, depth+1)
			}
		}
	}
	walk(roots, base)
	return entries
}

func (m viewModel) procRow(n pluginsdk.ProcessNode) procRow {
	recent := m.hist.cpu(processKey(n), recentLen)
	_, avg := peakAvg(recent)
	return procRow{node: n, id: strconv.Itoa(n.Process.PID), command: displayCommand(n, cellRunes), recent: recent, avg: avg}
}

func (m viewModel) comparator() func(a, b *procRow) int {
	var key func(a, b *procRow) int
	switch m.sort.Column {
	case colPID:
		key = func(a, b *procRow) int { return 0 }
	case colMem:
		key = func(a, b *procRow) int { return cmp.Compare(a.node.Process.RSSKiB, b.node.Process.RSSKiB) }
	case colTime:
		key = func(a, b *procRow) int { return b.node.Process.StartTime.Compare(a.node.Process.StartTime) }
	case colHistory:
		key = func(a, b *procRow) int { return cmp.Compare(a.avg, b.avg) }
	case colCommand:
		key = func(a, b *procRow) int { return strings.Compare(a.command, b.command) }
	default:
		key = func(a, b *procRow) int { return cmp.Compare(a.node.Process.CPU, b.node.Process.CPU) }
	}
	return func(a, b *procRow) int {
		c := cmp.Or(key(a, b), cmp.Compare(a.node.Process.PID, b.node.Process.PID))
		if m.sort.Desc {
			return -c
		}
		return c
	}
}

func (m viewModel) selectedNode() (pluginsdk.ProcessNode, bool) {
	i := slices.IndexFunc(m.nodes, func(n pluginsdk.ProcessNode) bool { return n.Process.PID == m.selected })
	if i < 0 {
		return pluginsdk.ProcessNode{}, false
	}
	return m.nodes[i], true
}

func (m viewModel) cells(row *procRow) []sdui.Cell {
	n, p := row.node, row.node.Process
	cpuBar := min(max(math.Round(p.CPU*10)/1000, 0), 1)
	mem := sdui.Cell{Text: formatMB(float64(p.RSSKiB) / kibPerMiB)}
	if m.memKiB > 0 {
		share := min(math.Round(float64(p.RSSKiB)/float64(m.memKiB)*1000)/1000, 1)
		mem.Bar, mem.Tone = &share, tone(share*100, 10, 30)
	}
	var history sdui.Cell
	if len(row.recent) > 0 {
		history.Spark = sdui.NewSpark(row.recent, cpuScale(row.recent))
	}
	commandCell := sdui.Cell{Text: row.command + badges(n)}
	if n.Orphan {
		commandCell.Tone = "warn"
	}
	return []sdui.Cell{
		{Text: row.id},
		{Text: strconv.FormatFloat(p.CPU, 'f', 1, 64), Bar: &cpuBar, Tone: tone(p.CPU, 50, 90)},
		mem,
		{Since: since(p.StartTime)},
		history,
		commandCell,
	}
}

func tone(v, warn, danger float64) string {
	switch {
	case v >= danger:
		return "danger"
	case v >= warn:
		return "warn"
	}
	return ""
}

func cpuScale(values []float64) float64 {
	peak, _ := peakAvg(values)
	return max(peak, 100)
}

func command(n pluginsdk.ProcessNode) string {
	if len(n.Process.Args) == 0 {
		return n.Process.Comm
	}
	return strings.Join(n.Process.Args, " ")
}

func displayCommand(n pluginsdk.ProcessNode, limit int) string {
	if len(n.Process.Args) == 0 {
		return middle(n.Process.Comm, limit)
	}
	return middle(strings.Join(shortArgs(n.Process.Args), " "), limit)
}

func shortArgs(argv []string) []string {
	args := make([]string, len(argv))
	for i, arg := range argv {
		key, value, ok := strings.Cut(arg, "=")
		if ok && filepath.IsAbs(value) {
			args[i] = key + "=" + shortPath(value)
			continue
		}
		args[i] = shortPath(arg)
	}
	return args
}

func shortPath(path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	parts := strings.Split(path, "/")
	if len(parts) <= 5 {
		return path
	}
	return strings.Join([]string{"", parts[1], "…", parts[len(parts)-2], parts[len(parts)-1]}, "/")
}

func titleText(text string) sdui.Node {
	return sdui.Node{Type: "text", Props: map[string]any{"text": text, "style": "title"}}
}

func executable(n pluginsdk.ProcessNode) string {
	if len(n.Process.Args) == 0 || !filepath.IsAbs(n.Process.Args[0]) {
		return ""
	}
	return n.Process.Args[0]
}

func shellCommand(n pluginsdk.ProcessNode) string {
	if len(n.Process.Args) == 0 {
		return n.Process.Comm
	}
	quoted := make([]string, len(n.Process.Args))
	for i, arg := range n.Process.Args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(arg string) string {
	if arg != "" && !strings.ContainsFunc(arg, func(r rune) bool {
		return !(r == '/' || r == '-' || r == '_' || r == '.' || r == '=' || r == ':' || r == ',' || r == '+' || r == '@' || r == '%' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

func shortCommand(n pluginsdk.ProcessNode) string {
	if len(n.Process.Args) == 0 {
		return n.Process.Comm
	}
	args := shortArgs(n.Process.Args)
	args[0] = filepath.Base(n.Process.Args[0])
	return strings.Join(args, " ")
}

func middle(text string, limit int) string {
	r := []rune(text)
	if len(r) <= limit {
		return text
	}
	head := (limit - 1) / 2
	return string(r[:head]) + "…" + string(r[len(r)-(limit-1-head):])
}

func startedAt(start, now time.Time) string {
	if start.Format(time.DateOnly) == now.Format(time.DateOnly) {
		return start.Format("15:04")
	}
	return start.Format("Jan 2 15:04")
}

func badges(n pluginsdk.ProcessNode) string {
	var b strings.Builder
	if n.Orphan {
		b.WriteString(" · orphan")
	}
	if n.Duplicate {
		b.WriteString(" · dup")
	}
	return b.String()
}

func since(start time.Time) int64 {
	if start.IsZero() {
		return 0
	}
	return start.Unix()
}

func formatMB(mb float64) string {
	if mb >= mibPerGiB {
		return strconv.FormatFloat(mb/mibPerGiB, 'f', 1, 64) + " GB"
	}
	return strconv.FormatFloat(mb, 'f', 0, 64) + " MB"
}

func validSort(s sdui.Sort) bool {
	return slices.Contains(columnIDs, s.Column)
}

func withID(n sdui.Node, id string) sdui.Node {
	n.ID = id
	return n
}
