package automation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/sdui"
)

func TestRunsTableCollapsesOnlyConsecutiveIdenticalRuns(t *testing.T) {
	at := time.Date(2026, 11, 3, 15, 46, 0, 0, time.UTC)
	var history []Run
	for i, status := range []string{StatusOK, StatusOK, StatusError, StatusOK, StatusOK} {
		history = append(history, Run{At: at.Add(time.Duration(i) * 2 * time.Minute), Status: status, Result: `{"text":"main is green"}`, Reason: "boom"})
	}
	cells := func(r Run) ([]string, string) { return nil, Words("github.ci", nil, r, nil) }
	link := func(r Run) (string, string) {
		if r.Status != StatusError {
			return "", ""
		}
		return "showLog", "Show log"
	}

	table, selected, picked := RunsTable(history, time.UTC, nil, cells, link, RunsView{})
	rows := tableRows(table)
	assert.Equal(t, [][]string{
		{"15:54 – 15:52", "ok ×2", "same result: main is green"},
		{"Nov 3 15:50:00", "failed", "boom"},
		{"15:48 – 15:46", "ok ×2", "same result: main is green"},
	}, rows)
	assert.Equal(t, history[4], selected)
	assert.False(t, picked)
	assert.NotContains(t, table.Props, "selected")
	links := map[string]string{}
	for _, r := range table.Props["rows"].([]sdui.TableRow) {
		if c := r.Cells[len(r.Cells)-1]; c.Action != "" {
			links[r.ID] = c.Action + " " + c.Label
		}
	}
	assert.Equal(t, map[string]string{RunID(history[2]): "showLog Show log"}, links)

	table, selected, picked = RunsTable(history, time.UTC, nil, cells, link, RunsView{Selected: RunID(history[0])})
	assert.Equal(t, history[0], selected)
	assert.True(t, picked)
	assert.Equal(t, "g"+RunID(history[0])[1:], table.Props["selected"])
}

func tableRows(n sdui.Node) [][]string {
	var out [][]string
	for _, r := range n.Props["rows"].([]sdui.TableRow) {
		var cells []string
		for _, c := range r.Cells {
			cells = append(cells, c.Text)
		}
		out = append(out, cells)
	}
	return out
}
