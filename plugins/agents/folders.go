package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

var (
	errNotFolder  = errors.New("not a folder of this workspace")
	errFolderUser = errors.New("only the user removes a folder")

	errFolderLeaving = errors.New("this folder is being removed")
)

const (
	cronJobsInCmd    = "cron.jobs-in"
	cronPauseCmd     = "cron.pause"
	cronResumeCmd    = "cron.resume"
	worktreeStateCmd = "git.worktree-state"
	cronPausedWhy    = "%s left %s"
	statedWorktrees  = 5
	cronResumeLimit  = 10 * time.Second
)

type folderSession struct {
	Session  string `json:"session"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Worktree string `json:"worktree"`
}

type folderWorktree struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Sessions int    `json:"sessions,omitempty"`
	Read     bool   `json:"read,omitempty"`
	Unread   bool   `json:"unread,omitempty"`
	Dirty    int    `json:"dirty,omitempty"`
	Unpushed int    `json:"unpushed,omitempty"`
	Ignored  int    `json:"ignored,omitempty"`
	Orphans  int    `json:"orphans,omitempty"`
	Nested   int    `json:"nested,omitempty"`
	Tip      string `json:"tip,omitempty"`
	Merged   bool   `json:"merged,omitempty"`
	Landed   bool   `json:"landed,omitempty"`
	Remote   bool   `json:"remote,omitempty"`
	Locked   bool   `json:"locked,omitempty"`
	LockWhy  string `json:"lockWhy,omitempty"`
}

type folderJob struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	In   string `json:"in,omitempty"`
}

type folderDrop struct {
	jobs    []folderJob
	cronErr error
	ticked  []folderWorktree
}

type folderRemoveResult struct {
	Removed  bool            `json:"removed"`
	Blockers []folderSession `json:"blockers,omitempty"`
	Tasks    []string        `json:"tasks,omitempty"`
	Last     bool            `json:"last,omitempty"`
	Paused   []folderJob     `json:"paused,omitempty"`
	Kept     []string        `json:"kept,omitempty"`
	Held     string          `json:"held,omitempty"`
}

type folderImpactResult struct {
	Name          string           `json:"name"`
	Workspace     string           `json:"workspace"`
	Path          string           `json:"path"`
	Sessions      []folderSession  `json:"sessions"`
	Worktrees     []folderWorktree `json:"worktrees"`
	Graphs        []string         `json:"graphs"`
	Hooks         []string         `json:"hooks"`
	Instructions  []string         `json:"instructions"`
	Skills        []string         `json:"skills"`
	TrustedGraphs []string         `json:"trustedGraphs"`
	TrustedHooks  bool             `json:"trustedHooks"`
	CronJobs      []folderJob      `json:"cronJobs"`
	CronUnread    bool             `json:"cronUnread,omitempty"`
	Blockers      []folderSession  `json:"blockers"`
	Tasks         []string         `json:"tasks"`
	Last          bool             `json:"last"`
}

type worktreeRef struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Repo   string `json:"repo,omitempty"`
	First  bool   `json:"first,omitempty"`
}

type worktreeState struct {
	Path     string `json:"path"`
	Dirty    int    `json:"dirty"`
	Unpushed int    `json:"unpushed"`
	Ignored  int    `json:"ignored"`
	Orphans  int    `json:"orphans"`
	Nested   int    `json:"nested"`
	Tip      string `json:"tip"`
	Merged   bool   `json:"merged"`
	Landed   bool   `json:"landed"`
	Remote   bool   `json:"remote"`
	Locked   bool   `json:"locked"`
	LockWhy  string `json:"lockWhy"`
	Error    string `json:"error"`
}

func (a *app) folderRemoveCommand(ctx context.Context, cmd pluginsdk.CommandRun, args pathArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errFolderUser
	}
	return a.removeFolder(ctx, cmd.WorkspaceID, args.Path)
}

func (a *app) removeFolder(ctx context.Context, workspaceID, path string) (folderRemoveResult, error) {
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return folderRemoveResult{}, err
	}
	folder, ok := folderIn(w, path)
	if !ok {
		return folderRemoveResult{}, fmt.Errorf("%w: %s", errNotFolder, path)
	}
	jobs, err := a.folderJobs(ctx, w, folder)
	res, err := a.dropFolder(ctx, w, folder, folderDrop{jobs: jobs, cronErr: err})
	if !res.Removed && (len(res.Blockers) > 0 || len(res.Tasks) > 0) {
		res.Held = fmt.Sprintf(folderHeld, holdWords(folderHolds{blockers: res.Blockers, tasks: res.Tasks}))
	}
	return res, err
}

func (a *app) dropFolder(ctx context.Context, w pluginsdk.Workspace, folder string, drop folderDrop) (folderRemoveResult, error) {
	dirs, refused := a.claimFolder(ctx, w, folder, drop.ticked)
	if dirs == nil {
		return refused, nil
	}
	defer a.releaseFolder(dirs)
	if drop.cronErr != nil {
		return folderRemoveResult{}, errCronGone
	}
	if err := a.leaveFolder(ctx, w, folder, drop.jobs); err != nil {
		return folderRemoveResult{}, err
	}
	return folderRemoveResult{Removed: true, Paused: drop.jobs, Kept: a.removeSheetWorktrees(ctx, w.ID, folder, drop.ticked)}, nil
}

func (a *app) claimFolder(ctx context.Context, w pluginsdk.Workspace, folder string, ticked []folderWorktree) ([]string, folderRemoveResult) {
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	dirs := holdDirs(folderDirs(ctx, folder), ticked)
	blockers := a.blockersIn(dirs)
	tasks := a.taskHolds(w.ID, folder, ticked)
	last := len(w.Paths()) == 1
	if len(blockers) > 0 || len(tasks) > 0 || last {
		return nil, folderRemoveResult{Blockers: blockers, Tasks: tasks, Last: last}
	}
	if a.leaving == nil {
		a.leaving = map[string]int{}
	}
	for _, dir := range dirs {
		a.leaving[dir]++
	}
	return dirs, folderRemoveResult{}
}

func (a *app) releaseFolder(dirs []string) {
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	for _, dir := range dirs {
		a.leaving[dir]--
		if a.leaving[dir] == 0 {
			delete(a.leaving, dir)
		}
	}
}

func (a *app) refuseLeaving(worktree string) error {
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	if a.leavingLocked(worktree) {
		return errFolderLeaving
	}
	return nil
}

func (a *app) leavingLocked(worktree string) bool {
	path := gitrepo.Canonical(worktree)
	for dir := range a.leaving {
		if gitrepo.Within(path, dir) {
			return true
		}
	}
	return false
}

func (a *app) leaveFolder(ctx context.Context, w pluginsdk.Workspace, folder string, jobs []folderJob) error {
	if err := a.pauseJobs(ctx, w.ID, fmt.Sprintf(cronPausedWhy, filepath.Base(folder), w.Name), jobs); err != nil {
		return err
	}
	if _, err := a.host.RemoveFolder(ctx, w.ID, folder); err != nil {
		return errors.Join(err, a.resumeJobs(ctx, w.ID, jobs))
	}
	return nil
}

func (a *app) pauseJobs(ctx context.Context, workspaceID, reason string, jobs []folderJob) error {
	paused := make([]folderJob, 0, len(jobs))
	for _, job := range jobs {
		if err := a.host.RunCommand(ctx, cronPauseCmd, workspaceID, map[string]string{"id": job.ID, "reason": reason}, nil); err != nil {
			return errors.Join(fmt.Errorf("pause %s: %w", job.Name, err), a.resumeJobs(ctx, workspaceID, paused))
		}
		paused = append(paused, job)
	}
	return nil
}

func (a *app) resumeJobs(ctx context.Context, workspaceID string, jobs []folderJob) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cronResumeLimit)
	defer cancel()
	var errs []error
	for _, job := range jobs {
		if err := a.host.RunCommand(ctx, cronResumeCmd, workspaceID, map[string]string{"id": job.ID}, nil); err != nil {
			errs = append(errs, fmt.Errorf("%s stays paused: %w", job.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (a *app) folderImpactCommand(ctx context.Context, cmd pluginsdk.CommandRun, args pathArgs) (any, error) {
	return a.folderImpact(ctx, cmd.WorkspaceID, args.Path)
}

func (a *app) folderImpact(ctx context.Context, workspaceID, path string) (folderImpactResult, error) {
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return folderImpactResult{}, err
	}
	folder, ok := folderIn(w, path)
	if !ok {
		return folderImpactResult{}, fmt.Errorf("%w: %s", errNotFolder, path)
	}
	out := folderImpactResult{
		Name: filepath.Base(folder), Workspace: w.Name, Path: folder,
		Sessions: []folderSession{}, Worktrees: []folderWorktree{}, Graphs: []string{}, Hooks: []string{}, Instructions: []string{},
		Skills: []string{}, TrustedGraphs: []string{}, CronJobs: []folderJob{}, Blockers: []folderSession{}, Tasks: []string{},
		Last: len(w.Paths()) == 1,
	}
	dirs := folderDirs(ctx, folder)
	for _, s := range a.sessionsIn(workspaceID, dirs) {
		out.Sessions = append(out.Sessions, folderSessionOf(s.snapshot()))
	}
	out.Blockers = append(out.Blockers, a.blockersIn(dirs)...)
	out.Tasks = append(out.Tasks, a.tasksOn(workspaceID, folder)...)
	out.Worktrees = a.worktreesOf(ctx, workspaceID, folder)
	var facts repoFacts
	if err := a.host.RunCommand(ctx, repoFactsCmd, workspaceID, map[string]string{"path": folder}, &facts); err == nil {
		out.Graphs, out.Hooks = append(out.Graphs, facts.Brings.Graphs...), append(out.Hooks, facts.Brings.Hooks...)
		out.Instructions, out.Skills = append(out.Instructions, facts.Brings.Instructions...), append(out.Skills, facts.Brings.Skills...)
		out.TrustedGraphs, out.TrustedHooks = append(out.TrustedGraphs, facts.Trusted.Graphs...), facts.Trusted.Hooks && len(facts.Brings.Hooks) > 0
	}
	jobs, err := a.folderJobs(ctx, w, folder)
	out.CronJobs, out.CronUnread = append(out.CronJobs, jobs...), err != nil
	return out, nil
}

func (a *app) worktreesOf(ctx context.Context, workspaceID, folder string) []folderWorktree {
	out := []folderWorktree{}
	f, err := gitrepo.OpenFolder(ctx, folder)
	if err != nil {
		return out
	}
	sessions := a.allSessions()
	defaultName := f.DefaultName(ctx)
	for _, t := range f.Trees[1:] {
		if t.Bare || t.Real == f.Checkout.Real || f.Primary(defaultName, t) {
			continue
		}
		wt := folderWorktree{Path: t.Path, Branch: t.Branch}
		for _, s := range sessions {
			if rec := s.snapshot(); !rec.closed() && gitrepo.Within(gitrepo.Canonical(rec.Worktree), t.Real) {
				wt.Sessions++
			}
		}
		out = append(out, wt)
	}
	slices.SortStableFunc(out, func(x, y folderWorktree) int { return cmp.Compare(y.Sessions, x.Sessions) })
	a.readWorktrees(ctx, workspaceID, out[:min(len(out), statedWorktrees)])
	return out
}

func (a *app) readWorktrees(ctx context.Context, workspaceID string, worktrees []folderWorktree) {
	a.stateOf(ctx, workspaceID, worktrees, false)
}

func (a *app) proveWorktrees(ctx context.Context, workspaceID string, worktrees []folderWorktree) {
	a.stateOf(ctx, workspaceID, worktrees, true)
}

func (a *app) stateOf(ctx context.Context, workspaceID string, worktrees []folderWorktree, proof bool) {
	paths := make([]string, 0, len(worktrees))
	for _, wt := range worktrees {
		paths = append(paths, wt.Path)
	}
	var states []worktreeState
	err := a.host.RunCommand(ctx, worktreeStateCmd, workspaceID, map[string]any{"paths": paths, "proof": proof}, &states)
	for i := range worktrees {
		wt := &worktrees[i]
		wt.Read, wt.Unread = true, true
		if err == nil && i < len(states) && states[i].Error == "" {
			wt.applyState(states[i])
		}
	}
}

func (wt *folderWorktree) applyState(s worktreeState) {
	wt.Unread, wt.Dirty, wt.Unpushed, wt.Ignored, wt.Orphans, wt.Nested = false, s.Dirty, s.Unpushed, s.Ignored, s.Orphans, s.Nested
	wt.Tip, wt.Merged, wt.Landed, wt.Remote, wt.Locked, wt.LockWhy = s.Tip, s.Merged, s.Landed, s.Remote, s.Locked, s.LockWhy
}

func (a *app) folderJobs(ctx context.Context, w pluginsdk.Workspace, folder string) ([]folderJob, error) {
	var trees []worktreeRef
	if f, err := gitrepo.OpenFolder(ctx, folder); err == nil {
		for _, t := range f.Trees {
			if !t.Bare {
				trees = append(trees, worktreeRef{Path: f.Mapped(t), Branch: t.Branch})
			}
		}
	} else {
		trees = []worktreeRef{{Path: folder}}
	}
	return a.jobsIn(ctx, w.ID, trees, isWorkFolder(ctx, w, folder))
}

func (a *app) jobsIn(ctx context.Context, workspaceID string, trees []worktreeRef, first bool) ([]folderJob, error) {
	var out struct {
		Jobs []struct {
			folderJob
			Enabled bool `json:"enabled"`
		} `json:"jobs"`
	}
	if err := a.host.RunCommand(ctx, cronJobsInCmd, workspaceID, map[string]any{"worktrees": trees, "first": first}, &out); err != nil {
		return nil, err
	}
	var jobs []folderJob
	for _, j := range out.Jobs {
		if j.Enabled {
			jobs = append(jobs, j.folderJob)
		}
	}
	return jobs, nil
}

func isWorkFolder(ctx context.Context, w pluginsdk.Workspace, folder string) bool {
	root, _, err := workRoot(w)
	if err != nil {
		return false
	}
	root = gitrepo.Canonical(root)
	return slices.ContainsFunc(folderDirs(ctx, folder), func(dir string) bool { return gitrepo.Within(root, dir) })
}

func (a *app) workspaceFolder(ctx context.Context, workspaceID, path string) (string, error) {
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	folder, ok := folderIn(w, path)
	if !ok {
		return "", fmt.Errorf("%w: %s", errNotFolder, path)
	}
	return folder, nil
}

func (a *app) blockersIn(dirs []string) []folderSession {
	var recs []record
	for _, s := range a.allSessions() {
		if s.worksIn(dirs) && s.blocksFolder() {
			recs = append(recs, s.snapshot())
		}
	}
	slices.SortFunc(recs, func(x, y record) int { return x.CreatedAt.Compare(y.CreatedAt) })
	var out []folderSession
	for _, rec := range recs {
		out = append(out, folderSessionOf(rec))
	}
	return out
}

func (a *app) sessionsIn(workspaceID string, dirs []string) []*session {
	return slices.DeleteFunc(a.workspaceSessions(workspaceID), func(s *session) bool { return !s.worksIn(dirs) })
}

func (a *app) allSessions() []*session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Collect(maps.Values(a.sessions))
}

func holdDirs(dirs []string, ticked []folderWorktree) []string {
	out := slices.Clone(dirs)
	for _, wt := range ticked {
		out = append(out, gitrepo.Canonical(wt.Path))
	}
	return out
}

func (a *app) taskHolds(workspaceID, folder string, ticked []folderWorktree) []string {
	tasks := append(a.tasksOn(workspaceID, folder), a.tasksHolding(ticked)...)
	slices.Sort(tasks)
	return slices.Compact(tasks)
}

func folderDirs(ctx context.Context, folder string) []string {
	f, err := gitrepo.OpenFolder(ctx, folder)
	if err != nil {
		return []string{gitrepo.Canonical(folder)}
	}
	dirs := make([]string, 0, len(f.Trees))
	for _, t := range f.Trees {
		dirs = append(dirs, f.Mapped(t))
	}
	return dirs
}

func folderSessionOf(rec record) folderSession {
	return folderSession{Session: rec.ID, Title: rec.Title, Status: rec.Status, Worktree: rec.Worktree}
}

func (s *session) worksIn(dirs []string) bool {
	worktree := gitrepo.Canonical(s.snapshot().Worktree)
	return slices.ContainsFunc(dirs, func(dir string) bool { return gitrepo.Within(worktree, dir) })
}

func (s *session) blocksFolder() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec.closed() {
		return false
	}
	return s.rec.Status == statusRunning || s.rec.Status == statusWaiting || len(s.queue) > 0 || s.setup.pending()
}
