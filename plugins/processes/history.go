package main

import (
	"math"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	historyLen = 900
	historyTTL = 15 * time.Minute
	recentLen  = 60
)

type seriesKind uint8

const (
	kindProcess seriesKind = iota + 1
	kindSession
	kindProject
)

type seriesKey struct {
	kind  seriesKind
	id    string
	pid   int
	start int64
}

type series struct {
	cpu  ring
	mem  ring
	seen time.Time
}

type ring struct {
	values []float64
	head   int
}

type history struct {
	series map[seriesKey]*series
}

func newHistory() *history {
	return &history{series: map[seriesKey]*series{}}
}

func (h *history) record(sessions []group, nodes []pluginsdk.ProcessNode, now time.Time) {
	type total struct{ cpu, mb float64 }
	projectOf := sessionProjects(sessions)
	totals := make(map[seriesKey]*total, 2*len(sessions))
	for _, s := range sessions {
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
		for _, t := range [...]*total{totals[sessionKey(n.SessionID)], totals[projectKey(project)]} {
			t.cpu += n.Process.CPU
			t.mb += mb
		}
	}
	for key, t := range totals {
		h.push(key, t.cpu, t.mb, now)
	}
	h.prune(now)
}

func (h *history) push(key seriesKey, cpu, memMB float64, now time.Time) {
	s, ok := h.series[key]
	if !ok {
		s = &series{}
		h.series[key] = s
	}
	s.cpu.push(math.Round(cpu))
	s.mem.push(math.Round(memMB*10) / 10)
	s.seen = now
}

func (h *history) prune(now time.Time) {
	for key, s := range h.series {
		if now.Sub(s.seen) >= historyTTL {
			delete(h.series, key)
		}
	}
}

func (h *history) cpu(key seriesKey, n int) []float64 {
	if s, ok := h.series[key]; ok {
		return s.cpu.tail(n)
	}
	return nil
}

func (h *history) mem(key seriesKey, n int) []float64 {
	if s, ok := h.series[key]; ok {
		return s.mem.tail(n)
	}
	return nil
}

func (r *ring) push(v float64) {
	if len(r.values) < historyLen {
		r.values = append(r.values, v)
		return
	}
	r.values[r.head] = v
	r.head = (r.head + 1) % historyLen
}

func (r *ring) tail(n int) []float64 {
	n = min(n, len(r.values))
	if n == 0 {
		return nil
	}
	out := make([]float64, n)
	start := (r.head + len(r.values) - n) % len(r.values)
	copied := copy(out, r.values[start:])
	copy(out[copied:], r.values)
	return out
}

func processKey(n pluginsdk.ProcessNode) seriesKey {
	return seriesKey{kind: kindProcess, pid: n.Process.PID, start: n.Process.StartTime.UnixNano()}
}

func sessionKey(id string) seriesKey {
	return seriesKey{kind: kindSession, id: id}
}

func projectKey(id string) seriesKey {
	return seriesKey{kind: kindProject, id: id}
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
