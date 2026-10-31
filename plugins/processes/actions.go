package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

var (
	errUnknownAction  = errors.New("unknown action")
	errUnknownCommand = errors.New("unknown command")
	errUnknownColumn  = errors.New("unknown sort column")
	errNoWorkspace    = errors.New("command needs a workspace")
)

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	st, err := a.state(ctx, h, ev.WorkspaceID)
	if err != nil {
		return nil, err
	}
	snap := a.snapshot(st)
	switch ev.Action {
	case "expand", "collapse":
		err = a.toggleCollapsed(ev, st)
	case "sort":
		err = a.setSort(ctx, h, ev, st)
	case killAction:
		err = killSelected(ctx, h, ev.WorkspaceID, snap)
	case orphanKill:
		_, err = killOrphans(ctx, h, ev.WorkspaceID, snap.session)
	case "select":
		err = a.selectSession(ctx, h, ev, st)
	case selectPID:
		err = a.selectProcess(ev, st)
	case autoKill:
		err = a.toggleAuto(ctx, h, ev, st)
	default:
		return nil, fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
	}
	if err != nil {
		return nil, err
	}
	return nil, a.render(ctx, h, ev.WorkspaceID)
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	if cmd.ID != "processes.kill-orphans" {
		return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
	}
	if cmd.WorkspaceID == "" {
		return nil, errNoWorkspace
	}
	st, err := a.state(ctx, h, cmd.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if _, err := killOrphans(ctx, h, cmd.WorkspaceID, a.snapshot(st).session); err != nil {
		return nil, err
	}
	return nil, a.render(ctx, h, cmd.WorkspaceID)
}

func (a *app) selectSession(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent, st *workspaceState) error {
	var id string
	if err := json.Unmarshal(ev.Value, &id); err != nil {
		return fmt.Errorf("select value: %w", err)
	}
	if id == allID {
		id = ""
	}
	a.mu.Lock()
	st.session, st.pid = id, 0
	a.mu.Unlock()
	return storeOrDelete(ctx, h, selectedKey(ev.WorkspaceID), id, id != "")
}

func (a *app) selectProcess(ev pluginsdk.UIEvent, st *workspaceState) error {
	pid, err := rowPID(ev)
	if err != nil || pid == 0 {
		return err
	}
	a.mu.Lock()
	st.pid = pid
	a.mu.Unlock()
	return nil
}

func (a *app) toggleCollapsed(ev pluginsdk.UIEvent, st *workspaceState) error {
	pid, err := rowPID(ev)
	if err != nil || pid == 0 {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ev.Action == "collapse" {
		st.collapsed[pid] = true
	} else {
		delete(st.collapsed, pid)
	}
	return nil
}

func (a *app) setSort(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent, st *workspaceState) error {
	var s sdui.Sort
	if err := json.Unmarshal(ev.Value, &s); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	if !validSort(s) {
		return fmt.Errorf("%w: %q", errUnknownColumn, s.Column)
	}
	a.mu.Lock()
	st.sort = s
	a.mu.Unlock()
	return h.StorageSet(ctx, sortKey(ev.WorkspaceID), s)
}

func (a *app) toggleAuto(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent, st *workspaceState) error {
	var on bool
	if err := json.Unmarshal(ev.Value, &on); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	a.mu.Lock()
	st.auto = on
	a.mu.Unlock()
	return storeOrDelete(ctx, h, autoKillKey(ev.WorkspaceID), true, on)
}

func rowPID(ev pluginsdk.UIEvent) (int, error) {
	var raw string
	if err := json.Unmarshal(ev.Value, &raw); err != nil {
		return 0, fmt.Errorf("%s value: %w", ev.Action, err)
	}
	if raw == moreID {
		return 0, nil
	}
	pid, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s value: %w", ev.Action, err)
	}
	return pid, nil
}
