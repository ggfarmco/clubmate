package testutil

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
)

func TestParseScaleFallsBackToDefault(t *testing.T) {
	assert.Equal(t, 0.5, parseScale("0.5"))
	assert.Equal(t, float64(defaultTimeScale), parseScale(""))
	assert.Equal(t, float64(defaultTimeScale), parseScale("-1"))
}

func TestWaitEventsReturnsOnceALateEventLands(t *testing.T) {
	b, err := bus.Open(filepath.Join(t.TempDir(), "bus.db"))
	require.NoError(t, err)
	t.Cleanup(func() { b.Close() })
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = b.Publish(context.Background(), "demo.turn", "test", []byte(`{"n":1}`))
	}()
	events := WaitEvents(t, b, "demo.turn", func(e []bus.Event) bool { return len(e) == 1 })
	assert.JSONEq(t, `{"n":1}`, string(events[0].Payload))
}

func TestWaitEventsSeesEventsPastOneReadPage(t *testing.T) {
	b, err := bus.Open(filepath.Join(t.TempDir(), "bus.db"))
	require.NoError(t, err)
	t.Cleanup(func() { b.Close() })
	for i := range eventReadLimit + 1 {
		_, err := b.Publish(t.Context(), "demo.turn", "test", []byte(fmt.Sprintf(`{"n":%d}`, i)))
		require.NoError(t, err)
	}
	events := WaitEvents(t, b, "demo.turn", func(e []bus.Event) bool { return len(e) == eventReadLimit+1 })
	assert.JSONEq(t, fmt.Sprintf(`{"n":%d}`, eventReadLimit), string(events[eventReadLimit].Payload))
}
