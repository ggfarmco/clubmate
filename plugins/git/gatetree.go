package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"clubmate/plugins/internal/gitrepo"
)

const (
	gateTreeMessage = "gate"
	gateIdentity    = "user.name=Clubmate"
	gateEmail       = "user.email=gate@clubmate.invalid"
)

var (
	errGateTreeRefused = errors.New("git.gate-tree is for the agents plugin only")
	errGateTreeArgs    = errors.New("gate, key, old and tree are required")
	gateIDPattern      = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
)

type gateTreeResult struct {
	Path   string `json:"path,omitempty"`
	Commit string `json:"commit,omitempty"`
}

func gateTreeCommand(ctx context.Context, _ *app, c call) (any, error) {
	if c.via != discardPlugin || c.agentCall {
		return nil, errGateTreeRefused
	}
	if !gateIDPattern.MatchString(c.args.Gate) {
		return nil, fmt.Errorf("gate %q is not a gate id", c.args.Gate)
	}
	if c.args.Key != "" && !gateIDPattern.MatchString(c.args.Key) {
		return nil, fmt.Errorf("key %q is not a landing key", c.args.Key)
	}
	f, err := gitrepo.OpenFolder(ctx, c.args.Repo)
	if err != nil {
		return nil, gitError(nil, err)
	}
	primary := f.Trees[0].Path
	if c.args.Remove {
		return gateTreeResult{}, removeGateTrees(ctx, f, primary, c.args.Gate, c.args.Key)
	}
	if c.args.Key == "" || c.args.Old == "" || c.args.Tree == "" {
		return nil, errGateTreeArgs
	}
	dir := gateTreeDir(primary, c.args.Gate, c.args.Key)
	_, member := f.Member(dir)
	if _, err := os.Lstat(dir); member && errors.Is(err, os.ErrNotExist) {
		if _, err := mutateInert(ctx, primary, "worktree", "prune"); err != nil {
			return nil, err
		}
		member = false
	}
	if member {
		commit, err := gateCommit(ctx, dir, c.args.Old, c.args.Tree)
		if err != nil {
			return nil, err
		}
		return gateTreeResult{Path: dir, Commit: commit}, resetGateTree(ctx, dir, commit)
	}
	commit, err := gateCommit(ctx, primary, c.args.Old, c.args.Tree)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if _, err := mutateInert(ctx, primary, "worktree", "add", "--detach", dir, commit); err != nil {
		return nil, err
	}
	if err := excludeWorktrees(ctx, primary); err != nil {
		return nil, removeFailed(ctx, primary, dir, err)
	}
	k := hooks{h: c.h, workspaceID: c.workspaceID, primary: primary}
	if k.trusted(ctx, dir, setupScript) {
		if _, err := k.run(ctx, dir, setupScript); err != nil {
			return nil, removeFailed(ctx, primary, dir, err)
		}
	}
	return gateTreeResult{Path: dir, Commit: commit}, nil
}

func gateTreeDir(primary, gate, key string) string {
	return filepath.Join(primary, worktreesDir, gitrepo.GateTreePrefix+gate+"-"+key)
}

func gateCommit(ctx context.Context, dir, old, tree string) (string, error) {
	if out, err := inert(ctx, dir, "rev-parse", "HEAD", "HEAD^{tree}", "HEAD^"); err == nil {
		if f := strings.Fields(string(out)); len(f) == 3 && f[1] == tree && f[2] == old {
			return f[0], nil
		}
	}
	out, err := mutateInert(ctx, dir, "-c", gateIdentity, "-c", gateEmail, "commit-tree", tree, "-p", old, "-m", gateTreeMessage)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func resetGateTree(ctx context.Context, dir, commit string) error {
	if _, err := mutateInert(ctx, dir, "checkout", "-f", "--detach", commit); err != nil {
		return err
	}
	_, err := mutateInert(ctx, dir, "clean", "-fdq")
	return err
}

func removeGateTrees(ctx context.Context, f *gitrepo.Folder, primary, gate, key string) error {
	var errs []error
	pruned := false
	for _, t := range f.Trees {
		if !f.GateTree(t) || !gateTreeOf(filepath.Base(t.Real), gate, key) {
			continue
		}
		if _, err := os.Lstat(t.Path); errors.Is(err, os.ErrNotExist) {
			if !pruned {
				_, err := mutateInert(ctx, primary, "worktree", "prune")
				errs, pruned = append(errs, err), true
			}
			continue
		}
		_, err := mutateInert(ctx, primary, "worktree", "remove", "--force", t.Path)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func gateTreeOf(name, gate, key string) bool {
	own := gitrepo.GateTreePrefix + gate
	if key != "" {
		return name == own+"-"+key
	}
	return name == own || strings.HasPrefix(name, own+"-")
}
