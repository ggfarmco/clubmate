package bus_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
)

func TestPublishReadAndCursor(t *testing.T) {
	b := openBus(t)
	e1, err := b.Publish(t.Context(), "session.created", "core", json.RawMessage(`{"id":"ses_1"}`))
	require.NoError(t, err)
	e2, err := b.Publish(t.Context(), "project.created", "core", nil)
	require.NoError(t, err)
	assert.Equal(t, e2.Seq, b.Head())
	got, err := b.Read(t.Context(), "session.*", 0, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, e1.Seq, got[0].Seq)
	assert.JSONEq(t, `{"id":"ses_1"}`, string(got[0].Payload))
	require.NoError(t, b.SetCursor(t.Context(), "agent-a", e1.Seq))
	cur, err := b.Cursor(t.Context(), "agent-a")
	require.NoError(t, err)
	assert.Equal(t, e1.Seq, cur)
}

func TestPublishRejectsBadInput(t *testing.T) {
	b := openBus(t)
	_, err := b.Publish(t.Context(), "Bad Topic", "core", nil)
	assert.ErrorIs(t, err, bus.ErrInvalidTopic)
	_, err = b.Publish(t.Context(), "a.b", "core", json.RawMessage(`[1]`))
	assert.ErrorIs(t, err, bus.ErrInvalidPayload)
	_, err = b.Publish(t.Context(), "a.b", "core", json.RawMessage(`{"v":"`+strings.Repeat("x", 64<<10)+`"}`))
	assert.ErrorIs(t, err, bus.ErrPayloadTooLarge)
}

func TestSubscribeBackfillThenLive(t *testing.T) {
	b := openBus(t)
	first, _ := b.Publish(t.Context(), "t.a", "x", nil)
	sub, err := b.Subscribe("t.*", 0)
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	second, _ := b.Publish(t.Context(), "t.b", "x", nil)
	assert.Equal(t, []int64{first.Seq, second.Seq}, []int64{recv(t, sub).Seq, recv(t, sub).Seq})
}

func TestSubscribeLargeBackfillDuringPublishHasNoGapsOrDuplicates(t *testing.T) {
	b := openBus(t)
	for range 300 {
		_, err := b.Publish(t.Context(), "t.x", "x", nil)
		require.NoError(t, err)
	}
	sub, err := b.Subscribe("*", 0)
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 300 {
			_, _ = b.Publish(context.Background(), "t.x", "x", nil)
		}
	}()
	for want := int64(1); want <= 600; want++ {
		require.Equal(t, want, recv(t, sub).Seq)
	}
	<-done
}

func TestFullBackfillBufferDoesNotDropOnGoingLive(t *testing.T) {
	b := openBus(t)
	for range 200 {
		_, err := b.Publish(t.Context(), "t.x", "x", nil)
		require.NoError(t, err)
	}
	sub, err := b.Subscribe("*", 0)
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	time.Sleep(100 * time.Millisecond)
	for range 100 {
		_, err := b.Publish(t.Context(), "t.x", "x", nil)
		require.NoError(t, err)
	}
	for want := int64(1); want <= 300; want++ {
		require.Equal(t, want, recv(t, sub).Seq)
	}
}

func TestCloseStopsBlockedBackfill(t *testing.T) {
	b := openBus(t)
	for range 300 {
		_, err := b.Publish(t.Context(), "t.x", "x", nil)
		require.NoError(t, err)
	}
	sub, err := b.Subscribe("*", 0)
	require.NoError(t, err)
	sub.Close()
	for range sub.C() {
	}
	assert.NoError(t, sub.Err())
}

func TestSlowSubscriberIsDroppedWithoutBlocking(t *testing.T) {
	b := openBus(t)
	sub, err := b.Subscribe("*", 0)
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			_, _ = b.Publish(context.Background(), "t.x", "x", nil)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("publisher blocked by slow subscriber")
	}
	for range sub.C() {
	}
	assert.ErrorIs(t, sub.Err(), bus.ErrSlowSubscriber)
}

func openBus(t *testing.T) *bus.Bus {
	t.Helper()
	b, err := bus.Open(filepath.Join(t.TempDir(), "bus.db"))
	require.NoError(t, err)
	t.Cleanup(func() { b.Close() })
	return b
}

func recv(t *testing.T, sub *bus.Subscription) bus.Event {
	t.Helper()
	select {
	case e, ok := <-sub.C():
		require.True(t, ok, "subscription closed: %v", sub.Err())
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no event within 2s")
		return bus.Event{}
	}
}
