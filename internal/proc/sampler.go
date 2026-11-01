package proc

import (
	"sync"
	"time"
)

const minSampleGap = 250 * time.Millisecond

type sampleKey struct {
	pid   int
	start int64
}

type cpuSample struct {
	cpuNs uint64
	at    time.Time
	cpu   float64
}

type Sampler struct {
	mu   sync.Mutex
	last map[sampleKey]cpuSample
}

func NewSampler() *Sampler {
	return &Sampler{last: make(map[sampleKey]cpuSample)}
}

func (s *Sampler) Sample(procs []Process) []Process {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	next := make(map[sampleKey]cpuSample, len(procs))
	out := make([]Process, 0, len(procs))
	for _, p := range procs {
		rss, cpuNs, ok := taskUsage(p.PID)
		if !ok {
			continue
		}
		key := sampleKey{pid: p.PID, start: p.StartTime.UnixNano()}
		cur := s.cpuOf(key, p.StartTime, cpuNs, now)
		next[key] = cur
		p.RSSKiB, p.CPU = int64(rss/1024), cur.cpu
		out = append(out, p)
	}
	s.last = next
	return out
}

func (s *Sampler) cpuOf(key sampleKey, start time.Time, cpuNs uint64, now time.Time) cpuSample {
	prev, seen := s.last[key]
	if !seen {
		return cpuSample{cpuNs: cpuNs, at: now, cpu: percent(cpuNs, now.Sub(start))}
	}
	wall := now.Sub(prev.at)
	if wall < minSampleGap {
		return prev
	}
	if cpuNs < prev.cpuNs {
		return cpuSample{cpuNs: cpuNs, at: now}
	}
	return cpuSample{cpuNs: cpuNs, at: now, cpu: percent(cpuNs-prev.cpuNs, wall)}
}

func percent(cpuNs uint64, wall time.Duration) float64 {
	if wall <= 0 {
		return 0
	}
	return float64(cpuNs) / float64(wall.Nanoseconds()) * 100
}
