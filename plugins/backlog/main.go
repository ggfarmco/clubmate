package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

const changedTopic = "backlog.changed"

type liveness interface {
	invalidate(workspaceID string)
	invalidateAll()
	drop(workspaceID string)
	stale(ctx context.Context, workspaceID, owner string) bool
	ancestor(ctx context.Context, workspaceID, ancestor, owner string) bool
	session(ctx context.Context, workspaceID, id string) (agentSession, bool)
}

type backlog struct {
	store   *store
	live    liveness
	h       *pluginsdk.Host
	mu      sync.Mutex
	ui      map[string]*workspaceUI
	present map[string]bool
}

type changedEvent struct {
	Workspace string `json:"workspace"`
	ID        int    `json:"id"`
	Kind      string `json:"kind"`
}

func main() {
	b := &backlog{store: &store{}, ui: map[string]*workspaceUI{}, present: map[string]bool{}}
	pluginsdk.Main(pluginsdk.Plugin{Init: b.init, Command: b.command, BusEvent: b.busEvent, UIEvent: b.uiEvent})
}

func (b *backlog) init(ctx context.Context, h *pluginsdk.Host) error {
	b.h = h
	b.store.h = h
	b.live = &sessions{h: h}
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	if _, err = h.Subscribe(ctx, "workspace.*", ready.Seq); err != nil {
		return err
	}
	if _, err = h.Subscribe(ctx, "agents.*", ready.Seq); err != nil {
		return err
	}
	if _, err = h.Subscribe(ctx, "ui.*", ready.Seq); err != nil {
		return err
	}
	return b.loadPresence(ctx)
}

func (b *backlog) busEvent(ctx context.Context, _ *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	if strings.HasPrefix(ev.Topic, "agents.") {
		b.agentsEvent(ctx, ev)
		return
	}
	if strings.HasPrefix(ev.Topic, "ui.") {
		b.presenceEvent(ctx, ev)
		return
	}
	if ev.Topic != contract.TopicWorkspaceDeleted {
		return
	}
	var p contract.WorkspaceDeleted
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		logError(ev.Topic, err)
		return
	}
	b.live.drop(p.Workspace)
	b.dropUI(p.Workspace)
	logError("drop "+p.Workspace, b.store.dropWorkspace(ctx, p.Workspace))
}

func (b *backlog) agentsEvent(ctx context.Context, ev pluginsdk.Event) {
	if ev.Topic == api.TopicReady {
		b.live.invalidateAll()
		for _, id := range b.presentIDs() {
			b.render(ctx, id)
		}
		return
	}
	if ev.Topic != api.TopicChanged && ev.Topic != api.TopicTurn {
		return
	}
	var p api.Changed
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		logError(ev.Topic, err)
		return
	}
	if p.Workspace == "" {
		return
	}
	b.live.invalidate(p.Workspace)
	switch {
	case ev.Topic != api.TopicChanged || p.Session == "":
	case p.Status == api.StatusClosed || p.Kind == api.ChangeDeleted:
		b.sessionStopped(ctx, p.Workspace, p.Session)
	case p.Kind == api.ChangeStatus:
		b.sessionStatus(ctx, p.Workspace, p.Session, p.Status)
	}
	b.render(ctx, p.Workspace)
}

func (b *backlog) changed(ctx context.Context, workspaceID string, id int, kind string) {
	_, err := b.h.Publish(ctx, changedTopic, changedEvent{Workspace: workspaceID, ID: id, Kind: kind})
	logError("publish "+changedTopic, err)
	b.render(ctx, workspaceID)
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "backlog: %s: %v\n", what, err)
	}
}
