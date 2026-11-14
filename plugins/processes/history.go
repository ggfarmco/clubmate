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

type seriesKind uint8

const (
	kindProcess seriesKind = iota + 1
	kindSession
	kindWorkspace
	kindSubtree
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
	tree   subtreeScratch
	steps  int
}

type subtreeScratch struct {
	nodes []*pluginsdk.ProcessNode
	heads map[link]int
	next  []int
	roots []int
}

type link struct {
	session string
	pid     int
}

func newHistory() *history {
	return &history{series: map[seriesKey]*series{}, tree: subtreeScratch{heads: map[link]int{}}}
}

func (h *history) recordSteps(steps int, sessions []group, nodes, core []pluginsdk.ProcessNode, workspaces []string, now time.Time) {
	h.steps = max(steps, 1)
	type total struct{ cpu, mb float64 }
	h.recordSubtrees(now, nodes, core)
	workspaceOf := sessionWorkspaces(sessions)
	totals := make(map[seriesKey]*total, 2*len(sessions)+len(workspaces)+1)
	for _, s := range sessions {
		totals[sessionKey(s.ID)] = &total{}
		totals[workspaceKey(s.WorkspaceID)] = &total{}
	}
	if len(core) > 0 {
		coreTotal := &total{}
		for _, n := range core {
			mb := float64(n.Process.RSSKiB) / kibPerMiB
			h.push(processKey(n), n.Process.CPU, mb, now)
			coreTotal.cpu += n.Process.CPU
			coreTotal.mb += mb
		}
		totals[sessionKey(coreID)] = coreTotal
		for _, id := range workspaces {
			t, ok := totals[workspaceKey(id)]
			if !ok {
				t = &total{}
				totals[workspaceKey(id)] = t
			}
			t.cpu += coreTotal.cpu
			t.mb += coreTotal.mb
		}
	}
	for _, n := range nodes {
		mb := float64(n.Process.RSSKiB) / kibPerMiB
		h.push(processKey(n), n.Process.CPU, mb, now)
		workspace, ok := workspaceOf[n.SessionID]
		if !ok {
			continue
		}
		for _, t := range [...]*total{totals[sessionKey(n.SessionID)], totals[workspaceKey(workspace)]} {
			t.cpu += n.Process.CPU
			t.mb += mb
		}
	}
	for key, t := range totals {
		h.push(key, t.cpu, t.mb, now)
	}
	h.prune(now)
}

func (h *history) recordSubtrees(now time.Time, sets ...[]pluginsdk.ProcessNode) {
	t := &h.tree
	for _, set := range sets {
		for i := range set {
			t.nodes = append(t.nodes, &set[i])
		}
	}
	for _, n := range t.nodes {
		t.heads[link{session: n.SessionID, pid: n.Process.PID}] = -1
	}
	t.next = slices.Grow(t.next[:0], len(t.nodes))[:len(t.nodes)]
	for i, n := range slices.Backward(t.nodes) {
		parent := link{session: n.SessionID, pid: n.Process.PPID}
		if head, ok := t.heads[parent]; ok && n.Process.PPID != n.Process.PID {
			t.next[i] = head
			t.heads[parent] = i
			continue
		}
		t.roots = append(t.roots, i)
	}
	for _, i := range slices.Backward(t.roots) {
		h.walkSubtree(i, now)
	}
	clear(t.nodes)
	t.nodes, t.roots = t.nodes[:0], t.roots[:0]
	clear(t.heads)
}

func (h *history) walkSubtree(i int, now time.Time) (cpu, mb float64) {
	t := &h.tree
	n := t.nodes[i]
	cpu, mb = n.Process.CPU, float64(n.Process.RSSKiB)/kibPerMiB
	head := t.heads[link{session: n.SessionID, pid: n.Process.PID}]
	for k := head; k >= 0; k = t.next[k] {
		c, m := h.walkSubtree(k, now)
		cpu, mb = cpu+c, mb+m
	}
	key := subtreeKey(*n)
	if _, ok := h.series[key]; !ok && head >= 0 {
		h.series[key] = h.series[processKey(*n)].clone()
	}
	if _, ok := h.series[key]; ok {
		h.push(key, cpu, mb, now)
	}
	return cpu, mb
}

func (h *history) push(key seriesKey, cpu, memMB float64, now time.Time) {
	s, ok := h.series[key]
	if !ok {
		s = &series{}
		h.series[key] = s
	}
	for range max(h.steps, 1) {
		s.cpu.push(math.Round(cpu))
		s.mem.push(math.Round(memMB*10) / 10)
	}
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

func (s *series) clone() *series {
	if s == nil {
		return &series{}
	}
	return &series{cpu: s.cpu.clone(), mem: s.mem.clone(), seen: s.seen}
}

func (r ring) clone() ring {
	return ring{values: slices.Clone(r.values), head: r.head}
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

func subtreeKey(n pluginsdk.ProcessNode) seriesKey {
	return seriesKey{kind: kindSubtree, pid: n.Process.PID, start: n.Process.StartTime.UnixNano()}
}

func sessionKey(id string) seriesKey {
	return seriesKey{kind: kindSession, id: id}
}

func workspaceKey(id string) seriesKey {
	return seriesKey{kind: kindWorkspace, id: id}
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
