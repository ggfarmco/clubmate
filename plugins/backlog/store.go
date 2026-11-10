package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
)

type store struct {
	h        *pluginsdk.Host
	mus      sync.Map
	starting sync.Map
}

func taskPrefix(workspaceID string) string {
	return "task/" + workspaceID + "/"
}

func taskKey(workspaceID string, id int) string {
	return fmt.Sprintf("%s%d", taskPrefix(workspaceID), id)
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

func (s *store) reserve(ctx context.Context, workspaceID string, id int, check func(*Task) error) (Task, func(), error) {
	defer s.lock(workspaceID)()
	key := taskKey(workspaceID, id)
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

func (s *store) add(ctx context.Context, workspaceID string, t Task) (Task, error) {
	defer s.lock(workspaceID)()
	var seq int
	if _, err := s.h.StorageGet(ctx, seqKey(workspaceID), &seq); err != nil {
		return Task{}, err
	}
	seq++
	now := time.Now().UTC()
	t.ID = seq
	t.CreatedAt, t.UpdatedAt = now, now
	t.move(OwnerMove{Owner: userOwner, At: now, Why: whyCreated})
	if err := s.checkLinks(ctx, workspaceID, t.ID, t.Links); err != nil {
		return Task{}, err
	}
	if err := s.h.StorageSet(ctx, seqKey(workspaceID), seq); err != nil {
		return Task{}, err
	}
	if err := s.h.StorageSet(ctx, taskKey(workspaceID, t.ID), t); err != nil {
		return Task{}, err
	}
	return t, s.syncLinks(ctx, workspaceID, t.ID, nil, t.Links)
}

func (s *store) mutate(ctx context.Context, workspaceID string, id int, fn func(*Task) error) (Task, error) {
	defer s.lock(workspaceID)()
	t, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return Task{}, err
	}
	old := slices.Clone(t.Links)
	if err := fn(&t); err != nil {
		return Task{}, err
	}
	if err := s.checkLinks(ctx, workspaceID, id, t.Links); err != nil {
		return Task{}, err
	}
	t.UpdatedAt = time.Now().UTC()
	if err := s.h.StorageSet(ctx, taskKey(workspaceID, id), t); err != nil {
		return Task{}, err
	}
	return t, s.syncLinks(ctx, workspaceID, id, old, t.Links)
}

func (s *store) remove(ctx context.Context, workspaceID string, id int) error {
	defer s.lock(workspaceID)()
	t, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if err := s.h.StorageDelete(ctx, taskKey(workspaceID, id)); err != nil {
		return err
	}
	return s.syncLinks(ctx, workspaceID, id, t.Links, nil)
}

func (s *store) checkLinks(ctx context.Context, workspaceID string, id int, links []Link) error {
	for _, l := range links {
		if _, err := s.load(ctx, workspaceID, l.Task); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) syncLinks(ctx context.Context, workspaceID string, id int, old, cur []Link) error {
	want := map[int]string{}
	for _, l := range old {
		want[l.Task] = ""
	}
	for _, l := range cur {
		want[l.Task] = inverse(l.Kind)
	}
	var errs []error
	for task, kind := range want {
		if kind != "" && slices.Contains(old, Link{Kind: inverse(kind), Task: task}) {
			continue
		}
		other, err := s.load(ctx, workspaceID, task)
		if errors.Is(err, errTaskNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		other.Links = slices.DeleteFunc(other.Links, func(l Link) bool { return l.Task == id })
		if kind != "" {
			other.Links = append(other.Links, Link{Kind: kind, Task: id})
		}
		other.UpdatedAt = time.Now().UTC()
		errs = append(errs, s.h.StorageSet(ctx, taskKey(workspaceID, task), other))
	}
	return errors.Join(errs...)
}

func (s *store) dropWorkspace(ctx context.Context, workspaceID string) error {
	defer s.lock(workspaceID)()
	return errors.Join(s.h.StorageDeletePrefix(ctx, taskPrefix(workspaceID)), s.h.StorageDelete(ctx, seqKey(workspaceID)))
}
