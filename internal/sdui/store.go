package sdui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"clubmate/pkg/sdui"
)

const subscriptionBuffer = 64

var (
	ErrSlowSubscriber = errors.New("slow subscriber dropped")
)

type SlotView struct {
	ProjectID string    `json:"projectId"`
	Slot      string    `json:"slot"`
	Owner     string    `json:"owner"`
	Rev       int64     `json:"rev"`
	Tree      sdui.Node `json:"tree"`
}

type slotKey struct {
	project, slot string
}

type Store struct {
	mu    sync.Mutex
	slots map[slotKey]SlotView
	revs  map[slotKey]int64
	subs  map[*Subscription]struct{}
}

type Subscription struct {
	store   *Store
	project string
	out     chan SlotView
	ended   bool
	err     error
}

func NewStore() *Store {
	return &Store{
		slots: map[slotKey]SlotView{},
		revs:  map[slotKey]int64{},
		subs:  map[*Subscription]struct{}{},
	}
}

func (s *Store) Render(ctx context.Context, projectID, slot, owner string, n sdui.Node) (int64, error) {
	if err := Validate(n); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	key := slotKey{projectID, slot}
	s.revs[key]++
	view := SlotView{ProjectID: projectID, Slot: slot, Owner: owner, Rev: s.revs[key], Tree: n}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	var cleared []SlotView
	for key, view := range s.slots {
		if view.Owner != owner {
			continue
		}
		delete(s.slots, key)
		s.revs[key]++
		cleared = append(cleared, SlotView{
			ProjectID: key.project,
			Slot:      key.slot,
			Owner:     owner,
			Rev:       s.revs[key],
			Tree:      sdui.Empty(""),
		})
	}
	slices.SortFunc(cleared, func(a, b SlotView) int {
		return strings.Compare(a.ProjectID+"\x00"+a.Slot, b.ProjectID+"\x00"+b.Slot)
	})
	for _, view := range cleared {
		s.notifyLocked(view)
	}
}

func (s *Store) Subscribe(projectID string) (*Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub := &Subscription{store: s, project: projectID, out: make(chan SlotView, subscriptionBuffer)}
	s.subs[sub] = struct{}{}
	for _, view := range s.viewsLocked(projectID) {
		if !sub.sendLocked(view) {
			break
		}
	}
	return sub, nil
}

func (sub *Subscription) C() <-chan SlotView {
	return sub.out
}

func (sub *Subscription) Err() error {
	sub.store.mu.Lock()
	defer sub.store.mu.Unlock()
	return sub.err
}

func (sub *Subscription) Close() {
	sub.store.mu.Lock()
	defer sub.store.mu.Unlock()
	sub.endLocked(nil)
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
			sub.sendLocked(view)
		}
	}
}

func (sub *Subscription) sendLocked(view SlotView) bool {
	select {
	case sub.out <- view:
		return true
	default:
		sub.endLocked(ErrSlowSubscriber)
		return false
	}
}

func (sub *Subscription) endLocked(err error) {
	if sub.ended {
		return
	}
	sub.ended = true
	sub.err = err
	delete(sub.store.subs, sub)
	close(sub.out)
}
