package sdui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"clubmate/pkg/sdui"
)

var ErrUnknownProject = errors.New("unknown project")

type SlotView struct {
	ProjectID string          `json:"projectId"`
	Slot      string          `json:"slot"`
	Owner     string          `json:"owner"`
	Rev       int64           `json:"rev"`
	Tree      json.RawMessage `json:"tree"`
}

type slotKey struct {
	project, slot string
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
	store   *Store
	project string
	ready   chan struct{}
	pending []SlotView
	ended   bool
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

func (s *Store) Render(ctx context.Context, projectID, slot, owner string, n sdui.Node) (int64, error) {
	if err := Validate(n); err != nil {
		return 0, err
	}
	tree, err := json.Marshal(n)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidTree, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if _, gone := s.dropped[projectID]; gone {
		return 0, ErrUnknownProject
	}
	key := slotKey{projectID, slot}
	s.revs[key]++
	view := SlotView{ProjectID: projectID, Slot: slot, Owner: owner, Rev: s.revs[key], Tree: tree}
	s.slots[key] = view
	s.notifyLocked(view)
	return view.Rev, nil
}

func (s *Store) Get(projectID string) []SlotView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewsLocked(projectID)
}

func (s *Store) ClearOwner(owner string) {
	s.clear(func(_ slotKey, view SlotView) bool { return view.Owner == owner })
}

func (s *Store) DropProject(projectID string) {
	s.mu.Lock()
	s.dropped[projectID] = struct{}{}
	s.mu.Unlock()
	s.clear(func(key slotKey, _ SlotView) bool { return key.project == projectID })
}

func (s *Store) Subscribe(projectID string) (*Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub := &Subscription{store: s, project: projectID, ready: make(chan struct{}, 1)}
	s.subs[sub] = struct{}{}
	for _, view := range s.viewsLocked(projectID) {
		sub.pushLocked(view)
	}
	return sub, nil
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
			ProjectID: key.project,
			Slot:      key.slot,
			Owner:     view.Owner,
			Rev:       s.revs[key],
			Tree:      s.empty,
		})
	}
	slices.SortFunc(cleared, func(a, b SlotView) int {
		return strings.Compare(a.ProjectID+"\x00"+a.Slot, b.ProjectID+"\x00"+b.Slot)
	})
	for _, view := range cleared {
		s.notifyLocked(view)
	}
}

func (s *Store) viewsLocked(projectID string) []SlotView {
	views := []SlotView{}
	for key, view := range s.slots {
		if key.project == projectID {
			views = append(views, view)
		}
	}
	slices.SortFunc(views, func(a, b SlotView) int { return strings.Compare(a.Slot, b.Slot) })
	return views
}

func (s *Store) notifyLocked(view SlotView) {
	for sub := range s.subs {
		if sub.project == view.ProjectID {
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
