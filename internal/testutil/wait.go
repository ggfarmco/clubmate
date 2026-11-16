package testutil

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
)

const (
	defaultTimeScale = 3
	pollEvery        = 20 * time.Millisecond
	eventWait        = 10 * time.Second
	eventReadLimit   = 1000
)

var timeScale = parseScale(os.Getenv("TEST_TIMEOUT_SCALE"))

func parseScale(s string) float64 {
	if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
		return v
	}
	return defaultTimeScale
}

func Timeout(d time.Duration) time.Duration {
	return time.Duration(float64(d) * timeScale)
}

func Eventually(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, cond, Timeout(timeout), pollEvery)
}

func Poll(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(Timeout(timeout))
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollEvery)
	}
	return true
}

func Never(t *testing.T, cond func() bool, d time.Duration) {
	t.Helper()
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		require.False(t, cond(), "condition satisfied")
	}
	require.False(t, cond(), "condition satisfied")
}

func WaitEvents(t *testing.T, b *bus.Bus, topic string, cond func([]bus.Event) bool) []bus.Event {
	t.Helper()
	var got []bus.Event
	reached := Poll(func() bool {
		events, err := readAll(t, b, topic)
		if err != nil {
			return false
		}
		got = events
		return cond(events)
	}, eventWait)
	require.True(t, reached, "bus topic %q never matched; last read %d events: %s", topic, len(got), eventSummary(got))
	return got
}

func readAll(t *testing.T, b *bus.Bus, topic string) ([]bus.Event, error) {
	var all []bus.Event
	var after int64
	for {
		page, err := b.Read(t.Context(), topic, after, eventReadLimit)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < eventReadLimit {
			return all, nil
		}
		after = page[len(page)-1].Seq
	}
}

func eventSummary(events []bus.Event) string {
	out := ""
	for _, e := range events {
		out += "\n  " + e.Topic + " " + string(e.Payload)
	}
	return out
}
