package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/docrender"
)

const (
	backlogListCmd    = "backlog.list"
	backlogRevealCmd  = "backlog.reveal"
	taskNotFound      = "task not found"
	taskLookupTimeout = 10 * time.Second
)

type taskLookups struct {
	mu      sync.Mutex
	pending map[string]map[*session]bool
	running map[string]bool
}

type backlogPage struct {
	Tasks []struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	} `json:"tasks"`
}

func (a *app) wantTasks(workspaceID string, s *session) {
	t := &a.taskLookups
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending == nil {
		t.pending, t.running = map[string]map[*session]bool{}, map[string]bool{}
	}
	if t.pending[workspaceID] == nil {
		t.pending[workspaceID] = map[*session]bool{}
	}
	t.pending[workspaceID][s] = true
	if !t.running[workspaceID] {
		t.running[workspaceID] = true
		go a.lookupTasks(workspaceID)
	}
}

func (a *app) lookupTasks(workspaceID string) {
	t := &a.taskLookups
	for {
		t.mu.Lock()
		batch := t.pending[workspaceID]
		delete(t.pending, workspaceID)
		if len(batch) == 0 {
			delete(t.running, workspaceID)
			t.mu.Unlock()
			return
		}
		t.mu.Unlock()
		titles, err := a.taskTitles(workspaceID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents: task links:", err)
			t.mu.Lock()
			delete(t.running, workspaceID)
			t.mu.Unlock()
			return
		}
		for s := range batch {
			s.setTaskTitles(titles)
		}
	}
}

func (a *app) taskTitles(workspaceID string) (map[int]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), taskLookupTimeout)
	defer cancel()
	var page backlogPage
	if err := a.host.RunCommand(ctx, backlogListCmd, workspaceID, map[string]any{"since": "", "slim": true}, &page); err != nil {
		return nil, err
	}
	titles := make(map[int]string, len(page.Tasks))
	for _, task := range page.Tasks {
		titles[task.ID] = task.Title
	}
	return titles, nil
}

func (a *app) revealTask(ctx context.Context, workspaceID string, id int) error {
	err := a.host.RunCommand(ctx, backlogRevealCmd, workspaceID, map[string]int{"id": id}, nil)
	if err != nil && strings.Contains(err.Error(), taskNotFound) {
		return nil
	}
	return err
}

func (s *session) setTaskTitles(titles map[int]string) {
	s.mu.Lock()
	known := map[int]string{}
	changed := false
	for _, en := range s.entries {
		for _, id := range en.taskIDs {
			if title, ok := titles[id]; ok {
				known[id] = title
			}
		}
	}
	s.taskTitles = known
	for _, en := range s.entries {
		if len(en.taskIDs) > 0 && s.linkTasksLocked(en) {
			changed = true
		}
	}
	deleted := s.deleted
	s.mu.Unlock()
	if changed && !deleted {
		s.changed(changeLinks, false)
	}
}

func (s *session) findTasksLocked(en *entry) {
	en.taskIDs = docrender.TaskIDs([]byte(en.Text))
	if len(en.taskIDs) > 0 {
		s.tasksWanted = true
	}
	s.linkTasksLocked(en)
}

func (s *session) linkTasksLocked(en *entry) bool {
	var links map[string]string
	for _, id := range en.taskIDs {
		if title, ok := s.taskTitles[id]; ok {
			if links == nil {
				links = map[string]string{}
			}
			links[strconv.Itoa(id)] = title
		}
	}
	if maps.Equal(links, en.TaskLinks) {
		return false
	}
	en.TaskLinks, en.node = links, nil
	return true
}

func (s *session) askTasksLocked() {
	if s.tasksWanted && s.wantTasks != nil {
		s.tasksWanted = false
		s.wantTasks()
	}
}
