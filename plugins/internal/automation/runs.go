package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	ActRunJSON   = "runJson"
	HistoryID    = "history"
	whenLayout   = "Jan 2 15:04:05"
	clockLayout  = "15:04"
	runJSONLines = 8
	dayLayout    = "Jan 2"
)

type RunsView struct {
	Selected string
	Expanded map[string]bool
}

type RunCells func(r Run) (extra []string, words string)

type runGroup struct {
	id    string
	runs  []Run
	extra []string
	words string
}

func RunsTable(history []Run, loc *time.Location, extra []sdui.Column, cells RunCells, v RunsView) (sdui.Node, Run, bool) {
	groups := groupRuns(history, cells)
	selected, run, picked := v.selection(groups)
	if !picked {
		selected = ""
	}
	rows := make([]sdui.TableRow, 0, len(history))
	for _, g := range groups {
		if len(g.runs) == 1 {
			rows = append(rows, runRow(RunID(g.runs[0]), g.runs[0], g.extra, g.words, loc, 0))
			continue
		}
		label, tone := StatusLabel(g.runs[0])
		newest, oldest := g.runs[0].At.In(loc), g.runs[len(g.runs)-1].At.In(loc)
		when := newest.Format(clockLayout) + " – " + oldest.Format(clockLayout)
		if newest.YearDay() != oldest.YearDay() || newest.Year() != oldest.Year() {
			when = newest.Format(dayLayout) + " – " + oldest.Format(dayLayout)
		}
		cellsOf := []sdui.Cell{{Text: when}}
		for _, e := range g.extra {
			cellsOf = append(cellsOf, sdui.Cell{Text: e})
		}
		cellsOf = append(cellsOf, sdui.Cell{Text: label + " ×" + strconv.Itoa(len(g.runs)), Tone: tone}, sdui.Cell{Text: "same result: " + g.words})
		open := v.Expanded[g.id]
		rows = append(rows, sdui.TableRow{ID: g.id, HasChildren: true, Expanded: open, Cells: cellsOf})
		if open {
			for _, r := range g.runs {
				rows = append(rows, runRow(RunID(r), r, g.extra, g.words, loc, 1))
			}
		}
	}
	columns := append([]sdui.Column{{ID: "when", Title: "When", Width: 150}}, extra...)
	columns = append(columns, sdui.Column{ID: "status", Title: "Status", Width: 80}, sdui.Column{ID: "result", Title: "Result"})
	return sdui.Table(HistoryID, columns, rows, selected, sdui.Sort{}), run, picked
}

func RunID(r Run) string {
	return "r" + strconv.FormatInt(r.At.UnixNano(), 10)
}

func RunsRows(id string, history []Run, selected Run, picked, open bool) []sdui.Node {
	which := map[bool]string{true: "selected", false: "latest"}[picked]
	toggle := DetailAction{Action: ActRunJSON, Label: "Show the " + which + " run as JSON", Icon: "chevron.right", Variant: sdui.VariantGhost}
	if open {
		toggle.Label, toggle.Icon = "Hide the "+which+" run's JSON", "chevron.down"
	}
	rows := []sdui.Node{sdui.HStack(sdui.MutedText("Runs"), sdui.Text(runCounts(history)), ActionNode(id, toggle))}
	if open {
		rows = append(rows, sdui.HStack(sdui.MutedText(""), sdui.WithLines(sdui.MutedText(runJSON(selected)), runJSONLines)))
	}
	return rows
}

func runCounts(history []Run) string {
	counts := map[string]int{}
	for _, r := range history {
		label, _ := StatusLabel(r)
		counts[label]++
	}
	var parts []string
	for _, label := range []string{LabelOK, LabelFailed, LabelMissed, LabelSkipped} {
		if n := counts[label]; n > 0 {
			parts = append(parts, label+" "+strconv.Itoa(n))
		}
	}
	return strings.Join(parts, " · ")
}

func runJSON(r Run) string {
	out := struct {
		Run
		Result any `json:"result,omitempty"`
	}{Run: r}
	if json.Valid([]byte(r.Result)) {
		out.Result = json.RawMessage(r.Result)
	} else if r.Result != "" {
		out.Result = r.Result
	}
	raw, _ := json.MarshalIndent(out, "", "  ")
	return string(raw)
}

func groupRuns(history []Run, cells RunCells) []runGroup {
	var groups []runGroup
	for _, r := range NewestFirst(history) {
		extra, words := cells(r)
		if n := len(groups); n > 0 && sameRun(groups[n-1], r, extra, words) {
			groups[n-1].runs = append(groups[n-1].runs, r)
			groups[n-1].id = "g" + strconv.FormatInt(r.At.UnixNano(), 10)
			continue
		}
		groups = append(groups, runGroup{id: "g" + strconv.FormatInt(r.At.UnixNano(), 10), runs: []Run{r}, extra: extra, words: words})
	}
	return groups
}

func sameRun(g runGroup, r Run, extra []string, words string) bool {
	label, _ := StatusLabel(r)
	last, _ := StatusLabel(g.runs[0])
	return label == last && words == g.words && strings.Join(extra, "\x00") == strings.Join(g.extra, "\x00")
}

func runRow(id string, r Run, extra []string, words string, loc *time.Location, depth int) sdui.TableRow {
	label, tone := StatusLabel(r)
	cells := []sdui.Cell{{Text: r.At.In(loc).Format(whenLayout)}}
	for _, e := range extra {
		cells = append(cells, sdui.Cell{Text: e})
	}
	cells = append(cells, sdui.Cell{Text: label, Tone: tone}, sdui.Cell{Text: words})
	return sdui.TableRow{ID: id, Depth: depth, Cells: cells}
}

func (v RunsView) selection(groups []runGroup) (string, Run, bool) {
	for _, g := range groups {
		if v.Selected == "" {
			break
		}
		if g.id == v.Selected {
			return g.id, g.runs[0], true
		}
		for _, r := range g.runs {
			switch {
			case RunID(r) != v.Selected:
			case len(g.runs) == 1 || v.Expanded[g.id]:
				return v.Selected, r, true
			default:
				return g.id, r, true
			}
		}
	}
	if len(groups) == 0 {
		return "", Run{}, false
	}
	return groups[0].id, groups[0].runs[0], false
}

type Runs struct {
	mu       sync.Mutex
	expanded map[string]map[string]bool
}

func runKey(workspace string) string {
	return "run/" + workspace
}

func (s *Runs) Handle(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) error {
	var id string
	if err := json.Unmarshal(ev.Value, &id); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	switch ev.Action {
	case "expand", "collapse":
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.expanded == nil {
			s.expanded = map[string]map[string]bool{}
		}
		if s.expanded[ev.WorkspaceID] == nil {
			s.expanded[ev.WorkspaceID] = map[string]bool{}
		}
		s.expanded[ev.WorkspaceID][id] = ev.Action == "expand"
		return nil
	case "select":
		return h.StorageSet(ctx, runKey(ev.WorkspaceID), id)
	}
	return nil
}

func (s *Runs) View(ctx context.Context, h *pluginsdk.Host, workspace string) (RunsView, error) {
	var v RunsView
	if _, err := h.StorageGet(ctx, runKey(workspace), &v.Selected); err != nil {
		return RunsView{}, err
	}
	s.mu.Lock()
	v.Expanded = maps.Clone(s.expanded[workspace])
	s.mu.Unlock()
	return v, nil
}
