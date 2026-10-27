package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"clubmate/pkg/pluginsdk"
)

const (
	mainSlot     = "projects.main"
	createAction = "create"
	selectAction = "select"
)

var (
	errUnknownAction = errors.New("unknown action")
	errNoRepos       = errors.New("choose at least one folder")
)

type createParams struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
}

type app struct {
	renderMu sync.Mutex
	mu       sync.Mutex
	selected string
	created  int
	home     string
}

func main() {
	home, _ := os.UserHomeDir()
	a := &app{home: home}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, BusEvent: a.busEvent})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, "project.*", ready.Seq); err != nil {
		return err
	}
	return a.render(ctx, h)
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	if err := a.render(ctx, h); err != nil {
		fmt.Fprintf(os.Stderr, "projects: render after %s: %v\n", ev.Topic, err)
	}
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	var err error
	switch ev.Action {
	case selectAction:
		err = a.selectProject(ev)
	case openAction:
		err = a.openSelected(ctx, h)
	case deleteAction:
		err = a.deleteSelected(ctx, h)
	case createAction:
		err = a.createProject(ctx, h, ev)
	default:
		return nil, fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
	}
	if err != nil {
		return nil, err
	}
	return nil, a.render(ctx, h)
}

func (a *app) current() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.selected
}

func (a *app) openSelected(ctx context.Context, h *pluginsdk.Host) error {
	id := a.current()
	if id == "" {
		return nil
	}
	return h.OpenProject(ctx, id)
}

func (a *app) selectProject(ev pluginsdk.UIEvent) error {
	var id string
	if err := json.Unmarshal(ev.Value, &id); err != nil {
		return fmt.Errorf("select value: %w", err)
	}
	a.mu.Lock()
	a.selected = id
	a.mu.Unlock()
	return nil
}

func (a *app) deleteSelected(ctx context.Context, h *pluginsdk.Host) error {
	id := a.current()
	if id == "" {
		return nil
	}
	if err := h.DeleteProject(ctx, id); err != nil {
		return err
	}
	a.mu.Lock()
	a.selected = ""
	a.mu.Unlock()
	return nil
}

func (a *app) createProject(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) error {
	var f createParams
	if err := json.Unmarshal(ev.Value, &f); err != nil {
		return fmt.Errorf("create value: %w", err)
	}
	if len(f.Repos) == 0 {
		return errNoRepos
	}
	if f.Name == "" {
		f.Name = filepath.Base(f.Repos[0])
	}
	if _, err := h.CreateProject(ctx, f.Name, f.Repos); err != nil {
		return err
	}
	a.mu.Lock()
	a.created++
	a.mu.Unlock()
	return nil
}

func (a *app) render(ctx context.Context, h *pluginsdk.Host) error {
	a.renderMu.Lock()
	defer a.renderMu.Unlock()
	projects, err := h.ListProjects(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !slices.ContainsFunc(projects, func(p pluginsdk.Project) bool { return p.ID == a.selected }) {
		a.selected = ""
	}
	_, err = h.Render(ctx, "", mainSlot, mainView(projects, a.selected, a.home, fmt.Sprintf("create-%d", a.created)))
	return err
}
