package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseScheduleExactlyOne(t *testing.T) {
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		cron     string
		timezone string
		every    string
		want     string
	}{
		{name: "none", want: "give exactly one of cron, every, at, in"},
		{name: "two", cron: "0 9 * * *", every: "15m", want: "give exactly one of cron, every, at, in"},
		{name: "every below minimum", every: "30s", want: "at least 1m"},
		{name: "bad cron", cron: "61 * * * *", want: "61"},
		{name: "bad timezone", cron: "0 9 * * *", timezone: "Mars/Base", want: "Mars/Base"},
		{name: "never fires", cron: "0 0 30 2 *", want: "never fires"},
		{name: "cron tz prefix", cron: "CRON_TZ=UTC 0 9 * * *", want: "timezone field"},
		{name: "tz prefix", cron: "TZ=UTC 0 9 * * *", want: "timezone field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSchedule(tt.cron, tt.timezone, tt.every, "", "", now)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestScheduleNext(t *testing.T) {
	minsk, err := time.LoadLocation("Europe/Minsk")
	require.NoError(t, err)
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, minsk)

	daily, err := parseSchedule("0 9 * * *", "Europe/Minsk", "", "", "", now)
	require.NoError(t, err)
	next, ok := daily.Next(now)
	require.True(t, ok)
	assert.True(t, next.Equal(time.Date(2026, 10, 31, 9, 0, 0, 0, minsk)), next)

	every, err := parseSchedule("", "", "15m", "", "", now)
	require.NoError(t, err)
	next, ok = every.Next(now)
	require.True(t, ok)
	assert.True(t, next.Equal(now.Add(15*time.Minute)), next)

	in, err := parseSchedule("", "", "", "", "30m", now)
	require.NoError(t, err)
	assert.True(t, in.OneShot())
	assert.True(t, in.At.Equal(now.Add(30*time.Minute)), in.At)
	next, ok = in.Next(now)
	require.True(t, ok)
	assert.True(t, next.Equal(now.Add(30*time.Minute)), next)
	_, ok = in.Next(now.Add(30 * time.Minute))
	assert.False(t, ok)
}

func TestDescribe(t *testing.T) {
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		cron     string
		timezone string
		every    string
		at       string
		want     string
	}{
		{name: "daily", cron: "0 9 * * *", want: "every day 09:00"},
		{name: "cron", cron: "*/5 * * * *", want: "cron */5 * * * *"},
		{name: "every", every: "1m", want: "every 1m"},
		{name: "at", timezone: "Europe/Minsk", at: "2026-10-31T06:00:00Z", want: "once at 2026-10-31 09:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := parseSchedule(tt.cron, tt.timezone, tt.every, tt.at, "", now)
			require.NoError(t, err)
			assert.Equal(t, tt.want, s.Describe())
		})
	}
}
