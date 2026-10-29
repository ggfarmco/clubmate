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
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	worktreesDir       = ".worktrees"
	stopRootCommand    = "editor-nvim.stop-root"
	releaseRootCommand = "editor-nvim.release-root"
	agentsStopCommand  = "agents.stop-root"
	editorOwner        = "editor-nvim"
	sessionRunning     = "running"
	creatingWorktree   = "Creating worktree…"
	removingWorktree   = "Removing worktree…"
	opPollWait         = time.Second
	keptOps            = 64
)

var (
	errNoGitFolder     = errors.New("no git folder in the workspace")
	errMainCheckout    = errors.New("cannot remove the main checkout")
	errFolderCheckout  = errors.New("cannot remove the checkout of the workspace folder")
	errDirtyWorktree   = errors.New("worktree has uncommitted changes")
	errRelativePath    = errors.New("path must be absolute")
	errSymlinkedFolder = errors.New("worktrees folder must not be a symlink")
	errOpBusy          = errors.New("another git operation is running")
	errUnknownOp       = errors.New("unknown operation")
	errHomeWorktrees   = errors.New("worktrees unavailable in the home workspace")
)

type folder struct {
	*gitrepo.Folder
	root string
}

type worktreeInfo struct {
	Repo        string `json:"repo"`
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	Head        string `json:"head"`
	Main        bool   `json:"main"`
	Current     bool   `json:"current"`
	DefaultBase string `json:"defaultBase,omitempty"`
}

type worktreeArgs struct {
	ID     string `json:"id"`
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

type opStarted struct {
	ID string `json:"id"`
}

type opStatus struct {
	Done  bool   `json:"done"`
	Error string `json:"error,omitempty"`
	Path  string `json:"path,omitempty"`
}

type operation struct {
	id      string
	project string
	done    chan struct{}
	path    string
	err     error
}

type opTable struct {
	mu    sync.Mutex
	seq   int
	byID  map[string]*operation
	order []string
}

type opFunc func(context.Context) (string, error)

func (a *app) worktreeCommand(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun, s *state) (any, error) {
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
		return nil, switchWorktree(ctx, h, cmd.ProjectID, s, args.Repo, args.Path)
	case "git.worktree-new":
		return a.startOp(ctx, h, cmd.ProjectID, s, creatingWorktree, func() (opFunc, error) {
			return prepareNew(ctx, h, cmd.ProjectID, args)
		})
	case "git.worktree-remove":
		return a.startOp(ctx, h, cmd.ProjectID, s, removingWorktree, func() (opFunc, error) {
			return prepareRemove(ctx, h, cmd.ProjectID, args)
		})
	case "git.worktree-op-status":
		return a.ops.status(ctx, cmd.ProjectID, args.ID)
	case "git.worktree-sessions":
		return worktreeSessions(ctx, h, cmd.ProjectID, args.Path)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (a *app) startOp(ctx context.Context, h *pluginsdk.Host, projectID string, s *state, busy string, prepare func() (opFunc, error)) (opStarted, error) {
	s.mu.Lock()
	if s.busy != "" {
		s.mu.Unlock()
		return opStarted{}, errOpBusy
	}
	s.busy = busy
	s.mu.Unlock()
	run, err := prepare()
	if err != nil {
		s.mu.Lock()
		s.busy = ""
		s.mu.Unlock()
		return opStarted{}, err
	}
	o := a.ops.add(projectID)
	logError("render "+projectID, a.render(ctx, h, projectID, s))
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mutateTimeout)
		defer cancel()
		path, err := run(ctx)
		s.mu.Lock()
		s.busy = ""
		s.mu.Unlock()
		o.finish(path, err)
		logError("render "+projectID, a.render(ctx, h, projectID, s))
		a.schedule(h, projectID, s, 0)
	}()
	return opStarted{ID: o.id}, nil
}

func (t *opTable) add(projectID string) *operation {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byID == nil {
		t.byID = map[string]*operation{}
	}
	t.seq++
	id := fmt.Sprintf("wt-%d", t.seq)
	o := &operation{id: id, project: projectID, done: make(chan struct{})}
	t.byID[id] = o
	t.order = append(t.order, id)
	for i := 0; len(t.order) > keptOps && i < len(t.order); {
		old := t.byID[t.order[i]]
		select {
		case <-old.done:
			delete(t.byID, t.order[i])
			t.order = append(t.order[:i], t.order[i+1:]...)
		default:
			i++
		}
	}
	return o
}

func (o *operation) finish(path string, err error) {
	o.path, o.err = path, err
	close(o.done)
}

func (t *opTable) status(ctx context.Context, projectID, id string) (opStatus, error) {
	t.mu.Lock()
	o := t.byID[id]
	t.mu.Unlock()
	if o == nil || o.project != projectID {
		return opStatus{}, fmt.Errorf("%w: %q", errUnknownOp, id)
	}
	timer := time.NewTimer(opPollWait)
	defer timer.Stop()
	select {
	case <-o.done:
	case <-timer.C:
		return opStatus{}, nil
	case <-ctx.Done():
		return opStatus{}, nil
	}
	out := opStatus{Done: true, Path: o.path}
	if o.err != nil {
		out.Error = o.err.Error()
	}
	return out, nil
}

func listWorktrees(ctx context.Context, h *pluginsdk.Host, projectID string) ([]worktreeInfo, error) {
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	list := []worktreeInfo{}
	if p.Home {
		return list, nil
	}
	for i := range p.Repos {
		f, err := openFolder(ctx, p, i)
		if errors.Is(err, gitrepo.ErrNotRepo) {
			continue
		}
		if err != nil {
			return nil, err
		}
		heads, err := f.shortHeads(ctx)
		if err != nil {
			return nil, err
		}
		base, _ := gitrepo.DefaultBase(ctx, f.Checkout.Path)
		for j, t := range f.Trees {
			if t.Bare {
				continue
			}
			list = append(list, worktreeInfo{Repo: f.Repo, Path: t.Path, Branch: t.Branch, Head: heads[t.Head], Main: j == 0, Current: f.current(t), DefaultBase: base})
		}
	}
	return list, nil
}

func switchWorktree(ctx context.Context, h *pluginsdk.Host, projectID string, s *state, repo, path string) error {
	s.mu.Lock()
	busy := s.busy != ""
	s.mu.Unlock()
	if busy {
		return errOpBusy
	}
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

func prepareNew(ctx context.Context, h *pluginsdk.Host, projectID string, args worktreeArgs) (opFunc, error) {
	f, err := gitFolder(ctx, h, projectID, args.Repo)
	if err != nil {
		return nil, err
	}
	cur := f.currentTree()
	out, err := run(ctx, cur.Path, "check-ref-format", "--branch", args.Branch)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(out)) != args.Branch {
		return nil, fmt.Errorf("'%s' is not a valid branch name", args.Branch)
	}
	primary := f.Trees[0]
	dir, err := worktreeDir(primary.Path, args.Branch)
	if err != nil {
		return nil, err
	}
	add := []string{"worktree", "add", "-b", args.Branch, dir, currentHead}
	if _, err := gitrepo.Run(ctx, cur.Path, "rev-parse", "-q", "--verify", "refs/heads/"+args.Branch); err == nil {
		add = []string{"worktree", "add", dir, args.Branch}
	}
	return func(ctx context.Context) (string, error) {
		if _, err := mutate(ctx, cur.Path, add...); err != nil {
			return "", err
		}
		if err := excludeWorktrees(ctx, primary.Path); err != nil {
			return "", err
		}
		g, err := gitFolder(ctx, h, projectID, f.Repo)
		if err != nil {
			return "", err
		}
		t, err := g.member(dir)
		if err != nil {
			return "", err
		}
		if _, err := h.SetWorktree(ctx, projectID, g.Repo, g.Selection(t)); err != nil {
			return "", err
		}
		return t.Path, nil
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
	return func(ctx context.Context) (string, error) {
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
			logError("release "+root, h.RunCommand(context.WithoutCancel(ctx), releaseRootCommand, projectID, stopRootArgs{Root: root}, nil))
		}()
		if err := h.RunCommand(ctx, stopRootCommand, projectID, stopRootArgs{Root: root, Force: args.Force}, nil); err != nil {
			return "", err
		}
		logError("stop agents in "+t.Path, h.RunCommand(ctx, agentsStopCommand, projectID, stopRootArgs{Root: t.Path}, nil))
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

func gitFolder(ctx context.Context, h *pluginsdk.Host, projectID, repo string) (*folder, error) {
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p.Home {
		return nil, errHomeWorktrees
	}
	for i, r := range p.Repos {
		if repo != "" && r != repo {
			continue
		}
		f, err := openFolder(ctx, p, i)
		if repo != "" || err == nil {
			return f, err
		}
		if !errors.Is(err, gitrepo.ErrNotRepo) {
			return nil, err
		}
	}
	if repo != "" {
		return nil, fmt.Errorf("%w: %q", errUnknownRepo, repo)
	}
	return nil, errNoGitFolder
}

func openFolder(ctx context.Context, p pluginsdk.Project, i int) (*folder, error) {
	f, err := gitrepo.OpenFolder(ctx, p.Repos[i])
	if err != nil {
		return nil, gitError(nil, err)
	}
	root := p.Repos[i]
	if i < len(p.Roots) {
		root = p.Roots[i]
	}
	return &folder{Folder: f, root: root}, nil
}

func (f *folder) member(path string) (gitrepo.Worktree, error) {
	if t, ok := f.Member(path); ok {
		return t, nil
	}
	return gitrepo.Worktree{}, fmt.Errorf("not a worktree of %s", f.Repo)
}

func (f *folder) current(t gitrepo.Worktree) bool {
	return gitrepo.Canonical(f.root) == f.Mapped(t)
}

func (f *folder) currentTree() gitrepo.Worktree {
	if t := gitrepo.Longest(f.Trees, gitrepo.Canonical(f.root)); t != nil {
		return *t
	}
	return f.Checkout
}

func (f *folder) shortHeads(ctx context.Context) (map[string]string, error) {
	heads := map[string]string{}
	for _, t := range f.Trees {
		if _, ok := heads[t.Head]; ok || t.Head == "" {
			continue
		}
		out, err := run(ctx, f.Checkout.Path, "rev-parse", "--short", t.Head)
		if err != nil {
			return nil, err
		}
		heads[t.Head] = strings.TrimSpace(string(out))
	}
	return heads, nil
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
