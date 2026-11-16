package main_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func TestUsageMergesRateLimitReportsAndSurvivesRestart(t *testing.T) {
	t.Parallel()
	rateLimit := func(info map[string]any) map[string]any {
		return map[string]any{"update": map[string]any{"sessionUpdate": "usage_update", "used": 10, "size": 100, "_meta": map[string]any{"_claude/rateLimit": info}}}
	}
	f := start(t, map[string]any{"prompt": []map[string]any{
		rateLimit(map[string]any{"status": "allowed", "unifiedWindows": map[string]any{
			"five_hour": map[string]any{"utilization": 0.424, "resetsAt": 4102444800, "status": "allowed"},
			"seven_day": map[string]any{"utilization": 0.18, "resetsAt": 4102531200, "status": "allowed"},
		}}),
		rateLimit(map[string]any{"status": "allowed_warning", "rateLimitType": "seven_day", "utilization": 0.81, "resetsAt": 4102531200}),
	}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "hi")
	f.waitStatus(t, workspace, session, "done")
	testutil.WaitEvents(t, f.h.Bus, "agents.usage", func(events []bus.Event) bool {
		return len(events) > 0 && strings.Contains(string(events[len(events)-1].Payload), "allowed_warning")
	})

	f.restartPlugin(t, workspace)

	var got struct {
		Windows map[string]struct {
			Used     int    `json:"used"`
			ResetsAt int64  `json:"resetsAt"`
			Status   string `json:"status"`
		} `json:"windows"`
		UpdatedAt int64 `json:"updatedAt"`
	}
	require.NoError(t, f.run(t, "", "agents.usage", nil, &got))
	assert.Equal(t, 42, got.Windows["five_hour"].Used)
	assert.Equal(t, "allowed", got.Windows["five_hour"].Status)
	assert.Equal(t, 81, got.Windows["seven_day"].Used)
	assert.Equal(t, int64(4102531200), got.Windows["seven_day"].ResetsAt)
	assert.Equal(t, "allowed_warning", got.Windows["seven_day"].Status)
	assert.NotZero(t, got.UpdatedAt)
}

func TestSessionShareSplitsLimitGrowthByCostAndSurvivesRestart(t *testing.T) {
	t.Parallel()
	rate := func(match string, fiveHour, sevenDay float64) map[string]any {
		return map[string]any{"match": match, "update": map[string]any{"sessionUpdate": "usage_update", "used": 10, "size": 100, "_meta": map[string]any{"_claude/rateLimit": map[string]any{"status": "allowed", "unifiedWindows": map[string]any{
			"five_hour": map[string]any{"utilization": fiveHour, "resetsAt": 4102444800, "status": "allowed"},
			"seven_day": map[string]any{"utilization": sevenDay, "resetsAt": 4102531200, "status": "allowed"},
		}}}}}
	}
	cost := func(match string, amount float64) map[string]any {
		return map[string]any{"match": match, "update": map[string]any{"sessionUpdate": "usage_update", "used": 10, "size": 100, "cost": map[string]any{"amount": amount, "currency": "USD"}}}
	}
	f := start(t, map[string]any{"prompt": []map[string]any{
		rate("first", 0.10, 0.50),
		cost("first", 1),
		rate("second", 0.12, 0.50),
		cost("second", 1),
		cost("second", 3),
		rate("second", 0.18, 0.54),
	}})
	workspace := f.workspace(t, newRepo(t))
	first := f.newSession(t, workspace)
	f.prompt(t, workspace, first, "first")
	f.waitStatus(t, workspace, first, "done")
	second := f.newSession(t, workspace)
	f.prompt(t, workspace, second, "second")
	f.waitStatus(t, workspace, second, "done")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return strings.Contains(string(rawTree(t, n)), "≈6% of 5h · ≈3% of 7d")
	})

	f.restartPlugin(t, workspace)

	var got struct {
		Sessions map[string]map[string]float64 `json:"sessions"`
	}
	require.NoError(t, f.run(t, "", "agents.usage", nil, &got))
	assert.InDelta(t, 2, got.Sessions[first]["five_hour"], 0.001)
	assert.InDelta(t, 1, got.Sessions[first]["seven_day"], 0.001)
	assert.InDelta(t, 6, got.Sessions[second]["five_hour"], 0.001)
	assert.InDelta(t, 3, got.Sessions[second]["seven_day"], 0.001)
}
