package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHistoryRing(t *testing.T) {
	now := time.Now()
	h := newHistory()
	for i := range 1000 {
		h.push(sessionKey("p"), float64(i), 1, now)
	}
	cpu := h.cpu(sessionKey("p"), historyLen)
	assert.Len(t, cpu, historyLen)
	assert.InDelta(t, 100, cpu[0], 0)
	recent := h.cpu(sessionKey("p"), 150)
	assert.Len(t, recent, 150)
	assert.InDelta(t, 850, recent[0], 0)
	assert.InDelta(t, 999, recent[149], 0)

	peak, avg := peakAvg(cpu)
	assert.InDelta(t, 999, peak, 0)
	assert.InDelta(t, 549.5, avg, 1e-9)

	h.prune(now.Add(historyTTL - time.Second))
	assert.NotEmpty(t, h.cpu(sessionKey("p"), historyLen))
	h.prune(now.Add(historyTTL))
	assert.Empty(t, h.cpu(sessionKey("p"), historyLen))
}
