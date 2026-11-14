package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/delta"
)

type storage interface {
	StorageList(ctx context.Context, prefix string) ([]string, error)
	StorageGet(ctx context.Context, key string, out any) (bool, error)
	StorageSet(ctx context.Context, key string, v any) error
	StorageDelete(ctx context.Context, key string) error
	StorageDeletePrefix(ctx context.Context, prefix string) error
}

type store struct {
	h        storage
	mus      sync.Map
	starting sync.Map
	cached   sync.Map
	trackers sync.Map
}

type taskPage struct {
	Tasks   []Task `json:"tasks"`
	Removed []int  `json:"removed"`
	Order   []int  `json:"order,omitempty"`
	Cursor  string `json:"cursor"`
	Delta   bool   `json:"delta"`
}

var errTaskChanged = errors.New("the task changed since you saw it")

func taskPrefix(workspaceID string) string {
	return "task/" + workspaceID + "/"
}

func taskKey(workspaceID string, id int) string {
	return fmt.Sprintf("%s%d", taskPrefix(workspaceID), id)
}

const orderKey = "order"

func trackerName(workspaceID string) string {
	return "tasks/" + workspaceID
}

func seqKey(workspaceID string) string {
	return "seq/" + workspaceID
}

func (s *store) lock(workspaceID string) func() {
	mu, _ := s.mus.LoadOrStore(workspaceID, &sync.Mutex{})
	m := mu.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

func (s *store) write(workspaceID string) func() {
	unlock := s.lock(workspaceID)
	s.cached.Delete(workspaceID)
	return unlock
}

func (s *store) load(ctx context.Context, workspaceID string, id int) (Task, error) {
	var t Task
	ok, err := s.h.StorageGet(ctx, taskKey(workspaceID, id), &t)
	if err != nil {
		return Task{}, err
	}
	if !ok {
		return Task{}, fmt.Errorf("%w: #%d", errTaskNotFound, id)
	}
	return t, nil
}

func (s *store) get(ctx context.Context, workspaceID string, id int) (Task, error) {
	defer s.lock(workspaceID)()
	return s.load(ctx, workspaceID, id)
}

func (s *store) reserve(ctx context.Context, workspaceID string, id int, seen string, check func(*Task) error) (Task, func(), error) {
	defer s.lock(workspaceID)()
	key := taskKey(workspaceID, id)
	if err := s.fresh(ctx, workspaceID, id, seen); err != nil {
		return Task{}, nil, err
	}
	if _, busy := s.starting.Load(key); busy {
		return Task{}, nil, fmt.Errorf("task #%d is already getting an agent", id)
	}
	t, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return Task{}, nil, err
	}
	if err := check(&t); err != nil {
		return Task{}, nil, err
	}
	s.starting.Store(key, struct{}{})
	return t, func() { s.starting.Delete(key) }, nil
}

func (s *store) list(ctx context.Context, workspaceID string) ([]Task, error) {
	defer s.lock(workspaceID)()
	return s.listLocked(ctx, workspaceID)
}

func (s *store) snapshot(ctx context.Context, workspaceID string) ([]Task, error) {
	defer s.lock(workspaceID)()
	if tasks, ok := s.cached.Load(workspaceID); ok {
		return tasks.([]Task), nil
	}
	tasks, err := s.listLocked(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	s.cached.Store(workspaceID, tasks)
	return tasks, nil
}

func (s *store) listLocked(ctx context.Context, workspaceID string) ([]Task, error) {
	keys, err := s.h.StorageList(ctx, taskPrefix(workspaceID))
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(keys))
	for _, k := range keys {
		var t Task
		ok, err := s.h.StorageGet(ctx, k, &t)
		if err != nil {
			return nil, err
		}
		if ok {
			tasks = append(tasks, t)
		}
	}
	sortTasks(tasks)
	return tasks, nil
}

func (s *store) add(ctx context.Context, workspaceID string, t Task) (Task, string, error) {
	defer s.write(workspaceID)()
	tr := s.tracker(ctx, workspaceID)
	var seq int
	if _, err := s.h.StorageGet(ctx, seqKey(workspaceID), &seq); err != nil {
		return Task{}, "", err
	}
	seq++
	now := time.Now().UTC()
	t.ID = seq
	t.CreatedAt, t.UpdatedAt = now, now
	t.move(OwnerMove{Owner: userOwner, At: now, Why: whyCreated})
	if err := s.checkLinks(ctx, workspaceID, t.ID, t.Links); err != nil {
		return Task{}, "", err
	}
	others, err := s.linkedChanges(ctx, workspaceID, t.ID, nil, t.Links)
	if err != nil {
		return Task{}, "", err
	}
	if err := s.h.StorageSet(ctx, seqKey(workspaceID), seq); err != nil {
		return Task{}, "", err
	}
	err = s.commit(ctx, workspaceID, tr, append(others, t)...)
	return t, tr.Cursor(), err
}

func (s *store) mutate(ctx context.Context, workspaceID string, id int, seen string, fn func(*Task) error) (Task, string, error) {
	defer s.write(workspaceID)()
	tr := s.tracker(ctx, workspaceID)
	if err := s.fresh(ctx, workspaceID, id, seen); err != nil {
		return Task{}, "", err
	}
	t, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return Task{}, "", err
	}
	old := slices.Clone(t.Links)
	if err := fn(&t); err != nil {
		return Task{}, "", err
	}
	if err := s.checkLinks(ctx, workspaceID, id, t.Links); err != nil {
		return Task{}, "", err
	}
	t.UpdatedAt = time.Now().UTC()
	others, err := s.linkedChanges(ctx, workspaceID, id, old, t.Links)
	if err != nil {
		return Task{}, "", err
	}
	err = s.commit(ctx, workspaceID, tr, append(others, t)...)
	return t, tr.Cursor(), err
}

func (s *store) remove(ctx context.Context, workspaceID string, id int) error {
	defer s.write(workspaceID)()
	tr := s.tracker(ctx, workspaceID)
	t, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	others, err := s.linkedChanges(ctx, workspaceID, id, t.Links, nil)
	if err != nil {
		return err
	}
	if err := s.h.StorageDelete(ctx, taskKey(workspaceID, id)); err != nil {
		return err
	}
	tr.Drop(strconv.Itoa(id))
	return s.commit(ctx, workspaceID, tr, others...)
}

func (s *store) since(ctx context.Context, workspaceID, cursor string) (taskPage, error) {
	defer s.lock(workspaceID)()
	tr := s.tracker(ctx, workspaceID)
	tasks, err := s.listLocked(ctx, workspaceID)
	if err != nil {
		return taskPage{}, err
	}
	order := make([]int, 0, len(tasks))
	for _, t := range tasks {
		order = append(order, t.ID)
	}
	if tr.Touch(orderKey, order) {
		logError("save order of "+workspaceID, tr.Save(ctx, s.h))
	}
	p := taskPage{Tasks: tasks, Removed: []int{}, Order: order, Cursor: tr.Cursor()}
	changed, removed, full := tr.Since(cursor)
	if full {
		return p, nil
	}
	p.Delta = true
	if _, found := slices.BinarySearch(changed, orderKey); !found {
		p.Order = nil
	}
	p.Tasks = slices.DeleteFunc(tasks, func(t Task) bool {
		_, found := slices.BinarySearch(changed, strconv.Itoa(t.ID))
		return !found
	})
	for _, key := range removed {
		if id, err := strconv.Atoi(key); err == nil {
			p.Removed = append(p.Removed, id)
		}
	}
	return p, nil
}

func (s *store) tracker(ctx context.Context, workspaceID string) *delta.Tracker {
	if tr, ok := s.trackers.Load(workspaceID); ok {
		return tr.(*delta.Tracker)
	}
	tr, err := delta.Load(ctx, s.h, trackerName(workspaceID))
	if err != nil {
		logError("load delta of "+workspaceID, err)
		tr = delta.New(trackerName(workspaceID))
	}
	s.trackers.Store(workspaceID, tr)
	return tr
}

func (s *store) fresh(ctx context.Context, workspaceID string, id int, seen string) error {
	if seen == "" {
		return nil
	}
	if s.tracker(ctx, workspaceID).Stale(strconv.Itoa(id), seen) {
		return pluginsdk.WithReason(contract.ReasonStale, fmt.Errorf("%w: #%d", errTaskChanged, id))
	}
	return nil
}

func (s *store) commit(ctx context.Context, workspaceID string, tr *delta.Tracker, tasks ...Task) error {
	for _, t := range tasks {
		tr.Stamp(strconv.Itoa(t.ID))
	}
	logError("save delta of "+workspaceID, tr.Save(ctx, s.h))
	var errs []error
	for _, t := range tasks {
		errs = append(errs, s.h.StorageSet(ctx, taskKey(workspaceID, t.ID), t))
	}
	return errors.Join(errs...)
}

func (s *store) checkLinks(ctx context.Context, workspaceID string, id int, links []Link) error {
	for _, l := range links {
		if _, err := s.load(ctx, workspaceID, l.Task); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) linkedChanges(ctx context.Context, workspaceID string, id int, old, cur []Link) ([]Task, error) {
	want := map[int]string{}
	for _, l := range old {
		want[l.Task] = ""
	}
	for _, l := range cur {
		want[l.Task] = inverse(l.Kind)
	}
	var out []Task
	for _, task := range slices.Sorted(maps.Keys(want)) {
		kind := want[task]
		if kind != "" && slices.Contains(old, Link{Kind: inverse(kind), Task: task}) {
			continue
		}
		other, err := s.load(ctx, workspaceID, task)
		if errors.Is(err, errTaskNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		other.Links = slices.DeleteFunc(other.Links, func(l Link) bool { return l.Task == id })
		if kind != "" {
			other.Links = append(other.Links, Link{Kind: kind, Task: id})
		}
		other.UpdatedAt = time.Now().UTC()
		out = append(out, other)
	}
	return out, nil
}

func (s *store) dropWorkspace(ctx context.Context, workspaceID string) error {
	defer s.write(workspaceID)()
	s.trackers.Delete(workspaceID)
	return errors.Join(s.h.StorageDeletePrefix(ctx, taskPrefix(workspaceID)), s.h.StorageDelete(ctx, seqKey(workspaceID)), delta.Delete(ctx, s.h, trackerName(workspaceID)))
}
