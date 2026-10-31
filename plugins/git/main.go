package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"clubmate/pkg/pluginsdk"
)

type uiPayload struct {
	ProjectID string `json:"projectId"`
}

type projectPayload struct {
	Project pluginsdk.Project `json:"project"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

type app struct {
	mu       sync.Mutex
	projects map[string]*state
	ops      opTable
}

func main() {
	a := &app{projects: map[string]*state{}}
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
	for _, pattern := range []string{"ui.*", "project.*"} {
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
	case "ui.opened", "ui.idle":
		var p uiPayload
		if !decodeEvent(ev, &p) {
			return
		}
		if ev.Topic == "ui.opened" {
			a.open(ctx, h, p.ProjectID)
		} else {
			a.idle(ctx, h, p.ProjectID)
		}
	case "project.created", "project.updated":
		var p projectPayload
		if !decodeEvent(ev, &p) {
			return
		}
		a.projectChanged(ctx, h, p.Project.ID, ev.Topic == "project.updated")
	case "project.deleted":
		var p deletedPayload
		if !decodeEvent(ev, &p) {
			return
		}
		a.drop(ctx, h, p.ID)
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
