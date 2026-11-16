package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLimitResetTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 20, 0, 0, 0, time.UTC)
	full := usageState{Windows: map[string]usageWindow{windowFiveHour: {Used: 100, ResetsAt: now.Add(time.Hour).Unix()}, windowSevenDay: {Used: 40, ResetsAt: now.Add(48 * time.Hour).Unix()}}}
	tests := []struct {
		name  string
		title string
		usage usageState
		want  time.Time
	}{
		{"date and zone", "You've hit your weekly limit · resets Nov 16 at 11am (Europe/Warsaw)", full, time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)},
		{"time later today", "You've hit your session limit · resets 9pm (UTC)", full, time.Date(2026, 11, 15, 21, 0, 0, 0, time.UTC)},
		{"time passed today is tomorrow", "You've hit your session limit · resets 3:30pm (UTC)", full, time.Date(2026, 11, 16, 15, 30, 0, 0, time.UTC)},
		{"date past this year is next year", "You've reached your weekly limit · resets Jan 2 at 1am (UTC)", full, time.Date(2027, 1, 2, 1, 0, 0, 0, time.UTC)},
		{"no time in the text uses the full window", "You've hit your limit", full, now.Add(time.Hour)},
		{"no time and no full window", "You've hit your limit", usageState{}, time.Time{}},
		{"credits never reset", "You're out of usage credits", full, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := limitReset(turnFailure{kind: failureQuota, title: tt.title}, tt.usage, now)
			assert.Equal(t, !tt.want.IsZero(), ok)
			assert.True(t, tt.want.Equal(got), "got %s", got)
		})
	}
}
