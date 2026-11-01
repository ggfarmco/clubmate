package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/fileindex"
)

const (
	changedTopic = "docs.changed"
	openedTopic  = "docs.opened"
)

type docs struct {
	h       *pluginsdk.Host
	clock   clock
	uploads string
	index   fileindex.Index
	mu      sync.Mutex
	spaces  map[string]*space
}

type openArgs struct {
	Path    string `json:"path"`
	Session string `json:"session"`
}

type readArgs struct {
	Path string `json:"path"`
}

type openResult struct {
	Path string `json:"path"`
}

type readResult struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	HTML  string `json:"html"`
	Rev   int    `json:"rev"`
	Kind  string `json:"kind"`
}

type changedEvent struct {
	Workspace string `json:"workspace"`
	Path      string `json:"path"`
	Rev       int    `json:"rev"`
}

type openedEvent struct {
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
	Path      string `json:"path"`
}

func newDocs(c clock, uploads string) *docs {
	return &docs{clock: c, uploads: uploads, index: fileindex.New(), spaces: map[string]*space{}}
}

func main() {
	var uploads string
	if dir := pluginsdk.StateDir(); dir != "" {
		uploads = filepath.Join(filepath.Dir(dir), "agents", "uploads")
	}
	d := newDocs(realClock{}, uploads)
	pluginsdk.Main(pluginsdk.Plugin{Init: d.init, Command: d.command, UIEvent: d.uiEvent, BusEvent: d.busEvent, FSChanged: d.fsChanged})
}

func (d *docs) init(ctx context.Context, h *pluginsdk.Host) error {
	d.h = h
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	_, err = h.Subscribe(ctx, contract.TopicWorkspaceDeleted, ready.Seq)
	return err
}

func (d *docs) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	switch cmd.ID {
	case "docs.open":
		var args openArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return d.open(ctx, h, cmd, args)
	case "docs.read":
		var args readArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return d.read(ctx, h, cmd.WorkspaceID, args.Path)
	case "docs.list":
		var args listArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return d.list(ctx, h, cmd.WorkspaceID, args)
	case "docs.close":
		return nil, d.close(ctx, h, cmd.WorkspaceID)
	}
	return nil, fmt.Errorf("unknown command %q", cmd.ID)
}

func (d *docs) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	if ev.Topic != contract.TopicWorkspaceDeleted {
		return
	}
	var p contract.WorkspaceDeleted
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		logError(ev.Topic, err)
		return
	}
	d.mu.Lock()
	s := d.spaces[p.Workspace]
	delete(d.spaces, p.Workspace)
	d.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	logError("unwatch "+p.Workspace, d.unwatch(ctx, h, s))
}

func (d *docs) open(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun, args openArgs) (any, error) {
	ws, err := h.GetWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return nil, err
	}
	session := cmp.Or(args.Session, cmd.Caller)
	allow := workspaceScope(ctx, ws, d.uploads)
	path, anchor := splitAnchor(args.Path)
	abs, src, err := load(d.absolute(ctx, h, ws, session, path), allow)
	if err != nil {
		return nil, err
	}
	s := d.space(ws.ID)
	s.mu.Lock()
	s.stack = []entry{{path: abs, anchor: anchor}}
	s.prune()
	err = d.ensureWatch(ctx, h, ws.ID, s)
	if err == nil {
		err = show(ctx, h, ws.ID, s.viewOf(src, nil, allow))
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	logError("focus "+ws.ID, h.Focus(ctx, ws.ID, "docs", popupSlot))
	if cmd.Agent {
		_, err := h.Publish(ctx, openedTopic, openedEvent{Workspace: ws.ID, Session: session, Path: abs})
		logError("publish "+openedTopic, err)
	}
	return openResult{Path: abs}, nil
}

func (d *docs) read(ctx context.Context, h *pluginsdk.Host, workspaceID, path string) (any, error) {
	ws, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	allow := workspaceScope(ctx, ws, d.uploads)
	abs, src, err := load(d.absolute(ctx, h, ws, "", path), allow)
	if err != nil {
		return nil, err
	}
	doc, kind := render(src, abs, allow)
	s := d.space(ws.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.read(abs)
	st := s.track(abs)
	if err := d.ensureWatch(ctx, h, ws.ID, s); err != nil {
		return nil, err
	}
	return readResult{Path: abs, Title: doc.Title, HTML: doc.HTML, Rev: st.rev, Kind: kind}, nil
}

func (d *docs) absolute(ctx context.Context, h *pluginsdk.Host, ws contract.Workspace, session, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if session != "" {
		if sessions, err := agentcall.Sessions(ctx, h, ws.ID); err == nil {
			if s, ok := agentcall.Find(sessions, session); ok && s.Worktree != "" {
				return filepath.Join(s.Worktree, path)
			}
		}
	}
	if len(ws.Folders) == 0 {
		return path
	}
	return filepath.Join(ws.Folders[0].Root(), path)
}

func (d *docs) space(workspaceID string) *space {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.spaces[workspaceID]
	if !ok {
		s = &space{docs: map[string]*docState{}, pending: map[string]bool{}}
		d.spaces[workspaceID] = s
	}
	return s
}

func (d *docs) lookup(workspaceID string) *space {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.spaces[workspaceID]
}

func eventPath(ev pluginsdk.UIEvent) (string, error) {
	var path string
	if err := json.Unmarshal(ev.Value, &path); err != nil {
		return "", fmt.Errorf("%s value: %w", ev.Action, err)
	}
	return path, nil
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "docs: %s: %v\n", what, err)
	}
}
