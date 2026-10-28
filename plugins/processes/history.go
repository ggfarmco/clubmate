package main

import (
	"math"
	"slices"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	historyLen = 900
	historyTTL = 15 * time.Minute
	recentLen  = 60
)

type series struct {
	cpu  []float64
	mem  []float64
	seen time.Time
}

type history struct {
	series map[string]*series
}

func newHistory() *history {
	return &history{series: map[string]*series{}}
}

func (h *history) push(key string, cpu, memMB float64, now time.Time) {
	s, ok := h.series[key]
	if !ok {
		s = &series{}
		h.series[key] = s
	}
	s.cpu = appendCapped(s.cpu, math.Round(cpu))
	s.mem = appendCapped(s.mem, math.Round(memMB*10)/10)
	s.seen = now
}

func (h *history) record(sessions []pluginsdk.Session, nodes []pluginsdk.ProcessNode, now time.Time) {
	type total struct{ cpu, mb float64 }
	projectOf := make(map[string]string, len(sessions))
	totals := make(map[string]*total, 2*len(sessions))
	for _, s := range sessions {
		projectOf[s.ID] = s.ProjectID
		totals[sessionKey(s.ID)] = &total{}
		totals[projectKey(s.ProjectID)] = &total{}
	}
	for _, n := range nodes {
		mb := float64(n.Process.RSSKiB) / kibPerMiB
		h.push(processKey(n), n.Process.CPU, mb, now)
		project, ok := projectOf[n.SessionID]
		if !ok {
			continue
		}
		for _, key := range []string{sessionKey(n.SessionID), projectKey(project)} {
			totals[key].cpu += n.Process.CPU
			totals[key].mb += mb
		}
	}
	for key, t := range totals {
		h.push(key, t.cpu, t.mb, now)
	}
	h.prune(now)
}

func (h *history) prune(now time.Time) {
	for key, s := range h.series {
		if now.Sub(s.seen) >= historyTTL {
			delete(h.series, key)
		}
	}
}

func (h *history) cpu(key string, n int) []float64 {
	if s, ok := h.series[key]; ok {
		return tail(s.cpu, n)
	}
	return nil
}

func (h *history) mem(key string, n int) []float64 {
	if s, ok := h.series[key]; ok {
		return tail(s.mem, n)
	}
	return nil
}

func appendCapped(values []float64, v float64) []float64 {
	if len(values) < historyLen {
		return append(values, v)
	}
	copy(values, values[1:])
	values[len(values)-1] = v
	return values
}

func tail(values []float64, n int) []float64 {
	return slices.Clone(values[max(0, len(values)-n):])
}

func peakAvg(values []float64) (peak, avg float64) {
	if len(values) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, v := range values {
		peak = max(peak, v)
		sum += v
	}
	return peak, sum / float64(len(values))
}
