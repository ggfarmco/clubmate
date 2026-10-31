package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	mutateTimeout = 10 * time.Minute
	opPollWait    = time.Second
	keptOps       = 64
	opPrefix      = "wt-"
)

var (
	errOpBusy    = errors.New("another git operation is running")
	errUnknownOp = errors.New("unknown operation")
)

type opFunc func(ctx context.Context, note func(string)) (string, error)

type prepareFunc func(ctx context.Context, h *pluginsdk.Host, projectID string, args worktreeArgs) (opFunc, error)

type operation struct {
	id      string
	project string
	done    chan struct{}
	path    string
	notice  string
	err     error
}

type opTable struct {
	mu    sync.Mutex
	seq   int
	byID  map[string]*operation
	order []string
}

func (a *app) startOp(ctx context.Context, c call, prepare prepareFunc) (opStarted, error) {
	if !c.state.claim() {
		return opStarted{}, errOpBusy
	}
	run, err := prepare(ctx, c.h, c.projectID, c.args)
	if err != nil {
		c.state.release()
		return opStarted{}, err
	}
	o := a.ops.add(c.projectID)
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mutateTimeout)
		defer cancel()
		path, err := run(ctx, func(text string) { o.notice = text })
		c.state.release()
		o.finish(path, err)
		a.schedule(c.h, c.projectID, c.state, 0)
	}()
	return opStarted{ID: o.id}, nil
}

func (t *opTable) add(projectID string) *operation {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byID == nil {
		t.byID = map[string]*operation{}
	}
	t.seq++
	id := opPrefix + strconv.Itoa(t.seq)
	o := &operation{id: id, project: projectID, done: make(chan struct{})}
	t.byID[id] = o
	t.order = append(t.order, id)
	for i := 0; len(t.order) > keptOps && i < len(t.order); {
		old := t.byID[t.order[i]]
		select {
		case <-old.done:
			delete(t.byID, t.order[i])
			t.order = append(t.order[:i], t.order[i+1:]...)
		default:
			i++
		}
	}
	return o
}

func (t *opTable) status(ctx context.Context, projectID, id string) (opStatus, error) {
	t.mu.Lock()
	o := t.byID[id]
	t.mu.Unlock()
	if o == nil || o.project != projectID {
		return opStatus{}, fmt.Errorf("%w: %q", errUnknownOp, id)
	}
	timer := time.NewTimer(opPollWait)
	defer timer.Stop()
	select {
	case <-o.done:
	case <-timer.C:
		return opStatus{}, nil
	case <-ctx.Done():
		return opStatus{}, nil
	}
	out := opStatus{Done: true, Path: o.path, Notice: o.notice}
	if o.err != nil {
		out.Error = o.err.Error()
	}
	return out, nil
}

func (o *operation) finish(path string, err error) {
	o.path, o.err = path, err
	close(o.done)
}
