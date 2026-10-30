package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMissedOnceRunsOnce(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	j := &Job{
		Enabled:  true,
		Schedule: Schedule{Cron: "0 9 * * *", Timezone: "UTC"},
		Missed:   "once",
		NextRun:  time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC),
	}

	assert.Equal(t, fire, decide(j, now))
	require.True(t, advance(j, now))
	assert.True(t, j.NextRun.Equal(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)), j.NextRun)
	assert.Equal(t, wait, decide(j, now))
}

func TestMissedSkipDoesNotRun(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	j := &Job{
		Enabled:  true,
		Schedule: Schedule{Every: "1m"},
		Missed:   "skip",
		NextRun:  now.Add(-8 * time.Hour),
	}

	assert.Equal(t, skipMissed, decide(j, now))
	require.True(t, advance(j, now))
	assert.True(t, j.NextRun.After(now), j.NextRun)
	assert.False(t, j.NextRun.After(now.Add(time.Minute)), j.NextRun)
}

func TestOnTimeFires(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	for _, missed := range []string{"once", "skip"} {
		t.Run(missed, func(t *testing.T) {
			j := &Job{
				Enabled:  true,
				Schedule: Schedule{Every: "1m"},
				Missed:   missed,
				NextRun:  now.Add(-20 * time.Second),
			}
			assert.Equal(t, fire, decide(j, now))
		})
	}
}

func TestPausesAfterThreeErrors(t *testing.T) {
	at := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	j := &Job{Enabled: true}

	assert.False(t, record(j, Run{At: at, Status: "error", Reason: "boom"}))
	assert.False(t, record(j, Run{At: at, Status: "ok"}))
	assert.Zero(t, j.Failures)
	assert.Empty(t, j.LastError)

	assert.False(t, record(j, Run{At: at, Status: "error", Reason: "first"}))
	assert.False(t, record(j, Run{At: at, Status: "skipped", Reason: "busy"}))
	assert.False(t, record(j, Run{At: at, Status: "error", Reason: "second"}))
	assert.True(t, j.Enabled)
	assert.True(t, record(j, Run{At: at, Status: "error", Reason: "third"}))
	assert.False(t, j.Enabled)
	assert.Equal(t, "third", j.LastError)
}

func TestHistoryCapped(t *testing.T) {
	at := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	j := &Job{Enabled: true}
	for i := range 25 {
		record(j, Run{At: at, Status: "ok", Result: fmt.Sprint(i)})
	}

	require.Len(t, j.History, 20)
	assert.Equal(t, "5", j.History[0].Result)
	assert.Equal(t, "24", j.History[19].Result)
}

func TestOneShotRemovedAfterRun(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	s, err := parseSchedule("", "", "", "", "5m", now)
	require.NoError(t, err)
	j := &Job{Enabled: true, Schedule: s, Missed: "once", NextRun: s.At}
	fireAt := now.Add(5 * time.Minute)

	assert.Equal(t, wait, decide(j, now))
	assert.Equal(t, fire, decide(j, fireAt))
	assert.False(t, advance(j, fireAt))

	corrupt := &Job{Enabled: true, Schedule: Schedule{Every: "bogus"}, NextRun: fireAt}
	assert.True(t, advance(corrupt, fireAt))
	assert.False(t, corrupt.Enabled)
	assert.Equal(t, "schedule has no next run", corrupt.LastError)
}
