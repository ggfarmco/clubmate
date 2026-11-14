package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

type app struct {
	mu         sync.Mutex
	workspaces map[string]*state
	ops        opTable
	meta       metaStore
	landMu     sync.Mutex
}

func main() {
	a := &app{workspaces: map[string]*state{}}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, Command: a.command, BusEvent: a.busEvent, FSChanged: a.fsChanged})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	present, err := h.Presence(ctx)
	if err != nil {
		return err
	}
	for _, pattern := range []string{"ui.*", "workspace.*"} {
		if _, err := h.Subscribe(ctx, pattern, ready.Seq); err != nil {
			return err
		}
	}
	for _, id := range present {
		a.open(ctx, h, id)
	}
	return nil
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case contract.TopicUIOpened, contract.TopicUIIdle:
		var p contract.UIPresence
		if !decodeEvent(ev, &p) {
			return
		}
		if ev.Topic == contract.TopicUIOpened {
			a.open(ctx, h, p.Workspace)
		} else {
			a.idle(ctx, h, p.Workspace)
		}
	case contract.TopicWorkspaceCreated, contract.TopicWorkspaceUpdated:
		var p contract.WorkspaceChanged
		if !decodeEvent(ev, &p) {
			return
		}
		a.workspaceChanged(ctx, h, p.Workspace, ev.Topic == contract.TopicWorkspaceUpdated)
	case contract.TopicWorkspaceDeleted:
		var p contract.WorkspaceDeleted
		if !decodeEvent(ev, &p) {
			return
		}
		a.drop(ctx, h, p.Workspace)
	}
}

func decodeEvent(ev pluginsdk.Event, v any) bool {
	if err := json.Unmarshal(ev.Payload, v); err != nil {
		logError(ev.Topic, err)
		return false
	}
	return true
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "git:", what+":", err)
	}
}
