package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const taskBranchPrefix = "feat/"

var (
	errTaskSlug     = errors.New("not a valid task slug")
	errTaskEmpty    = errors.New("a task needs at least one repo")
	errTaskOwnRepo  = errors.New("a child corrects only the rule of its own repo in the orchestrator's task")
	errTaskRepoPath = errors.New("a task's repo needs an absolute path")
)

type taskRecord struct {
	Slug  string     `json:"slug"`
	Repos []taskRepo `json:"repos"`
}

type taskRepo struct {
	Path       string    `json:"path"`
	Order      int       `json:"order"`
	LandsBy    string    `json:"landsBy"`
	Source     string    `json:"source"`
	Name       string    `json:"name,omitempty"`
	State      string    `json:"state,omitempty"`
	For        string    `json:"for,omitempty"`
	Session    string    `json:"session,omitempty"`
	PR         string    `json:"pr,omitempty"`
	Tail       string    `json:"tail,omitempty"`
	Request    string    `json:"request,omitempty"`
	Capped     bool      `json:"capped,omitempty"`
	Since      time.Time `json:"since,omitzero"`
	Landing    bool      `json:"landing,omitempty"`
	LandPrompt string    `json:"landPrompt,omitempty"`
	Finished   bool      `json:"finished,omitempty"`
	ReportTurn int       `json:"reportTurn,omitempty"`
	LandTurn   int       `json:"landTurn,omitempty"`
	TakenTurn  int       `json:"takenTurn,omitempty"`
	Tip        string    `json:"tip,omitempty"`
	Base       string    `json:"base,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	Into       string    `json:"into,omitempty"`
	Commits    int       `json:"commits,omitempty"`
	FixAsked   bool      `json:"fixAsked,omitempty"`
}

func (a *app) taskCommand(ctx context.Context, cmd pluginsdk.CommandRun, args taskRecord) (any, error) {
	caller, err := a.sessionCaller(cmd)
	if err != nil {
		return nil, err
	}
	if err := args.validate(); err != nil {
		return nil, err
	}
	a.landMu.Lock()
	defer a.landMu.Unlock()
	owner := a.topLevel(caller)
	if owner == caller {
		next := args.declared(ctx, owner.snapshot().Task)
		owner.putTask(next)
		a.saveNow(ctx, owner)
		a.reconcileTasks(ctx)
		return next, nil
	}
	next, err := owner.correctRule(caller.snapshot(), args)
	if err != nil {
		return nil, err
	}
	a.saveNow(ctx, owner)
	a.reconcileTasks(ctx)
	return next, nil
}

func (t taskRecord) declared(ctx context.Context, old *taskRecord) *taskRecord {
	next := &taskRecord{Slug: t.Slug, Repos: make([]taskRepo, 0, len(t.Repos))}
	for _, repo := range t.Repos {
		r := taskRepo{Name: filepath.Base(repo.Path), State: landWaits}
		if old != nil {
			if i := old.repoIndex(repo.Path); i >= 0 {
				r = old.Repos[i]
			}
		}
		r.Path, r.Order, r.LandsBy, r.Source = repo.Path, repo.Order, repo.LandsBy, repo.Source
		if r.Into == "" {
			r.Into, _ = gitrepo.DefaultBranch(ctx, r.Path)
		}
		next.Repos = append(next.Repos, r)
	}
	slices.SortStableFunc(next.Repos, func(x, y taskRepo) int { return cmp.Compare(x.Order, y.Order) })
	return next
}

func (t taskRecord) validate() error {
	if !gitrepo.ValidBranchName(taskBranchPrefix + t.Slug) {
		return fmt.Errorf("%w: %s", errTaskSlug, t.Slug)
	}
	if len(t.Repos) == 0 {
		return errTaskEmpty
	}
	for i, repo := range t.Repos {
		if !filepath.IsAbs(repo.Path) {
			return fmt.Errorf("%w: %s", errTaskRepoPath, repo.Path)
		}
		if !validLandsBy(repo.LandsBy) {
			return fmt.Errorf("%w: %s", errLandsBy, repo.LandsBy)
		}
		t.Repos[i].Source = clipRunes(repo.Source, connectTextRunes)
	}
	return nil
}

func (s *session) correctRule(child record, args taskRecord) (taskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.rec.Task
	if task == nil || task.Slug != args.Slug || len(args.Repos) != 1 || child.RepoPath == "" || args.Repos[0].Path != child.RepoPath {
		return taskRecord{}, errTaskOwnRepo
	}
	i := slices.IndexFunc(task.Repos, func(r taskRepo) bool { return r.Path == child.RepoPath })
	if i < 0 {
		return taskRecord{}, errTaskOwnRepo
	}
	next := taskRecord{Slug: task.Slug, Repos: slices.Clone(task.Repos)}
	next.Repos[i].LandsBy, next.Repos[i].Source = args.Repos[0].LandsBy, args.Repos[0].Source
	s.rec.Task = &next
	return next, nil
}
