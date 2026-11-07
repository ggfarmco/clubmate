package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	stepRemove = "remove"
	stepDelete = "delete"
	stepKeep   = "keep"
	stepNone   = "none"

	archiveKeepTree   = "Keep worktree"
	archiveDropTree   = "Remove worktree"
	archiveNoTree     = "No worktree"
	archiveKeepBranch = "Keep branch"
	archiveDropBranch = "Delete branch"
	archiveNoBranch   = "No branch"

	archiveClean      = "%s · clean"
	archivePushed     = "%s · clean · pushed"
	archiveLive       = "a session works in it"
	archiveWouldLose  = "%s would be lost"
	archiveInCode     = "it is open in Code"
	archiveNoChild    = "no session started here"
	archiveOwnCheck   = "it worked in the folder's checkout"
	archiveTreeGone   = "its worktree is gone"
	archivePROpen     = "%s is open — the branch is the PR"
	archiveOnRemote   = "still on the remote"
	archiveNotLanded  = "its commits aren't in the default branch"
	archiveWithTree   = "it stays with its worktree"
	archiveStayRule   = "its rule keeps the branch"
	archiveDetached   = "the worktree is detached"
	archiveMergedPR   = "merged in %s · local only"
	archiveMerged     = "merged · local only"
	archiveSummary    = "Worktree %s · branch %s"
	archiveHeldRepo   = "%s hasn't finished"
	archiveFolderKept = "%s stays in %s: %s."
	restoreStayedLine = "%s stayed closed: %s"
)

var (
	errArchiveUser = errors.New("only the user archives a task")
	errRestoreUser = errors.New("only the user restores a session")
	errNoTask      = errors.New("this session has no task to archive")
	errArchiveHeld = errors.New("refused")
)

type archiveStep struct {
	Action string `json:"action"`
	Title  string `json:"title"`
	Why    string `json:"why,omitempty"`
	Tone   string `json:"tone,omitempty"`
}

type archiveRepo struct {
	Name      string         `json:"name"`
	Path      string         `json:"path"`
	Session   string         `json:"session,omitempty"`
	Worktree  archiveStep    `json:"worktree"`
	Branch    archiveStep    `json:"branch"`
	IfRemoved archiveStep    `json:"ifRemoved"`
	Anyway    bool           `json:"anyway"`
	Lost      string         `json:"lost,omitempty"`
	Seen      string         `json:"seen,omitempty"`
	Closes    []string       `json:"closes,omitempty"`
	Words     string         `json:"closesWords,omitempty"`
	Summary   string         `json:"summary"`
	State     folderWorktree `json:"state"`
}

type archiveFolder struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type archivePlan struct {
	Session    string          `json:"session"`
	Title      string          `json:"title"`
	Workspace  string          `json:"workspace"`
	Sessions   int             `json:"sessions"`
	Children   int             `json:"children"`
	Repos      []archiveRepo   `json:"repos"`
	Cron       []folderJob     `json:"cron"`
	CronUnread bool            `json:"cronUnread,omitempty"`
	Folders    []archiveFolder `json:"folders"`
	Blockers   []folderSession `json:"blockers"`
	Holds      []string        `json:"holds"`
}

type archiveChoice struct {
	Path   string `json:"path"`
	Seen   string `json:"seen"`
	Anyway bool   `json:"anyway"`
	Folder bool   `json:"folder"`
}

type archiveArgs struct {
	Session string          `json:"session"`
	Choices []archiveChoice `json:"choices"`
}

type restoreStayed struct {
	Session string `json:"session"`
	Title   string `json:"title"`
	Why     string `json:"why"`
}

type restoreResult struct {
	Restored []string        `json:"restored"`
	Stayed   []restoreStayed `json:"stayed"`
}

type archiveResult struct {
	removalResult
	Closed int `json:"closed"`
}

func (a *app) archivePlanCommand(ctx context.Context, cmd pluginsdk.CommandRun, s *session) (any, error) {
	if !a.userWord(cmd) {
		return nil, errArchiveUser
	}
	return a.archivePlan(ctx, cmd.WorkspaceID, s)
}

func (a *app) archiveCommand(ctx context.Context, cmd pluginsdk.CommandRun, args archiveArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errArchiveUser
	}
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	return a.archive(ctx, cmd.WorkspaceID, s, args.Choices, nil)
}

func (a *app) archivePlan(ctx context.Context, workspaceID string, s *session) (archivePlan, error) {
	rec := s.snapshot()
	if !ownsTask(rec) || len(rec.Task.Repos) == 0 {
		return archivePlan{}, errNoTask
	}
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return archivePlan{}, err
	}
	roots, err := a.codeRoots(ctx)
	if err != nil {
		return archivePlan{}, err
	}
	kids := a.repoChildren(s)
	plan := archivePlan{
		Session: rec.ID, Title: rec.Title, Workspace: w.Name,
		Repos: make([]archiveRepo, 0, len(rec.Task.Repos)), Cron: []folderJob{}, Folders: []archiveFolder{}, Blockers: []folderSession{}, Holds: []string{},
	}
	tree := a.subtree(s)
	for _, x := range tree {
		if !x.snapshot().closed() {
			plan.Sessions++
		}
	}
	plan.Blockers, plan.Holds = a.archiveHolds(rec, tree)
	var trees []worktreeRef
	var found []int
	var states []folderWorktree
	for _, r := range rec.Task.Repos {
		row := archiveRepo{Name: r.Name, Path: r.Path, Worktree: archiveStep{Action: stepNone, Title: archiveNoTree, Why: archiveNoChild}, Branch: archiveStep{Action: stepNone, Title: archiveNoBranch}}
		if kid := kids[gitrepo.Canonical(r.Path)]; kid != nil {
			plan.Children++
			kidRec := kid.snapshot()
			row.Session = kidRec.ID
			if wt, why := childTree(ctx, r.Path, kidRec.Worktree); why == "" {
				found = append(found, len(plan.Repos))
				states = append(states, wt)
				trees = append(trees, treeRef(r.Path, wt, isWorkFolder(ctx, w, r.Path)))
			} else {
				row.Worktree.Why = why
			}
		}
		plan.Repos = append(plan.Repos, row)
	}
	a.proveWorktrees(ctx, workspaceID, states)
	for i, wt := range states {
		row := &plan.Repos[found[i]]
		r := rec.Task.Repos[found[i]]
		row.State, row.Seen = wt, tipSeen(wt)
		outside := slices.DeleteFunc(a.openIn(wt.Path), func(open record) bool { return inTree(tree, open.ID) })
		row.Worktree, row.Anyway = treeStep(wt, inCode(roots, wt.Path), len(a.blockersIn(holdDirs(nil, []folderWorktree{wt}))) > 0, outside)
		if len(outside) > 0 {
			row.Words = closesWords(outside)
		}
		for _, open := range outside {
			row.Closes = append(row.Closes, open.ID)
		}
		row.Lost = lostWords(wt, lostDetached)
		row.IfRemoved = r.branchStep(wt, true)
		row.Branch = r.branchStep(wt, row.Worktree.Action == stepRemove)
	}
	for i := range plan.Repos {
		plan.Repos[i].Summary = fmt.Sprintf(archiveSummary, stepWord(plan.Repos[i].Worktree), stepWord(plan.Repos[i].Branch))
	}
	jobs, err := a.namedJobs(ctx, workspaceID, trees)
	plan.Cron, plan.CronUnread = append(plan.Cron, jobs...), err != nil
	plan.Folders = a.archiveFolders(ctx, w, rec, tree)
	return plan, nil
}

func stepWord(step archiveStep) string {
	switch step.Action {
	case stepRemove:
		return "removed"
	case stepDelete:
		return "deleted"
	case stepKeep:
		return "kept"
	}
	return "untouched"
}

func childTree(ctx context.Context, folder, worktree string) (folderWorktree, string) {
	if worktree == "" {
		return folderWorktree{}, archiveTreeGone
	}
	if _, err := os.Stat(worktree); err != nil {
		return folderWorktree{}, archiveTreeGone
	}
	f, err := gitrepo.OpenFolder(ctx, folder)
	if err != nil {
		return folderWorktree{}, archiveTreeGone
	}
	t := gitrepo.Longest(f.Trees, gitrepo.Canonical(worktree))
	if t == nil || t.Bare {
		return folderWorktree{}, archiveTreeGone
	}
	if t.Real == f.Checkout.Real || f.Primary(f.DefaultName(ctx), *t) {
		return folderWorktree{}, archiveOwnCheck
	}
	return folderWorktree{Path: t.Path, Branch: t.Branch}, ""
}

func inTree(tree []*session, id string) bool {
	return slices.ContainsFunc(tree, func(s *session) bool { return s.snapshot().ID == id })
}

func treeStep(wt folderWorktree, code, live bool, outside []record) (archiveStep, bool) {
	keep := func(why, tone string) archiveStep {
		return archiveStep{Action: stepKeep, Title: archiveKeepTree, Why: why, Tone: tone}
	}
	lost := lostWords(wt, lostDetached)
	switch {
	case wt.Unread:
		return keep(folderUnread, "warning"), false
	case wt.Nested > 0:
		return keep(folderNested, "warning"), false
	case wt.Locked:
		return keep(lockedWords(wt), "warning"), false
	case live:
		return keep(archiveLive, "warning"), false
	case code:
		return keep(archiveInCode, "warning"), false
	case lost != "" && len(outside) > 0:
		return keep(fmt.Sprintf(archiveWouldLose, lost)+" · "+closesWords(outside), "warning"), true
	case lost != "":
		return keep(fmt.Sprintf(archiveWouldLose, lost), "warning"), true
	case len(outside) > 0:
		return keep(closesWords(outside), "warning"), true
	case wt.Remote && wt.Unpushed == 0:
		return archiveStep{Action: stepRemove, Title: archiveDropTree, Why: fmt.Sprintf(archivePushed, branchOr(wt))}, false
	}
	return archiveStep{Action: stepRemove, Title: archiveDropTree, Why: fmt.Sprintf(archiveClean, branchOr(wt))}, false
}

func (r taskRepo) branchStep(wt folderWorktree, removing bool) archiveStep {
	keep := func(why string) archiveStep {
		return archiveStep{Action: stepKeep, Title: archiveKeepBranch, Why: why}
	}
	switch {
	case wt.Branch == "":
		return archiveStep{Action: stepNone, Title: archiveNoBranch, Why: archiveDetached}
	case wt.Unread:
		return keep(folderUnread)
	case r.LandsBy == landsByBranch:
		return keep(archiveStayRule)
	case r.State == landPROpen:
		return keep(fmt.Sprintf(archivePROpen, prLabel(r.PR)))
	case wt.Remote:
		return keep(archiveOnRemote)
	case !wt.Landed:
		return keep(archiveNotLanded)
	case !removing:
		return keep(archiveWithTree)
	case r.PR != "":
		return archiveStep{Action: stepDelete, Title: archiveDropBranch, Why: fmt.Sprintf(archiveMergedPR, prLabel(r.PR))}
	}
	return archiveStep{Action: stepDelete, Title: archiveDropBranch, Why: archiveMerged}
}

func (a *app) archiveHolds(rec record, tree []*session) ([]folderSession, []string) {
	blockers := []folderSession{}
	for _, x := range tree {
		if x.blocksFolder() {
			blockers = append(blockers, folderSessionOf(x.snapshot()))
		}
	}
	holds := []string{}
	for _, r := range rec.Task.Repos {
		if !r.archivable() {
			holds = append(holds, fmt.Sprintf(archiveHeldRepo, r.Name))
		}
	}
	return blockers, holds
}

func (r taskRepo) archivable() bool {
	return r.landed() || r.State == landLeft || r.State == landPROpen
}

func (t *taskRecord) archivable() bool {
	return t != nil && len(t.Repos) > 0 && !slices.ContainsFunc(t.Repos, func(r taskRepo) bool { return !r.archivable() })
}

func (a *app) archiveFolders(ctx context.Context, w pluginsdk.Workspace, rec record, tree []*session) []archiveFolder {
	out := []archiveFolder{}
	if rec.Title == "" || len(w.Folders) < 2 {
		return out
	}
	mine := func(s *session) bool { return slices.Contains(tree, s) }
	for _, f := range w.Folders[1:] {
		if f.AddedBy != rec.Title || rec.Task.repoIndex(f.Path) < 0 {
			continue
		}
		dirs := folderDirs(ctx, f.Path)
		other := slices.ContainsFunc(a.allSessions(), func(s *session) bool {
			return !mine(s) && !s.snapshot().closed() && s.worksIn(dirs)
		})
		tasks := slices.DeleteFunc(a.tasksOn(w.ID, f.Path), func(slug string) bool { return slug == rec.Task.Slug })
		if !other && len(tasks) == 0 {
			out = append(out, archiveFolder{Path: f.Path, Name: filepath.Base(f.Path)})
		}
	}
	return out
}

func (a *app) archive(ctx context.Context, workspaceID string, s *session, choices []archiveChoice, done func(int)) (archiveResult, error) {
	plan, err := a.archivePlan(ctx, workspaceID, s)
	if err != nil {
		return archiveResult{}, err
	}
	out := archiveResult{removalResult: newRemovalResult(), Closed: plan.Sessions}
	rows, kept := plan.rows(choices)
	out.Kept = kept
	rec := s.snapshot()
	tree := a.subtree(s)
	if err := refusedArchive(a.archiveHolds(rec, tree)); err != nil {
		return archiveResult{}, err
	}
	for i := range rows {
		rows[i].jobs = jobsOf(plan.Cron, rows[i].tree.Path)
		if err := a.pauseJobs(ctx, workspaceID, cronReason(rows[i].repo, rows[i].tree), rows[i].jobs); err != nil {
			for _, row := range rows[:i] {
				err = errors.Join(err, a.resumeJobs(ctx, workspaceID, row.jobs))
			}
			return archiveResult{}, err
		}
	}
	a.closeTree(ctx, s)
	a.removeRows(ctx, workspaceID, rows, &out.removalResult, done)
	for _, c := range choices {
		if c.Folder {
			out.Kept = append(out.Kept, a.archiveDropFolder(ctx, workspaceID, plan, c.Path)...)
		}
	}
	return out, nil
}

func refusedArchive(blockers []folderSession, holds []string) error {
	var why []string
	for _, b := range blockers {
		why = append(why, blockerText(b))
	}
	why = append(why, holds...)
	if len(why) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", errArchiveHeld, strings.Join(why, ", "))
}

func (p archivePlan) rows(choices []archiveChoice) ([]removalRow, []string) {
	var rows []removalRow
	var kept []string
	for _, c := range choices {
		if c.Folder {
			continue
		}
		i := slices.IndexFunc(p.Repos, func(r archiveRepo) bool { return r.State.Path == c.Path })
		if i < 0 {
			kept = append(kept, fmt.Sprintf(folderStayed, mdCode(c.Path), archiveTreeGone))
			continue
		}
		r := p.Repos[i]
		branch := r.Branch
		switch {
		case r.Seen != c.Seen:
			kept = append(kept, keptLine(r.State, errRowChanged))
			continue
		case r.Worktree.Action == stepRemove:
		case c.Anyway && r.Anyway:
			branch = r.IfRemoved
		default:
			kept = append(kept, keptLine(r.State, errors.New(r.Worktree.Why)))
			continue
		}
		rows = append(rows, removalRow{repo: r.Path, tree: r.State, branch: branch.Action == stepDelete, closes: r.Closes})
	}
	return rows, kept
}

func (a *app) archiveDropFolder(ctx context.Context, workspaceID string, plan archivePlan, path string) []string {
	if !slices.ContainsFunc(plan.Folders, func(f archiveFolder) bool { return f.Path == path }) {
		return []string{fmt.Sprintf(archiveFolderKept, mdPlain(filepath.Base(path)), mdPlain(plan.Workspace), "it isn't only this task's any more")}
	}
	res, err := a.removeFolder(ctx, workspaceID, path)
	switch {
	case err != nil:
		return []string{fmt.Sprintf(archiveFolderKept, mdPlain(filepath.Base(path)), mdPlain(plan.Workspace), mdPlain(keptWhy(err)))}
	case !res.Removed:
		return []string{fmt.Sprintf(archiveFolderKept, mdPlain(filepath.Base(path)), mdPlain(plan.Workspace), mdPlain(holdWords(folderHolds{blockers: res.Blockers, tasks: res.Tasks})))}
	}
	return res.Kept
}

func (a *app) restoreCommand(ctx context.Context, cmd pluginsdk.CommandRun, s *session) (any, error) {
	if !a.userWord(cmd) {
		return nil, errRestoreUser
	}
	return a.restoreSession(ctx, cmd.WorkspaceID, s)
}

func (a *app) restoreSession(ctx context.Context, workspaceID string, s *session) (restoreResult, error) {
	rec := s.snapshot()
	if rec.HandoffTo != "" {
		return restoreResult{}, errHandedOff
	}
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return restoreResult{}, err
	}
	if err := restoreRefused(ctx, w, rec); err != nil {
		return restoreResult{}, err
	}
	out := restoreResult{Restored: []string{}, Stayed: []restoreStayed{}}
	var back []*session
	for _, x := range a.subtree(s) {
		kid := x.snapshot()
		if !kid.closed() || kid.HandoffTo != "" {
			continue
		}
		if err := restoreRefused(ctx, w, kid); err != nil {
			out.Stayed = append(out.Stayed, restoreStayed{Session: kid.ID, Title: kid.Title, Why: err.Error()})
			continue
		}
		back = append(back, x)
		out.Restored = append(out.Restored, kid.ID)
	}
	for _, x := range back {
		a.resume(x)
	}
	return out, nil
}

func (r restoreResult) note() string {
	lines := make([]string, 0, len(r.Stayed))
	for _, kid := range r.Stayed {
		lines = append(lines, fmt.Sprintf(restoreStayedLine, kid.Title, kid.Why))
	}
	return strings.Join(lines, "\n")
}

func (a *app) reopenRefused(ctx context.Context, workspaceID string, s *session) error {
	rec := s.snapshot()
	if !rec.closed() {
		return nil
	}
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	return restoreRefused(ctx, w, rec)
}

func (a *app) reopenAct(ctx context.Context, ev pluginsdk.UIEvent, s *session) error {
	err := a.reopenRefused(ctx, ev.WorkspaceID, s)
	sid := s.snapshot().ID
	st := a.sessionState(ev.WorkspaceID, sid)
	a.uiMu.Lock()
	st.restoreNote = ""
	if err != nil {
		st.restoreNote = err.Error()
	}
	a.uiMu.Unlock()
	if err == nil {
		a.resume(s)
	}
	return nil
}

func (a *app) restoreAct(ctx context.Context, ev pluginsdk.UIEvent, s *session) error {
	res, err := a.restoreSession(ctx, ev.WorkspaceID, s)
	sid := s.snapshot().ID
	st := a.sessionState(ev.WorkspaceID, sid)
	a.uiMu.Lock()
	st.restoreNote = res.note()
	if err != nil {
		st.restoreNote = err.Error()
	}
	a.uiMu.Unlock()
	if err != nil || len(res.Stayed) > 0 {
		return a.show(ctx, ev, sid)
	}
	return nil
}

func restoreRefused(ctx context.Context, w pluginsdk.Workspace, rec record) error {
	if rec.Worktree == "" {
		return nil
	}
	if _, err := os.Stat(rec.Worktree); err != nil {
		return fmt.Errorf("its worktree is gone: %s", tildePath(rec.Worktree))
	}
	if branch, err := gitrepo.Branch(ctx, rec.Worktree); rec.ClosedBranch != "" && (err != nil || branch != rec.ClosedBranch) {
		return fmt.Errorf("its worktree is on %s now, not %s", cmp.Or(branch, "no branch"), rec.ClosedBranch)
	}
	path := gitrepo.Canonical(rec.Worktree)
	for _, folder := range w.Paths() {
		if slices.ContainsFunc(folderDirs(ctx, folder), func(dir string) bool { return gitrepo.Within(path, dir) }) {
			return nil
		}
	}
	return fmt.Errorf("its folder left %s: %s", w.Name, tildePath(rec.Worktree))
}
