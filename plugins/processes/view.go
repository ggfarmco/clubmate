package main

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	allID       = "all"
	sessionsID  = "sessions"
	tableID     = "processes"
	moreID      = "more"
	selectPID   = "select-pid"
	killAction  = "kill"
	orphanKill  = "kill-orphans"
	autoKill    = "autokill"
	kibPerMiB   = 1024
	mibPerGiB   = 1024
	maxRows     = 1000
	maxDepth    = 32
	pidWidth    = 64
	indent      = 12
	chartHeight = 64
	cellRunes   = 512
	titleRunes  = 2048

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
)

type viewModel struct {
	nodes     []pluginsdk.ProcessNode
	selected  int
	auto      bool
	sort      sdui.Sort
	collapsed map[int]bool
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

func sidebarView(sessions []group, selected string) sdui.Node {
	items := []sdui.Item{{ID: allID, Label: "All"}}
	for _, s := range sessions {
		items = append(items, sdui.Item{ID: s.ID, Label: s.Title, Detail: s.Owner})
	}
	if selected == "" {
		selected = allID
	}
	return sdui.List(sessionsID, items, selected)
}

func mainView(m viewModel) (sdui.Node, int) {
	rows, depth := processRows(m)
	depth = max(depth, m.pidDepth)
	selectedID := ""
	if m.selected != 0 {
		selectedID = strconv.Itoa(m.selected)
	}
	table := sdui.WithAction(sdui.Table(tableID, tableColumns(depth), rows, selectedID, m.sort), selectPID)
	return withID(sdui.VStack(toolbarView(m), summaryView(m), withID(sdui.Scroll(table), "table-scroll"), detailView(m)), "main"), depth
}

func toolbarView(m viewModel) sdui.Node {
	orphans := 0
	for _, n := range m.nodes {
		if n.Orphan {
			orphans++
		}
	}
	var controls []sdui.Node
	if n, ok := m.selectedNode(); ok {
		confirm := fmt.Sprintf("Kill %s (%d) and its children?", n.Process.Comm, m.selected)
		controls = append(controls, sdui.WithConfirm(sdui.Button(killAction, "Kill", "xmark.circle"), confirm))
	}
	controls = append(controls,
		sdui.WithConfirm(sdui.Button(orphanKill, "Kill orphans", "trash"), "Kill all orphan processes?"),
		sdui.WithAction(sdui.Toggle(autoKill, m.auto), autoKill),
		sdui.Text("Auto-kill orphans"),
		sdui.MutedText(fmt.Sprintf("%d processes · %d orphans", len(m.nodes), orphans)),
	)
	return withID(sdui.HStack(controls...), "toolbar")
}

func summaryView(m viewModel) sdui.Node {
	cpu, rss := 0.0, int64(0)
	for _, n := range m.nodes {
		cpu += n.Process.CPU
		rss += n.Process.RSSKiB
	}
	recent := m.hist.cpu(m.totalKey, recentLen)
	return withID(sdui.HStack(
		sdui.Text(m.scope),
		sdui.MutedText(fmt.Sprintf("CPU %.0f%% · MEM %s", cpu, formatMB(float64(rss)/kibPerMiB))),
		sdui.Sparkline("total-cpu", recent, cpuScale(recent)),
	), "summary")
}

func detailView(m viewModel) sdui.Node {
	title, key := m.scope+" total", m.totalKey
	if n, ok := m.selectedNode(); ok {
		title, key = fmt.Sprintf("%s (%d)", command(n, titleRunes), m.selected), processKey(n)
	}
	cpu, mem := m.hist.cpu(key, historyLen), m.hist.mem(key, historyLen)
	cpuPeak, cpuAvg := peakAvg(cpu)
	memPeak, memAvg := peakAvg(mem)
	return withID(sdui.VStack(
		sdui.Text(title),
		sdui.MutedText(fmt.Sprintf("CPU · peak %.0f%% · avg %.1f%%", cpuPeak, cpuAvg)),
		sdui.SparklineSized("detail-cpu", cpu, cpuScale(cpu), chartHeight),
		sdui.MutedText(fmt.Sprintf("MEM · peak %s · avg %s", formatMB(memPeak), formatMB(memAvg))),
		sdui.SparklineSized("detail-mem", mem, max(memPeak, 1), chartHeight),
	), "detail")
}

func tableColumns(depth int) []sdui.Column {
	return []sdui.Column{
		{ID: colPID, Title: "PID", Width: pidWidth + indent*float64(depth)},
		{ID: colCPU, Title: "CPU%", Align: "right", Width: 64},
		{ID: colMem, Title: "MEM", Align: "right", Width: 72},
		{ID: colTime, Title: "TIME", Align: "right", Width: 72},
		{ID: colHistory, Title: "HISTORY", Width: 72},
		{ID: colCommand, Title: "COMMAND"},
	}
}

func processRows(m viewModel) ([]sdui.TableRow, int) {
	entries := treeEntries(m)
	shown := entries
	if len(entries) > maxRows {
		shown = entries[:maxRows-1]
	}
	rows := make([]sdui.TableRow, 0, len(shown)+1)
	depth := 0
	for _, e := range shown {
		depth = max(depth, e.depth)
		rows = append(rows, sdui.TableRow{
			ID:          e.proc.id,
			Depth:       e.depth,
			HasChildren: e.hasChildren,
			Expanded:    e.expanded,
			Cells:       m.cells(e.proc),
		})
	}
	if hidden := len(entries) - len(shown); hidden > 0 {
		cells := make([]sdui.Cell, len(columnIDs))
		cells[len(cells)-1].Text = fmt.Sprintf("… %d more", hidden)
		rows = append(rows, sdui.TableRow{ID: moreID, Muted: true, Cells: cells})
	}
	return rows, depth
}

func treeEntries(m viewModel) []treeEntry {
	procs := make([]procRow, len(m.nodes))
	present := make(map[int]bool, len(m.nodes))
	for i, n := range m.nodes {
		procs[i] = m.procRow(n)
		present[n.Process.PID] = true
	}
	children := make(map[int][]*procRow, len(m.nodes))
	var roots []*procRow
	for i := range procs {
		p := &procs[i].node.Process
		if present[p.PPID] && p.PPID != p.PID {
			children[p.PPID] = append(children[p.PPID], &procs[i])
		} else {
			roots = append(roots, &procs[i])
		}
	}
	compare := m.comparator()
	entries := make([]treeEntry, 0, len(m.nodes))
	var walk func(siblings []*procRow, depth int)
	walk = func(siblings []*procRow, depth int) {
		slices.SortFunc(siblings, compare)
		for _, p := range siblings {
			pid := p.node.Process.PID
			kids := children[pid]
			expanded := len(kids) > 0 && !m.collapsed[pid]
			entries = append(entries, treeEntry{proc: p, depth: min(depth, maxDepth), hasChildren: len(kids) > 0, expanded: expanded})
			if expanded {
				walk(kids, depth+1)
			}
		}
	}
	walk(roots, 0)
	return entries
}

func (m viewModel) procRow(n pluginsdk.ProcessNode) procRow {
	recent := m.hist.cpu(processKey(n), recentLen)
	_, avg := peakAvg(recent)
	return procRow{node: n, id: strconv.Itoa(n.Process.PID), command: command(n, cellRunes), recent: recent, avg: avg}
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
	cpuBar := min(max(p.CPU/100, 0), 1)
	mem := sdui.Cell{Text: formatMB(float64(p.RSSKiB) / kibPerMiB)}
	if m.memKiB > 0 {
		share := min(float64(p.RSSKiB)/float64(m.memKiB), 1)
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
		{Text: elapsed(p.StartTime, m.now)},
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

func command(n pluginsdk.ProcessNode, limit int) string {
	text := n.Process.Comm
	if len(n.Process.Args) > 0 {
		text = strings.Join(n.Process.Args, " ")
	}
	if len(text) <= limit || utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit-1]) + "…"
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

func elapsed(start, now time.Time) string {
	if start.IsZero() {
		return ""
	}
	s := int(max(now.Sub(start), 0) / time.Second)
	b := strconv.AppendInt(make([]byte, 0, 16), int64(s/3600), 10)
	b = append(b, ':')
	b = appendTwoDigits(b, s/60%60)
	b = append(b, ':')
	return string(appendTwoDigits(b, s%60))
}

func appendTwoDigits(b []byte, v int) []byte {
	return append(b, byte('0'+v/10), byte('0'+v%10))
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
