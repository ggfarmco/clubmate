package main

import (
	"maps"
	"slices"
	"strings"
	"sync"
)

type editorKey struct {
	project string
	root    string
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

func (a *app) editorAt(projectID, root string) *editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deleted[projectID] {
		return nil
	}
	return a.editorLocked(projectID, root)
}

func (a *app) editorLocked(projectID, root string) *editor {
	key := editorKey{project: projectID, root: root}
	e, ok := a.editors[key]
	if !ok {
		e = &editor{id: projectID, root: root}
		a.editors[key] = e
	}
	return e
}

func (a *app) lookup(projectID, root string) *editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.editors[editorKey{project: projectID, root: root}]
}

func (a *app) forget(e *editor) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := editorKey{project: e.id, root: e.root}
	if a.editors[key] == e {
		delete(a.editors, key)
	}
}

func (a *app) projectEditors(projectID string) []*editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*editor
	for k, e := range a.editors {
		if k.project == projectID {
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
	return !a.deleted[e.id] && a.current[e.id] == e.root && a.editors[editorKey{project: e.id, root: e.root}] == e
}

func (a *app) drop(projectID string) {
	a.mu.Lock()
	var editors []*editor
	for k, e := range a.editors {
		if k.project == projectID {
			editors = append(editors, e)
			delete(a.editors, k)
		}
	}
	delete(a.current, projectID)
	delete(a.projects, projectID)
	for k := range a.stopping {
		if k.project == projectID {
			delete(a.stopping, k)
		}
	}
	a.deleted[projectID] = true
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

func (a *app) isDeleted(projectID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deleted[projectID]
}

func (a *app) setStopping(projectID, root string, on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := editorKey{project: projectID, root: root}
	if on {
		a.stopping[key] = true
	} else {
		delete(a.stopping, key)
	}
}

func (a *app) isStopping(projectID, root string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopping[editorKey{project: projectID, root: root}]
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
