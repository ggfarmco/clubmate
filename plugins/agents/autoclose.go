package main

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"clubmate/plugins/internal/gitrepo"
)

var errParentBranch = errors.New("the parent's branch can't be resolved")

const (
	closeAfter    = time.Hour
	closeAfterEnv = "CLUBMATE_AGENTS_CLOSE_AFTER"
	closeGateEnv  = "CLUBMATE_AGENTS_CLOSE_GATE"
	closeGatePoll = 20 * time.Millisecond
)

type ownedJob struct {
	Owner string `json:"owner"`
}

func (a *app) closeDoneLoop(after time.Duration) {
	if after <= 0 {
		return
	}
	t := time.NewTicker(max(min(suspendTick, after/2), time.Millisecond))
	defer t.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-t.C:
			a.closeDone(after)
		}
	}
}

func (a *app) closeDone(after time.Duration) {
	a.mu.Lock()
	var roots []*session
	for id, s := range a.sessions {
		if rec := s.snapshot(); a.dropped[id] == 0 && a.orphanLocked(rec) && !rec.restricted() && !rec.closed() {
			roots = append(roots, s)
		}
	}
	a.unlandedMu.Lock()
	maps.DeleteFunc(a.unlandedSeen, func(id string, _ unlandedVerdict) bool { return a.sessions[id] == nil })
	a.unlandedMu.Unlock()
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	owners := map[string]map[string]bool{}
	running := map[string]map[string]bool{}
	for _, root := range roots {
		rec := root.snapshot()
		tree := a.subtree(root)
		if slices.ContainsFunc(tree, func(s *session) bool {
			return !s.settled(after, s == root && rec.Run != nil) || a.gateBusy(ctx, s.snapshot())
		}) || !a.asidesSettled(tree, after) {
			continue
		}
		workspace := rec.Workspace
		if rec.Run != nil {
			if _, ok := running[workspace]; !ok {
				running[workspace] = a.runningRuns(workspace)
			}
			if live := running[workspace]; live == nil || live[rec.Run.ID] {
				continue
			}
		}
		if _, ok := owners[workspace]; !ok {
			owners[workspace] = a.jobOwners(ctx, workspace)
		}
		if owned := owners[workspace]; owned == nil || slices.ContainsFunc(tree, func(s *session) bool { return owned[s.snapshot().ID] }) {
			continue
		}
		if slices.ContainsFunc(tree, func(s *session) bool { return a.inLandQueue(s.snapshot().ID) || a.unlanded(ctx, s.snapshot()) }) {
			continue
		}
		a.holdClose()
		for _, s := range tree {
			if !s.snapshot().closed() {
				a.closeUnlessBusy(ctx, s, s != root || rec.ParentSession != "", s == root && rec.Run != nil, after)
			}
		}
	}
}

func (a *app) holdClose() {
	if a.closeGate == "" {
		return
	}
	logError("close gate", os.WriteFile(a.closeGate+".held", nil, 0o600))
	t := time.NewTicker(closeGatePoll)
	defer t.Stop()
	for {
		if _, err := os.Stat(a.closeGate); err == nil {
			return
		}
		select {
		case <-a.quit:
			return
		case <-t.C:
		}
	}
}

func (a *app) gateBusy(ctx context.Context, rec record) bool {
	if rec.reviewRun(a.gateRunOpen) != "" {
		return true
	}
	gf := rec.GateFor
	return gf != nil && gf.Run != "" && !rec.closed() && a.graphs.GateRunning(ctx, gf.Run)
}

func (a *app) jobOwners(ctx context.Context, workspace string) map[string]bool {
	var cron struct {
		Jobs []ownedJob `json:"jobs"`
	}
	var triggers struct {
		Triggers []ownedJob `json:"triggers"`
	}
	if a.host.RunCommand(ctx, "cron.list", workspace, struct{}{}, &cron) != nil || a.host.RunCommand(ctx, "triggers.list", workspace, struct{}{}, &triggers) != nil {
		return nil
	}
	owned := map[string]bool{}
	for _, j := range append(cron.Jobs, triggers.Triggers...) {
		owned[j.Owner] = true
	}
	return owned
}

func (a *app) orphanLocked(rec record) bool {
	if rec.ParentSession == "" {
		return true
	}
	parent, ok := a.sessions[rec.ParentSession]
	return !ok || parent.snapshot().closed()
}

func (a *app) runningRuns(workspace string) map[string]bool {
	return a.graphs.Running(workspace)
}

func (s *session) settled(after time.Duration, idleOK bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rec.closed() || (s.rec.Status == statusDone || idleOK && s.rec.Status == statusIdle) && !s.rec.Review && s.rec.OpenCount == 0 && !s.rec.connectWaits() && !s.rec.gateWaits() && !s.rec.taskOpen() && s.idleLocked(after) && (s.rec.ParentSession != "" || !s.asksLocked())
}

func (a *app) asidesSettled(tree []*session, after time.Duration) bool {
	for _, s := range tree {
		if slices.ContainsFunc(a.asides(s.snapshot().ID), func(x *session) bool { return !x.settled(after, true) }) {
			return false
		}
	}
	return true
}

type unlandedVerdict struct {
	tips     string
	unlanded bool
}

func (a *app) unlanded(ctx context.Context, rec record) bool {
	if rec.closed() || rec.RepoPath != "" || rec.ParentSession == "" {
		a.unlandedMu.Lock()
		delete(a.unlandedSeen, rec.ID)
		a.unlandedMu.Unlock()
		return false
	}
	repo, refs, err := a.landRefs(ctx, rec)
	if err != nil {
		return true
	}
	if refs == nil {
		return false
	}
	tips, err := gitrepo.Tips(ctx, repo, refs...)
	if err != nil {
		return true
	}
	if tips[0] == "" {
		return false
	}
	key := strings.Join(tips, " ")
	a.unlandedMu.Lock()
	v, ok := a.unlandedSeen[rec.ID]
	a.unlandedMu.Unlock()
	if ok && v.tips == key {
		return v.unlanded
	}
	out, err := gitrepo.Unlanded(ctx, repo, tips[0], slices.DeleteFunc(slices.Clone(tips[1:]), func(t string) bool { return t == "" })...)
	if err != nil {
		return true
	}
	a.unlandedMu.Lock()
	if a.unlandedSeen == nil {
		a.unlandedSeen = map[string]unlandedVerdict{}
	}
	a.unlandedSeen[rec.ID] = unlandedVerdict{tips: key, unlanded: out}
	a.unlandedMu.Unlock()
	return out
}

func (a *app) landRefs(ctx context.Context, rec record) (string, []string, error) {
	t, err := a.childTree(ctx, rec)
	if err != nil || t == nil {
		return "", nil, err
	}
	parent, err := a.parentBranch(ctx, rec, t)
	if err != nil || parent == "" || parent == t.own.Branch {
		return "", nil, err
	}
	refs := []string{headsPrefix + t.own.Branch, headsPrefix + parent}
	if t.isDefault(parent) {
		refs = append(refs, originRefPrefix+parent)
	}
	return t.primary.Path, refs, nil
}

func (a *app) childTree(ctx context.Context, rec record) (*taskTree, error) {
	if _, err := os.Stat(rec.Worktree); !errors.Is(err, os.ErrNotExist) {
		f, err := gitrepo.OpenFolder(ctx, rec.Worktree)
		if errors.Is(err, gitrepo.ErrNotRepo) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return taskTreeOf(ctx, f.Trees, f.Checkout), nil
	}
	top, ok := gitrepo.RepoRoot(rec.Worktree)
	if rec.ForkBranch == "" || !ok {
		return nil, nil
	}
	trees, err := gitrepo.Worktrees(ctx, top)
	if err != nil || len(trees) == 0 {
		return nil, err
	}
	return taskTreeOf(ctx, trees, gitrepo.Worktree{Path: rec.Worktree, Real: gitrepo.Canonical(rec.Worktree), Branch: rec.ForkBranch}), nil
}

func (a *app) parentBranch(ctx context.Context, rec record, t *taskTree) (string, error) {
	if p := a.session(rec.ParentSession); p != nil {
		prec := p.snapshot()
		if _, err := os.Stat(prec.Worktree); !errors.Is(err, os.ErrNotExist) {
			if w := gitrepo.Longest(t.trees, gitrepo.Canonical(prec.Worktree)); w != nil && w.Real != t.own.Real {
				return w.Branch, nil
			}
			return "", nil
		}
		if branch := cmp.Or(prec.ClosedBranch, prec.ForkBranch); branch != "" {
			return branch, nil
		}
	}
	if base := a.baseOf(ctx, rec); base != "" {
		return base, nil
	}
	return "", errParentBranch
}
