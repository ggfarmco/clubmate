package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/internal/proc"
)

func TestBackgroundWork(t *testing.T) {
	t.Parallel()
	up := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	claude := proc.Process{PID: 2, PPID: 1, Comm: "claude", StartTime: up}
	mcp := []proc.Process{
		claude,
		{PID: 3, PPID: 2, Comm: "npm exec chrome-devtools-mcp@latest", StartTime: up.Add(time.Second)},
		{PID: 4, PPID: 3, Comm: "chrome-devtools-mcp", StartTime: up.Add(2 * time.Second)},
	}
	tests := []struct {
		name  string
		procs []proc.Process
		want  bool
	}{
		{"mcp servers at startup", mcp, false},
		{"shell", append(mcp[:1:1], proc.Process{PID: 5, PPID: 2, Comm: "zsh", StartTime: up.Add(time.Second)}), true},
		{"late descendant", append(mcp[:3:3], proc.Process{PID: 6, PPID: 4, Comm: "Google Chrome", StartTime: up.Add(time.Hour)}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, backgroundWork(1, tt.procs))
		})
	}
}

func TestSessionBackground(t *testing.T) {
	t.Parallel()
	up := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	procs := []proc.Process{
		{PID: 2, PPID: 1, Comm: "claude", StartTime: up},
		{PID: 3, PPID: 1, Comm: "claude", StartTime: up},
		{PID: 4, PPID: 3, Comm: "zsh", StartTime: up.Add(time.Hour)},
		{PID: 5, PPID: 2, Comm: "chrome-devtools-mcp", StartTime: up},
		{PID: 6, PPID: 5, Comm: "Google Chrome", StartTime: up.Add(time.Hour)},
	}
	args := func(pid int) []string {
		return map[int][]string{2: {"claude", "--resume=a"}, 3: {"claude", "--session-id=b"}}[pid]
	}
	tests := []struct {
		name         string
		agentID      string
		alone        bool
		alive, known bool
	}{
		{"own claude with a late non-shell descendant", "a", false, false, true},
		{"own claude runs a shell", "b", false, true, true},
		{"unknown session among others", "c", false, false, false},
		{"unknown session alone", "c", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alive, known := sessionBackground(1, tt.agentID, tt.alone, procs, args)
			assert.Equal(t, []bool{tt.alive, tt.known}, []bool{alive, known})
		})
	}
}
