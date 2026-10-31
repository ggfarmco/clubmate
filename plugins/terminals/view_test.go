package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/contract"
	"clubmate/pkg/sdui"
)

func TestResolveSelection(t *testing.T) {
	two := []terminal{{ID: "a"}, {ID: "b"}}
	assert.Equal(t, "b", resolveSelection(two, "b"))
	assert.Equal(t, "a", resolveSelection(two, "gone"))
	assert.Equal(t, "a", resolveSelection(two, ""))
	assert.Equal(t, "", resolveSelection(nil, "gone"))
}

func TestSidebarShowsOnlyAttention(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { at := now.Add(-d); return &at }
	code := func(c int) *int { return &c }
	running := contract.SessionStateRunning
	exited := contract.SessionStateExited
	got := sidebarView([]terminal{
		{ID: "busy", State: running, OutputAt: ago(30 * time.Second)},
		{ID: "idle", State: running, OutputAt: ago(95 * time.Minute)},
		{ID: "bell", State: running, OutputAt: ago(time.Second), Bell: true},
		{ID: "ok", State: exited, ExitCode: code(0)},
		{ID: "failed", State: exited, ExitCode: code(2)},
		{ID: "lost", State: contract.SessionStateLost},
	}, "busy", now).Children[1].Props["items"]
	assert.Equal(t, []sdui.Item{
		{ID: "busy"},
		{ID: "idle", Detail: "idle 1h"},
		{ID: "bell", Badge: "bell"},
		{ID: "ok", Detail: "exited"},
		{ID: "failed", Badge: "exit 2"},
		{ID: "lost", Badge: "lost"},
	}, got)
}

func TestRefreshPollsOnlyWorkspacesWithRunningTerminals(t *testing.T) {
	a := &app{rendered: map[string]views{"live": {running: true}, "quiet": {}}}
	assert.Equal(t, []string{"live"}, a.polled())
}
