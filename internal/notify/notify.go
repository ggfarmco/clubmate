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
	perKey int
	total  int
	window time.Duration
	mu     sync.Mutex
	seen   map[string][]time.Time
	all    []time.Time
}

func NewLimiter(perKey, total int, window time.Duration) *Limiter {
	return &Limiter{perKey: perKey, total: total, window: window, seen: map[string][]time.Time{}}
}

func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	expired := func(t time.Time) bool { return !t.After(cutoff) }
	l.all = slices.DeleteFunc(l.all, expired)
	recent := slices.DeleteFunc(l.seen[key], expired)
	if len(recent) >= l.perKey || len(l.all) >= l.total {
		if len(recent) == 0 {
			delete(l.seen, key)
		} else {
			l.seen[key] = recent
		}
		return false
	}
	l.seen[key] = append(recent, now)
	l.all = append(l.all, now)
	return true
}
