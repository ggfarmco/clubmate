package main

import (
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

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	worktreesDir   = ".worktrees"
	sessionRunning = "running"
	cleanupTimeout = time.Minute
)

var (
	errMainCheckout    = errors.New("cannot remove the main checkout")
	errFolderCheckout  = errors.New("cannot remove the checkout of the workspace folder")
	errDirtyWorktree   = errors.New("worktree has uncommitted changes")
	errRelativePath    = errors.New("path must be absolute")
	errSymlinkedFolder = errors.New("worktrees folder must not be a symlink")
)

func listWorktrees(ctx context.Context, h *pluginsdk.Host, projectID string) ([]worktreeInfo, error) {
	list := []worktreeInfo{}
	err := eachFolder(ctx, h, projectID, func(f *folder) error {
		heads, err := f.shortHeads(ctx)
		if err != nil {
			return err
		}
		for j, t := range f.Trees {
			if t.Bare {
				continue
			}
			list = append(list, worktreeInfo{Repo: f.Repo, Path: t.Path, Branch: t.Branch, Head: heads[t.Head], Main: j == 0, Current: f.current(t)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

func switchWorktree(ctx context.Context, h *pluginsdk.Host, projectID string, s *state, repo, path string) error {
	if !s.claim() {
		return errOpBusy
	}
	defer s.release()
	f, err := gitFolder(ctx, h, projectID, repo)
	if err != nil {
		return err
	}
	t, err := f.member(path)
	if err != nil {
		return err
	}
	_, err = h.SetWorktree(ctx, projectID, f.Repo, f.Selection(t))
	return err
}

func worktreeSessions(ctx context.Context, h *pluginsdk.Host, projectID, path string) ([]sessionInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, errRelativePath
	}
	dir := gitrepo.Canonical(filepath.Clean(path))
	sessions, err := h.ListSessions(ctx, projectID, false)
	if err != nil {
		return nil, err
	}
	out := []sessionInfo{}
	for _, s := range sessions {
		if s.State == sessionRunning && s.Owner != editorOwner && filepath.IsAbs(s.Cwd) && gitrepo.Within(gitrepo.Canonical(s.Cwd), dir) {
			out = append(out, sessionInfo{ID: s.ID, Title: s.Title})
		}
	}
	return out, nil
}

func prepareNew(ctx context.Context, h *pluginsdk.Host, projectID string, args worktreeArgs) (opFunc, error) {
	f, err := gitFolder(ctx, h, projectID, args.Repo)
	if err != nil {
		return nil, err
	}
	cur := f.currentTree()
	if err := validBranch(ctx, cur.Path, args.Branch); err != nil {
		return nil, err
	}
	primary := f.Trees[0]
	dir, err := worktreeDir(primary.Path, args.Branch)
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
	k := hooks{h: h, projectID: projectID, primary: primary.Path}
	add := []string{"worktree", "add", "-b", args.Branch, dir, base}
	if hasRef(ctx, cur.Path, headsPrefix+args.Branch) {
		add = []string{"worktree", "add", dir, args.Branch}
	}
	return func(ctx context.Context, note func(string)) (string, error) {
		if _, err := mutate(ctx, cur.Path, add...); err != nil {
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
		if len(notes) > 0 {
			note(strings.Join(notes, "\n"))
		}
		g, err := gitFolder(ctx, h, projectID, f.Repo)
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
		if _, err := h.SetWorktree(ctx, projectID, g.Repo, g.Selection(t)); err != nil {
			return "", err
		}
		return t.Path, nil
	}, nil
}

func prepareRemove(ctx context.Context, h *pluginsdk.Host, projectID string, args worktreeArgs) (opFunc, error) {
	f, err := gitFolder(ctx, h, projectID, args.Repo)
	if err != nil {
		return nil, err
	}
	t, err := f.member(args.Path)
	if err != nil {
		return nil, err
	}
	switch t.Real {
	case f.Trees[0].Real:
		return nil, errMainCheckout
	case f.Checkout.Real:
		return nil, errFolderCheckout
	}
	k := hooks{h: h, projectID: projectID, primary: f.Trees[0].Path}
	users := consumers{h: h, projectID: projectID}
	return func(ctx context.Context, note func(string)) (string, error) {
		status, err := run(ctx, t.Path, "status", "--porcelain")
		if err != nil {
			return "", err
		}
		if len(strings.TrimSpace(string(status))) > 0 {
			return "", errDirtyWorktree
		}
		g, err := gitFolder(ctx, h, projectID, f.Repo)
		if err != nil {
			return "", err
		}
		root := filepath.Join(t.Path, g.Rel)
		if g.current(t) {
			root = g.root
		}
		defer func() {
			logError("release "+root, users.releaseEditor(context.WithoutCancel(ctx), root))
		}()
		if err := users.stopEditor(ctx, root, args.Force); err != nil {
			return "", err
		}
		logError("stop agents in "+t.Path, users.stopAgents(ctx, t.Path))
		if skipped, err := k.run(ctx, t.Path, teardownScript); err != nil {
			note(err.Error())
		} else if skipped != "" {
			note(skipped)
		}
		if _, err := mutate(ctx, f.Checkout.Path, "worktree", "remove", t.Path); err != nil {
			return "", err
		}
		p, err := h.GetProject(ctx, projectID)
		if err != nil {
			return "", err
		}
		if slices.ContainsFunc(p.Missing, func(m string) bool { return gitrepo.Canonical(m) == g.Mapped(t) }) {
			_, err = h.SetWorktree(ctx, projectID, f.Repo, "")
		}
		return "", err
	}, nil
}

func worktreeDir(primary, branch string) (string, error) {
	base := filepath.Join(primary, worktreesDir)
	dir := filepath.Join(base, branch)
	cur := base
	for _, part := range append([]string{""}, strings.Split(branch, "/")...) {
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
			return "", fmt.Errorf("%s already exists", filepath.Join(worktreesDir, branch))
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

func removeFailed(ctx context.Context, repo, dir string, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	_, err := mutate(ctx, repo, "worktree", "remove", "--force", dir)
	return errors.Join(cause, err)
}
