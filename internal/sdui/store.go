package sdui

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"clubmate/pkg/sdui"
)

var ErrUnknownWorkspace = errors.New("unknown workspace")

type SlotView struct {
	WorkspaceID string          `json:"workspaceId"`
	Slot        string          `json:"slot"`
	Owner       string          `json:"owner"`
	Rev         int64           `json:"rev"`
	Tree        json.RawMessage `json:"tree,omitempty"`
	Base        int64           `json:"base,omitempty"`
	Delta       json.RawMessage `json:"delta,omitempty"`
}

type slotKey struct {
	workspace, slot string
}

type Store struct {
	mu    sync.Mutex
	slots map[slotKey]SlotView
	revs  map[slotKey]int64
	subs  map[*Subscription]struct{}
	empty json.RawMessage

	dropped map[string]struct{}
}

type Subscription struct {
	store     *Store
	workspace string
	ready     chan struct{}
	pending   []SlotView
	ended     bool
}

func NewStore() *Store {
	return &Store{
		slots: map[slotKey]SlotView{},
		revs:  map[slotKey]int64{},
		subs:  map[*Subscription]struct{}{},
		empty: json.RawMessage(`{"type":"empty","props":{"text":""}}`),

		dropped: map[string]struct{}{},
	}
}

func (s *Store) Render(ctx context.Context, workspaceID, slot, owner string, n sdui.Node) (int64, error) {
	if err := Validate(n); err != nil {
		return 0, err
	}
	tree, err := json.Marshal(n)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidTree, err)
	}
	return s.put(ctx, workspaceID, slot, owner, tree)
}

func (s *Store) RenderJSON(ctx context.Context, workspaceID, slot, owner string, tree json.RawMessage) (int64, error) {
	var n sdui.Node
	if err := jsonv2.Unmarshal(tree, &n, jsonv2.RejectUnknownMembers(true)); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidTree, err)
	}
	if err := Validate(n); err != nil {
		return 0, err
	}
	if bytes.IndexByte(tree, '\n') >= 0 {
		var buf bytes.Buffer
		if err := json.Compact(&buf, tree); err != nil {
			return 0, fmt.Errorf("%w: %v", ErrInvalidTree, err)
		}
		tree = buf.Bytes()
	}
	return s.put(ctx, workspaceID, slot, owner, tree)
}

func (s *Store) put(ctx context.Context, workspaceID, slot, owner string, tree json.RawMessage) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if _, gone := s.dropped[workspaceID]; gone {
		return 0, ErrUnknownWorkspace
	}
	key := slotKey{workspaceID, slot}
	s.revs[key]++
	view := SlotView{WorkspaceID: workspaceID, Slot: slot, Owner: owner, Rev: s.revs[key], Tree: tree}
	s.slots[key] = view
	s.notifyLocked(view)
	return view.Rev, nil
}

func (s *Store) Get(workspaceID string) []SlotView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewsLocked(workspaceID)
}

func (s *Store) ClearOwner(owner string) {
	s.clear(func(_ slotKey, view SlotView) bool { return view.Owner == owner })
}

func (s *Store) DropWorkspace(workspaceID string) {
	s.mu.Lock()
	s.dropped[workspaceID] = struct{}{}
	s.mu.Unlock()
	s.clear(func(key slotKey, _ SlotView) bool { return key.workspace == workspaceID })
}

func (s *Store) Subscribe(workspaceID string) (*Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub := &Subscription{store: s, workspace: workspaceID, ready: make(chan struct{}, 1)}
	s.subs[sub] = struct{}{}
	for _, view := range s.viewsLocked(workspaceID) {
		sub.pushLocked(view)
	}
	return sub, nil
}

func (v SlotView) AppendJSON(dst []byte) []byte {
	dst = slices.Grow(dst, len(v.Tree)+len(v.Delta)+len(v.WorkspaceID)+len(v.Slot)+len(v.Owner)+96)
	dst = append(dst, `{"workspaceId":`...)
	dst, _ = jsontext.AppendQuote(dst, v.WorkspaceID)
	dst = append(dst, `,"slot":`...)
	dst, _ = jsontext.AppendQuote(dst, v.Slot)
	dst = append(dst, `,"owner":`...)
	dst, _ = jsontext.AppendQuote(dst, v.Owner)
	dst = append(dst, `,"rev":`...)
	dst = strconv.AppendInt(dst, v.Rev, 10)
	if len(v.Delta) > 0 {
		dst = append(dst, `,"base":`...)
		dst = strconv.AppendInt(dst, v.Base, 10)
		dst = append(dst, `,"delta":`...)
		return append(append(dst, v.Delta...), '}')
	}
	dst = append(dst, `,"tree":`...)
	if len(v.Tree) == 0 {
		dst = append(dst, "null"...)
	} else {
		dst = append(dst, v.Tree...)
	}
	return append(dst, '}')
}

func (v SlotView) Node() (sdui.Node, error) {
	var n sdui.Node
	err := json.Unmarshal(v.Tree, &n)
	return n, err
}

func (sub *Subscription) Ready() <-chan struct{} {
	return sub.ready
}

func (sub *Subscription) Take() ([]SlotView, bool) {
	sub.store.mu.Lock()
	defer sub.store.mu.Unlock()
	if sub.ended {
		return nil, false
	}
	views := sub.pending
	sub.pending = nil
	return views, true
}

func (sub *Subscription) Close() {
	sub.store.mu.Lock()
	defer sub.store.mu.Unlock()
	if sub.ended {
		return
	}
	sub.ended = true
	sub.pending = nil
	delete(sub.store.subs, sub)
	close(sub.ready)
}

func (s *Store) clear(match func(slotKey, SlotView) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cleared []SlotView
	for key, view := range s.slots {
		if !match(key, view) {
			continue
		}
		delete(s.slots, key)
		s.revs[key]++
		cleared = append(cleared, SlotView{
			WorkspaceID: key.workspace,
			Slot:        key.slot,
			Owner:       view.Owner,
			Rev:         s.revs[key],
			Tree:        s.empty,
		})
	}
	slices.SortFunc(cleared, func(a, b SlotView) int {
		return strings.Compare(a.WorkspaceID+"\x00"+a.Slot, b.WorkspaceID+"\x00"+b.Slot)
	})
	for _, view := range cleared {
		s.notifyLocked(view)
	}
}

func (s *Store) viewsLocked(workspaceID string) []SlotView {
	views := []SlotView{}
	for key, view := range s.slots {
		if key.workspace == workspaceID {
			views = append(views, view)
		}
	}
	slices.SortFunc(views, func(a, b SlotView) int { return strings.Compare(a.Slot, b.Slot) })
	return views
}

func (s *Store) notifyLocked(view SlotView) {
	for sub := range s.subs {
		if sub.workspace == view.WorkspaceID {
			sub.pushLocked(view)
		}
	}
}

func (sub *Subscription) pushLocked(view SlotView) {
	sub.pending = slices.DeleteFunc(sub.pending, func(v SlotView) bool { return v.Slot == view.Slot })
	sub.pending = append(sub.pending, view)
	select {
	case sub.ready <- struct{}{}:
	default:
	}
}
