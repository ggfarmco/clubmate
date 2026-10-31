package fswatch

import (
	"strconv"
	"testing"

	"github.com/fsnotify/fsevents"
	"github.com/stretchr/testify/assert"
)

func TestPendingOverflow(t *testing.T) {
	many := make([]fsevents.Event, 0, OverflowOver+1)
	for i := range OverflowOver + 1 {
		many = append(many, fsevents.Event{Path: "repo/f" + strconv.Itoa(i)})
	}
	tests := []struct {
		name  string
		batch []fsevents.Event
	}{
		{"too many paths", many},
		{"lossy flag", []fsevents.Event{{Path: "repo/a"}, {Path: "repo", Flags: fsevents.KernelDropped}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPending(Keep, OverflowOver)
			p.add(tt.batch)
			var gotPaths []string
			var gotOverflow bool
			p.emit(func(paths []string, overflow bool) { gotPaths, gotOverflow = paths, overflow })
			assert.True(t, gotOverflow)
			assert.Empty(t, gotPaths)
			assert.True(t, p.empty())
		})
	}
}
