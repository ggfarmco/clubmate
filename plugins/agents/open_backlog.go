package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	backlogAddCmd  = "backlog.add"
	backlogTodo    = "todo"
	taskTitleRunes = 200

	openAfter    = 7 * 24 * time.Hour
	openAfterEnv = "CLUBMATE_AGENTS_OPEN_AFTER"
)

type backlogAddArgs struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type backlogTask struct {
	ID int `json:"id"`
}

func (a *app) ageOpenLoop(after time.Duration) {
	if after <= 0 {
		return
	}
	t := time.NewTicker(min(suspendTick, after/2))
	defer t.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-t.C:
		case <-a.openWake:
		}
		a.ageOpen(after)
	}
}

func (a *app) wakeOpenAging() {
	select {
	case a.openWake <- struct{}{}:
	default:
	}
}

func (a *app) ageOpen(after time.Duration) {
	now := time.Now()
	a.mu.Lock()
	var due []*session
	for id, s := range a.sessions {
		if rec := s.snapshot(); a.dropped[id] < dropRemoved && rec.Night == "" && rec.OpenCount > 0 && (rec.closed() || now.Sub(rec.OpenSince) >= after) {
			due = append(due, s)
		}
	}
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	for _, s := range due {
		if err := a.fileDue(ctx, s, now, after); err != nil {
			fmt.Fprintln(os.Stderr, "agents: age open items:", err)
		}
	}
}

func (a *app) fileDue(ctx context.Context, s *session, now time.Time, after time.Duration) error {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	if s.gone() {
		return nil
	}
	closed := s.snapshot().closed()
	var due []openItem
	for _, it := range s.openList() {
		if closed || now.Sub(it.At) >= after {
			due = append(due, it)
		}
	}
	tasks, err := a.fileItems(ctx, s, due)
	if len(tasks) > 0 {
		s.notice(movedTitle(len(tasks)), taskRefs(tasks))
	}
	return err
}

func (s *session) gone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleted
}

func (a *app) fileItems(ctx context.Context, s *session, items []openItem) ([]int, error) {
	rec := s.snapshot()
	if rec.Night != "" && len(items) > 0 {
		return nil, errNightBacklog
	}
	tasks := make([]int, 0, len(items))
	resolved := make([]openResolution, 0, len(items))
	var failure error
	for _, it := range items {
		id, err := a.fileItem(ctx, rec, it)
		if err != nil {
			failure = fmt.Errorf("file %q: %w", it.title(), err)
			break
		}
		tasks = append(tasks, id)
		resolved = append(resolved, openResolution{Item: it.ID, State: itemBacklog, Answer: it.Answer, Task: id})
	}
	if len(resolved) > 0 {
		s.resolveOpen(openChange{Resolved: resolved})
	}
	return tasks, failure
}

func (a *app) fileItem(ctx context.Context, rec record, it openItem) (int, error) {
	var task backlogTask
	args := backlogAddArgs{Title: clipRunes(firstLine(it.taskTitle()), taskTitleRunes), Description: it.taskText(rec), Status: backlogTodo}
	if err := a.host.RunCommand(ctx, backlogAddCmd, rec.Workspace, args, &task); err != nil {
		return 0, err
	}
	return task.ID, nil
}

func (it openItem) taskTitle() string {
	if it.Kind == itemOffer {
		return it.Label
	}
	return it.Question
}

func (it openItem) taskText(rec record) string {
	lines := []string{"> " + it.Quote, ""}
	if it.Description != "" {
		lines = append(lines, it.Description, "")
	}
	for _, o := range it.Options {
		lines = append(lines, "- **"+o.Label+"**: "+o.Description)
	}
	if len(it.Options) > 0 {
		lines = append(lines, "")
	}
	if it.Answer != "" {
		lines = append(lines, "Answer: "+it.Answer, "")
	}
	return strings.Join(append(lines, "From agent session "+rec.ID+" ("+firstLine(rec.Title)+")."), "\n")
}

func movedTitle(n int) string {
	if n == 1 {
		return "1 open item moved to the backlog"
	}
	return strconv.Itoa(n) + " open items moved to the backlog"
}

func taskRefs(tasks []int) string {
	refs := make([]string, 0, len(tasks))
	for _, id := range tasks {
		refs = append(refs, "#"+strconv.Itoa(id))
	}
	return strings.Join(refs, ", ")
}
