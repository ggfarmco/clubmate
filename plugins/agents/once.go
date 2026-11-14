package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

const (
	onceKey   = "once/"
	onceKeep  = 7 * 24 * time.Hour
	oncePrune = time.Hour
)

var (
	errOnceCutOff  = errors.New("the first try was cut off, so its outcome is unknown")
	errTurnChanged = errors.New("the session's turn changed since you saw it")

	refusals = map[string][]error{
		contract.ReasonAnswered: {errConnectStale, errLandStale},
		contract.ReasonStale:    {errConnectSeen, errArchiveSeen, errArchiveHeld, errNoTask, errNoSession, errTurnChanged},
		contract.ReasonUnknown:  {errOnceCutOff},
	}
)

type onceRecord struct {
	At     time.Time       `json:"at"`
	Done   bool            `json:"done"`
	Result json.RawMessage `json:"result,omitempty"`
}

type onceLock struct {
	sync.Mutex
	refs int
}

type onceLocks struct {
	mu     sync.Mutex
	keys   map[string]*onceLock
	pruned time.Time
}

func (l *onceLocks) lock(key string) func() {
	l.mu.Lock()
	if l.keys == nil {
		l.keys = map[string]*onceLock{}
	}
	k := l.keys[key]
	if k == nil {
		k = &onceLock{}
		l.keys[key] = k
	}
	k.refs++
	l.mu.Unlock()
	k.Lock()
	return func() {
		k.Unlock()
		l.mu.Lock()
		defer l.mu.Unlock()
		if k.refs--; k.refs == 0 {
			delete(l.keys, key)
		}
	}
}

func (l *onceLocks) pruneDue(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.pruned) < oncePrune {
		return false
	}
	l.pruned = now
	return true
}

func (a *app) once(ctx context.Context, cmd pluginsdk.CommandRun, key string, run func() (any, error)) (any, error) {
	if key == "" {
		return run()
	}
	return a.onceLocks.run(ctx, a.host, cmd.WorkspaceID, cmd.ID, key, time.Now(), run)
}

func (l *onceLocks) run(ctx context.Context, kv sessionStore, workspaceID, command, key string, now time.Time, run func() (any, error)) (any, error) {
	sum := sha256.Sum256([]byte(command + "\x00" + key))
	stored := onceKey + workspaceID + "/" + hex.EncodeToString(sum[:])
	defer l.lock(stored)()
	var rec onceRecord
	found, err := kv.StorageGet(ctx, stored, &rec)
	if err != nil {
		return nil, err
	}
	if found && now.Sub(rec.At) < onceKeep {
		if !rec.Done {
			return nil, errOnceCutOff
		}
		return rec.Result, nil
	}
	if err := kv.StorageSet(ctx, stored, onceRecord{At: now}); err != nil {
		return nil, err
	}
	out, err := run()
	if err != nil {
		return nil, errors.Join(err, kv.StorageDelete(ctx, stored))
	}
	result, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if err := kv.StorageSet(ctx, stored, onceRecord{At: now, Done: true, Result: result}); err != nil {
		return nil, err
	}
	if l.pruneDue(now) {
		pruneOnce(ctx, kv, now)
	}
	return out, nil
}

func pruneOnce(ctx context.Context, kv sessionStore, now time.Time) {
	keys, err := kv.StorageList(ctx, onceKey)
	if err != nil {
		return
	}
	for _, key := range keys {
		var rec onceRecord
		if found, err := kv.StorageGet(ctx, key, &rec); err == nil && found && now.Sub(rec.At) >= onceKeep {
			_ = kv.StorageDelete(ctx, key)
		}
	}
}
