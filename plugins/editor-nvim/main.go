package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"

	"clubmate/pkg/pluginsdk"
)

const (
	mainSlot     = "editor-nvim.main"
	statusSlot   = "editor-nvim.status"
	sessionTitle = "nvim"
	stateRunning = "running"
)

var errUnknownAction = errors.New("unknown action")

type app struct {
	slotMu   sync.Mutex
	mu       sync.Mutex
	editors  map[editorKey]*editor
	current  map[string]string
	projects map[string]pluginsdk.Project
	deleted  map[string]bool
	stopping map[editorKey]bool
	offerMu  sync.Mutex
	themeMu  sync.Mutex
	watchMu  sync.Mutex
	watches  map[string]projectWatch
	branchMu sync.Mutex
}

type projectEvent struct {
	ProjectID string `json:"projectId"`
}

type sessionExited struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Owner     string `json:"owner"`
	ExitCode  *int   `json:"exitCode"`
}

type projectPayload struct {
	Project pluginsdk.Project `json:"project"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

func main() {
	a := &app{
		editors:  map[editorKey]*editor{},
		current:  map[string]string{},
		projects: map[string]pluginsdk.Project{},
		deleted:  map[string]bool{},
		stopping: map[editorKey]bool{},
		watches:  map[string]projectWatch{},
	}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, FSChanged: a.fsChanged})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	sessions, err := h.ListSessions(ctx, "", true)
	if err != nil {
		return err
	}
	present, err := h.Presence(ctx)
	if err != nil {
		return err
	}
	for _, pattern := range []string{"ui.opened", "session.exited", "project.updated", "project.deleted"} {
		if _, err := h.Subscribe(ctx, pattern, ready.Seq); err != nil {
			return err
		}
	}
	var themeSeq int64
	if _, err := h.StorageGet(ctx, themeSeqKey, &themeSeq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, themeTopic, themeSeq); err != nil {
		return err
	}
	projects := present
	for _, s := range sessions {
		if s.State == stateRunning && s.Title == sessionTitle {
			projects = append(projects, s.ProjectID)
		}
	}
	slices.Sort(projects)
	for _, id := range slices.Compact(projects) {
		go a.ensureLogged(ctx, h, id)
	}
	return nil
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case "ui.opened":
		var opened projectEvent
		if err := json.Unmarshal(ev.Payload, &opened); err != nil {
			logError("ui.opened", err)
			return
		}
		go a.ensureLogged(ctx, h, opened.ProjectID)
	case "session.exited":
		var exited sessionExited
		if err := json.Unmarshal(ev.Payload, &exited); err != nil {
			logError("session.exited", err)
			return
		}
		if exited.Owner == h.PluginID() {
			a.exited(ctx, h, exited)
		}
	case "project.updated":
		var p projectPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			logError("project.updated", err)
			return
		}
		a.projectUpdated(ctx, h, p.Project)
	case "project.deleted":
		var p deletedPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			logError("project.deleted", err)
			return
		}
		a.drop(p.ID)
		a.unwatch(ctx, h, p.ID)
	case themeTopic:
		a.themeChanged(ev.Payload)
		if err := h.StorageSet(ctx, themeSeqKey, ev.Seq); err != nil {
			logError(themeTopic, err)
		}
	}
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	if ev.Action != "restart" {
		return nil, fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
	}
	e, err := a.active(ctx, h, ev.ProjectID)
	if e == nil || err != nil {
		return nil, err
	}
	return nil, a.restart(ctx, h, e, false, false)
}

func logError(what string, err error) {
	fmt.Fprintln(os.Stderr, "editor-nvim:", what+":", err)
}
