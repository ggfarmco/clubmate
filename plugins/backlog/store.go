package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
)

type store struct {
	h   *pluginsdk.Host
	mus sync.Map
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
	if err := s.h.StorageSet(ctx, seqKey(workspaceID), seq); err != nil {
		return Task{}, err
	}
	if err := s.h.StorageSet(ctx, taskKey(workspaceID, t.ID), t); err != nil {
		return Task{}, err
	}
	return t, nil
}

func (s *store) mutate(ctx context.Context, workspaceID string, id int, fn func(*Task) error) (Task, error) {
	defer s.lock(workspaceID)()
	t, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return Task{}, err
	}
	if err := fn(&t); err != nil {
		return Task{}, err
	}
	t.UpdatedAt = time.Now().UTC()
	if err := s.h.StorageSet(ctx, taskKey(workspaceID, id), t); err != nil {
		return Task{}, err
	}
	return t, nil
}

func (s *store) remove(ctx context.Context, workspaceID string, id int) error {
	defer s.lock(workspaceID)()
	return s.h.StorageDelete(ctx, taskKey(workspaceID, id))
}

func (s *store) dropWorkspace(ctx context.Context, workspaceID string) error {
	defer s.lock(workspaceID)()
	return errors.Join(s.h.StorageDeletePrefix(ctx, taskPrefix(workspaceID)), s.h.StorageDelete(ctx, seqKey(workspaceID)))
}
