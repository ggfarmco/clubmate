package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	gateTreeMessage = "gate"
	gateIdentity    = "user.name=Clubmate"
	gateEmail       = "user.email=gate@clubmate.invalid"
	setupUntrusted  = "untrusted"
)

var (
	errGateTreeRefused = errors.New("git.gate-tree is for the agents plugin only")
	errGateTreeArgs    = errors.New("gate, key, old and tree are required")
	gateIDPattern      = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
)

type gateTreeResult struct {
	Path   string `json:"path,omitempty"`
	Commit string `json:"commit,omitempty"`
	Setup  string `json:"setup,omitempty"`
}

func gateTreeCommand(ctx context.Context, a *app, c call) (any, error) {
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
		gate, key := c.args.Gate, c.args.Key
		return gateTreeResult{}, a.sweepGateTrees(ctx, primary, func(name string, _ bool) bool { return gateTreeOf(name, gate, key) })
	}
	if c.args.Key == "" || c.args.Old == "" || c.args.Tree == "" {
		return nil, errGateTreeArgs
	}
	res, made, err := a.prepareGateTree(ctx, primary, c.args.Gate, c.args.Key, c.args.Old, c.args.Tree)
	if err != nil || !made {
		return res, err
	}
	logError("copy included files into "+res.Path, copyIncluded(primary, res.Path))
	k := hooks{h: c.h, workspaceID: c.workspaceID, primary: primary}
	if _, err := os.Lstat(filepath.Join(res.Path, setupScript)); errors.Is(err, fs.ErrNotExist) {
		return res, nil
	}
	if !k.trusted(ctx, res.Path, setupScript) {
		res.Setup = setupUntrusted
		return res, nil
	}
	if _, err := k.run(ctx, res.Path, setupScript); err != nil {
		return nil, removeFailed(ctx, primary, res.Path, err)
	}
	return res, nil
}

func (a *app) prepareGateTree(ctx context.Context, primary, gate, key, old, tree string) (gateTreeResult, bool, error) {
	a.gateTreeMu.Lock()
	defer a.gateTreeMu.Unlock()
	f, err := gitrepo.OpenFolder(ctx, primary)
	if err != nil {
		return gateTreeResult{}, false, gitError(nil, err)
	}
	dir := gateTreeDir(primary, gate, key)
	_, member := f.Member(dir)
	if _, err := os.Lstat(dir); member && errors.Is(err, os.ErrNotExist) {
		if _, err := mutateInert(ctx, primary, "worktree", "prune"); err != nil {
			return gateTreeResult{}, false, err
		}
		member = false
	}
	if member {
		commit, err := gateCommit(ctx, dir, old, tree)
		if err != nil {
			return gateTreeResult{}, false, err
		}
		return gateTreeResult{Path: dir, Commit: commit}, false, resetGateTree(ctx, dir, commit)
	}
	commit, err := gateCommit(ctx, primary, old, tree)
	if err != nil {
		return gateTreeResult{}, false, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return gateTreeResult{}, false, err
	}
	if _, err := mutateInert(ctx, primary, "worktree", "add", "--detach", dir, commit); err != nil {
		return gateTreeResult{}, false, err
	}
	if err := excludeWorktrees(ctx, primary); err != nil {
		return gateTreeResult{}, false, removeFailed(ctx, primary, dir, err)
	}
	return gateTreeResult{Path: dir, Commit: commit}, true, nil
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

func (a *app) sweepGateTrees(ctx context.Context, primary string, match func(name string, registered bool) bool) error {
	a.gateTreeMu.Lock()
	defer a.gateTreeMu.Unlock()
	f, err := gitrepo.OpenFolder(ctx, primary)
	if err != nil {
		return gitError(nil, err)
	}
	registered := map[string]bool{}
	prune := false
	var errs []error
	for _, t := range f.Trees[1:] {
		registered[filepath.Base(t.Path)], registered[filepath.Base(t.Real)] = true, true
		if !f.GateTree(t) || !match(filepath.Base(t.Real), true) {
			continue
		}
		if _, err := os.Lstat(t.Path); errors.Is(err, fs.ErrNotExist) {
			prune = true
			continue
		}
		if _, err := mutateInert(ctx, primary, "worktree", "remove", "--force", t.Path); err != nil {
			errs = append(errs, err)
		}
	}
	root := filepath.Join(primary, worktreesDir)
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, gitrepo.GateTreePrefix) || !match(name, registered[name]) {
			continue
		}
		errs = append(errs, os.RemoveAll(filepath.Join(root, name)))
		prune = prune || registered[name]
	}
	if prune {
		_, err := mutateInert(ctx, primary, "worktree", "prune")
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

func staleGateTree(name string, registered bool) bool {
	return !registered || !strings.Contains(strings.TrimPrefix(name, gitrepo.GateTreePrefix), "-")
}

func (a *app) firstOpen(workspaceID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sweptWS[workspaceID] {
		return false
	}
	if a.sweptWS == nil {
		a.sweptWS = map[string]bool{}
	}
	a.sweptWS[workspaceID] = true
	return true
}

func (a *app) sweepStartTrees(h *pluginsdk.Host, workspaceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	logError("sweep gate worktrees of "+workspaceID, eachFolder(ctx, h, workspaceID, func(f *folder) error {
		primary := f.Trees[0].Real
		a.mu.Lock()
		done := a.swept[primary]
		if a.swept == nil {
			a.swept = map[string]bool{}
		}
		a.swept[primary] = true
		a.mu.Unlock()
		if done {
			return nil
		}
		logError("sweep gate worktrees in "+primary, a.sweepGateTrees(ctx, f.Trees[0].Path, staleGateTree))
		return nil
	}))
}
