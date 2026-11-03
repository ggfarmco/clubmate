package main_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
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
	testutil.Eventually(t, func() bool {
		events, err := f.h.Bus.Read(t.Context(), "agents.usage", 0, 10)
		require.NoError(t, err)
		return len(events) > 0 && strings.Contains(string(events[len(events)-1].Payload), "allowed_warning")
	}, wait)

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
