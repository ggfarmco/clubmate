package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"clubmate/pkg/pluginsdk"
)

const (
	sidebarSlot  = "terminals.sidebar"
	mainSlot     = "terminals.main"
	titlePrefix  = "Terminal "
	defaultShell = "/bin/zsh"
)

var errUnknownAction = errors.New("unknown action")

type projectEvent struct {
	Project pluginsdk.Project `json:"project"`
}

type sessionEvent struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Owner     string `json:"owner"`
}

type closeArgs struct {
	SessionID string `json:"sessionId"`
}

type newArgs struct {
	Cwd string `json:"cwd"`
}

type app struct {
	mu    sync.Mutex
	known map[string]string
}

func main() {
	a := &app{known: map[string]string{}}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	ready, err := h.Publish(ctx, "terminals.ready", nil)
	if err != nil {
		return err
	}
	projects, err := h.ListProjects(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range projects {
		if err := a.render(ctx, h, p.ID); err != nil {
			return err
		}
	}
	if _, err := h.Subscribe(ctx, "project.*", ready.Seq); err != nil {
		return err
	}
	_, err = h.Subscribe(ctx, "session.*", ready.Seq)
	return err
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.act(ctx, h, ev); err != nil {
		return nil, err
	}
	return nil, a.render(ctx, h, ev.ProjectID)
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch cmd.ID {
	case "terminals.new":
		var args newArgs
		if len(cmd.Args) > 0 {
			if err := json.Unmarshal(cmd.Args, &args); err != nil {
				return nil, fmt.Errorf("terminals.new args: %w", err)
			}
		}
		if err := a.create(ctx, h, cmd.ProjectID, args.Cwd); err != nil {
			return nil, err
		}
	case "terminals.close":
		var args closeArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("terminals.close args: %w", err)
		}
		if err := a.closeSession(ctx, h, cmd.ProjectID, args.SessionID); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: command %q", errUnknownAction, cmd.ID)
	}
	return nil, a.render(ctx, h, cmd.ProjectID)
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	projectID, err := a.eventProject(ev)
	if err != nil {
		fmt.Fprintln(os.Stderr, "terminals:", ev.Topic, err)
		return
	}
	if projectID == "" {
		return
	}
	if err := a.render(ctx, h, projectID); err != nil {
		fmt.Fprintln(os.Stderr, "terminals: render:", err)
	}
}

func (a *app) eventProject(ev pluginsdk.Event) (string, error) {
	switch ev.Topic {
	case "project.created":
		var created projectEvent
		err := json.Unmarshal(ev.Payload, &created)
		return created.Project.ID, err
	case "session.exited":
		var exited sessionEvent
		if err := json.Unmarshal(ev.Payload, &exited); err != nil {
			return "", err
		}
		if exited.Owner != "terminals" {
			return "", nil
		}
		return exited.ProjectID, nil
	case "session.killed", "session.removed":
		var gone sessionEvent
		err := json.Unmarshal(ev.Payload, &gone)
		return a.known[gone.ID], err
	}
	return "", nil
}

func (a *app) act(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) error {
	switch ev.Action {
	case "new":
		return a.create(ctx, h, ev.ProjectID, "")
	case "select":
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("select value: %w", err)
		}
		return h.StorageSet(ctx, selectedKey(ev.ProjectID), id)
	case "restart":
		return a.restart(ctx, h, ev.ProjectID)
	case "close":
		selected, err := a.selected(ctx, h, ev.ProjectID)
		if err != nil {
			return err
		}
		return a.closeSession(ctx, h, ev.ProjectID, selected)
	}
	return fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
}

func (a *app) create(ctx context.Context, h *pluginsdk.Host, projectID, cwd string) error {
	sessions, err := h.ListSessions(ctx, projectID, true)
	if err != nil {
		return err
	}
	return a.spawn(ctx, h, projectID, titlePrefix+strconv.Itoa(maxNumber(sessions)+1), cwd)
}

func (a *app) restart(ctx context.Context, h *pluginsdk.Host, projectID string) error {
	selected, err := a.selected(ctx, h, projectID)
	if err != nil {
		return err
	}
	sessions, err := h.ListSessions(ctx, projectID, true)
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(sessions, func(s pluginsdk.Session) bool { return s.ID == selected })
	if idx < 0 || sessions[idx].State == "running" {
		return nil
	}
	if err := a.spawn(ctx, h, projectID, sessions[idx].Title, sessions[idx].Cwd); err != nil {
		return err
	}
	return a.removeSession(ctx, h, sessions[idx])
}

func (a *app) closeSession(ctx context.Context, h *pluginsdk.Host, projectID, id string) error {
	sessions, err := h.ListSessions(ctx, projectID, true)
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(sessions, func(s pluginsdk.Session) bool { return s.ID == id })
	if idx < 0 {
		return nil
	}
	if err := a.removeSession(ctx, h, sessions[idx]); err != nil {
		return err
	}
	selected, err := a.selected(ctx, h, projectID)
	if err != nil || selected != id {
		return err
	}
	rest := slices.Delete(slices.Clone(sessions), idx, idx+1)
	next := ""
	if len(rest) > 0 {
		next = rest[0].ID
	}
	return h.StorageSet(ctx, selectedKey(projectID), next)
}

func (a *app) removeSession(ctx context.Context, h *pluginsdk.Host, s pluginsdk.Session) error {
	if s.State == "running" {
		if err := h.KillSession(ctx, s.ID); err != nil {
			return err
		}
	}
	return h.RemoveSession(ctx, s.ID)
}

func (a *app) spawn(ctx context.Context, h *pluginsdk.Host, projectID, title, cwd string) error {
	s, err := h.CreateSession(ctx, pluginsdk.SessionRequest{ProjectID: projectID, Title: title, Command: []string{shell(), "-l"}, Cwd: cwd})
	if err != nil {
		return err
	}
	a.known[s.ID] = projectID
	return h.StorageSet(ctx, selectedKey(projectID), s.ID)
}

func (a *app) render(ctx context.Context, h *pluginsdk.Host, projectID string) error {
	sessions, err := h.ListSessions(ctx, projectID, true)
	if err != nil {
		return err
	}
	selected, err := a.selected(ctx, h, projectID)
	if err != nil {
		return err
	}
	terminals := make([]terminal, 0, len(sessions))
	for _, s := range sessions {
		a.known[s.ID] = projectID
		terminals = append(terminals, terminal{ID: s.ID, Title: s.Title, State: s.State, ExitCode: s.ExitCode})
	}
	resolved := resolveSelection(terminals, selected)
	if resolved != selected {
		if err := a.persistSelection(ctx, h, projectID, resolved); err != nil {
			return err
		}
		selected = resolved
	}
	if _, err := h.Render(ctx, projectID, sidebarSlot, sidebarView(terminals, selected)); err != nil {
		return err
	}
	_, err = h.Render(ctx, projectID, mainSlot, mainView(terminals, selected))
	return err
}

func (a *app) persistSelection(ctx context.Context, h *pluginsdk.Host, projectID, id string) error {
	if id == "" {
		return h.StorageDelete(ctx, selectedKey(projectID))
	}
	return h.StorageSet(ctx, selectedKey(projectID), id)
}

func (a *app) selected(ctx context.Context, h *pluginsdk.Host, projectID string) (string, error) {
	var id string
	_, err := h.StorageGet(ctx, selectedKey(projectID), &id)
	return id, err
}

func maxNumber(sessions []pluginsdk.Session) int {
	highest := 0
	for _, s := range sessions {
		num, ok := strings.CutPrefix(s.Title, titlePrefix)
		if !ok {
			continue
		}
		n, err := strconv.Atoi(num)
		if err == nil && n > highest {
			highest = n
		}
	}
	return highest
}

func shell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return defaultShell
}

func selectedKey(projectID string) string {
	return "selected/" + projectID
}
