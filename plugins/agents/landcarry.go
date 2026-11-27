package main

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/landgate"
)

const (
	carryFilesShown = 10
	carryMoreFormat = "%s and %d more"
)

func carryable(ctx context.Context, repo, old0, tree0, old1, tree1 string) (bool, error) {
	_, ok, err := carryMove(ctx, repo, old0, tree0, old1, tree1)
	return ok, err
}

func carryMove(ctx context.Context, repo, old0, tree0, old1, tree1 string) ([]string, bool, error) {
	moved, err := changedNames(ctx, repo, old0, old1)
	if err != nil {
		return nil, false, err
	}
	landing, err := changedNames(ctx, repo, old1, tree1)
	if err != nil {
		return nil, false, err
	}
	if slices.ContainsFunc(landing, func(f string) bool { return slices.Contains(moved, f) }) {
		return moved, false, nil
	}
	before, err := patchOf(ctx, repo, old0, tree0)
	if err != nil {
		return nil, false, err
	}
	after, err := patchOf(ctx, repo, old1, tree1)
	if err != nil {
		return nil, false, err
	}
	return moved, bytes.Equal(before, after), nil
}

func changedNames(ctx context.Context, repo, from, to string) ([]string, error) {
	out, err := gitrepo.Run(ctx, repo, "diff", "--name-only", "--no-renames", "-z", from, to, "--")
	if err != nil {
		return nil, err
	}
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 }), nil
}

func patchOf(ctx context.Context, repo, from, to string) ([]byte, error) {
	return gitrepo.Run(ctx, repo, "diff", "--no-renames", "--no-ext-diff", "--no-textconv", "--full-index", "--binary", from, to, "--")
}

func (a *app) carriedMove(ctx context.Context, made landReview, args landGateArgs) (string, bool) {
	if made.Old == "" || made.Tree == "" || made.Old == args.Old {
		return "", false
	}
	if v, ok := a.readVerdict(ctx, made.Old, made.Tree); !ok || !reviewedApproval(v) {
		return "", false
	}
	moved, ok, err := carryMove(ctx, args.Target, made.Old, made.Tree, args.Old, args.Tree)
	logError("carry land review", err)
	if !ok || len(moved) == 0 {
		return "", false
	}
	return fmt.Sprintf(gateCarryFormat, shownFiles(moved), made.Old, args.Old, args.Old, args.Tree), true
}

func (a *app) carryTree(ctx context.Context, workspaceID string, g gateConfig, args landGateArgs) (landGateResult, bool, error) {
	prior, ok := a.treeVerdict(ctx, args.Target, args.Old, args.Tree)
	if !ok || prior.Old == args.Old || !reviewedApproval(prior) {
		return landGateResult{}, false, nil
	}
	v := landgate.Verdict{Old: args.Old, OldTree: prior.OldTree, Tree: args.Tree, Outcome: gateApproved, By: landgate.ByCarried, Reviewer: prior.Reviewer, Summary: prior.Summary, At: time.Now().UTC()}
	if err := a.writeVerdict(ctx, args.Target, v); err != nil {
		return landGateResult{}, false, err
	}
	round := 1
	if lander := a.session(args.Session); lander != nil {
		a.gateMu.Lock()
		st := lander.landReview(args.Into, args.Source)
		round = st.Rounds + 1
		if st.Old != "" {
			st.Carried, st.Old, st.Tree, st.Reason, st.Ahead, st.Last = prior.Old, args.Old, args.Tree, reasonBaseMoved, false, gateApproved
			lander.putLandReview(st)
		}
		a.gateMu.Unlock()
		a.saveNow(ctx, lander)
	}
	a.logGateVerdict(ctx, gateVerdict{Gate: g.ID, Workspace: workspaceID, Outcome: gateApproved, By: landgate.ByCarried, Lander: args.Session, Title: a.titleOf(args.Session), Into: args.Into, Source: args.Source, Round: round, Reviewer: v.Reviewer, Summary: v.Summary, Old: args.Old, OldTree: v.OldTree, Tree: args.Tree, Reason: reasonBaseMoved, CarriedFrom: prior.Old, At: v.At})
	a.gatesChanged()
	return landGateResult{State: gateApproved, Summary: reviewedSummary(v, true)}, true, nil
}

func reviewedApproval(v landgate.Verdict) bool {
	return v.Outcome == gateApproved && (v.By == landgate.ByReviewer || v.By == landgate.ByCheck || v.By == landgate.ByCarried)
}

func shownFiles(files []string) string {
	if len(files) <= carryFilesShown {
		return strings.Join(files, ", ")
	}
	return fmt.Sprintf(carryMoreFormat, strings.Join(files[:carryFilesShown], ", "), len(files)-carryFilesShown)
}

func carriedBy(st landReview, gf gateFor, v landgate.Verdict) string {
	if st.Carried == "" || v.Outcome != gateApproved || v.By != landgate.ByReviewer && v.By != landgate.ByCheck || gf.Old != st.Old || gf.Tree != st.Tree {
		return v.By
	}
	return landgate.ByCarried
}
