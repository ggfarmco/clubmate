package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	maxLocalBranches  = 1500
	maxRemoteBranches = 500
	leavingMarker     = "you are leaving"
)

var errUnknownBranch = errors.New("unknown branch")

func listBranches(ctx context.Context, h *pluginsdk.Host, projectID string) ([]branchInfo, error) {
	list := []branchInfo{}
	err := eachFolder(ctx, h, projectID, func(f *folder) error {
		local, remote, err := gitrepo.RecentBranches(ctx, f.Checkout.Path)
		if err != nil {
			return gitError(nil, err)
		}
		remotes, err := gitrepo.Remotes(ctx, f.Checkout.Path)
		if err != nil {
			return gitError(nil, err)
		}
		taken := map[string]bool{}
		for _, t := range f.Trees {
			if t.Branch != "" {
				taken[t.Branch] = true
			}
		}
		var locals, tracking []branchInfo
		for _, b := range local {
			if !taken[b] {
				locals = append(locals, branchInfo{Repo: f.Repo, Name: b})
			}
			taken[b] = true
		}
		for _, b := range remote {
			if name, ok := localName(b, remotes); ok && !taken[name] {
				tracking = append(tracking, branchInfo{Repo: f.Repo, Name: b, Remote: true})
			}
		}
		list = append(list, locals[:min(len(locals), maxLocalBranches)]...)
		list = append(list, tracking[:min(len(tracking), maxRemoteBranches)]...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

func prepareBranchSwitch(ctx context.Context, h *pluginsdk.Host, projectID string, args worktreeArgs) (opFunc, error) {
	f, err := gitFolder(ctx, h, projectID, args.Repo)
	if err != nil {
		return nil, err
	}
	cur := f.currentTree()
	if err := validBranch(ctx, cur.Path, args.Branch); err != nil {
		return nil, err
	}
	local, track := args.Branch, ""
	if !hasRef(ctx, cur.Path, headsPrefix+local) {
		remotes, err := gitrepo.Remotes(ctx, cur.Path)
		if err != nil {
			return nil, gitError(nil, err)
		}
		name, ok := localName(args.Branch, remotes)
		if !ok || !hasRef(ctx, cur.Path, remotesPrefix+args.Branch) {
			return nil, fmt.Errorf("%w %s", errUnknownBranch, args.Branch)
		}
		local = name
		if !hasRef(ctx, cur.Path, headsPrefix+local) {
			track = args.Branch
		}
	}
	if i := slices.IndexFunc(f.Trees, func(t gitrepo.Worktree) bool { return !t.Bare && t.Branch == local }); i >= 0 {
		t := f.Trees[i]
		return func(ctx context.Context, _ func(string)) (string, error) {
			_, err := h.SetWorktree(ctx, projectID, f.Repo, f.Selection(t))
			return t.Path, err
		}, nil
	}
	users := consumers{h: h, projectID: projectID}
	if err := users.checkUnsaved(ctx, f.root); err != nil {
		return nil, err
	}
	switchArgs := []string{"switch", "--no-guess", "--", local}
	if track != "" {
		switchArgs = []string{"switch", "--track", "--", track}
	}
	return func(ctx context.Context, note func(string)) (string, error) {
		stderr, err := mutateNotice(ctx, cur.Path, switchArgs...)
		if err != nil {
			return "", err
		}
		if text := leftBehind(stderr); text != "" {
			note(text)
		}
		logError("checktime "+f.root, users.checktime(ctx, f.root))
		return cur.Path, nil
	}, nil
}

func leftBehind(stderr string) string {
	at := strings.Index(stderr, leavingMarker)
	if at < 0 {
		return ""
	}
	start := strings.LastIndex(stderr[:at], "\n") + 1
	paragraphs := strings.SplitN(stderr[start:], "\n\n", 3)
	return strings.Join(strings.Fields(strings.Join(paragraphs[:min(2, len(paragraphs))], " ")), " ")
}

func localName(ref string, remotes []string) (string, bool) {
	best := ""
	for _, r := range remotes {
		if strings.HasPrefix(ref, r+"/") && len(r) > len(best) {
			best = r
		}
	}
	if best == "" || len(ref) == len(best)+1 {
		return "", false
	}
	return ref[len(best)+1:], true
}
