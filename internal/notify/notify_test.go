package notify_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/internal/notify"
)

func TestLimiterFreesSlotsAfterWindow(t *testing.T) {
	l := notify.NewLimiter(2, 3, 10*time.Second)
	start := time.Unix(1000, 0)
	assert.True(t, l.Allow("w", start))
	assert.True(t, l.Allow("w", start.Add(time.Second)))
	assert.False(t, l.Allow("w", start.Add(2*time.Second)))
	assert.True(t, l.Allow("w", start.Add(10*time.Second)))
	assert.True(t, l.Allow("v", start.Add(10*time.Second)))
	assert.False(t, l.Allow("u", start.Add(10*time.Second)))
	assert.True(t, l.Allow("u", start.Add(11*time.Second)))
}
