package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/pluginsdk"
)

func (h *history) record(sessions []group, nodes, core []pluginsdk.ProcessNode, workspaces []string, now time.Time) {
	h.recordSteps(1, sessions, nodes, core, workspaces, now)
}

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

func TestHiddenSampleFillsTheSecondsItCovers(t *testing.T) {
	h := newHistory()
	n := pluginsdk.ProcessNode{Process: pluginsdk.ProcessInfo{PID: 7, CPU: 5}, SessionID: "s"}

	h.recordSteps(hiddenSteps, nil, []pluginsdk.ProcessNode{n}, nil, nil, time.Now())
	h.record(nil, []pluginsdk.ProcessNode{n}, nil, nil, time.Now())

	assert.Len(t, h.cpu(processKey(n), historyLen), hiddenSteps+1)
}

func TestArgvIsReadOncePerProcessStart(t *testing.T) {
	reads := 0
	c := newArgvCache(func(int) []string { reads++; return []string{"x"} })
	start := time.Now()
	procs := func(at time.Time) []pluginsdk.ProcessInfo { return []pluginsdk.ProcessInfo{{PID: 7, StartTime: at}} }

	c.fill(procs(start))
	got := procs(start)
	c.fill(got)
	c.fill(procs(start.Add(time.Second)))

	assert.Equal(t, 2, reads)
	assert.Equal(t, []string{"x"}, got[0].Args)
}

func TestHiddenSectionSamplesEveryTenTicks(t *testing.T) {
	a := newApp(0)
	for range hiddenSteps - 1 {
		assert.Zero(t, a.stepsDue(false))
	}
	assert.Equal(t, hiddenSteps, a.stepsDue(false))
	assert.Zero(t, a.stepsDue(false))
	assert.Equal(t, 2, a.stepsDue(true))
	assert.Equal(t, 1, a.stepsDue(true))
}
