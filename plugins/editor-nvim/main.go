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
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/editorroot"
)

const (
	mainSlot     = "editor-nvim.main"
	statusSlot   = "editor-nvim.status"
	reviewSlot   = "editor-nvim.review"
	reviewTopic  = "editor-nvim.review"
	sessionTitle = "nvim"
)

var errUnknownAction = errors.New("unknown action")

type app struct {
	slotMu   sync.Mutex
	mu       sync.Mutex
	editors  map[editorKey]*editor
	roots    editorroot.Roots
	stopping map[editorKey]bool
	offerMu  sync.Mutex
	themeMu  sync.Mutex
	heads    *editorroot.Heads
	branchMu sync.Mutex

	reviewMu         sync.Mutex
	reviews          map[string]*review
	reviewSel        map[string]string
	reviewLayouts    map[string]string
	reviewPublished  map[string]reviewPayload
	reviewSeq        int
	reviewWatched    map[*client]bool
	reviewStatusHint func(workspaceID string)
	reviewDBMu       sync.Mutex
	reviewDBPending  map[string]bool
	reviewDBDelay    time.Duration
	switchRoot       func(ctx context.Context, r *review)
	nrev             string
	readComments     commentReader
	sheetSeq         int64
}

func main() {
	a := &app{
		editors:  map[editorKey]*editor{},
		roots:    editorroot.NewRoots(),
		stopping: map[editorKey]bool{},

		reviews:         map[string]*review{},
		reviewSel:       map[string]string{},
		reviewLayouts:   map[string]string{},
		reviewPublished: map[string]reviewPayload{},
		reviewWatched:   map[*client]bool{},
		reviewDBDelay:   reviewDBDelay,
	}
	a.heads = editorroot.NewHeads(a.isDeleted)
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, FSChanged: a.fsChanged})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	a.reviewStatusHint = func(workspaceID string) { a.refreshStatus(h, workspaceID) }
	a.nrev = filepath.Join(pluginsdk.ResourcesDir(), "tools", "nrev")
	a.readComments = nrevComments(a.nrev)
	a.switchRoot = func(ctx context.Context, r *review) {
		if _, err := a.target(ctx, h, r.Workspace, r.Root); err != nil {
			logError("review switch", err)
		}
	}
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
	for _, pattern := range []string{contract.TopicUIOpened, contract.TopicSessionExited, contract.TopicWorkspaceUpdated, contract.TopicWorkspaceDeleted} {
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
	workspaces := present
	for _, s := range sessions {
		if s.State == contract.SessionStateRunning && s.Title == sessionTitle {
			workspaces = append(workspaces, s.WorkspaceID)
		}
	}
	slices.Sort(workspaces)
	for _, id := range slices.Compact(workspaces) {
		go a.ensureLogged(ctx, h, id)
	}
	return nil
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case contract.TopicUIOpened:
		var opened contract.UIPresence
		if err := json.Unmarshal(ev.Payload, &opened); err != nil {
			logError(ev.Topic, err)
			return
		}
		go a.ensureLogged(ctx, h, opened.Workspace)
	case contract.TopicSessionExited:
		var exited contract.SessionExited
		if err := json.Unmarshal(ev.Payload, &exited); err != nil {
			logError(ev.Topic, err)
			return
		}
		if exited.Owner == h.PluginID() {
			a.exited(ctx, h, exited)
		}
	case contract.TopicWorkspaceUpdated:
		var p contract.WorkspaceChanged
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			logError(ev.Topic, err)
			return
		}
		a.workspaceUpdated(ctx, h, p.Info)
	case contract.TopicWorkspaceDeleted:
		var p contract.WorkspaceDeleted
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			logError(ev.Topic, err)
			return
		}
		a.drop(p.Workspace)
		a.unwatch(ctx, h, p.Workspace)
	case themeTopic:
		a.themeChanged(ev.Payload)
		if err := h.StorageSet(ctx, themeSeqKey, ev.Seq); err != nil {
			logError(themeTopic, err)
		}
	}
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	if ev.Slot == reviewSlot {
		return nil, a.reviewUIEvent(ctx, h, ev)
	}
	if ev.Slot == statusSlot && ev.Action == sendAction {
		return nil, a.sendFromStatus(ctx, h, ev.WorkspaceID)
	}
	switch ev.Action {
	case "restart":
		e, err := a.active(ctx, h, ev.WorkspaceID)
		if e == nil || err != nil {
			return nil, err
		}
		return nil, a.restart(ctx, h, e, false, false)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
}

func logError(what string, err error) {
	fmt.Fprintln(os.Stderr, "editor-nvim:", what+":", err)
}
