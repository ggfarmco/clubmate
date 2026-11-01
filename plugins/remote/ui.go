package main

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

const (
	popupPattern   = "*.popup"
	popupSuffix    = ".popup"
	maxWatchSlots  = 64
	maxSlotName    = 96
	workspaceScope = "workspace"
	unwatchTimeout = 10 * time.Second
)

type space struct {
	sub      string
	presence bool
	views    map[string]pluginsdk.SlotView
}

type uiQueue struct {
	mu      sync.Mutex
	order   []string
	pending map[string]pluginsdk.SlotView
	ready   chan struct{}
}

type uiGetRequest struct {
	Workspace string   `json:"workspace"`
	Slots     []string `json:"slots"`
}

type uiEventRequest struct {
	Workspace string          `json:"workspace"`
	Slot      string          `json:"slot"`
	NodeID    string          `json:"nodeId"`
	Action    string          `json:"action"`
	Value     json.RawMessage `json:"value"`
}

type uiWatchRequest struct {
	Stream    string   `json:"stream"`
	Workspace string   `json:"workspace"`
	Slots     []string `json:"slots"`
}

type sectionScope struct {
	Scope string `json:"scope"`
}

type uiFrame struct {
	Workspace string          `json:"workspace"`
	Slot      string          `json:"slot"`
	Rev       int64           `json:"rev"`
	Tree      json.RawMessage `json:"tree"`
}

type streamFrame struct {
	ID string `json:"id"`
}

func (s *server) uiSections(ctx context.Context) ([]json.RawMessage, error) {
	all, err := s.backend.UISections(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(all))
	for _, raw := range all {
		var sec sectionScope
		if err := json.Unmarshal(raw, &sec); err != nil {
			return nil, err
		}
		if sec.Scope == "" || sec.Scope == workspaceScope {
			out = append(out, raw)
		}
	}
	return out, nil
}

func (s *server) uiGet(ctx context.Context, p uiGetRequest) ([]pluginsdk.SlotView, error) {
	if err := validWatch(p.Workspace, p.Slots); err != nil {
		return nil, err
	}
	views, err := s.backend.UIGet(ctx, p.Workspace)
	if err != nil {
		return nil, err
	}
	out := make([]pluginsdk.SlotView, 0, len(views))
	for _, v := range views {
		if watches(p.Slots, v.Slot) {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *server) uiEvent(ctx context.Context, p uiEventRequest) (json.RawMessage, error) {
	if p.Workspace == "" {
		return nil, &apiError{Code: codeInvalidParams, Message: "workspace is required"}
	}
	out, err := s.backend.UIEvent(ctx, pluginsdk.UIEvent{WorkspaceID: p.Workspace, Slot: p.Slot, NodeID: p.NodeID, Action: p.Action, Value: p.Value})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		out = json.RawMessage("null")
	}
	return out, nil
}

func (s *server) uiWatch(ctx context.Context, d device, p uiWatchRequest) error {
	if err := validWatch(p.Workspace, p.Slots); err != nil {
		return err
	}
	if len(p.Slots) > 0 {
		known, err := s.knownWorkspace(ctx, p.Workspace)
		if err != nil {
			return err
		}
		if !known {
			return &apiError{Code: codeInvalidParams, Message: "unknown workspace: " + p.Workspace}
		}
	}
	if !s.hub.watch(d.ID, p.Stream, p.Workspace, p.Slots) {
		return &apiError{Code: codeInvalidParams, Message: "unknown event stream: " + p.Stream}
	}
	return s.reconcile(ctx, p.Workspace)
}

func (s *server) uiUpdated(view pluginsdk.SlotView) {
	s.hub.deliver(view)
}

func (s *server) closeStream(ctx context.Context, st *stream) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unwatchTimeout)
	defer cancel()
	for _, ws := range s.hub.close(st) {
		if err := s.reconcile(ctx, ws); err != nil {
			logf("stop watching %s: %v", ws, err)
		}
	}
}

func (s *server) reconcile(ctx context.Context, ws string) error {
	s.uiMu.Lock()
	defer s.uiMu.Unlock()
	defer s.announce(ws)
	watched, presence := s.hub.wants(ws)
	cur, ok := s.hub.space(ws)
	if !watched {
		if !ok {
			return nil
		}
		s.hub.dropSpace(ws)
		return s.backend.UIUnsubscribe(ctx, cur.sub)
	}
	if ok && cur.sub != "" && cur.presence == presence {
		return nil
	}
	s.hub.openSpace(ws)
	sid, err := s.backend.UISubscribe(ctx, pluginsdk.UISubscribe{WorkspaceID: ws, Presence: presence})
	if err != nil {
		if !ok || cur.sub == "" {
			s.hub.dropSpace(ws)
		}
		return err
	}
	s.hub.setSpace(ws, sid, presence)
	if ok && cur.sub != "" {
		return s.backend.UIUnsubscribe(ctx, cur.sub)
	}
	return nil
}

func (s *server) clearWatching(ctx context.Context) error {
	workspaces, err := s.backend.Workspaces(ctx)
	if err != nil {
		return err
	}
	for _, w := range workspaces {
		s.watching(contract.RemoteWatching{Workspace: w.ID, Sections: []string{}})
	}
	return nil
}

func (s *server) announce(ws string) {
	cur := s.hub.sections(ws)
	if slices.Equal(cur, s.sections[ws]) {
		return
	}
	if len(cur) == 0 {
		delete(s.sections, ws)
	} else {
		if s.sections == nil {
			s.sections = map[string][]string{}
		}
		s.sections[ws] = cur
	}
	s.watching(contract.RemoteWatching{Workspace: ws, Sections: cur})
}

func (s *server) knownWorkspace(ctx context.Context, id string) (bool, error) {
	workspaces, err := s.backend.Workspaces(ctx)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(workspaces, func(w pluginsdk.Workspace) bool { return w.ID == id }), nil
}

func (h *hub) watch(device, id, ws string, slots []string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.streamLocked(id)
	if st == nil || st.device != device {
		return false
	}
	old := st.watch[ws]
	if len(slots) == 0 {
		delete(st.watch, ws)
		return true
	}
	st.watch[ws] = slices.Clone(slots)
	sp := h.spaces[ws]
	if sp == nil {
		return true
	}
	for _, slot := range slices.Sorted(maps.Keys(sp.views)) {
		if watches(slots, slot) && !watches(old, slot) {
			st.ui.push(sp.views[slot])
		}
	}
	return true
}

func (h *hub) deliver(view pluginsdk.SlotView) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sp := h.spaces[view.WorkspaceID]
	if sp == nil {
		return
	}
	if cur, ok := sp.views[view.Slot]; ok && cur.Rev >= view.Rev {
		return
	}
	sp.views[view.Slot] = view
	for st := range h.streams {
		if watches(st.watch[view.WorkspaceID], view.Slot) {
			st.ui.push(view)
		}
	}
}

func (h *hub) wants(ws string) (watched, presence bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for st := range h.streams {
		for _, slot := range st.watch[ws] {
			watched = true
			presence = presence || !strings.HasSuffix(slot, popupSuffix)
		}
	}
	return watched, presence
}

func (h *hub) sections(ws string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []string{}
	for st := range h.streams {
		for _, slot := range st.watch[ws] {
			if section, _, ok := strings.Cut(slot, "."); ok && !strings.HasSuffix(slot, popupSuffix) {
				out = append(out, section)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (h *hub) space(ws string) (space, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sp := h.spaces[ws]
	if sp == nil {
		return space{}, false
	}
	return space{sub: sp.sub, presence: sp.presence}, true
}

func (h *hub) openSpace(ws string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.spaces[ws] == nil {
		h.spaces[ws] = &space{views: map[string]pluginsdk.SlotView{}}
	}
}

func (h *hub) setSpace(ws, sub string, presence bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sp := h.spaces[ws]; sp != nil {
		sp.sub, sp.presence = sub, presence
	}
}

func (h *hub) dropSpace(ws string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.spaces, ws)
}

func (h *hub) streamLocked(id string) *stream {
	for st := range h.streams {
		if st.id == id {
			return st
		}
	}
	return nil
}

func newUIQueue() *uiQueue {
	return &uiQueue{pending: map[string]pluginsdk.SlotView{}, ready: make(chan struct{}, 1)}
}

func (q *uiQueue) push(view pluginsdk.SlotView) {
	key := view.WorkspaceID + "\x00" + view.Slot
	q.mu.Lock()
	if _, ok := q.pending[key]; !ok {
		q.order = append(q.order, key)
	}
	q.pending[key] = view
	q.mu.Unlock()
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

func (q *uiQueue) take() []pluginsdk.SlotView {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]pluginsdk.SlotView, 0, len(q.order))
	for _, key := range q.order {
		out = append(out, q.pending[key])
	}
	q.order = nil
	clear(q.pending)
	return out
}

func validWatch(ws string, slots []string) error {
	if ws == "" {
		return &apiError{Code: codeInvalidParams, Message: "workspace is required"}
	}
	if len(slots) > maxWatchSlots {
		return &apiError{Code: codeInvalidParams, Message: "at most 64 slots"}
	}
	for _, slot := range slots {
		if !validSlot(slot) {
			return &apiError{Code: codeInvalidParams, Message: "invalid slot: " + slot}
		}
	}
	return nil
}

func validSlot(slot string) bool {
	if slot == popupPattern {
		return true
	}
	section, name, ok := strings.Cut(slot, ".")
	return ok && section != "" && name != "" && len(slot) <= maxSlotName && !strings.Contains(slot, "*")
}

func watches(patterns []string, slot string) bool {
	for _, p := range patterns {
		if p == slot || (p == popupPattern && strings.HasSuffix(slot, popupSuffix)) {
			return true
		}
	}
	return false
}

func uiSSEFrame(view pluginsdk.SlotView) string {
	tree := view.Tree
	if len(tree) == 0 {
		tree = json.RawMessage("null")
	}
	data, err := json.Marshal(uiFrame{Workspace: view.WorkspaceID, Slot: view.Slot, Rev: view.Rev, Tree: tree})
	if err != nil {
		return ""
	}
	return "event: ui\ndata: " + string(data) + "\n\n"
}

func streamSSEFrame(id string) string {
	data, _ := json.Marshal(streamFrame{ID: id})
	return "event: stream\ndata: " + string(data) + "\n\n"
}
