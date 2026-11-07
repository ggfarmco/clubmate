package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"clubmate/pkg/id"
	"clubmate/pkg/pluginsdk"
)

const (
	mutateTimeout = 10 * time.Minute
	cloneTimeout  = 30 * time.Minute
	opPollWait    = time.Second
	keptOps       = 64
	opPrefix      = "wt"
	outcomeTopic  = "git.outcome"
)

var (
	errOpBusy    = errors.New("another git operation is running")
	errUnknownOp = errors.New("unknown operation")
)

type opFunc func(ctx context.Context, report *opReport) (string, error)

type prepareFunc func(ctx context.Context, h *pluginsdk.Host, workspaceID string, args worktreeArgs) (opFunc, error)

type opReport struct {
	notice string
	note   string
}

type operation struct {
	id        string
	workspace string
	done      chan struct{}
	path      string
	report    opReport
	err       error
}

type outcomeEvent struct {
	Workspace string  `json:"workspace"`
	ID        string  `json:"id,omitempty"`
	Message   string  `json:"message,omitempty"`
	Confirm   *prompt `json:"confirm,omitempty"`
}

type confirmError struct {
	err    error
	prompt prompt
}

type opTable struct {
	mu    sync.Mutex
	byID  map[string]*operation
	order []string
}

func (a *app) startOp(ctx context.Context, c call, prepare prepareFunc) (opStarted, error) {
	if !c.state.claim() {
		return opStarted{}, errOpBusy
	}
	run, err := prepare(ctx, c.h, c.workspaceID, c.args)
	if err != nil {
		c.state.release()
		return opStarted{}, err
	}
	return a.runOp(ctx, c, run, mutateTimeout, c.state.release), nil
}

func (a *app) startClone(ctx context.Context, c call) (opStarted, error) {
	run, err := prepareClone(ctx, c.h, c.workspaceID, c.args)
	if err != nil {
		return opStarted{}, err
	}
	return a.runOp(ctx, c, run, cloneTimeout, func() {}), nil
}

func (a *app) runOp(ctx context.Context, c call, run opFunc, limit time.Duration, release func()) opStarted {
	o := a.ops.add(c.workspaceID)
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), limit)
		defer cancel()
		path, err := run(ctx, &o.report)
		release()
		<-a.refreshIndex(c.h, c.workspaceID, c.state)
		if c.ui {
			_, perr := c.h.Publish(ctx, outcomeTopic, opOutcome(c.workspaceID, o.id, o.report.notice, err))
			logError("publish "+outcomeTopic, perr)
		}
		o.finish(path, err)
		a.schedule(c.h, c.workspaceID, c.state, 0)
	}()
	return opStarted{ID: o.id}
}

func (t *opTable) add(workspaceID string) *operation {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byID == nil {
		t.byID = map[string]*operation{}
	}
	opID := id.New(opPrefix)
	o := &operation{id: opID, workspace: workspaceID, done: make(chan struct{})}
	t.byID[opID] = o
	t.order = append(t.order, opID)
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

func (t *opTable) status(ctx context.Context, workspaceID, id string) (opStatus, error) {
	t.mu.Lock()
	o := t.byID[id]
	t.mu.Unlock()
	if o == nil || o.workspace != workspaceID {
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
	out := opStatus{Done: true, Path: o.path, Notice: o.report.notice, Note: o.report.note}
	if o.err != nil {
		out.Error = o.err.Error()
	}
	return out, nil
}

func (o *operation) finish(path string, err error) {
	o.path, o.err = path, err
	close(o.done)
}

func (e *confirmError) Error() string {
	return e.err.Error()
}

func (e *confirmError) Unwrap() error {
	return e.err
}

func opOutcome(workspaceID, opID, notice string, err error) outcomeEvent {
	out := outcomeEvent{Workspace: workspaceID, ID: opID, Message: notice}
	switch ce, ok := errors.AsType[*confirmError](err); {
	case ok:
		out.Message, out.Confirm = "", &ce.prompt
	case err != nil:
		out.Message = err.Error()
	}
	return out
}
