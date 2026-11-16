package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

const changedTopic = "wiki.changed"

type wiki struct {
	h       *pluginsdk.Host
	now     func() time.Time
	mu      sync.Mutex
	ui      map[string]*workspaceUI
	present map[string]bool
	sigs    map[string]string
}

type changedEvent struct {
	Workspace string `json:"workspace"`
	Page      string `json:"page"`
}

func newWiki() *wiki {
	return &wiki{now: time.Now, ui: map[string]*workspaceUI{}, present: map[string]bool{}, sigs: map[string]string{}}
}

func main() {
	w := newWiki()
	pluginsdk.Main(pluginsdk.Plugin{Init: w.init, Command: w.command, UIEvent: w.uiEvent, BusEvent: w.busEvent})
}

func (w *wiki) init(ctx context.Context, h *pluginsdk.Host) error {
	w.h = h
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, contract.TopicWorkspaceDeleted, ready.Seq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, "ui.*", ready.Seq); err != nil {
		return err
	}
	go w.poll(context.Background())
	return w.loadPresence(ctx)
}

func (w *wiki) busEvent(ctx context.Context, _ *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	if strings.HasPrefix(ev.Topic, "ui.") {
		w.presenceEvent(ctx, ev)
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
	w.mu.Lock()
	delete(w.ui, p.Workspace)
	delete(w.present, p.Workspace)
	w.mu.Unlock()
}

func (w *wiki) changed(ctx context.Context, workspaceID, page string) {
	_, err := w.h.Publish(ctx, changedTopic, changedEvent{Workspace: workspaceID, Page: page})
	logError("publish "+changedTopic, err)
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "wiki: %s: %v\n", what, err)
	}
}
