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
)

const (
	onceKey  = "once/"
	onceKeep = 7 * 24 * time.Hour
)

var (
	errOnceCutOff = errors.New("the first try was cut off, so its outcome is unknown")

	refusals = map[string][]error{
		contract.ReasonAnswered: {errConnectStale, errLandStale},
		contract.ReasonStale:    {errConnectSeen, errArchiveSeen, errArchiveHeld, errNoTask, errNoSession},
		contract.ReasonUnknown:  {errOnceCutOff},
	}
)

type onceRecord struct {
	At     time.Time       `json:"at"`
	Done   bool            `json:"done"`
	Result json.RawMessage `json:"result,omitempty"`
}

type onceLocks struct {
	mu   sync.Mutex
	keys map[string]*sync.Mutex
}

func (l *onceLocks) of(key string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.keys == nil {
		l.keys = map[string]*sync.Mutex{}
	}
	if l.keys[key] == nil {
		l.keys[key] = &sync.Mutex{}
	}
	return l.keys[key]
}

func (a *app) once(ctx context.Context, workspaceID, key string, run func() (any, error)) (any, error) {
	if key == "" {
		return run()
	}
	return a.onceLocks.run(ctx, a.host, workspaceID, key, time.Now(), run)
}

func (l *onceLocks) run(ctx context.Context, kv sessionStore, workspaceID, key string, now time.Time, run func() (any, error)) (any, error) {
	sum := sha256.Sum256([]byte(key))
	stored := onceKey + workspaceID + "/" + hex.EncodeToString(sum[:])
	lock := l.of(stored)
	lock.Lock()
	defer lock.Unlock()
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
	pruneOnce(ctx, kv, now)
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
