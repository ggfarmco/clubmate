package testutil

import (
	"context"
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
