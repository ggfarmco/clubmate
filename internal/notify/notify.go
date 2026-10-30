package notify

import (
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	Topic       = "notify.posted"
	DefaultKind = "info"
	MaxMessage  = 1000
	MaxTitle    = 120
	MaxSession  = 64
	ellipsis    = "…"
)

var Kinds = []string{"needs-input", "permission", "done", "stuck", DefaultKind}

func ValidKind(kind string) bool {
	return slices.Contains(Kinds, kind)
}

func Clip(s string, limit int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return strings.TrimSpace(string(runes[:limit-1])) + ellipsis
}

type Limiter struct {
	max    int
	window time.Duration
	mu     sync.Mutex
	seen   map[string][]time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, seen: map[string][]time.Time{}}
}

func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	recent := slices.DeleteFunc(l.seen[key], func(t time.Time) bool { return !t.After(cutoff) })
	if len(recent) >= l.max {
		l.seen[key] = recent
		return false
	}
	l.seen[key] = append(recent, now)
	return true
}
