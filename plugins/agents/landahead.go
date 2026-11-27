package main

import (
	"context"
	"strings"
	"time"

	"clubmate/plugins/internal/gitrepo"
)

const (
	aheadKey     = "landQueue.speculate"
	aheadID      = "land-queue-speculate"
	aheadHint    = "While a landing waits for its review, the gate also reviews the next landing in the queue on the tree the first one leaves. It costs one more review run and gate worktree."
	aheadMessage = "gate ahead"
	aheadLimit   = 5 * time.Minute
	aheadNote    = "\nThis is a review ahead: %s is not the tip of %s yet but the tree it holds after the landing ahead of this one lands."
)

func (a *app) aheadEnabled(ctx context.Context) bool {
	var on bool
	found, err := a.host.StorageGet(ctx, aheadKey, &on)
	return err == nil && found && on
}

func (a *app) setAheadEnabled(ctx context.Context, on bool) error {
	if err := a.host.StorageSet(ctx, aheadKey, on); err != nil {
		return err
	}
	a.queues.mu.Lock()
	a.queues.shown = on
	a.queues.mu.Unlock()
	a.refreshSettings(ctx)
	for _, q := range a.queueList() {
		a.kick(q.Workspace)
	}
	if on {
		go a.checkQueues("")
	}
	return nil
}

func (a *app) aheadShown() bool {
	a.queues.mu.Lock()
	defer a.queues.mu.Unlock()
	return a.queues.shown
}

func (a *app) reviewAheadOf(ctx context.Context, args landGateArgs) {
	if _, common, err := gitrepo.Dirs(ctx, args.Target); err == nil {
		go a.reviewAhead(queueID(common, args.Into))
	}
}

func (a *app) reviewAhead(qid string) {
	ctx, cancel := context.WithTimeout(context.Background(), aheadLimit)
	defer cancel()
	if !a.aheadEnabled(ctx) {
		return
	}
	a.queues.mu.Lock()
	if a.queues.ahead == nil {
		a.queues.ahead = map[string]bool{}
	}
	if _, busy := a.queues.ahead[qid]; busy {
		a.queues.ahead[qid] = true
		a.queues.mu.Unlock()
		return
	}
	a.queues.ahead[qid] = false
	a.queues.mu.Unlock()
	for {
		a.reviewAheadOnce(ctx, qid)
		a.queues.mu.Lock()
		again := a.queues.ahead[qid]
		if !again || ctx.Err() != nil {
			delete(a.queues.ahead, qid)
			a.queues.mu.Unlock()
			return
		}
		a.queues.ahead[qid] = false
		a.queues.mu.Unlock()
	}
}

func (a *app) reviewAheadOnce(ctx context.Context, qid string) {
	a.queues.mu.Lock()
	cur, ok := a.queues.byID[qid]
	var q landQueue
	if ok {
		q = cur.clone()
	}
	a.queues.mu.Unlock()
	if !ok {
		return
	}
	h := q.head()
	if h < 0 || h+1 >= len(q.Entries) {
		return
	}
	head, next := q.Entries[h], q.Entries[h+1]
	if !a.aheadDue(q, head, next) {
		return
	}
	lander := a.session(next.Session)
	g := a.landingGate(ctx, q.Workspace, next.Session, q.Target, q.Into, next.Source)
	if lander == nil || g == nil {
		return
	}
	if st := lander.landReview(q.Into, next.Source); st.Request != "" || st.Rounds >= g.Rounds+st.Extra {
		return
	}
	base, err := gitrepo.CommitTree(ctx, q.Target, head.Tree, head.Old, aheadMessage)
	if err != nil {
		logError("make the predicted base", err)
		return
	}
	tree, ok := aheadTree(ctx, q.Target, base, next.SourceHead)
	if !ok || tree == head.Tree {
		return
	}
	args := landGateArgs{Session: next.Session, Target: q.Target, Worktree: next.Worktree, Into: q.Into, Source: next.Source, Head: next.SourceHead, Old: base, Tree: tree, ahead: true}
	if _, decided := a.readVerdict(ctx, base, tree); !decided && !docsOnly(ctx, q.Target, base, tree) {
		if _, err := a.reviewLanding(ctx, q.Workspace, lander, *g, args, a.gateGraphReady(ctx, *g)); err != nil {
			logError("review the next landing ahead", err)
			return
		}
		if st := lander.landReview(q.Into, next.Source); st.Old != base || st.Tree != tree {
			return
		}
	}
	_, err = a.mutateQueue(ctx, qid, nil, "", func(q *landQueue) error {
		i := q.entry(next.ID)
		if i < 0 || q.Entries[i].SourceHead != next.SourceHead {
			return nil
		}
		e := &q.Entries[i]
		e.Ahead, e.AheadOn, e.AheadBase, e.AheadHead, e.AheadOld, e.AheadTree = true, head.Old, head.Tree, next.SourceHead, base, tree
		return nil
	})
	logError("mark the landing reviewed ahead", err)
}

func (a *app) aheadDue(q landQueue, head, next queueEntry) bool {
	if head.State != queueHead && head.State != queueAsks || head.Tree == "" || head.Old != q.Tip {
		return false
	}
	if next.Mode != queueModeBranch || next.CalledAt.IsZero() || !next.Clean || next.SourceHead == "" || next.Passed {
		return false
	}
	if aheadValid(head, next) {
		return false
	}
	s := a.session(head.Session)
	if s == nil {
		return false
	}
	st := s.landReview(q.Into, head.Source)
	return st.Run != "" && st.Old == head.Old && st.Tree == head.Tree
}

func aheadValid(head, next queueEntry) bool {
	return next.Ahead && next.AheadOn == head.Old && next.AheadBase == head.Tree && next.AheadHead == next.SourceHead
}

func aheadTree(ctx context.Context, repo, base, head string) (string, bool) {
	out, err := gitrepo.Run(ctx, repo, "merge-tree", "--write-tree", "--name-only", "--no-messages", base, head)
	if err != nil {
		return "", false
	}
	tree, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return tree, tree != ""
}
