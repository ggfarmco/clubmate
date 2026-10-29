package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrNotRepo = errors.New("not a git folder")

type Worktree struct {
	Path   string
	Real   string
	Head   string
	Branch string
	Bare   bool
}

type Folder struct {
	Repo     string
	Real     string
	Rel      string
	Trees    []Worktree
	Checkout Worktree
}

func OpenFolder(ctx context.Context, repo string) (*Folder, error) {
	top, ok := RepoRoot(repo)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotRepo, repo)
	}
	trees, err := Worktrees(ctx, top)
	if err != nil {
		return nil, err
	}
	resolved := Canonical(repo)
	checkout := Longest(trees, resolved)
	if checkout == nil {
		return nil, fmt.Errorf("%w: %s", ErrNotRepo, repo)
	}
	rel, err := filepath.Rel(checkout.Real, resolved)
	if err != nil {
		return nil, err
	}
	return &Folder{Repo: repo, Real: resolved, Rel: rel, Trees: trees, Checkout: *checkout}, nil
}

func (f *Folder) Mapped(t Worktree) string {
	return filepath.Join(t.Real, f.Rel)
}

func (f *Folder) Selection(t Worktree) string {
	if f.Mapped(t) == f.Real {
		return ""
	}
	return filepath.Join(t.Path, f.Rel)
}

func (f *Folder) Member(path string) (Worktree, bool) {
	if !filepath.IsAbs(path) {
		return Worktree{}, false
	}
	target := Canonical(filepath.Clean(path))
	for _, t := range f.Trees {
		if t.Real == target && !t.Bare {
			return t, true
		}
	}
	return Worktree{}, false
}

func Worktrees(ctx context.Context, top string) ([]Worktree, error) {
	out, err := Run(ctx, top, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	var trees []Worktree
	for field := range strings.SplitSeq(string(out), "\x00") {
		if path, ok := strings.CutPrefix(field, "worktree "); ok {
			trees = append(trees, Worktree{Path: path, Real: Canonical(path)})
			continue
		}
		if len(trees) == 0 {
			continue
		}
		t := &trees[len(trees)-1]
		if head, ok := strings.CutPrefix(field, "HEAD "); ok {
			t.Head = head
		} else if branch, ok := strings.CutPrefix(field, "branch "); ok {
			t.Branch = strings.TrimPrefix(branch, "refs/heads/")
		} else if field == "bare" {
			t.Bare = true
		}
	}
	return trees, nil
}

func Longest(trees []Worktree, path string) *Worktree {
	var best *Worktree
	for i, t := range trees {
		if Within(path, t.Real) && (best == nil || len(t.Real) > len(best.Real)) {
			best = &trees[i]
		}
	}
	return best
}

func Within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

func RepoRoot(dir string) (string, bool) {
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

func Canonical(path string) string {
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
