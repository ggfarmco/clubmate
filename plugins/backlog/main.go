package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"clubmate/pkg/pluginsdk"
)

const changedTopic = "backlog.changed"

type liveness interface {
	invalidate(projectID string)
	invalidateAll()
	drop(projectID string)
	stale(ctx context.Context, projectID, owner string) bool
	ancestor(ctx context.Context, projectID, ancestor, owner string) bool
	title(ctx context.Context, projectID, id string) (string, bool)
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

type projectDeleted struct {
	ID string `json:"id"`
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
	if _, err = h.Subscribe(ctx, "project.*", ready.Seq); err != nil {
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
	if ev.Topic != "project.deleted" {
		return
	}
	var p projectDeleted
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		logError(ev.Topic, err)
		return
	}
	b.live.drop(p.ID)
	b.dropUI(p.ID)
	logError("drop "+p.ID, b.store.dropProject(ctx, p.ID))
}

func (b *backlog) agentsEvent(ctx context.Context, ev pluginsdk.Event) {
	if ev.Topic == "agents.ready" {
		b.live.invalidateAll()
		for _, id := range b.presentIDs() {
			b.render(ctx, id)
		}
		return
	}
	if ev.Topic != "agents.changed" && ev.Topic != "agents.turn" {
		return
	}
	var p struct {
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		logError(ev.Topic, err)
		return
	}
	if p.Workspace == "" {
		return
	}
	b.live.invalidate(p.Workspace)
	b.render(ctx, p.Workspace)
}

func (b *backlog) changed(ctx context.Context, projectID string, id int, kind string) {
	_, err := b.h.Publish(ctx, changedTopic, changedEvent{Workspace: projectID, ID: id, Kind: kind})
	logError("publish "+changedTopic, err)
	b.render(ctx, projectID)
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "backlog: %s: %v\n", what, err)
	}
}
