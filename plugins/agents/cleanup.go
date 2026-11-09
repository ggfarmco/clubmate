package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	worktreeUnpushedCmd = "git.worktree-unpushed"
	cleanupBatch        = 24

	cleanupMerged   = "merged"
	cleanupPRMerged = "prMerged"
	cleanupGone     = "gone"
	cleanupIdle     = "idle"
	cleanupRisky    = "risky"
	cleanupHand     = "byHand"

	cleanupGoBoth   = "worktree and local branch go"
	cleanupGoTree   = "worktree goes · branch stays"
	cleanupOneByOne = "tick one at a time"
	cleanupTitleAll = "Clean up worktrees in %s"
	cleanupCount    = "%d · %d stale, %d by hand"
)

var errCleanupUser = errors.New("only the user cleans up worktrees")

type cleanupDef struct {
	id      string
	title   string
	note    string
	tone    string
	bulk    bool
	preTick bool
}

var cleanupDefs = []cleanupDef{
	{id: cleanupMerged, title: "Merged and clean", note: cleanupGoBoth, tone: "success", bulk: true, preTick: true},
	{id: cleanupPRMerged, title: "PR merged, remote branch deleted", note: cleanupGoBoth, tone: "success", bulk: true, preTick: true},
	{id: cleanupGone, title: "Remote branch deleted, not found in main", note: "only copy is local", tone: "warning"},
	{id: cleanupIdle, title: "No commits for 30 d, all pushed", note: cleanupGoTree},
	{id: cleanupRisky, title: "Unpushed commits or uncommitted files", note: cleanupOneByOne, tone: "warning"},
	{id: cleanupHand, title: "Made by hand", note: cleanupOneByOne, tone: "warning"},
}

type cleanupRow struct {
	Path   string         `json:"path"`
	Repo   string         `json:"repo"`
	Folder string         `json:"folder"`
	Branch string         `json:"branch"`
	Task   string         `json:"task,omitempty"`
	Group  string         `json:"group"`
	Pushed bool           `json:"-"`
	Merged bool           `json:"-"`
	Gone   bool           `json:"-"`
	ByHand bool           `json:"-"`
	Closes []string       `json:"closes,omitempty"`
	Words  string         `json:"closesWords,omitempty"`
	Locked bool           `json:"locked,omitempty"`
	Why    string         `json:"lockWhy,omitempty"`
	Stale  bool           `json:"-"`
	Report bool           `json:"-"`
	Land   taskRepo       `json:"-"`
	State  folderWorktree `json:"state"`
}

type cleanupGroup struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Count int      `json:"count"`
	Note  string   `json:"note"`
	Tone  string   `json:"tone,omitempty"`
	Bulk  bool     `json:"bulk"`
	Rows  []string `json:"rows"`
}

type cleanupPlan struct {
	Title      string         `json:"title"`
	Summary    string         `json:"summary"`
	Workspace  string         `json:"workspace"`
	Total      int            `json:"total"`
	Stale      int            `json:"stale"`
	ByHand     int            `json:"byHand"`
	Groups     []cleanupGroup `json:"groups"`
	Rows       []cleanupRow   `json:"rows"`
	Folders    []string       `json:"folders"`
	Cron       []folderJob    `json:"cron"`
	CronUnread bool           `json:"cronUnread,omitempty"`
	index      map[string]int
}

type cleanupArgs struct {
	Folder string `json:"folder"`
}

func (a *app) cleanupPlanCommand(ctx context.Context, cmd pluginsdk.CommandRun, args cleanupArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errCleanupUser
	}
	plan, err := a.cleanupPlan(ctx, cmd.WorkspaceID, args.Folder)
	if err != nil {
		return nil, err
	}
	a.proveRows(ctx, cmd.WorkspaceID, &plan, plan.preTicked())
	plan.regroup()
	return plan, nil
}

func (a *app) cleanupPlan(ctx context.Context, workspaceID, folder string) (cleanupPlan, error) {
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return cleanupPlan{}, err
	}
	if folder != "" {
		if folder, err = a.workspaceFolder(ctx, workspaceID, folder); err != nil {
			return cleanupPlan{}, err
		}
	}
	joined, err := a.worktreeRows(ctx, workspaceID)
	if err != nil {
		return cleanupPlan{}, err
	}
	roots, err := a.codeRoots(ctx)
	if err != nil {
		return cleanupPlan{}, err
	}
	var unpushed []string
	if err := a.host.RunCommand(ctx, worktreeUnpushedCmd, workspaceID, struct{}{}, &unpushed); err != nil {
		return cleanupPlan{}, err
	}
	notPushed := make(map[string]bool, len(unpushed))
	for _, path := range unpushed {
		notPushed[path] = true
	}
	plan := cleanupPlan{Title: fmt.Sprintf(cleanupTitleAll, w.Name), Workspace: w.Name, Rows: []cleanupRow{}, Folders: w.Paths(), Cron: []folderJob{}}
	var refs []worktreeRef
	firsts := map[string]bool{}
	held := a.heldDirs()
	listed := map[string]bool{}
	for _, row := range joined.Worktrees {
		if !row.listed(roots) || folder != "" && row.Repo != folder || inCode(held, row.Path) || listed[row.Path] {
			continue
		}
		listed[row.Path] = true
		wt := folderWorktree{Path: row.Path, Branch: row.Branch}
		c := cleanupRow{
			Path: row.Path, Repo: row.Repo, Folder: row.Folder, Branch: row.Branch, Merged: row.Merged, Gone: row.RemoteGone,
			ByHand: row.ByHand, Stale: row.Stale, Report: row.landing.State == landMerged, Land: row.landing, State: wt,
			Pushed: !notPushed[row.Path], Locked: row.Locked, Why: row.LockWhy,
		}
		if open := a.openIn(row.Path); len(open) > 0 {
			c.Words = closesWords(open)
			for _, rec := range open {
				c.Closes = append(c.Closes, rec.ID)
			}
		}
		if row.Task != nil {
			c.Task = row.Task.Title
		}
		plan.Rows = append(plan.Rows, c)
		first, known := firsts[row.Repo]
		if !known {
			first = isWorkFolder(ctx, w, row.Repo)
			firsts[row.Repo] = first
		}
		refs = append(refs, treeRef(row.Repo, wt, first))
	}
	jobs, err := a.namedJobs(ctx, workspaceID, refs)
	plan.Cron, plan.CronUnread = append(plan.Cron, jobs...), err != nil
	plan.regroup()
	return plan, nil
}

func (w worktreeRow) listed(roots []string) bool {
	return (w.Stale || w.ByHand) && !w.Live && !w.Now && !w.Current && !w.Main && !inCode(roots, w.Path)
}

func (a *app) heldDirs() []string {
	var out []string
	for _, s := range a.allSessions() {
		if s.blocksFolder() {
			out = append(out, gitrepo.Canonical(s.snapshot().Worktree))
		}
	}
	for _, rec := range a.taskRecords() {
		if rec.closed() {
			continue
		}
		for _, r := range rec.Task.Repos {
			if child := a.session(r.Session); child != nil && r.unsettled() {
				out = append(out, gitrepo.Canonical(child.snapshot().Worktree))
			}
		}
	}
	return out
}

func (a *app) worktreeRows(ctx context.Context, workspaceID string) (worktreesResult, error) {
	var index []gitWorktree
	if err := a.host.RunCommand(ctx, worktreeIndexCmd, workspaceID, struct{}{}, &index); err != nil {
		return worktreesResult{}, err
	}
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return worktreesResult{}, err
	}
	sessions := a.workspaceSessions(workspaceID)
	facts := make([]sessionFact, 0, len(sessions))
	for _, s := range sessions {
		facts = append(facts, sessionFact{rec: s.snapshot(), live: s.blocksFolder()})
	}
	now := time.Now()
	rows, tasks := joinWorktrees(index, facts, a.codeFolder(ctx, workspaceID), now)
	folders := worktreeFolders(w, rows, facts, now)
	return worktreesResult{Folders: folders, Tasks: tasks, Worktrees: rows, Picker: buildPicker(w.Name, folders, rows)}, nil
}

func (a *app) proveRows(ctx context.Context, workspaceID string, p *cleanupPlan, paths []string) {
	for chunk := range slices.Chunk(paths, cleanupBatch) {
		trees := p.trees(chunk)
		a.proveWorktrees(ctx, workspaceID, trees)
		p.setStates(trees)
	}
}

func (p *cleanupPlan) row(path string) (*cleanupRow, bool) {
	i, ok := p.index[path]
	if !ok {
		return nil, false
	}
	return &p.Rows[i], true
}

func (p *cleanupPlan) trees(paths []string) []folderWorktree {
	trees := make([]folderWorktree, 0, len(paths))
	for _, path := range paths {
		if r, ok := p.row(path); ok {
			trees = append(trees, folderWorktree{Path: r.Path, Branch: r.Branch})
		}
	}
	return trees
}

func (p *cleanupPlan) setStates(read []folderWorktree) {
	for _, wt := range read {
		if r, ok := p.row(wt.Path); ok {
			r.State = wt
		}
	}
}

func (p *cleanupPlan) preTicked() []string {
	var out []string
	for _, r := range p.Rows {
		if r.Group == cleanupMerged || r.Group == cleanupPRMerged || r.Report {
			out = append(out, r.Path)
		}
	}
	return out
}

func (r cleanupRow) group() string {
	wt := r.State
	unpushed := wt.Unpushed > 0 || !wt.Read && !r.Pushed
	proof := wt.Landed || !wt.Read && r.Merged || r.Report && !unpushed
	switch {
	case r.ByHand:
		return cleanupHand
	case r.Locked, wt.Read && (wt.Unread || wt.Nested > 0 || wt.Dirty > 0 || wt.Orphans > 0):
		return cleanupRisky
	case proof && r.Gone:
		return cleanupPRMerged
	case proof:
		return cleanupMerged
	case r.Gone:
		return cleanupGone
	case unpushed:
		return cleanupRisky
	}
	return cleanupIdle
}

func (p *cleanupPlan) regroup() {
	p.Groups = make([]cleanupGroup, 0, len(cleanupDefs))
	p.Total, p.Stale, p.ByHand = len(p.Rows), 0, 0
	p.index = make(map[string]int, len(p.Rows))
	defer func() { p.Summary = p.countWords("") }()
	byGroup := map[string][]string{}
	for i := range p.Rows {
		r := &p.Rows[i]
		r.Group = r.group()
		byGroup[r.Group] = append(byGroup[r.Group], r.Path)
		p.index[r.Path] = i
	}
	for _, def := range cleanupDefs {
		group := cleanupGroup{ID: def.id, Title: def.title, Note: def.note, Tone: def.tone, Bulk: def.bulk, Rows: byGroup[def.id]}
		group.Count = len(group.Rows)
		if group.Count > 0 {
			p.Groups = append(p.Groups, group)
		}
		if def.id == cleanupHand {
			p.ByHand = group.Count
		} else {
			p.Stale += group.Count
		}
	}
}

func (p *cleanupPlan) countWords(folder string) string {
	total, hand := 0, 0
	for _, r := range p.Rows {
		if folder == "" || r.Repo == folder {
			total++
			if r.Group == cleanupHand {
				hand++
			}
		}
	}
	return fmt.Sprintf(cleanupCount, total, total-hand, hand)
}

func (r cleanupRow) lost() string {
	return lostWords(r.State, lostDetached)
}

func (r cleanupRow) tickable() bool {
	return r.State.Read && !r.State.Unread && r.State.Nested == 0 && !r.Locked && !r.State.Locked
}

func (r cleanupRow) preTick() bool {
	def := cleanupDefs[slices.IndexFunc(cleanupDefs, func(d cleanupDef) bool { return d.id == r.Group })]
	return def.preTick && r.tickable() && r.lost() == "" && len(r.Closes) == 0
}

func (r cleanupRow) branchGoes() bool {
	return (r.Group == cleanupMerged || r.Group == cleanupPRMerged) && r.Land.branchStep(r.State, true).Action == stepDelete
}

func (r cleanupRow) removal(jobs []folderJob) removalRow {
	return removalRow{repo: r.Repo, tree: r.State, branch: r.branchGoes(), jobs: jobsOf(jobs, r.Path), closes: r.Closes}
}

func (r cleanupRow) label() string {
	return r.Folder + " ▸ " + cmp.Or(r.Branch, tildePath(r.Path))
}

func (r cleanupRow) matches(query string) bool {
	return strings.Contains(strings.ToLower(strings.Join([]string{r.Folder, r.Branch, r.Task}, "\n")), strings.ToLower(query))
}
