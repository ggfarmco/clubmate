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

type sessionRow struct {
	ID    string
	Title string
	Owner string
}

type viewModel struct {
	nodes     []pluginsdk.ProcessNode
	selected  int
	auto      bool
	sort      sdui.Sort
	collapsed map[int]bool
	scope     string
	totalKey  string
	hist      *history
	memKiB    int64
	pidDepth  int
	now       time.Time
}

type treeEntry struct {
	node        pluginsdk.ProcessNode
	depth       int
	hasChildren bool
	expanded    bool
}

func sidebarView(sessions []sessionRow, selected string) sdui.Node {
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
	if confirm, ok := killConfirm(m.nodes, m.selected); ok {
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
	if i := slices.IndexFunc(m.nodes, func(n pluginsdk.ProcessNode) bool { return n.Process.PID == m.selected }); i >= 0 {
		title, key = fmt.Sprintf("%s (%d)", command(m.nodes[i], titleRunes), m.selected), processKey(m.nodes[i])
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
			ID:          strconv.Itoa(e.node.Process.PID),
			Depth:       e.depth,
			HasChildren: e.hasChildren,
			Expanded:    e.expanded,
			Cells:       m.cells(e.node),
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
	present := make(map[int]bool, len(m.nodes))
	for _, n := range m.nodes {
		present[n.Process.PID] = true
	}
	children := make(map[int][]pluginsdk.ProcessNode, len(m.nodes))
	var roots []pluginsdk.ProcessNode
	for _, n := range m.nodes {
		if present[n.Process.PPID] && n.Process.PPID != n.Process.PID {
			children[n.Process.PPID] = append(children[n.Process.PPID], n)
		} else {
			roots = append(roots, n)
		}
	}
	compare := m.comparator()
	entries := make([]treeEntry, 0, len(m.nodes))
	var walk func(siblings []pluginsdk.ProcessNode, depth int)
	walk = func(siblings []pluginsdk.ProcessNode, depth int) {
		slices.SortFunc(siblings, compare)
		for _, n := range siblings {
			kids := children[n.Process.PID]
			expanded := len(kids) > 0 && !m.collapsed[n.Process.PID]
			entries = append(entries, treeEntry{node: n, depth: min(depth, maxDepth), hasChildren: len(kids) > 0, expanded: expanded})
			if expanded {
				walk(kids, depth+1)
			}
		}
	}
	walk(roots, 0)
	return entries
}

func (m viewModel) comparator() func(a, b pluginsdk.ProcessNode) int {
	var key func(a, b pluginsdk.ProcessNode) int
	switch m.sort.Column {
	case colPID:
		key = func(a, b pluginsdk.ProcessNode) int { return 0 }
	case colMem:
		key = func(a, b pluginsdk.ProcessNode) int { return cmp.Compare(a.Process.RSSKiB, b.Process.RSSKiB) }
	case colTime:
		key = func(a, b pluginsdk.ProcessNode) int { return b.Process.StartTime.Compare(a.Process.StartTime) }
	case colHistory:
		key = func(a, b pluginsdk.ProcessNode) int { return cmp.Compare(m.recentAvg(a), m.recentAvg(b)) }
	case colCommand:
		key = func(a, b pluginsdk.ProcessNode) int {
			return strings.Compare(command(a, cellRunes), command(b, cellRunes))
		}
	default:
		key = func(a, b pluginsdk.ProcessNode) int { return cmp.Compare(a.Process.CPU, b.Process.CPU) }
	}
	return func(a, b pluginsdk.ProcessNode) int {
		c := cmp.Or(key(a, b), cmp.Compare(a.Process.PID, b.Process.PID))
		if m.sort.Desc {
			return -c
		}
		return c
	}
}

func (m viewModel) recentAvg(n pluginsdk.ProcessNode) float64 {
	_, avg := peakAvg(m.hist.cpu(processKey(n), recentLen))
	return avg
}

func (m viewModel) cells(n pluginsdk.ProcessNode) []sdui.Cell {
	p := n.Process
	cpuBar := min(max(p.CPU/100, 0), 1)
	mem := sdui.Cell{Text: formatMB(float64(p.RSSKiB) / kibPerMiB)}
	if m.memKiB > 0 {
		share := min(float64(p.RSSKiB)/float64(m.memKiB), 1)
		mem.Bar, mem.Tone = &share, tone(share*100, 10, 30)
	}
	history := sdui.Cell{}
	if recent := m.hist.cpu(processKey(n), recentLen); len(recent) > 0 {
		history.Spark = sdui.NewSpark(recent, cpuScale(recent))
	}
	commandCell := sdui.Cell{Text: command(n, cellRunes) + badges(n)}
	if n.Orphan {
		commandCell.Tone = "warn"
	}
	return []sdui.Cell{
		{Text: strconv.Itoa(p.PID)},
		{Text: fmt.Sprintf("%.1f", p.CPU), Bar: &cpuBar, Tone: tone(p.CPU, 50, 90)},
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
	if utf8.RuneCountInString(text) <= limit {
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
	d := max(now.Sub(start), 0)
	return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

func formatMB(mb float64) string {
	if mb >= mibPerGiB {
		return fmt.Sprintf("%.1f GB", mb/mibPerGiB)
	}
	return fmt.Sprintf("%.0f MB", mb)
}

func validSort(s sdui.Sort) bool {
	return slices.Contains(columnIDs, s.Column)
}

func processKey(n pluginsdk.ProcessNode) string {
	return fmt.Sprintf("p/%d/%d", n.Process.PID, n.Process.StartTime.UnixNano())
}

func sessionKey(id string) string {
	return "s/" + id
}

func projectKey(id string) string {
	return "a/" + id
}

func withID(n sdui.Node, id string) sdui.Node {
	n.ID = id
	return n
}

func killConfirm(nodes []pluginsdk.ProcessNode, selected int) (string, bool) {
	i := slices.IndexFunc(nodes, func(n pluginsdk.ProcessNode) bool { return n.Process.PID == selected })
	if i < 0 {
		return "", false
	}
	return fmt.Sprintf("Kill %s (%d) and its children?", nodes[i].Process.Comm, selected), true
}
