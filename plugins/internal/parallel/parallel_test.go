package parallel_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/plugins/internal/parallel"
)

func TestForEachRunsEveryJobWithinTheLimit(t *testing.T) {
	t.Parallel()
	var running, peak atomic.Int32
	ran := make([]bool, 40)

	parallel.ForEach(len(ran), 4, func(i int) {
		now := running.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		ran[i] = true
		running.Add(-1)
	})

	assert.LessOrEqual(t, peak.Load(), int32(4))
	assert.NotContains(t, ran, false)
}
