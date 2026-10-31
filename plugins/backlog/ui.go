package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/agentcall"
)

const kindDeleted = "deleted"

var errUnknownAction = errors.New("unknown action")

type workspaceUI struct {
	render    sync.Mutex
	selected  int
	collapsed map[string]bool
	formRev   int
	sent      map[string][32]byte
}

type presenceEvent struct {
	ProjectID string `json:"projectId"`
}

func (b *backlog) workspace(projectID string) *workspaceUI {
	b.mu.Lock()
	defer b.mu.Unlock()
	w, ok := b.ui[projectID]
	if !ok {
		w = &workspaceUI{collapsed: map[string]bool{statusDone: true}, sent: map[string][32]byte{}}
		b.ui[projectID] = w
	}
	return w
}

func (b *backlog) isPresent(projectID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.present[projectID]
}

func (b *backlog) presentIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Collect(maps.Keys(b.present))
}

func (b *backlog) setPresent(projectID string, on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if on {
		b.present[projectID] = true
		return
	}
	delete(b.present, projectID)
	if w, ok := b.ui[projectID]; ok {
		w.sent = map[string][32]byte{}
	}
}

func (b *backlog) dropUI(projectID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.present, projectID)
	delete(b.ui, projectID)
}

func (b *backlog) loadPresence(ctx context.Context) error {
	present, err := b.h.Presence(ctx)
	if err != nil {
		return err
	}
	for _, id := range present {
		b.setPresent(id, true)
	}
	for _, id := range present {
		b.render(ctx, id)
	}
	return nil
}

func (b *backlog) presenceEvent(ctx context.Context, ev pluginsdk.Event) {
	if ev.Topic != "ui.opened" && ev.Topic != "ui.idle" {
		return
	}
	var p presenceEvent
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		logError(ev.Topic, err)
		return
	}
	b.setPresent(p.ProjectID, ev.Topic == "ui.opened")
	if ev.Topic == "ui.opened" {
		b.render(ctx, p.ProjectID)
	}
}

func (b *backlog) render(ctx context.Context, projectID string) {
	if !b.isPresent(projectID) {
		return
	}
	logError("render "+projectID, b.renderNow(ctx, projectID))
}

func (b *backlog) renderNow(ctx context.Context, projectID string) error {
	w := b.workspace(projectID)
	w.render.Lock()
	defer w.render.Unlock()
	tasks, err := b.store.list(ctx, projectID)
	if err != nil {
		return err
	}
	b.mu.Lock()
	selected, collapsed, rev := w.selected, maps.Clone(w.collapsed), w.formRev
	b.mu.Unlock()
	var current *Task
	for i := range tasks {
		if tasks[i].ID == selected {
			current = &tasks[i]
		}
	}
	selectedID := ""
	if current != nil {
		selectedID = strconv.Itoa(current.ID)
	}
	owners := b.ownerLabels(ctx, projectID, tasks, current)
	if err := b.push(ctx, w, projectID, sidebarSlot, sidebarView(tasks, owners, selectedID, collapsed, rev)); err != nil {
		return err
	}
	return b.push(ctx, w, projectID, mainSlot, mainView(current, owners, len(tasks) > 0))
}

func (b *backlog) push(ctx context.Context, w *workspaceUI, projectID, slot string, n sdui.Node) error {
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	b.mu.Lock()
	same := w.sent[slot] == sum
	b.mu.Unlock()
	if same {
		return nil
	}
	if _, err := b.h.Render(ctx, projectID, slot, n); err != nil {
		return err
	}
	b.mu.Lock()
	w.sent[slot] = sum
	b.mu.Unlock()
	return nil
}

func (b *backlog) ownerLabels(ctx context.Context, projectID string, tasks []Task, current *Task) map[string]string {
	ids := map[string]bool{}
	for _, t := range tasks {
		if t.Status == statusInProgress {
			ids[t.ClaimedBy] = true
		}
	}
	if current != nil && current.LastClaimedBy != "" {
		ids[current.LastClaimedBy] = true
	}
	owners := make(map[string]string, len(ids))
	for id := range ids {
		if !strings.HasPrefix(id, sessionPfx) {
			owners[id] = ownerLabel(id, "", false)
			continue
		}
		title, _ := b.live.title(ctx, projectID, id)
		owners[id] = ownerLabel(id, title, b.live.stale(ctx, projectID, id))
	}
	return owners
}

func (b *backlog) uiEvent(ctx context.Context, _ *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	err := b.act(ctx, ev)
	b.render(ctx, ev.ProjectID)
	return nil, err
}

func (b *backlog) act(ctx context.Context, ev pluginsdk.UIEvent) error {
	w := b.workspace(ev.ProjectID)
	user := pluginsdk.CommandRun{ProjectID: ev.ProjectID}
	switch {
	case ev.NodeID == "new" && ev.Action == "add":
		var args commandArgs
		if err := json.Unmarshal(ev.Value, &args); err != nil {
			return fmt.Errorf("add value: %w", err)
		}
		res, err := b.addCmd(ctx, user, commandArgs{Title: args.Title})
		if err != nil {
			return err
		}
		b.mu.Lock()
		w.selected = res.(Task).ID
		w.formRev++
		b.mu.Unlock()
		return nil
	case ev.NodeID == "edit":
		verb, value := parseAction(ev.Action)
		if verb != "save" {
			return fmt.Errorf("%w: %s %q", errUnknownAction, ev.NodeID, ev.Action)
		}
		id, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("save task id %q: %w", value, err)
		}
		var args commandArgs
		if err := json.Unmarshal(ev.Value, &args); err != nil {
			return fmt.Errorf("save value: %w", err)
		}
		args.ID = id
		args.Note, args.Owner, args.keepClaim = nil, "", true
		_, err = b.updateCmd(ctx, user, args)
		return err
	case ev.Slot == mainSlot:
		return b.taskAction(ctx, ev.ProjectID, w, ev.Action, b.selected(w))
	case ev.NodeID != "tasks":
		return fmt.Errorf("%w: %s %q", errUnknownAction, ev.NodeID, ev.Action)
	}
	var item string
	if err := json.Unmarshal(ev.Value, &item); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	if status, ok := strings.CutPrefix(item, groupPrefix); ok {
		if ev.Action == "expand" || ev.Action == "collapse" {
			b.mu.Lock()
			w.collapsed[status] = ev.Action == "collapse"
			b.mu.Unlock()
		}
		return nil
	}
	id, err := strconv.Atoi(item)
	if err != nil {
		return fmt.Errorf("task id %q: %w", item, err)
	}
	if ev.Action == "select" {
		b.mu.Lock()
		w.selected = id
		b.mu.Unlock()
		return nil
	}
	return b.taskAction(ctx, ev.ProjectID, w, ev.Action, id)
}

func (b *backlog) selected(w *workspaceUI) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return w.selected
}

func (b *backlog) taskAction(ctx context.Context, projectID string, w *workspaceUI, action string, id int) error {
	user := pluginsdk.CommandRun{ProjectID: projectID}
	args := commandArgs{ID: id}
	var err error
	switch verb, value := parseAction(action); verb {
	case "start":
		_, err = b.claimCmd(ctx, user, args)
	case "release":
		_, err = b.releaseCmd(ctx, user, args)
	case "done":
		_, err = b.doneCmd(ctx, user, args)
	case "status":
		args.Status = &value
		_, err = b.updateCmd(ctx, user, args)
	case "priority":
		args.Priority = &value
		_, err = b.updateCmd(ctx, user, args)
	case actReveal:
		var t Task
		if t, err = b.store.get(ctx, projectID, id); err != nil {
			return err
		}
		if t.Status != statusInProgress || !strings.HasPrefix(t.ClaimedBy, sessionPfx) {
			return fmt.Errorf("task #%d is not claimed by an agent session", id)
		}
		err = agentcall.Reveal(ctx, b.h, projectID, t.ClaimedBy)
	case "delete":
		if err = b.store.remove(ctx, projectID, id); err != nil {
			return err
		}
		b.mu.Lock()
		if w.selected == id {
			w.selected = 0
		}
		b.mu.Unlock()
		b.changed(ctx, projectID, id, kindDeleted)
	default:
		err = fmt.Errorf("%w: %q", errUnknownAction, action)
	}
	return err
}
