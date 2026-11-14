package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

const (
	requestPrefix   = "req/"
	requestKeep     = 7 * 24 * time.Hour
	requestPruneGap = 6 * time.Hour
	maxRequestID    = 64
	requestRunning  = "running"
	requestDone     = "done"
)

type requestRecord struct {
	State  string          `json:"state"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *apiError       `json:"error,omitempty"`
	At     time.Time       `json:"at"`
}

type requestRun struct {
	done   chan struct{}
	result json.RawMessage
	err    error
}

type requestLog struct {
	kv      kvStore
	now     func() time.Time
	mu      sync.Mutex
	running map[string]*requestRun
}

func newRequestLog(kv kvStore, now func() time.Time) *requestLog {
	return &requestLog{kv: kv, now: now, running: map[string]*requestRun{}}
}

func requestKey(id string) string {
	return requestPrefix + id
}

func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestID {
		return false
	}
	return !strings.ContainsFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	})
}

func (l *requestLog) do(ctx context.Context, id string, run func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	r, rec, err := l.claim(ctx, id, run)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return rec.outcome()
	}
	select {
	case <-r.done:
		return r.result, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *requestLog) claim(ctx context.Context, id string, run func(context.Context) (json.RawMessage, error)) (*requestRun, requestRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if r, ok := l.running[id]; ok {
		return r, requestRecord{}, nil
	}
	var rec requestRecord
	found, err := l.kv.StorageGet(ctx, requestKey(id), &rec)
	if err != nil || found {
		return nil, rec, err
	}
	if err := l.kv.StorageSet(ctx, requestKey(id), requestRecord{State: requestRunning, At: l.now()}); err != nil {
		return nil, rec, err
	}
	r := &requestRun{done: make(chan struct{})}
	l.running[id] = r
	go l.finish(context.WithoutCancel(ctx), id, r, run)
	return r, rec, nil
}

func (rec requestRecord) outcome() (json.RawMessage, error) {
	switch {
	case rec.State != requestDone:
		return nil, &apiError{Code: contract.ReasonUnknown, Message: "the Mac stopped while it ran this, so its outcome is unknown"}
	case rec.Error != nil:
		return nil, rec.Error
	case len(rec.Result) == 0:
		return json.RawMessage("null"), nil
	}
	return rec.Result, nil
}

func (l *requestLog) finish(ctx context.Context, id string, r *requestRun, run func(context.Context) (json.RawMessage, error)) {
	runCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	r.result, r.err = run(runCtx)
	cancel()
	if r.err != nil {
		if stored, ok := storedError(r.err); ok {
			r.err = stored
		}
	}
	l.record(ctx, id, r)
	l.mu.Lock()
	delete(l.running, id)
	l.mu.Unlock()
	close(r.done)
}

func (l *requestLog) record(ctx context.Context, id string, r *requestRun) {
	key := requestKey(id)
	rec := requestRecord{State: requestDone, Result: r.result, At: l.now()}
	if r.err != nil {
		api := toAPIError(r.err)
		if api.Code == codeFailed {
			if err := l.kv.StorageDelete(ctx, key); err != nil {
				logf("forget request %s: %v", id, err)
			}
			return
		}
		rec.Error = &api
	}
	if l.kv.StorageSet(ctx, key, rec) == nil {
		return
	}
	rec.Result = nil
	if err := l.kv.StorageSet(ctx, key, rec); err != nil {
		logf("keep request %s: %v", id, err)
	}
}

func (l *requestLog) prune(ctx context.Context) error {
	keys, err := l.kv.StorageList(ctx, requestPrefix)
	if err != nil {
		return err
	}
	cutoff := l.now().Add(-requestKeep)
	for _, key := range keys {
		var rec requestRecord
		found, err := l.kv.StorageGet(ctx, key, &rec)
		if err != nil {
			return err
		}
		if found && rec.At.Before(cutoff) {
			if err := l.kv.StorageDelete(ctx, key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *requestLog) pruneLoop(ctx context.Context) {
	ticker := time.NewTicker(requestPruneGap)
	defer ticker.Stop()
	for {
		if err := l.prune(ctx); err != nil {
			logf("prune requests: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func storedError(err error) (*apiError, bool) {
	if outcomeUnknown(err) {
		return &apiError{Code: contract.ReasonUnknown, Message: "Clubmate stopped waiting for it after it started, so it may have run"}, true
	}
	api := toAPIError(err)
	return &api, api.Code != codeFailed
}

func outcomeUnknown(err error) bool {
	if pe, ok := pluginsdk.PluginErrorOf(err); ok {
		return contextMessage(pe.Message)
	}
	if _, ok := errors.AsType[*jsonrpc2.Error](err); ok {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func pluginRefusal(err error) bool {
	pe, ok := pluginsdk.PluginErrorOf(err)
	return ok && !contextMessage(pe.Message)
}

func contextMessage(message string) bool {
	return strings.HasSuffix(message, context.DeadlineExceeded.Error()) || strings.HasSuffix(message, context.Canceled.Error())
}
