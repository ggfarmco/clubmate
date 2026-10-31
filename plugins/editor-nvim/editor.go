package main

import (
	"maps"
	"slices"
	"strings"
	"sync"
)

type editorKey struct {
	workspace string
	root      string
}

type editor struct {
	id       string
	root     string
	life     sync.Mutex
	renderMu sync.Mutex
	mu       sync.Mutex
	session  string
	client   *client
	status   status
	pending  bool
}

func (a *app) editorAt(workspaceID, root string) *editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deleted[workspaceID] {
		return nil
	}
	return a.editorLocked(workspaceID, root)
}

func (a *app) editorLocked(workspaceID, root string) *editor {
	key := editorKey{workspace: workspaceID, root: root}
	e, ok := a.editors[key]
	if !ok {
		e = &editor{id: workspaceID, root: root}
		a.editors[key] = e
	}
	return e
}

func (a *app) lookup(workspaceID, root string) *editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.editors[editorKey{workspace: workspaceID, root: root}]
}

func (a *app) forget(e *editor) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := editorKey{workspace: e.id, root: e.root}
	if a.editors[key] == e {
		delete(a.editors, key)
	}
}

func (a *app) workspaceEditors(workspaceID string) []*editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*editor
	for k, e := range a.editors {
		if k.workspace == workspaceID {
			out = append(out, e)
		}
	}
	return out
}

func (a *app) connectedClients() []*client {
	a.mu.Lock()
	editors := slices.Collect(maps.Values(a.editors))
	a.mu.Unlock()
	var clients []*client
	for _, e := range editors {
		if c := e.connected(); c != nil {
			clients = append(clients, c)
		}
	}
	return clients
}

func (a *app) isCurrent(e *editor) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.deleted[e.id] && a.current[e.id] == e.root && a.editors[editorKey{workspace: e.id, root: e.root}] == e
}

func (a *app) drop(workspaceID string) {
	a.mu.Lock()
	var editors []*editor
	for k, e := range a.editors {
		if k.workspace == workspaceID {
			editors = append(editors, e)
			delete(a.editors, k)
		}
	}
	delete(a.current, workspaceID)
	delete(a.workspaces, workspaceID)
	for k := range a.stopping {
		if k.workspace == workspaceID {
			delete(a.stopping, k)
		}
	}
	a.deleted[workspaceID] = true
	a.mu.Unlock()
	for _, e := range editors {
		go func() {
			e.life.Lock()
			defer e.life.Unlock()
			if c := e.reset(); c != nil {
				c.close()
			}
		}()
	}
}

func (a *app) isDeleted(workspaceID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deleted[workspaceID]
}

func (a *app) setStopping(workspaceID, root string, on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := editorKey{workspace: workspaceID, root: root}
	if on {
		a.stopping[key] = true
	} else {
		delete(a.stopping, key)
	}
}

func (a *app) isStopping(workspaceID, root string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopping[editorKey{workspace: workspaceID, root: root}]
}

func (e *editor) connected() *client {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.client != nil && e.client.alive() {
		return e.client
	}
	return nil
}

func (e *editor) setSession(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.session = id
}

func (e *editor) sessionID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.session
}

func (e *editor) setClient(c *client) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.client = c
}

func (e *editor) reset() *client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.detachLocked()
}

func (e *editor) exited(sessionID string) (*client, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session != sessionID {
		return nil, false
	}
	return e.detachLocked(), true
}

func (e *editor) current() status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

func (e *editor) setOpenError(msg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status.OpenError = msg
}

func (e *editor) update(ev nvimEvent, branch string, refresh bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status.File, e.status.Mode, e.status.LSP, e.status.Light = ev.File, ev.Mode, ev.LSP, ev.Light
	if ev.Event == "BufEnter" {
		e.status.OpenError = ""
	}
	if refresh {
		e.status.Branch = branch
	}
}

func (e *editor) setBranch(file, branch string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.status.File != file || e.status.Branch == branch {
		return false
	}
	e.status.Branch = branch
	return true
}

func (e *editor) detachLocked() *client {
	c := e.client
	e.client, e.session, e.status = nil, "", status{}
	return c
}

func lockAll(editors []*editor) func() {
	slices.SortFunc(editors, func(x, y *editor) int { return strings.Compare(x.root, y.root) })
	for _, e := range editors {
		e.life.Lock()
	}
	return func() {
		for _, e := range editors {
			e.life.Unlock()
		}
	}
}
