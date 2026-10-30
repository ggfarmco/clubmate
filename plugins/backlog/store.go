package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
)

type store struct {
	h   *pluginsdk.Host
	mus sync.Map
}

func taskPrefix(projectID string) string {
	return "task/" + projectID + "/"
}

func taskKey(projectID string, id int) string {
	return fmt.Sprintf("%s%d", taskPrefix(projectID), id)
}

func seqKey(projectID string) string {
	return "seq/" + projectID
}

func (s *store) lock(projectID string) func() {
	mu, _ := s.mus.LoadOrStore(projectID, &sync.Mutex{})
	m := mu.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

func (s *store) load(ctx context.Context, projectID string, id int) (Task, error) {
	var t Task
	ok, err := s.h.StorageGet(ctx, taskKey(projectID, id), &t)
	if err != nil {
		return Task{}, err
	}
	if !ok {
		return Task{}, fmt.Errorf("%w: #%d", errTaskNotFound, id)
	}
	return t, nil
}

func (s *store) get(ctx context.Context, projectID string, id int) (Task, error) {
	defer s.lock(projectID)()
	return s.load(ctx, projectID, id)
}

func (s *store) list(ctx context.Context, projectID string) ([]Task, error) {
	defer s.lock(projectID)()
	keys, err := s.h.StorageList(ctx, taskPrefix(projectID))
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

func (s *store) add(ctx context.Context, projectID string, t Task) (Task, error) {
	defer s.lock(projectID)()
	var seq int
	if _, err := s.h.StorageGet(ctx, seqKey(projectID), &seq); err != nil {
		return Task{}, err
	}
	seq++
	now := time.Now().UTC()
	t.ID = seq
	t.CreatedAt, t.UpdatedAt = now, now
	if err := s.h.StorageSet(ctx, seqKey(projectID), seq); err != nil {
		return Task{}, err
	}
	if err := s.h.StorageSet(ctx, taskKey(projectID, t.ID), t); err != nil {
		return Task{}, err
	}
	return t, nil
}

func (s *store) mutate(ctx context.Context, projectID string, id int, fn func(*Task) error) (Task, error) {
	defer s.lock(projectID)()
	t, err := s.load(ctx, projectID, id)
	if err != nil {
		return Task{}, err
	}
	if err := fn(&t); err != nil {
		return Task{}, err
	}
	t.UpdatedAt = time.Now().UTC()
	if err := s.h.StorageSet(ctx, taskKey(projectID, id), t); err != nil {
		return Task{}, err
	}
	return t, nil
}

func (s *store) remove(ctx context.Context, projectID string, id int) error {
	defer s.lock(projectID)()
	return s.h.StorageDelete(ctx, taskKey(projectID, id))
}

func (s *store) dropProject(ctx context.Context, projectID string) error {
	defer s.lock(projectID)()
	keys, err := s.h.StorageList(ctx, taskPrefix(projectID))
	if err != nil {
		return err
	}
	var errs []error
	for _, k := range keys {
		if strings.HasPrefix(k, taskPrefix(projectID)) {
			errs = append(errs, s.h.StorageDelete(ctx, k))
		}
	}
	errs = append(errs, s.h.StorageDelete(ctx, seqKey(projectID)))
	return errors.Join(errs...)
}
