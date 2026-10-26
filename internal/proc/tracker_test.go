package proc_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/internal/proc"
)

func TestAttribute(t *testing.T) {
	t0 := time.Unix(1000, 0)
	envs := map[int][]string{200: {"CLUBMATE_SESSION_ID=ses_a"}}
	tr := proc.NewTracker(func(pid int) ([]string, error) { return envs[pid], nil }, 99)
	roots := []proc.SessionRoot{{SessionID: "ses_a", RootPID: 100, PGID: 100}}
	snap := []proc.Process{
		{PID: 99, PPID: 1, PGID: 99, Comm: "clubmate-core", StartTime: t0},
		{PID: 100, PPID: 50, PGID: 100, Comm: "nvim", StartTime: t0},
		{PID: 101, PPID: 100, PGID: 101, Comm: "gopls", StartTime: t0},
		{PID: 200, PPID: 1, PGID: 200, Comm: "gopls", StartTime: t0},
		{PID: 300, PPID: 1, PGID: 300, Comm: "zsh", StartTime: t0},
		{PID: 400, PPID: 101, PGID: 400, Comm: "go", StartTime: t0},
	}
	nodes := tr.Attribute(snap, roots)
	assert.ElementsMatch(t, []int{100, 101, 200, 400}, pids(nodes))
	assert.True(t, node(nodes, 200).Orphan)
	assert.False(t, node(nodes, 101).Orphan)
	assert.True(t, node(nodes, 101).Duplicate)
	assert.True(t, node(nodes, 200).Duplicate)

	reused := []proc.Process{{PID: 400, PPID: 1, PGID: 400, Comm: "other", StartTime: t0.Add(time.Hour)}}
	assert.Empty(t, tr.Attribute(reused, roots))
}

func TestAttributeCachesEnvAndSortsOutput(t *testing.T) {
	t0 := time.Unix(1000, 0)
	reads := map[int]int{}
	envs := map[int][]string{
		7: {"CLUBMATE_SESSION_ID=ses_b"},
		8: {"CLUBMATE_SESSION_ID=ses_unknown"},
		9: {"CLUBMATE_SESSION_ID=ses_a"},
	}
	tr := proc.NewTracker(func(pid int) ([]string, error) {
		reads[pid]++
		return envs[pid], nil
	})
	roots := []proc.SessionRoot{{SessionID: "ses_a", RootPID: 500, PGID: 500}, {SessionID: "ses_b", RootPID: 600, PGID: 600}}
	snap := []proc.Process{
		{PID: 9, PPID: 1, PGID: 9, Comm: "x", StartTime: t0},
		{PID: 8, PPID: 1, PGID: 8, Comm: "y", StartTime: t0},
		{PID: 7, PPID: 1, PGID: 7, Comm: "z", StartTime: t0},
	}
	nodes := tr.Attribute(snap, roots)
	assert.Equal(t, []int{9, 7}, pids(nodes))
	tr.Attribute(snap, roots)
	assert.Equal(t, 1, reads[8])
	assert.Equal(t, 1, reads[9])
}
