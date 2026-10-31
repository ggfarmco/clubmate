package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	sidebarSlot  = "terminals.sidebar"
	mainSlot     = "terminals.main"
	sectionID    = "terminals"
	titlePrefix  = "Terminal "
	defaultShell = "/bin/zsh"
	refreshEvery = 2 * time.Second
)

var errUnknownAction = errors.New("unknown action")

type closeArgs struct {
	SessionID string `json:"sessionId"`
}

type newArgs struct {
	Cwd string `json:"cwd"`
}

type busyResult struct {
	Busy    bool   `json:"busy"`
	Process string `json:"process"`
}

type app struct {
	mu       sync.Mutex
	known    map[string]string
	seen     map[string]time.Time
	rendered map[string]views
}

type views struct {
	sidebar sdui.Node
	main    sdui.Node
	running bool
}

func main() {
	a := &app{known: map[string]string{}, seen: map[string]time.Time{}, rendered: map[string]views{}}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	ready, err := h.Publish(ctx, "terminals.ready", nil)
	if err != nil {
		return err
	}
	workspaces, err := h.ListWorkspaces(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range workspaces {
		if err := a.render(ctx, h, p.ID); err != nil {
			return err
		}
	}
	if _, err := h.Subscribe(ctx, "workspace.*", ready.Seq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, "session.*", ready.Seq); err != nil {
		return err
	}
	go every(ctx, refreshEvery, func() { a.refresh(ctx, h) })
	return nil
}

func (a *app) refresh(ctx context.Context, h *pluginsdk.Host) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, workspaceID := range a.polled() {
		if err := a.renderChanged(ctx, h, workspaceID, true); err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "terminals: refresh:", err)
		}
	}
}

func (a *app) polled() []string {
	var ids []string
	for workspaceID, v := range a.rendered {
		if v.running {
			ids = append(ids, workspaceID)
		}
	}
	return ids
}

func every(ctx context.Context, interval time.Duration, run func()) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		run()
	}
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.act(ctx, h, ev); err != nil {
		return nil, err
	}
	if err := a.render(ctx, h, ev.WorkspaceID); err != nil {
		return nil, err
	}
	if ev.Action == "new" {
		return nil, h.Focus(ctx, ev.WorkspaceID, sectionID, mainSlot)
	}
	return nil, nil
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	if cmd.ID == "terminals.busy" {
		var args closeArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("terminals.busy args: %w", err)
		}
		return busy(ctx, h, cmd.WorkspaceID, args.SessionID)
	}
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
		if err := a.create(ctx, h, cmd.WorkspaceID, args.Cwd); err != nil {
			return nil, err
		}
	case "terminals.close":
		var args closeArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("terminals.close args: %w", err)
		}
		if err := a.closeSession(ctx, h, cmd.WorkspaceID, args.SessionID); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: command %q", errUnknownAction, cmd.ID)
	}
	if err := a.render(ctx, h, cmd.WorkspaceID); err != nil {
		return nil, err
	}
	if cmd.ID == "terminals.new" {
		return nil, h.Focus(ctx, cmd.WorkspaceID, sectionID, mainSlot)
	}
	return nil, nil
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	workspaceID, err := a.eventWorkspace(ev)
	if err != nil {
		fmt.Fprintln(os.Stderr, "terminals:", ev.Topic, err)
		return
	}
	if workspaceID == "" {
		return
	}
	if err := a.render(ctx, h, workspaceID); err != nil {
		fmt.Fprintln(os.Stderr, "terminals: render:", err)
	}
}

func (a *app) eventWorkspace(ev pluginsdk.Event) (string, error) {
	switch ev.Topic {
	case contract.TopicWorkspaceCreated:
		var created contract.WorkspaceChanged
		err := json.Unmarshal(ev.Payload, &created)
		return created.Workspace, err
	case contract.TopicWorkspaceDeleted:
		var deleted contract.WorkspaceDeleted
		err := json.Unmarshal(ev.Payload, &deleted)
		delete(a.rendered, deleted.Workspace)
		return "", err
	case contract.TopicSessionExited:
		var exited contract.SessionExited
		if err := json.Unmarshal(ev.Payload, &exited); err != nil {
			return "", err
		}
		if exited.Owner != "terminals" {
			return "", nil
		}
		return exited.Workspace, nil
	case contract.TopicSessionKilled, contract.TopicSessionRemoved:
		var gone contract.SessionGone
		err := json.Unmarshal(ev.Payload, &gone)
		return a.known[gone.ID], err
	}
	return "", nil
}

func (a *app) act(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) error {
	switch ev.Action {
	case "new":
		return a.create(ctx, h, ev.WorkspaceID, "")
	case "select":
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("select value: %w", err)
		}
		return h.StorageSet(ctx, selectedKey(ev.WorkspaceID), id)
	case "restart":
		return a.restart(ctx, h, ev.WorkspaceID)
	case "close":
		selected, err := a.selected(ctx, h, ev.WorkspaceID)
		if err != nil {
			return err
		}
		return a.closeSession(ctx, h, ev.WorkspaceID, selected)
	}
	return fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
}

func (a *app) create(ctx context.Context, h *pluginsdk.Host, workspaceID, cwd string) error {
	sessions, err := h.ListSessions(ctx, workspaceID, true)
	if err != nil {
		return err
	}
	return a.spawn(ctx, h, workspaceID, titlePrefix+strconv.Itoa(maxNumber(sessions)+1), cwd)
}

func (a *app) restart(ctx context.Context, h *pluginsdk.Host, workspaceID string) error {
	selected, err := a.selected(ctx, h, workspaceID)
	if err != nil {
		return err
	}
	sessions, err := h.ListSessions(ctx, workspaceID, true)
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(sessions, func(s pluginsdk.Session) bool { return s.ID == selected })
	if idx < 0 || sessions[idx].State == contract.SessionStateRunning {
		return nil
	}
	if err := a.spawn(ctx, h, workspaceID, sessions[idx].Title, sessions[idx].Cwd); err != nil {
		return err
	}
	return a.removeSession(ctx, h, sessions[idx])
}

func (a *app) closeSession(ctx context.Context, h *pluginsdk.Host, workspaceID, id string) error {
	sessions, err := h.ListSessions(ctx, workspaceID, true)
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
	selected, err := a.selected(ctx, h, workspaceID)
	if err != nil || selected != id {
		return err
	}
	rest := slices.Delete(slices.Clone(sessions), idx, idx+1)
	next := ""
	if len(rest) > 0 {
		next = rest[0].ID
	}
	return h.StorageSet(ctx, selectedKey(workspaceID), next)
}

func (a *app) removeSession(ctx context.Context, h *pluginsdk.Host, s pluginsdk.Session) error {
	if s.State == contract.SessionStateRunning {
		if err := h.KillSession(ctx, s.ID); err != nil {
			return err
		}
	}
	if err := h.RemoveSession(ctx, s.ID); err != nil {
		return err
	}
	delete(a.seen, s.ID)
	return h.StorageDelete(ctx, bellKey(s.ID))
}

func (a *app) spawn(ctx context.Context, h *pluginsdk.Host, workspaceID, title, cwd string) error {
	req := pluginsdk.SessionRequest{WorkspaceID: workspaceID, Title: title, Command: []string{shell(), "-l"}, Cwd: cwd}
	if tools := pluginsdk.ToolsDir(); tools != "" {
		req.PathAppend = []string{tools}
	}
	s, err := h.CreateSession(ctx, req)
	if err != nil {
		return err
	}
	a.known[s.ID] = workspaceID
	return h.StorageSet(ctx, selectedKey(workspaceID), s.ID)
}

func (a *app) render(ctx context.Context, h *pluginsdk.Host, workspaceID string) error {
	return a.renderChanged(ctx, h, workspaceID, false)
}

func (a *app) renderChanged(ctx context.Context, h *pluginsdk.Host, workspaceID string, changedOnly bool) error {
	sessions, err := h.ListSessions(ctx, workspaceID, true)
	if err != nil {
		return err
	}
	selected, err := a.selected(ctx, h, workspaceID)
	if err != nil {
		return err
	}
	terminals := make([]terminal, 0, len(sessions))
	for _, s := range sessions {
		a.known[s.ID] = workspaceID
		terminals = append(terminals, terminal{ID: s.ID, Title: s.Title, State: s.State, ExitCode: s.ExitCode, OutputAt: s.OutputAt})
	}
	resolved := resolveSelection(terminals, selected)
	if resolved != selected {
		if err := a.persistSelection(ctx, h, workspaceID, resolved); err != nil {
			return err
		}
		selected = resolved
	}
	for i, s := range sessions {
		if terminals[i].Bell, err = a.unseenBell(ctx, h, s, s.ID == selected); err != nil {
			return err
		}
	}
	running := slices.ContainsFunc(terminals, func(t terminal) bool { return t.State == contract.SessionStateRunning })
	return a.show(ctx, h, workspaceID, views{sidebar: sidebarView(terminals, selected, time.Now()), main: mainView(terminals, selected), running: running}, changedOnly)
}

func (a *app) unseenBell(ctx context.Context, h *pluginsdk.Host, s pluginsdk.Session, selected bool) (bool, error) {
	if s.BellAt == nil {
		return false, nil
	}
	seen, ok := a.seen[s.ID]
	if !ok {
		if _, err := h.StorageGet(ctx, bellKey(s.ID), &seen); err != nil {
			return false, err
		}
		a.seen[s.ID] = seen
	}
	if !s.BellAt.After(seen) {
		return false, nil
	}
	if !selected {
		return true, nil
	}
	a.seen[s.ID] = *s.BellAt
	return false, h.StorageSet(ctx, bellKey(s.ID), *s.BellAt)
}

func (a *app) show(ctx context.Context, h *pluginsdk.Host, workspaceID string, v views, changedOnly bool) error {
	last, ok := a.rendered[workspaceID]
	ok = ok && changedOnly
	if !ok || !reflect.DeepEqual(last.sidebar, v.sidebar) {
		if _, err := h.Render(ctx, workspaceID, sidebarSlot, v.sidebar); err != nil {
			return err
		}
		last.sidebar = v.sidebar
		a.rendered[workspaceID] = last
	}
	if !ok || !reflect.DeepEqual(last.main, v.main) {
		if _, err := h.Render(ctx, workspaceID, mainSlot, v.main); err != nil {
			return err
		}
	}
	a.rendered[workspaceID] = v
	return nil
}

func (a *app) persistSelection(ctx context.Context, h *pluginsdk.Host, workspaceID, id string) error {
	if id == "" {
		return h.StorageDelete(ctx, selectedKey(workspaceID))
	}
	return h.StorageSet(ctx, selectedKey(workspaceID), id)
}

func (a *app) selected(ctx context.Context, h *pluginsdk.Host, workspaceID string) (string, error) {
	var id string
	_, err := h.StorageGet(ctx, selectedKey(workspaceID), &id)
	return id, err
}

func busy(ctx context.Context, h *pluginsdk.Host, workspaceID, id string) (busyResult, error) {
	sessions, err := h.ListSessions(ctx, workspaceID, true)
	if err != nil {
		return busyResult{}, err
	}
	idx := slices.IndexFunc(sessions, func(s pluginsdk.Session) bool { return s.ID == id })
	if idx < 0 || sessions[idx].State != contract.SessionStateRunning || sessions[idx].ChildPID <= 0 {
		return busyResult{}, nil
	}
	nodes, err := h.ProcessTree(ctx, workspaceID, id)
	if err != nil {
		return busyResult{}, err
	}
	newest, ok := newestDescendant(nodes, sessions[idx].ChildPID)
	if !ok {
		return busyResult{}, nil
	}
	return busyResult{Busy: true, Process: newest.Comm}, nil
}

func newestDescendant(nodes []pluginsdk.ProcessNode, root int) (pluginsdk.ProcessInfo, bool) {
	children := make(map[int][]pluginsdk.ProcessInfo, len(nodes))
	for _, n := range nodes {
		children[n.Process.PPID] = append(children[n.Process.PPID], n.Process)
	}
	var newest pluginsdk.ProcessInfo
	found := false
	queue := []int{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, p := range children[pid] {
			if !found || p.StartTime.After(newest.StartTime) || (p.StartTime.Equal(newest.StartTime) && p.PID > newest.PID) {
				newest, found = p, true
			}
			queue = append(queue, p.PID)
		}
	}
	return newest, found
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

func selectedKey(workspaceID string) string {
	return "selected/" + workspaceID
}

func bellKey(sessionID string) string {
	return "bellSeen/" + sessionID
}
