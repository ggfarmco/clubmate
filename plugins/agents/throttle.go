package main

import (
	"sync"
	"time"
)

type clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) func() bool
}

type realClock struct{}

type throttle struct {
	mu      sync.Mutex
	clock   clock
	every   time.Duration
	last    time.Time
	pending func() bool
	stopped bool
	fn      func()
}

type ticker struct {
	mu      sync.Mutex
	clock   clock
	every   time.Duration
	stopAt  func() bool
	stopped bool
	fn      func()
}

func (realClock) Now() time.Time {
	return time.Now()
}

func (realClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

func newThrottle(c clock, every time.Duration, fn func()) *throttle {
	return &throttle{clock: c, every: every, fn: fn}
}

func (t *throttle) kick() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending != nil || t.stopped {
		return
	}
	t.pending = t.clock.AfterFunc(max(t.every-t.clock.Now().Sub(t.last), 0), t.fire)
}

func (t *throttle) fire() {
	t.mu.Lock()
	t.pending = nil
	t.last = t.clock.Now()
	stopped := t.stopped
	t.mu.Unlock()
	if !stopped {
		t.fn()
	}
}

func (t *throttle) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	if t.pending != nil {
		t.pending()
		t.pending = nil
	}
}

func newTicker(c clock, every time.Duration, fn func()) *ticker {
	return &ticker{clock: c, every: every, fn: fn}
}

func (t *ticker) arm() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopAt != nil || t.stopped {
		return
	}
	t.stopAt = t.clock.AfterFunc(t.every, t.fire)
}

func (t *ticker) fire() {
	t.mu.Lock()
	t.stopAt = nil
	stopped := t.stopped
	t.mu.Unlock()
	if !stopped {
		t.fn()
	}
}

func (t *ticker) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	if t.stopAt != nil {
		t.stopAt()
		t.stopAt = nil
	}
}
