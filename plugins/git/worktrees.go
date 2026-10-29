package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	worktreesDir    = ".worktrees"
	stopRootCommand = "editor-nvim.stop-root"
	sessionRunning  = "running"
)

var (
	errNotGitFolder   = errors.New("not a git folder")
	errNoGitFolder    = errors.New("no git folder in the workspace")
	errMainCheckout   = errors.New("cannot remove the main checkout")
	errFolderCheckout = errors.New("cannot remove the checkout of the workspace folder")
	errDirtyWorktree  = errors.New("worktree has uncommitted changes")
	errRelativePath   = errors.New("path must be absolute")
)

type worktree struct {
	path   string
	real   string
	head   string
	branch string
	bare   bool
}

type folder struct {
	repo     string
	root     string
	real     string
	rel      string
	trees    []worktree
	checkout worktree
}

type worktreeInfo struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Head    string `json:"head"`
	Main    bool   `json:"main"`
	Current bool   `json:"current"`
}

type worktreeArgs struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Force  bool   `json:"force"`
}

type stopRootArgs struct {
	Root  string `json:"root"`
	Force bool   `json:"force,omitempty"`
}

type sessionInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func worktreeCommand(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	var args worktreeArgs
	if len(cmd.Args) > 0 {
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("%s args: %w", cmd.ID, err)
		}
	}
	switch cmd.ID {
	case "git.worktrees":
		return listWorktrees(ctx, h, cmd.ProjectID)
	case "git.worktree-switch":
		return nil, switchWorktree(ctx, h, cmd.ProjectID, args)
	case "git.worktree-new":
		return nil, newWorktree(ctx, h, cmd.ProjectID, args)
	case "git.worktree-sessions":
		return worktreeSessions(ctx, h, cmd.ProjectID, args.Path)
	case "git.worktree-remove":
		return nil, removeWorktree(ctx, h, cmd.ProjectID, args)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func listWorktrees(ctx context.Context, h *pluginsdk.Host, projectID string) ([]worktreeInfo, error) {
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	list := []worktreeInfo{}
	for i := range p.Repos {
		f, err := openFolder(ctx, p, i)
		if errors.Is(err, errNotGitFolder) {
			continue
		}
		if err != nil {
			return nil, err
		}
		heads, err := f.shortHeads(ctx)
		if err != nil {
			return nil, err
		}
		for j, t := range f.trees {
			if t.bare {
				continue
			}
			list = append(list, worktreeInfo{Repo: f.repo, Path: t.path, Branch: t.branch, Head: heads[t.head], Main: j == 0, Current: f.current(t)})
		}
	}
	return list, nil
}

func switchWorktree(ctx context.Context, h *pluginsdk.Host, projectID string, args worktreeArgs) error {
	f, err := gitFolder(ctx, h, projectID, args.Repo)
	if err != nil {
		return err
	}
	t, err := f.member(args.Path)
	if err != nil {
		return err
	}
	_, err = h.SetWorktree(ctx, projectID, f.repo, f.selection(t))
	return err
}

func newWorktree(ctx context.Context, h *pluginsdk.Host, projectID string, args worktreeArgs) error {
	f, err := gitFolder(ctx, h, projectID, args.Repo)
	if err != nil {
		return err
	}
	cur := f.currentTree()
	out, err := run(ctx, cur.path, "check-ref-format", "--branch", args.Branch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != args.Branch {
		return fmt.Errorf("'%s' is not a valid branch name", args.Branch)
	}
	primary := f.trees[0]
	dir := filepath.Join(primary.path, worktreesDir, args.Branch)
	if _, err := os.Lstat(dir); err == nil {
		return fmt.Errorf("%s already exists", filepath.Join(worktreesDir, args.Branch))
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	add := []string{"worktree", "add", "-b", args.Branch, dir, currentHead}
	if _, err := gitrepo.Run(ctx, cur.path, "rev-parse", "-q", "--verify", "refs/heads/"+args.Branch); err == nil {
		add = []string{"worktree", "add", dir, args.Branch}
	}
	if _, err := mutate(ctx, cur.path, add...); err != nil {
		return err
	}
	if err := excludeWorktrees(ctx, primary.path); err != nil {
		return err
	}
	return switchWorktree(ctx, h, projectID, worktreeArgs{Repo: f.repo, Path: dir})
}

func worktreeSessions(ctx context.Context, h *pluginsdk.Host, projectID, path string) ([]sessionInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, errRelativePath
	}
	dir := canonical(filepath.Clean(path))
	sessions, err := h.ListSessions(ctx, projectID, false)
	if err != nil {
		return nil, err
	}
	out := []sessionInfo{}
	for _, s := range sessions {
		if s.State == sessionRunning && filepath.IsAbs(s.Cwd) && within(canonical(s.Cwd), dir) {
			out = append(out, sessionInfo{ID: s.ID, Title: s.Title})
		}
	}
	return out, nil
}

func removeWorktree(ctx context.Context, h *pluginsdk.Host, projectID string, args worktreeArgs) error {
	f, err := gitFolder(ctx, h, projectID, args.Repo)
	if err != nil {
		return err
	}
	t, err := f.member(args.Path)
	if err != nil {
		return err
	}
	switch t.real {
	case f.trees[0].real:
		return errMainCheckout
	case f.checkout.real:
		return errFolderCheckout
	}
	status, err := run(ctx, t.path, "status", "--porcelain")
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(status))) > 0 {
		return errDirtyWorktree
	}
	current := f.current(t)
	root := filepath.Join(t.path, f.rel)
	if current {
		root = f.root
	}
	if err := h.RunCommand(ctx, stopRootCommand, projectID, stopRootArgs{Root: root, Force: args.Force}, nil); err != nil {
		return err
	}
	if _, err := mutate(ctx, f.checkout.path, "worktree", "remove", t.path); err != nil {
		return err
	}
	if current {
		_, err = h.SetWorktree(ctx, projectID, f.repo, "")
	}
	return err
}

func gitFolder(ctx context.Context, h *pluginsdk.Host, projectID, repo string) (*folder, error) {
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for i, r := range p.Repos {
		if repo != "" && r != repo {
			continue
		}
		f, err := openFolder(ctx, p, i)
		if repo != "" || err == nil {
			return f, err
		}
		if !errors.Is(err, errNotGitFolder) {
			return nil, err
		}
	}
	if repo != "" {
		return nil, fmt.Errorf("%w: %q", errUnknownRepo, repo)
	}
	return nil, errNoGitFolder
}

func openFolder(ctx context.Context, p pluginsdk.Project, i int) (*folder, error) {
	repo := p.Repos[i]
	top, ok := repoRoot(repo)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNotGitFolder, repo)
	}
	trees, err := worktrees(ctx, top)
	if err != nil {
		return nil, err
	}
	resolved := canonical(repo)
	checkout := longest(trees, resolved)
	if checkout == nil {
		return nil, fmt.Errorf("%w: %s", errNotGitFolder, repo)
	}
	rel, err := filepath.Rel(checkout.real, resolved)
	if err != nil {
		return nil, err
	}
	root := repo
	if i < len(p.Roots) {
		root = p.Roots[i]
	}
	return &folder{repo: repo, root: root, real: resolved, rel: rel, trees: trees, checkout: *checkout}, nil
}

func (f *folder) member(path string) (worktree, error) {
	if filepath.IsAbs(path) {
		target := canonical(filepath.Clean(path))
		for _, t := range f.trees {
			if t.real == target && !t.bare {
				return t, nil
			}
		}
	}
	return worktree{}, fmt.Errorf("not a worktree of %s", f.repo)
}

func (f *folder) selection(t worktree) string {
	if filepath.Join(t.real, f.rel) == f.real {
		return ""
	}
	return filepath.Join(t.path, f.rel)
}

func (f *folder) current(t worktree) bool {
	return canonical(f.root) == filepath.Join(t.real, f.rel)
}

func (f *folder) currentTree() worktree {
	if t := longest(f.trees, canonical(f.root)); t != nil {
		return *t
	}
	return f.checkout
}

func (f *folder) shortHeads(ctx context.Context) (map[string]string, error) {
	heads := map[string]string{}
	for _, t := range f.trees {
		if _, ok := heads[t.head]; ok || t.head == "" {
			continue
		}
		out, err := run(ctx, f.checkout.path, "rev-parse", "--short", t.head)
		if err != nil {
			return nil, err
		}
		heads[t.head] = strings.TrimSpace(string(out))
	}
	return heads, nil
}

func worktrees(ctx context.Context, top string) ([]worktree, error) {
	out, err := gitrepo.Run(ctx, top, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, gitError(out, err)
	}
	var trees []worktree
	for field := range strings.SplitSeq(string(out), "\x00") {
		if path, ok := strings.CutPrefix(field, "worktree "); ok {
			trees = append(trees, worktree{path: path, real: canonical(path)})
			continue
		}
		if len(trees) == 0 {
			continue
		}
		t := &trees[len(trees)-1]
		switch {
		case strings.HasPrefix(field, "HEAD "):
			t.head = strings.TrimPrefix(field, "HEAD ")
		case strings.HasPrefix(field, "branch "):
			t.branch = strings.TrimPrefix(strings.TrimPrefix(field, "branch "), "refs/heads/")
		case field == "bare":
			t.bare = true
		}
	}
	return trees, nil
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

func longest(trees []worktree, path string) *worktree {
	var best *worktree
	for i, t := range trees {
		if within(path, t.real) && (best == nil || len(t.real) > len(best.real)) {
			best = &trees[i]
		}
	}
	return best
}

func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

func repoRoot(dir string) (string, bool) {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func canonical(path string) string {
	rest := ""
	for dir := path; ; {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}
