package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/plugins/internal/gitrepo"
)

const (
	branchDeleteCmd = "git.branch-delete"
	removeRowLimit  = 5 * time.Minute

	keptChanged   = "it changed since you looked"
	keptInCode    = "it is open in Code"
	keptCode      = "couldn't check what Code has open"
	keptBranch    = "Branch %s stays: %s."
	keptCloses    = "closes %s"
	keptLocked    = "it is locked"
	keptLockedWhy = "it is locked: %s"
	keptTerminal  = "terminal %s is open in it"
	keptNoTerms   = "couldn't check its terminals"
)

var (
	errRowChanged = errors.New(keptChanged)
	errRowInCode  = errors.New(keptInCode)
)

type removalRow struct {
	repo   string
	tree   folderWorktree
	branch bool
	jobs   []folderJob
	closes []string
}

type removalResult struct {
	Removed []string    `json:"removed"`
	Deleted []string    `json:"deleted"`
	Kept    []string    `json:"kept"`
	Paused  []folderJob `json:"paused"`
}

func newRemovalResult() removalResult {
	return removalResult{Removed: []string{}, Deleted: []string{}, Kept: []string{}, Paused: []folderJob{}}
}

func (a *app) removeRows(ctx context.Context, workspaceID string, rows []removalRow, out *removalResult, done func(int)) {
	for i, row := range rows {
		kept := a.removeOne(ctx, workspaceID, row, out)
		if kept != "" {
			out.Kept = append(out.Kept, kept)
		}
		if done != nil {
			done(i + 1)
		}
	}
}

func (a *app) removeOne(ctx context.Context, workspaceID string, row removalRow, out *removalResult) string {
	ctx, cancel := context.WithTimeout(ctx, removeRowLimit)
	defer cancel()
	if err := a.removeTree(ctx, workspaceID, row); err != nil {
		resumed := a.resumeJobs(ctx, workspaceID, row.jobs)
		return keptLine(row.tree, errors.Join(err, resumed))
	}
	out.Removed = append(out.Removed, row.tree.Path)
	out.Paused = append(out.Paused, row.jobs...)
	if !row.branch {
		return ""
	}
	args := map[string]string{"repo": row.repo, "branch": row.tree.Branch, "tip": row.tree.Tip}
	if err := a.host.RunCommand(ctx, branchDeleteCmd, workspaceID, args, nil); err != nil {
		return fmt.Sprintf(keptBranch, mdCode(row.tree.Branch), mdPlain(keptWhy(err)))
	}
	out.Deleted = append(out.Deleted, row.tree.Branch)
	return ""
}

func (a *app) removeTree(ctx context.Context, workspaceID string, row removalRow) error {
	now := []folderWorktree{row.tree}
	a.proveWorktrees(ctx, workspaceID, now)
	if now[0] != row.tree {
		return errRowChanged
	}
	roots, err := a.codeRoots(ctx)
	if err != nil {
		return errors.New(keptCode)
	}
	if inCode(roots, row.tree.Path) {
		return errRowInCode
	}
	for _, rec := range a.openIn(row.tree.Path) {
		if !slices.Contains(row.closes, rec.ID) {
			return errRowChanged
		}
	}
	release, err := a.claimTree(row.tree)
	if err != nil {
		return err
	}
	defer release()
	return a.removeSeen(ctx, workspaceID, row.tree, map[string]any{"repo": row.repo, "path": row.tree.Path, "discard": true})
}

func (a *app) openIn(path string) []record {
	dirs := []string{gitrepo.Canonical(path)}
	var out []record
	for _, s := range a.allSessions() {
		if rec := s.snapshot(); !rec.closed() && s.worksIn(dirs) {
			out = append(out, rec)
		}
	}
	slices.SortFunc(out, func(x, y record) int { return x.CreatedAt.Compare(y.CreatedAt) })
	return out
}

func closesWords(recs []record) string {
	titles := make([]string, 0, len(recs))
	for _, rec := range recs {
		titles = append(titles, rec.Title)
	}
	return fmt.Sprintf(keptCloses, listWords(titles, func(title string) string { return title }))
}

func lockedWords(wt folderWorktree) string {
	if wt.LockWhy != "" {
		return fmt.Sprintf(keptLockedWhy, wt.LockWhy)
	}
	return keptLocked
}

func (a *app) claimTree(wt folderWorktree) (func(), error) {
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	ticked := []folderWorktree{wt}
	dirs := holdDirs(nil, ticked)
	if blockers := a.blockersIn(dirs); len(blockers) > 0 {
		return nil, errors.New(blockerText(blockers[0]))
	}
	if tasks := a.tasksHolding(ticked); len(tasks) > 0 {
		return nil, fmt.Errorf(removeTask, tasks[0])
	}
	if a.leaving == nil {
		a.leaving = map[string]int{}
	}
	for _, dir := range dirs {
		a.leaving[dir]++
	}
	return func() { a.releaseFolder(dirs) }, nil
}

func (a *app) terminalIn(ctx context.Context, path string) error {
	spaces, err := a.host.ListWorkspaces(ctx)
	if err != nil {
		return errors.New(keptNoTerms)
	}
	for _, w := range spaces {
		var open []struct {
			Title string `json:"title"`
		}
		if err := a.host.RunCommand(ctx, worktreeSessionsCmd, w.ID, map[string]string{"path": path}, &open); err != nil {
			return errors.New(keptNoTerms)
		}
		if len(open) > 0 {
			return fmt.Errorf(keptTerminal, open[0].Title)
		}
	}
	return nil
}

func (a *app) codeRoots(ctx context.Context) ([]string, error) {
	spaces, err := a.host.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, w := range spaces {
		for _, root := range w.Roots() {
			roots = append(roots, gitrepo.Canonical(root))
		}
	}
	return roots, nil
}

func inCode(roots []string, path string) bool {
	path = gitrepo.Canonical(path)
	return slices.ContainsFunc(roots, func(root string) bool { return gitrepo.Within(root, path) })
}

func (a *app) namedJobs(ctx context.Context, workspaceID string, trees []worktreeRef) ([]folderJob, error) {
	if len(trees) == 0 {
		return nil, nil
	}
	return a.jobsIn(ctx, workspaceID, trees, false)
}

func keptLine(wt folderWorktree, err error) string {
	return fmt.Sprintf(folderStayed, mdCode(wt.Path), mdPlain(keptWhy(err)))
}

func keptWhy(err error) string {
	return strings.TrimPrefix(strings.TrimRight(err.Error(), ".\n "), gitFatal)
}

func tipSeen(wt folderWorktree) string {
	return strings.Join([]string{
		wt.Tip, strconv.Itoa(wt.Dirty), strconv.Itoa(wt.Ignored), strconv.Itoa(wt.Orphans), strconv.Itoa(wt.Nested),
		strconv.FormatBool(wt.Merged), strconv.FormatBool(wt.Landed), strconv.FormatBool(wt.Remote), strconv.FormatBool(wt.Locked),
	}, "/")
}

func treeRef(repo string, wt folderWorktree, first bool) worktreeRef {
	return worktreeRef{Path: wt.Path, Branch: wt.Branch, Repo: repo, First: first}
}

func jobsOf(jobs []folderJob, path string) []folderJob {
	var out []folderJob
	for _, job := range jobs {
		if job.In == path {
			out = append(out, job)
		}
	}
	return out
}

func worktreeLabel(repo string, wt folderWorktree) string {
	return filepath.Base(repo) + " ▸ " + branchOr(wt)
}

func cronReason(repo string, wt folderWorktree) string {
	return fmt.Sprintf(cronPausedWhy, branchOr(wt), filepath.Base(repo))
}
