package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"clubmate/internal/proc"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/plural"
)

const (
	worktreesDir        = ".worktrees"
	sessionRunning      = "running"
	cleanupTimeout      = time.Minute
	removeCommand       = "git.worktree-remove"
	removePromptCommand = "git.worktree-remove-prompt"
	discardPlugin       = "agents"
	unsavedMarker       = "unsaved buffers: "
	removeTitle         = "Remove worktree %s? Its branch is kept."
	removeDetachedTitle = "Remove detached worktree %s?"
	lostSecretsDetail   = "Deletes ignored "
	lostOrphansDetail   = "Loses %d %s on no branch."
	maxCompared         = 1 << 20
	terminalsDetail     = "Terminals still in it: "
	unsavedTitle        = "Worktree %s has %s. Remove it and discard the changes?"
	missingText         = "Worktree %s no longer exists — back on main"
	baseDefault         = "default"
	originRemote        = "origin"
	invalidSlugText     = "'%s' is not a valid worktree name"
	staleBaseText       = "Started from the local %s: couldn't update it from origin: %v"
	notMergedText       = "is not fully merged"
	noSectionText       = "no such section"
)

var (
	errMainCheckout   = errors.New("cannot remove the main checkout")
	errUnseenFolder   = errors.New("it was connected while you were away and you haven't seen it yet")
	errLookFirst      = fmt.Errorf("%w: open the title picker to look at it first", errUnseenFolder)
	errFolderCheckout = errors.New("cannot remove the checkout of the workspace folder")
	errDirtyWorktree  = errors.New("worktree has uncommitted changes")
	errDiscardRefused = errors.New("only the app discards a worktree's uncommitted changes")
	errCostGrew       = errors.New("it has more to lose than when you looked")
	errNestedRepos    = errors.New("it holds submodules or repos Clubmate can't read")
	errLosesWork      = errors.New("removing it would lose")
	errSeenRefused    = errors.New("only the app removes a worktree it would lose files or commits with")

	errLockedWorktree      = errors.New("it is locked")
	errBranchDeleteRefused = errors.New("only the app deletes a branch")
	errBranchMoved         = errors.New("it moved since you looked")
	errBranchOnRemote      = errors.New("it is still on the remote")
	errDefaultBranch       = errors.New("it is the default branch")
	errRelativePath        = errors.New("path must be absolute")
	errSymlinkedFolder     = errors.New("worktrees folder must not be a symlink")
	errNoMissingFolder     = errors.New("no git folder holds the missing worktree")
)

func listWorktrees(ctx context.Context, h *pluginsdk.Host, workspaceID string) ([]worktreeInfo, error) {
	list := []worktreeInfo{}
	err := eachFolder(ctx, h, workspaceID, func(f *folder) error {
		heads, err := f.heads(ctx)
		if err != nil {
			return err
		}
		start := len(list)
		for j, t := range f.Trees {
			if t.Bare {
				continue
			}
			list = append(list, worktreeInfo{Repo: f.Repo, Path: t.Path, Branch: t.Branch, Head: heads[t.Head].short, Committed: heads[t.Head].committed, Main: j == 0, Current: f.current(t)})
		}
		slices.SortStableFunc(list[start:], newestFirst)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

func newestFirst(a, b worktreeInfo) int {
	if a.Current != b.Current {
		if a.Current {
			return -1
		}
		return 1
	}
	return b.Committed.Compare(a.Committed)
}

func switchWorktree(ctx context.Context, h *pluginsdk.Host, workspaceID string, s *state, repo, path string) error {
	if !s.claim() {
		return errOpBusy
	}
	defer s.release()
	f, err := gitFolder(ctx, h, workspaceID, repo)
	if err != nil {
		return err
	}
	t, err := f.member(path)
	if err != nil {
		return err
	}
	_, err = h.SetWorktree(ctx, workspaceID, f.Repo, f.Selection(t))
	return err
}

func worktreeSessions(ctx context.Context, h *pluginsdk.Host, workspaceID, path string) ([]sessionInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, errRelativePath
	}
	dir := gitrepo.Canonical(filepath.Clean(path))
	sessions, err := h.ListSessions(ctx, workspaceID, false)
	if err != nil {
		return nil, err
	}
	vacate, err := removingOwners(ctx, h)
	if err != nil {
		return nil, err
	}
	inside, err := movedIn(ctx, h, workspaceID, dir)
	if err != nil {
		return nil, err
	}
	out := []sessionInfo{}
	for _, s := range sessions {
		started := filepath.IsAbs(s.Cwd) && gitrepo.Within(gitrepo.Canonical(s.Cwd), dir)
		if s.State == sessionRunning && !vacate[s.Owner] && (started || inside[s.ID]) {
			out = append(out, sessionInfo{ID: s.ID, Title: s.Title})
		}
	}
	return out, nil
}

func movedIn(ctx context.Context, h *pluginsdk.Host, workspaceID, dir string) (map[string]bool, error) {
	nodes, err := h.ProcessTree(ctx, workspaceID, "")
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, n := range nodes {
		if cwd, ok := proc.Cwd(n.Process.PID); ok && gitrepo.Within(gitrepo.Canonical(cwd), dir) {
			out[n.SessionID] = true
		}
	}
	return out, nil
}

func prepareNew(ctx context.Context, h *pluginsdk.Host, workspaceID string, args worktreeArgs) (opFunc, error) {
	f, err := gitFolder(ctx, h, workspaceID, args.Repo)
	if err != nil {
		return nil, err
	}
	cur := f.currentTree()
	if err := validBranch(ctx, cur.Path, args.Branch); err != nil {
		return nil, err
	}
	name := args.Branch
	if args.Slug != "" {
		name = args.Slug
	}
	if !gitrepo.ValidBranchName(name) {
		return nil, fmt.Errorf(invalidSlugText, name)
	}
	primary := f.Trees[0]
	dir, err := worktreeDir(primary.Path, name)
	if err != nil {
		return nil, err
	}
	base, source := currentHead, cur.Path
	if args.From != "" {
		from, err := f.member(args.From)
		if err != nil {
			return nil, err
		}
		base, source = from.Head, from.Path
	}
	k := hooks{h: h, workspaceID: workspaceID, primary: primary.Path}
	exists := hasRef(ctx, cur.Path, headsPrefix+args.Branch)
	return func(ctx context.Context, report *opReport) (string, error) {
		add, made := []string{dir, args.Branch}, ""
		if !exists {
			if args.Base == baseDefault {
				var err error
				if base, report.note, err = defaultStart(ctx, cur.Path, f.unseen); err != nil {
					return "", err
				}
			}
			add, made = []string{"-b", args.Branch, dir, base}, args.Branch
		}
		if err := addWorktree(ctx, cur.Path, dir, f.unseen, made, add); err != nil {
			return "", err
		}
		if err := excludeWorktrees(ctx, primary.Path); err != nil {
			return "", err
		}
		var notes []string
		if err := copyIncluded(source, dir); err != nil {
			notes = append(notes, err.Error())
		}
		skipped, err := k.run(ctx, dir, setupScript)
		if err != nil {
			return "", removeFailed(ctx, cur.Path, dir, err)
		}
		if skipped != "" {
			notes = append(notes, skipped)
		}
		report.notice = strings.Join(notes, "\n")
		g, err := gitFolder(ctx, h, workspaceID, f.Repo)
		if err != nil {
			return "", err
		}
		t, err := g.member(dir)
		if err != nil {
			return "", err
		}
		if args.Switch != nil && !*args.Switch {
			return t.Path, nil
		}
		if _, err := h.SetWorktree(ctx, workspaceID, g.Repo, g.Selection(t)); err != nil {
			return "", err
		}
		return t.Path, nil
	}, nil
}

func removePrompt(ctx context.Context, h *pluginsdk.Host, workspaceID string, args worktreeArgs) (prompt, error) {
	f, t, err := removable(ctx, h, workspaceID, args)
	if err != nil {
		return prompt{}, err
	}
	sessions, err := worktreeSessions(ctx, h, workspaceID, t.Path)
	if err != nil {
		return prompt{}, err
	}
	lost, err := readLoss(ctx, f, t)
	if err != nil {
		return prompt{}, err
	}
	title := removeTitle
	if t.Branch == "" {
		title = removeDetachedTitle
	}
	out := prompt{Title: fmt.Sprintf(title, f.treeName(ctx, t)), Command: removeCommand, Args: removeArgs{Repo: f.Repo, Path: t.Path, SeenSecrets: len(lost.secrets), SeenOrphans: lost.orphans}}
	var detail []string
	if len(sessions) > 0 {
		titles := make([]string, 0, len(sessions))
		for _, s := range sessions {
			titles = append(titles, s.Title)
		}
		detail = append(detail, terminalsDetail+strings.Join(titles, ", "))
	}
	out.Detail = strings.Join(append(detail, lost.lines()...), "\n")
	return out, nil
}

func prepareRemove(ctx context.Context, h *pluginsdk.Host, workspaceID string, args worktreeArgs) (opFunc, error) {
	f, t, err := removable(ctx, h, workspaceID, args)
	if err != nil {
		return nil, err
	}
	k := hooks{h: h, workspaceID: workspaceID, primary: f.Trees[0].Path}
	users := consumers{h: h, workspaceID: workspaceID}
	return func(ctx context.Context, report *opReport) (string, error) {
		if err := args.keeps(ctx, f, t); err != nil {
			return "", err
		}
		if !args.Discard {
			status, err := run(ctx, t.Path, "status", "--porcelain")
			if err != nil {
				return "", err
			}
			if len(strings.TrimSpace(string(status))) > 0 {
				return "", errDirtyWorktree
			}
		}
		g, err := removalFolder(ctx, h, workspaceID, args)
		if err != nil {
			return "", err
		}
		root := filepath.Join(t.Path, g.Rel)
		if g.current(t) {
			root = g.root
		}
		defer func() {
			logError("release "+root, users.released(context.WithoutCancel(ctx), t.Path, root))
		}()
		if err := users.removing(ctx, t.Path, root, args.Force); err != nil {
			return "", forceable(ctx, f, t, args, err)
		}
		logError("vacate "+t.Path, users.vacating(ctx, t.Path, root))
		if err := args.keeps(ctx, f, t); err != nil {
			return "", err
		}
		if skipped, err := k.run(ctx, t.Path, teardownScript); err != nil {
			report.notice = err.Error()
		} else if skipped != "" {
			report.notice = skipped
		}
		remove := []string{"worktree", "remove", t.Path}
		if args.seesCost() {
			remove = append(remove, "--force")
		}
		if _, err := mutate(ctx, f.Checkout.Path, remove...); err != nil {
			return "", err
		}
		p, err := h.GetWorkspace(ctx, workspaceID)
		if err != nil {
			return "", err
		}
		if slices.ContainsFunc(p.MissingWorktrees(), func(m string) bool { return gitrepo.Canonical(m) == g.Mapped(t) }) {
			_, err = h.SetWorktree(ctx, workspaceID, f.Repo, "")
		}
		return "", err
	}, nil
}

func deleteBranch(ctx context.Context, h *pluginsdk.Host, workspaceID string, args worktreeArgs) error {
	f, err := gitFolder(ctx, h, workspaceID, args.Repo)
	if err != nil {
		return err
	}
	return deleteBranchIn(ctx, f.Checkout.Path, args.Branch, args.Tip)
}

func deleteBranchIn(ctx context.Context, top, branch, seen string) error {
	if !gitrepo.ValidBranchName(branch) {
		return fmt.Errorf(invalidBranchText, branch)
	}
	if name, err := gitrepo.DefaultBranch(ctx, top); err == nil && name == branch {
		return errDefaultBranch
	}
	tip, err := run(ctx, top, "rev-parse", "--verify", headsPrefix+branch)
	if err != nil {
		return err
	}
	if seen == "" || strings.TrimSpace(string(tip)) != seen {
		return errBranchMoved
	}
	remotes, err := remoteBranches(ctx, top)
	if err != nil {
		return err
	}
	if remotes[branch] {
		return errBranchOnRemote
	}
	_, err = mutateInert(ctx, top, "branch", "-d", "--", branch)
	if err != nil && strings.Contains(err.Error(), notMergedText) && gitrepo.LandedInert(ctx, top, seen) {
		return forceDeleteBranch(ctx, top, branch, seen)
	}
	return err
}

func forceDeleteBranch(ctx context.Context, top, branch, seen string) error {
	if _, err := mutateInert(ctx, top, "update-ref", "-d", headsPrefix+branch, seen); err != nil {
		if tip, terr := run(ctx, top, "rev-parse", "--verify", headsPrefix+branch); terr == nil && strings.TrimSpace(string(tip)) != seen {
			return errBranchMoved
		}
		return err
	}
	_, err := mutateInert(ctx, top, "config", "--remove-section", "branch."+branch)
	if err != nil && !strings.Contains(err.Error(), noSectionText) {
		return err
	}
	return nil
}

func fallBack(ctx context.Context, h *pluginsdk.Host, workspaceID, missing string) error {
	var home, first *folder
	var lost string
	count := 0
	target := gitrepo.Canonical(missing)
	err := eachFolder(ctx, h, workspaceID, func(f *folder) error {
		count++
		if first == nil {
			first = f
		}
		owner := gitrepo.Longest(f.Trees, target)
		if home != nil || owner == nil {
			return nil
		}
		home = f
		if owner.Real != f.Trees[0].Real {
			lost = f.treeName(ctx, *owner)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if home == nil && count == 1 {
		home = first
	}
	if home == nil {
		return fmt.Errorf("%w: %s", errNoMissingFolder, missing)
	}
	if lost == "" {
		lost = filepath.Base(missing)
	}
	if _, err := h.SetWorktree(ctx, workspaceID, home.Repo, ""); err != nil {
		return err
	}
	_, err = h.Publish(ctx, outcomeTopic, outcomeEvent{Workspace: workspaceID, Message: fmt.Sprintf(missingText, lost)})
	return err
}

func defaultStart(ctx context.Context, dir string, unseen bool) (string, string, error) {
	fetchErr := errUnseenFolder
	local := !unseen && !hasOrigin(ctx, dir)
	if !unseen && !local {
		fetchErr = gitError(nil, gitrepo.Fetch(ctx, dir, originRemote))
	}
	name, err := gitrepo.DefaultBranch(ctx, dir)
	if err != nil {
		return "", "", err
	}
	if local && hasRef(ctx, dir, headsPrefix+name) {
		return headsPrefix + name, "", nil
	}
	remote := remotesPrefix + originRemote + "/" + name
	if fetchErr == nil && hasRef(ctx, dir, remote) {
		return remote, "", nil
	}
	if fetchErr == nil {
		fetchErr = fmt.Errorf("%s not found", strings.TrimPrefix(remote, remotesPrefix))
	}
	for _, ref := range []string{headsPrefix + name, remote} {
		if hasRef(ctx, dir, ref) {
			return ref, fmt.Sprintf(staleBaseText, name, fetchErr), nil
		}
	}
	return "", "", fetchErr
}

func hasOrigin(ctx context.Context, dir string) bool {
	remotes, err := gitrepo.Remotes(ctx, dir)
	return err != nil || slices.Contains(remotes, originRemote)
}

func worktreeDir(primary, name string) (string, error) {
	base := filepath.Join(primary, worktreesDir)
	dir := filepath.Join(base, name)
	cur := base
	for _, part := range append([]string{""}, strings.Split(name, "/")...) {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return "", errSymlinkedFolder
		}
		if cur == dir {
			return "", fmt.Errorf("%s already exists", filepath.Join(worktreesDir, name))
		}
	}
	if !gitrepo.Within(gitrepo.Canonical(filepath.Dir(dir)), filepath.Join(gitrepo.Canonical(primary), worktreesDir)) {
		return "", errSymlinkedFolder
	}
	return dir, nil
}

func excludeWorktrees(ctx context.Context, top string) error {
	_, err := gitrepo.Run(ctx, top, "check-ignore", "-q", worktreesDir+"/")
	if err == nil {
		return nil
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); !ok || ee.ExitCode() != 1 {
		return gitError(nil, err)
	}
	out, err := run(ctx, top, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(top, path)
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	line := worktreesDir + "/\n"
	if len(data) > 0 && data[len(data)-1] != '\n' {
		line = "\n" + line
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = file.WriteString(line)
	return errors.Join(err, file.Close())
}

func addWorktree(ctx context.Context, repo, dir string, unseen bool, made string, add []string) error {
	if !unseen {
		_, err := mutate(ctx, repo, append([]string{"worktree", "add"}, add...)...)
		return err
	}
	if _, err := mutateInert(ctx, repo, append([]string{"worktree", "add", "--no-checkout"}, add...)...); err != nil {
		return err
	}
	tip := madeTip(ctx, repo, made)
	if _, err := mutateInert(ctx, dir, "reset", "--hard"); err != nil {
		err = removeFailed(ctx, repo, dir, err)
		if tip == "" {
			return err
		}
		return errors.Join(err, dropMadeBranch(ctx, repo, made, tip))
	}
	return nil
}

func madeTip(ctx context.Context, repo, made string) string {
	if made == "" {
		return ""
	}
	out, err := run(ctx, repo, "rev-parse", "--verify", headsPrefix+made)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func removeFailed(ctx context.Context, repo, dir string, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	_, err := mutateInert(ctx, repo, "worktree", "remove", "--force", dir)
	return errors.Join(cause, err)
}

func dropMadeBranch(ctx context.Context, repo, branch, tip string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	return forceDeleteBranch(ctx, repo, branch, tip)
}

func removalFolder(ctx context.Context, h *pluginsdk.Host, workspaceID string, args worktreeArgs) (*folder, error) {
	if !args.Left {
		return gitFolder(ctx, h, workspaceID, args.Repo)
	}
	f, err := gitrepo.OpenFolder(ctx, args.Repo)
	if err != nil {
		return nil, gitError(nil, err)
	}
	return &folder{Folder: f, root: args.Repo}, nil
}

func (args worktreeArgs) seesCost() bool {
	return args.Discard || args.Left
}

func (args worktreeArgs) keeps(ctx context.Context, f *folder, t gitrepo.Worktree) error {
	if args.seesCost() {
		return args.costHolds(ctx, t.Path)
	}
	lost, err := readLoss(ctx, f, t)
	if err != nil {
		return err
	}
	if len(lost.secrets) > args.SeenSecrets || lost.orphans > args.SeenOrphans {
		return fmt.Errorf("%w %s; remove it from the app, which asks first", errLosesWork, lost)
	}
	return nil
}

func (args worktreeArgs) costHolds(ctx context.Context, dir string) error {
	var now worktreeState
	readState(ctx, &now, dir, nil)
	switch {
	case now.Error != "":
		return errors.New(now.Error)
	case now.Nested > 0:
		return errNestedRepos
	case now.Dirty > args.SeenDirty, len(now.Secrets) > args.SeenSecrets, now.Orphans > args.SeenOrphans:
		return errCostGrew
	}
	return nil
}

func removable(ctx context.Context, h *pluginsdk.Host, workspaceID string, args worktreeArgs) (*folder, gitrepo.Worktree, error) {
	f, err := removalFolder(ctx, h, workspaceID, args)
	if err != nil {
		return nil, gitrepo.Worktree{}, err
	}
	t, err := f.member(args.Path)
	if err != nil {
		return nil, gitrepo.Worktree{}, err
	}
	switch {
	case f.Primary(f.DefaultName(ctx), t):
		return nil, gitrepo.Worktree{}, errMainCheckout
	case t.Real == f.Checkout.Real:
		return nil, gitrepo.Worktree{}, errFolderCheckout
	case t.Locked && t.LockWhy != "":
		return nil, gitrepo.Worktree{}, fmt.Errorf("%w: %s", errLockedWorktree, t.LockWhy)
	case t.Locked:
		return nil, gitrepo.Worktree{}, errLockedWorktree
	case f.unseen && !args.seesCost():
		return nil, gitrepo.Worktree{}, errLookFirst
	}
	return f, t, nil
}

func forceable(ctx context.Context, f *folder, t gitrepo.Worktree, args worktreeArgs, err error) error {
	_, rest, ok := strings.Cut(err.Error(), unsavedMarker)
	if args.Force || !ok {
		return err
	}
	title := fmt.Sprintf(unsavedTitle, f.treeName(ctx, t), unsavedMarker+rest)
	return &confirmError{err: err, prompt: prompt{Title: title, Command: removeCommand, Args: removeArgs{Repo: f.Repo, Path: t.Path, Force: true, SeenSecrets: args.SeenSecrets, SeenOrphans: args.SeenOrphans}}}
}

type worktreeLoss struct {
	secrets []string
	orphans int
}

func readLoss(ctx context.Context, f *folder, t gitrepo.Worktree) (worktreeLoss, error) {
	secrets, err := secretFiles(ctx, t.Path)
	if err != nil {
		return worktreeLoss{}, err
	}
	orphans, err := countRevs(ctx, t.Path, "rev-list", "--count", currentHead, "--not", "--branches", "--tags", "--remotes")
	if err != nil {
		return worktreeLoss{}, err
	}
	out := worktreeLoss{orphans: orphans}
	for _, rel := range secrets {
		if !f.keptElsewhere(t, rel) {
			out.secrets = append(out.secrets, rel)
		}
	}
	return out, nil
}

func (f *folder) keptElsewhere(t gitrepo.Worktree, rel string) bool {
	return slices.ContainsFunc(f.Trees, func(o gitrepo.Worktree) bool {
		return o.Path != t.Path && sameContent(filepath.Join(o.Path, rel), filepath.Join(t.Path, rel))
	})
}

func sameContent(a, b string) bool {
	ia, err := os.Lstat(a)
	if err != nil || !ia.Mode().IsRegular() || ia.Size() > maxCompared {
		return false
	}
	ib, err := os.Lstat(b)
	if err != nil || !ib.Mode().IsRegular() || ib.Size() != ia.Size() {
		return false
	}
	da, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	db, err := os.ReadFile(b)
	return err == nil && bytes.Equal(da, db)
}

func (l worktreeLoss) lines() []string {
	var out []string
	if len(l.secrets) > 0 {
		out = append(out, lostSecretsDetail+strings.Join(l.secrets, ", ")+".")
	}
	if l.orphans > 0 {
		out = append(out, fmt.Sprintf(lostOrphansDetail, l.orphans, plural.Word(l.orphans, "commit", "commits")))
	}
	return out
}

func (l worktreeLoss) String() string {
	var parts []string
	if len(l.secrets) > 0 {
		parts = append(parts, "ignored "+strings.Join(l.secrets, ", "))
	}
	if l.orphans > 0 {
		parts = append(parts, fmt.Sprintf("%d %s on no branch", l.orphans, plural.Word(l.orphans, "commit", "commits")))
	}
	return strings.Join(parts, " and ")
}
