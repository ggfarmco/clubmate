package graphs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	gateCheckPrefix = "gate-check/"
	maxGateChecks   = 50
)

type gateChecks struct {
	Pin     string           `json:"pin"`
	Entries []gateCheckEntry `json:"entries,omitempty"`
}

type gateCheckEntry struct {
	Key string    `json:"key"`
	At  time.Time `json:"at"`
}

func cacheable(r *Run, n *Node) bool {
	return r.Gate && r.GateID != "" && r.GatePin != "" && n.Run != "" && len(n.Decide) > 0
}

func gateCheckKey(r *Run, n *Node, tree string) string {
	sum := sha256.Sum256([]byte(r.GateTest))
	return n.ID + "/" + hex.EncodeToString(sum[:]) + "/" + tree
}

func (a *app) gateChecksOf(ctx context.Context, r *Run) gateChecks {
	var c gateChecks
	found, err := a.kvGet(ctx, gateCheckPrefix+r.GateID, &c)
	logError(err)
	if !found || c.Pin != r.GatePin {
		return gateChecks{Pin: r.GatePin}
	}
	return c
}

func (a *app) cachedPass(ctx context.Context, r *Run, n *Node) (time.Time, bool) {
	if !cacheable(r, n) || r.GateTree == "" {
		return time.Time{}, false
	}
	a.checkMu.Lock()
	defer a.checkMu.Unlock()
	key := gateCheckKey(r, n, r.GateTree)
	c := a.gateChecksOf(ctx, r)
	i := slices.IndexFunc(c.Entries, func(e gateCheckEntry) bool { return e.Key == key })
	if i < 0 {
		return time.Time{}, false
	}
	return c.Entries[i].At, true
}

func (a *app) storePass(ctx context.Context, r *Run, n *Node, tree string) {
	if !cacheable(r, n) {
		return
	}
	a.checkMu.Lock()
	defer a.checkMu.Unlock()
	key := gateCheckKey(r, n, tree)
	c := a.gateChecksOf(ctx, r)
	c.Entries = slices.DeleteFunc(c.Entries, func(e gateCheckEntry) bool { return e.Key == key })
	c.Entries = append(c.Entries, gateCheckEntry{Key: key, At: a.now()})
	if len(c.Entries) > maxGateChecks {
		c.Entries = c.Entries[len(c.Entries)-maxGateChecks:]
	}
	logError(a.kvSet(ctx, gateCheckPrefix+r.GateID, c))
}

func (e *Engine) DropGateChecks(ctx context.Context, gate string) error {
	a := e.a
	a.checkMu.Lock()
	defer a.checkMu.Unlock()
	return a.kvSet(ctx, gateCheckPrefix+gate, gateChecks{})
}

func (a *app) testLock(ctx context.Context, r *Run, n *Node, spec stepSpec, note func(string)) (func(), bool) {
	if !r.Gate || !strings.Contains(n.Run, gateTestRun) {
		return func() {}, true
	}
	key := spec.Main
	if key == "" {
		key = spec.Dir
	}
	if real, err := filepath.EvalSymlinks(key); err == nil {
		key = real
	}
	a.mu.Lock()
	if a.testLocks == nil {
		a.testLocks = map[string]chan struct{}{}
	}
	ch := a.testLocks[key]
	if ch == nil {
		ch = make(chan struct{}, 1)
		a.testLocks[key] = ch
	}
	a.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, true
	default:
	}
	note(noteTestWait)
	select {
	case ch <- struct{}{}:
		note("")
		return func() { <-ch }, true
	case <-ctx.Done():
		return nil, false
	}
}
