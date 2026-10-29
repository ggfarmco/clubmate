package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
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
	refreshDelay  = 300 * time.Millisecond
	diffCommand   = "editor-nvim.diff"
	closeCommand  = "editor-nvim.review-close"
	gitSection    = "git"
	editorSection = "editor-nvim"
	editorSlot    = "editor-nvim.main"
	mutateTimeout = 10 * time.Minute
	committing    = "Committing…"
	staging       = "Staging…"
	currentHead   = "HEAD"
)

var (
	errUnknownAction  = errors.New("unknown action")
	errUnknownCommand = errors.New("unknown command")
	errNoSelection    = errors.New("no file selected")
	errNotChanges     = errors.New("only unstaged changes can be discarded")
	errNoMessage      = errors.New("commit message is required")
	errUnknownRepo    = errors.New("unknown repository")
	errStaleTarget    = errors.New("the file is no longer selected")
	errBusy           = errors.New("git is busy")
	errNoRepo         = errors.New("no repository to review")
)

type projectEvent struct {
	ProjectID string `json:"projectId"`
}

type projectPayload struct {
	Project pluginsdk.Project `json:"project"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

type commitForm struct {
	Repo    string `json:"repo"`
	Message string `json:"message"`
}

type diffArgs struct {
	Path   string `json:"path"`
	Base   string `json:"base"`
	Target string `json:"target"`
}

type state struct {
	mu       sync.Mutex
	synced   bool
	dead     bool
	pending  bool
	running  bool
	dirty    bool
	renderMu sync.Mutex
	repos    []string
	gitDirs  []string
	views    map[string]repoView
	selected string
	folded   map[string]bool
	commits  int
	reviews  map[string]*review
	active   string
	filters  filters
	busy     string
	failure  string
	watch    string
	missing  []string
}

type app struct {
	mu       sync.Mutex
	projects map[string]*state
	ops      opTable
}

func main() {
	a := &app{projects: map[string]*state{}}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, FSChanged: a.fsChanged})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	present, err := h.Presence(ctx)
	if err != nil {
		return err
	}
	for _, pattern := range []string{"ui.*", "project.*", "review.*", startedTopic} {
		if _, err := h.Subscribe(ctx, pattern, ready.Seq); err != nil {
			return err
		}
	}
	for _, id := range present {
		a.open(ctx, h, id)
	}
	return nil
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case "ui.opened", "ui.idle":
		var p projectEvent
		if !decodeEvent(ev, &p) {
			return
		}
		if ev.Topic == "ui.opened" {
			a.open(ctx, h, p.ProjectID)
		} else {
			a.idle(ctx, h, p.ProjectID)
		}
	case "project.created", "project.updated":
		var p projectPayload
		if !decodeEvent(ev, &p) {
			return
		}
		a.projectChanged(ctx, h, p.Project.ID, ev.Topic == "project.updated")
	case "project.deleted":
		var p deletedPayload
		if !decodeEvent(ev, &p) {
			return
		}
		a.drop(ctx, h, p.ID)
	case reviewTopic:
		var p reviewChanged
		if !decodeEvent(ev, &p) {
			return
		}
		a.reviewChanged(ctx, h, p)
	case startedTopic:
		var p rootEvent
		if !decodeEvent(ev, &p) {
			return
		}
		a.editorStarted(h, p)
	}
}

func (a *app) fsChanged(_ context.Context, h *pluginsdk.Host, ev pluginsdk.FSChange) {
	s := a.lookup(ev.ProjectID)
	if s == nil {
		return
	}
	s.mu.Lock()
	relevant := s.watch != "" && s.watch == ev.Watch && (ev.Overflow || slices.ContainsFunc(ev.Paths, func(p string) bool { return containing(s.repos, p) || containing(s.gitDirs, p) }))
	s.mu.Unlock()
	if relevant {
		a.schedule(h, ev.ProjectID, s, refreshDelay)
	}
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	s := a.get(ev.ProjectID)
	if err := a.ensure(ctx, h, ev.ProjectID, s); err != nil {
		return nil, err
	}
	if ev.Slot == reviewSlot {
		return nil, a.reviewEvent(ctx, h, ev, s)
	}
	if ev.Action != "expand" && ev.Action != "collapse" {
		s.clearFailure()
	}
	switch ev.Action {
	case "select":
		var item string
		if err := json.Unmarshal(ev.Value, &item); err != nil {
			return nil, fmt.Errorf("select value: %w", err)
		}
		return nil, a.selectItem(ctx, h, ev.ProjectID, s, item)
	case "expand", "collapse":
		var item string
		if err := json.Unmarshal(ev.Value, &item); err != nil {
			return nil, fmt.Errorf("%s value: %w", ev.Action, err)
		}
		return nil, a.fold(ctx, h, ev.ProjectID, s, item, ev.Action == "collapse")
	case stageAction, unstageAction, discardAction:
		if err := a.apply(ctx, s, ev.Action, ev.NodeID); err != nil {
			return nil, err
		}
		a.schedule(h, ev.ProjectID, s, 0)
		return nil, nil
	case openDiffAction:
		return nil, a.openDiff(ctx, h, ev.ProjectID, s)
	case commitAction:
		var f commitForm
		if err := json.Unmarshal(ev.Value, &f); err != nil {
			return nil, fmt.Errorf("commit value: %w", err)
		}
		return nil, a.commit(ctx, h, ev.ProjectID, s, f)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	s := a.get(cmd.ProjectID)
	if err := a.sync(ctx, h, cmd.ProjectID, s); err != nil {
		return nil, err
	}
	if strings.HasPrefix(cmd.ID, "git.worktree") {
		return a.worktreeCommand(ctx, h, cmd, s)
	}
	if cmd.ID == "git.end-review" || strings.HasPrefix(cmd.ID, "git.review") {
		return a.reviewCommand(ctx, h, cmd, s)
	}
	s.clearFailure()
	switch cmd.ID {
	case "git.refresh":
		a.schedule(h, cmd.ProjectID, s, 0)
		return nil, nil
	case "git.stage-all":
		return nil, a.background(ctx, h, cmd.ProjectID, s, staging, s.live(), func(ctx context.Context, repo string) error {
			_, err := mutate(ctx, repo, "add", "-A")
			return err
		})
	case "git.unstage-all":
		var errs []error
		for _, repo := range s.live() {
			if err := unstage(ctx, repo, "."); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(repo), err))
			}
		}
		a.schedule(h, cmd.ProjectID, s, 0)
		return nil, errors.Join(errs...)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (a *app) lookup(id string) *state {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.projects[id]
}

func (a *app) get(id string) *state {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.projects[id]
	if !ok {
		s = &state{folded: map[string]bool{}, reviews: map[string]*review{}}
		a.projects[id] = s
	}
	return s
}

func (a *app) open(ctx context.Context, h *pluginsdk.Host, id string) {
	s := a.get(id)
	if err := a.sync(ctx, h, id, s); err != nil {
		logError("open "+id, err)
		return
	}
	logError("watch "+id, a.rewatch(ctx, h, id, s))
	logError("render "+id, a.render(ctx, h, id, s))
	a.schedule(h, id, s, 0)
}

func (a *app) idle(ctx context.Context, h *pluginsdk.Host, id string) {
	s := a.get(id)
	s.mu.Lock()
	watch := s.watch
	s.watch = ""
	s.mu.Unlock()
	if watch != "" {
		logError("unwatch "+id, h.UnwatchFS(ctx, watch))
	}
}

func (a *app) projectChanged(ctx context.Context, h *pluginsdk.Host, id string, updated bool) {
	s := a.get(id)
	if err := a.sync(ctx, h, id, s); err != nil {
		logError("sync "+id, err)
		return
	}
	s.mu.Lock()
	watching := s.watch != ""
	s.mu.Unlock()
	if updated && watching {
		logError("watch "+id, a.rewatch(ctx, h, id, s))
	}
	logError("render "+id, a.render(ctx, h, id, s))
	if watching {
		a.schedule(h, id, s, 0)
	}
}

func (a *app) drop(ctx context.Context, h *pluginsdk.Host, id string) {
	a.mu.Lock()
	s := a.projects[id]
	delete(a.projects, id)
	a.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	s.dead = true
	watch := s.watch
	s.watch = ""
	s.mu.Unlock()
	if watch != "" {
		logError("unwatch "+id, h.UnwatchFS(ctx, watch))
	}
	logError("drop "+id, h.StorageDelete(ctx, reviewKey(id)))
}

func (a *app) rewatch(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	s.mu.Lock()
	old := s.watch
	s.watch = ""
	s.mu.Unlock()
	if old != "" {
		logError("unwatch "+id, h.UnwatchFS(ctx, old))
	}
	watch, err := h.WatchFS(ctx, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.watch = watch
	s.mu.Unlock()
	return nil
}

func (a *app) ensure(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	s.mu.Lock()
	synced := s.synced
	s.mu.Unlock()
	if synced {
		return nil
	}
	return a.sync(ctx, h, id, s)
}

func (a *app) sync(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	p, err := h.GetProject(ctx, id)
	if err != nil {
		return err
	}
	repos := make([]string, 0, len(p.Roots))
	for _, r := range p.Roots {
		repos = append(repos, resolve(r))
	}
	var gitDirs []string
	for _, r := range repos {
		gitDir, commonDir, err := gitrepo.Dirs(ctx, r)
		if err != nil {
			continue
		}
		gitDirs = append(gitDirs, gitDir, commonDir)
	}
	s.mu.Lock()
	first := !s.synced
	s.mu.Unlock()
	var stored storedReviews
	if first {
		if _, err := h.StorageGet(ctx, reviewKey(id), &stored); err != nil {
			logError("restore review "+id, err)
			stored = storedReviews{}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.synced {
		s.restore(stored)
	}
	s.repos, s.gitDirs, s.synced = repos, gitDirs, true
	return nil
}

func (a *app) schedule(h *pluginsdk.Host, id string, s *state, delay time.Duration) {
	s.mu.Lock()
	if s.pending || s.dead {
		s.mu.Unlock()
		return
	}
	s.pending = true
	s.mu.Unlock()
	time.AfterFunc(delay, func() {
		s.mu.Lock()
		s.pending = false
		if s.running {
			s.dirty = true
			s.mu.Unlock()
			return
		}
		s.running = true
		s.mu.Unlock()
		for {
			logError("refresh "+id, a.refresh(context.Background(), h, id, s))
			s.mu.Lock()
			if !s.dirty {
				s.running = false
				s.mu.Unlock()
				return
			}
			s.dirty = false
			s.mu.Unlock()
		}
	})
}

func (a *app) refresh(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	a.checkMissing(ctx, h, id, s)
	s.mu.Lock()
	repos := slices.Clone(s.repos)
	s.mu.Unlock()
	views := make(map[string]repoView, len(repos))
	for _, repo := range repos {
		_, entries, err := gitrepo.Status(ctx, repo)
		if err != nil {
			logError("status "+repo, err)
			views[repo] = repoView{Path: repo, Problem: problem(ctx, repo, err)}
			continue
		}
		branch, err := gitrepo.Branch(ctx, repo)
		logError("branch "+repo, err)
		rv := repoView{Path: repo, Branch: branch, Entries: entries, Restorable: map[string]bool{}}
		for _, l := range leaves(rv) {
			if l.Group == groupChanges && blocked(l) == "" {
				rv.Restorable[l.Path] = true
			}
		}
		views[repo] = rv
	}
	s.mu.Lock()
	s.views = views
	s.selected = s.follow(s.selected)
	s.mu.Unlock()
	a.refreshReview(ctx, h, id, s)
	return a.render(ctx, h, id, s)
}

func (a *app) checkMissing(ctx context.Context, h *pluginsdk.Host, id string, s *state) {
	p, err := h.GetProject(ctx, id)
	if err != nil {
		logError("project "+id, err)
		return
	}
	s.mu.Lock()
	skip := s.busy != "" || slices.Equal(s.missing, p.Missing)
	if !skip {
		s.missing = p.Missing
	}
	s.mu.Unlock()
	if !skip && len(p.Missing) > 0 {
		_, err = h.CheckWorktrees(ctx, id)
		logError("check worktrees "+id, err)
	}
}

func (a *app) render(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	s.mu.Lock()
	if s.dead {
		s.mu.Unlock()
		return nil
	}
	views := make([]repoView, len(s.repos))
	for i, r := range s.repos {
		views[i] = s.view(r)
	}
	live := s.liveLocked()
	folded := maps.Clone(s.folded)
	sel := s.leaf(s.selected)
	m := mainModel{Sel: sel, Repos: live, FormID: fmt.Sprintf("commit-%d", s.commits), Busy: s.busy, Failure: s.failure}
	review := s.reviewModel()
	s.mu.Unlock()
	selected := ""
	if sel != nil {
		selected = sel.id()
		if info, err := os.Lstat(sel.Path); err == nil && sel.Group == groupUntracked {
			sel.Dir = info.IsDir()
		}
		text, truncated, err := bodyFor(ctx, *sel)
		m.Body = bodyNode(text, truncated, sel.Dir, err)
	}
	_, err := h.Render(ctx, id, sidebarSlot, sidebarView(views, selected, folded))
	logError("sidebar "+id, err)
	_, err = h.Render(ctx, id, reviewSlot, reviewView(review))
	logError("review "+id, err)
	_, err = h.Render(ctx, id, mainSlot, mainView(m))
	return err
}

func (a *app) selectItem(ctx context.Context, h *pluginsdk.Host, id string, s *state, item string) error {
	if strings.HasSuffix(item, idSep+moreID) {
		return nil
	}
	s.mu.Lock()
	if s.leaf(item) != nil {
		s.selected = item
	} else {
		s.selected = ""
	}
	s.mu.Unlock()
	return a.render(ctx, h, id, s)
}

func (a *app) fold(ctx context.Context, h *pluginsdk.Host, id string, s *state, item string, on bool) error {
	s.mu.Lock()
	if on {
		s.folded[item] = true
	} else {
		delete(s.folded, item)
	}
	s.mu.Unlock()
	return a.render(ctx, h, id, s)
}

func (a *app) apply(ctx context.Context, s *state, action, target string) error {
	s.mu.Lock()
	sel := s.leaf(s.selected)
	s.mu.Unlock()
	if sel == nil {
		return errNoSelection
	}
	rel := sel.rel()
	var err error
	switch action {
	case stageAction:
		_, err = run(ctx, sel.Repo, "add", "--", rel)
	case unstageAction:
		paths := []string{rel}
		if sel.Orig != "" {
			paths = append(paths, relPath(sel.Repo, sel.Orig))
		}
		err = unstage(ctx, sel.Repo, paths...)
	case discardAction:
		if target != sel.id() {
			return errStaleTarget
		}
		if sel.Group != groupChanges {
			return errNotChanges
		}
		if reason := blocked(*sel); reason != "" {
			return fmt.Errorf("cannot discard %s: %s", rel, reason)
		}
		_, err = run(ctx, sel.Repo, "restore", "--", rel)
	}
	return err
}

func (a *app) openDiff(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	s.mu.Lock()
	sel := s.leaf(s.selected)
	s.mu.Unlock()
	if sel == nil {
		return errNoSelection
	}
	args := diffArgs{Path: sel.Path, Base: "index", Target: "worktree"}
	switch sel.Group {
	case groupStaged:
		args.Base, args.Target = "head", "index"
	}
	if err := h.RunCommand(ctx, diffCommand, id, args, nil); err != nil {
		return err
	}
	logError("focus "+id, h.Focus(ctx, id, editorSection, editorSlot))
	return nil
}

func (a *app) commit(ctx context.Context, h *pluginsdk.Host, id string, s *state, f commitForm) error {
	if strings.TrimSpace(f.Message) == "" {
		return errNoMessage
	}
	live := s.live()
	repo := f.Repo
	if repo == "" && len(live) == 1 {
		repo = live[0]
	}
	if !slices.Contains(live, repo) {
		return fmt.Errorf("%w: %q", errUnknownRepo, repo)
	}
	return a.background(ctx, h, id, s, committing, []string{repo}, func(ctx context.Context, repo string) error {
		if _, err := mutate(ctx, repo, "commit", "-m", f.Message); err != nil {
			return err
		}
		s.mu.Lock()
		s.commits++
		s.mu.Unlock()
		return nil
	})
}

func (a *app) background(ctx context.Context, h *pluginsdk.Host, id string, s *state, busy string, repos []string, op func(context.Context, string) error) error {
	s.mu.Lock()
	if s.busy != "" {
		s.mu.Unlock()
		return errBusy
	}
	s.busy = busy
	s.mu.Unlock()
	logError("render "+id, a.render(ctx, h, id, s))
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mutateTimeout)
		defer cancel()
		var errs []error
		for _, repo := range repos {
			if err := op(ctx, repo); err != nil {
				errs = append(errs, err)
			}
		}
		s.mu.Lock()
		s.busy = ""
		if err := errors.Join(errs...); err != nil {
			s.failure = err.Error()
		}
		s.mu.Unlock()
		logError("render "+id, a.render(ctx, h, id, s))
		a.schedule(h, id, s, 0)
	}()
	return nil
}

func (s *state) clearFailure() {
	s.mu.Lock()
	s.failure = ""
	s.mu.Unlock()
}

func (s *state) view(repo string) repoView {
	v, ok := s.views[repo]
	if !ok {
		v = repoView{Path: repo}
	}
	return v
}

func (s *state) live() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.liveLocked()
}

func (s *state) liveLocked() []string {
	var live []string
	for _, r := range s.repos {
		if s.view(r).Problem == "" {
			live = append(live, r)
		}
	}
	return live
}

func (s *state) leaf(id string) *leaf {
	if id == "" {
		return nil
	}
	for _, r := range s.repos {
		for _, l := range leaves(s.view(r)) {
			if l.id() == id {
				return &l
			}
		}
	}
	return nil
}

func (s *state) follow(id string) string {
	if id == "" || s.leaf(id) != nil {
		return id
	}
	parts := strings.SplitN(id, idSep, 3)
	if len(parts) != 3 {
		return ""
	}
	for _, g := range groupTitles {
		if l := s.leaf(parts[0] + idSep + g.id + idSep + parts[2]); l != nil {
			return l.id()
		}
	}
	return ""
}

func bodyFor(ctx context.Context, l leaf) (string, bool, error) {
	rel := l.rel()
	switch {
	case l.Group == groupStaged:
		args := []string{"diff", "--no-color", "--no-ext-diff", "--cached", "--", rel}
		if l.Orig != "" {
			args = append(args, relPath(l.Repo, l.Orig))
		}
		return capture(ctx, l.Repo, args...)
	case l.Group == groupChanges:
		return capture(ctx, l.Repo, "diff", "--no-color", "--no-ext-diff", "--", rel)
	case l.Dir:
		out, truncated, err := capture(ctx, l.Repo, "ls-files", "--others", "--exclude-standard", "-z", "--", rel+"/")
		return strings.ReplaceAll(strings.TrimSuffix(out, "\x00"), "\x00", "\n"), truncated, err
	}
	out, truncated, err := gitrepo.RunLimited(ctx, l.Repo, maxDiffBytes, "diff", "--no-color", "--no-ext-diff", "--no-index", "--", "/dev/null", rel)
	if ee, ok := errors.AsType[*exec.ExitError](err); ok && ee.ExitCode() == 1 && len(ee.Stderr) == 0 {
		return string(out), truncated, nil
	}
	return string(out), truncated, gitError(out, err)
}

func reviewError(base string, err error) string {
	switch {
	case errors.Is(err, gitrepo.ErrUnknownRef):
		return "unknown branch " + strings.TrimPrefix(err.Error(), gitrepo.ErrUnknownRef.Error()+": ")
	case errors.Is(err, gitrepo.ErrNoMergeBase):
		return "no merge base with " + base
	}
	first, _, _ := strings.Cut(gitError(nil, err).Error(), "\n")
	return first
}

func reviewKey(projectID string) string {
	return "review/" + projectID
}

func capture(ctx context.Context, repo string, args ...string) (string, bool, error) {
	out, truncated, err := gitrepo.RunLimited(ctx, repo, maxDiffBytes, append([]string{"--literal-pathspecs"}, args...)...)
	return string(out), truncated, gitError(out, err)
}

func blocked(l leaf) string {
	switch {
	case l.Unmerged:
		return "unmerged"
	case l.Code == "M" || l.Code == "T":
		info, err := os.Lstat(l.Path)
		if err != nil {
			return "missing from the worktree"
		}
		if !info.Mode().IsRegular() && info.Mode()&fs.ModeSymlink == 0 {
			return "not a regular file or symlink in the worktree"
		}
		return ""
	case l.Code == "D":
		for dir := filepath.Dir(l.Path); strings.HasPrefix(dir, l.Repo+string(filepath.Separator)); dir = filepath.Dir(dir) {
			info, err := os.Lstat(dir)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil || !info.IsDir() {
				return relPath(l.Repo, dir) + " is not a directory"
			}
		}
		if _, err := os.Lstat(l.Path); !errors.Is(err, fs.ErrNotExist) {
			return "the path exists in the worktree"
		}
		return ""
	case l.Code == "A" || l.Code == "R" || l.Code == "C":
		return "intent-to-add file"
	}
	return "unsupported status " + l.Code
}

func unstage(ctx context.Context, repo string, paths ...string) error {
	args := append([]string{"restore", "--staged", "--"}, paths...)
	out, err := gitrepo.Run(ctx, repo, "rev-parse", "--verify", "-q", "HEAD")
	if ee, ok := errors.AsType[*exec.ExitError](err); ok && ee.ExitCode() == 1 && len(ee.Stderr) == 0 {
		args = append([]string{"rm", "--cached", "-r", "-f", "-q", "--ignore-unmatch", "--"}, paths...)
	} else if err != nil {
		return gitError(out, err)
	}
	_, err = run(ctx, repo, args...)
	return err
}

func problem(ctx context.Context, repo string, statusErr error) string {
	if _, err := os.Stat(repo); err != nil {
		return missingDetail
	}
	out, err := gitrepo.Run(ctx, repo, "rev-parse", "--is-inside-work-tree")
	if ee, ok := errors.AsType[*exec.ExitError](err); ok && strings.Contains(string(ee.Stderr), "not a git repository") {
		return notGitDetail
	}
	if err == nil {
		err = statusErr
	}
	first, _, _ := strings.Cut(gitError(out, err).Error(), "\n")
	return "git error: " + first
}

func mutate(ctx context.Context, repo string, args ...string) ([]byte, error) {
	out, err := gitrepo.Mutate(ctx, repo, append([]string{"--literal-pathspecs"}, args...)...)
	if errors.Is(err, exec.ErrWaitDelay) {
		return out, nil
	}
	return out, gitError(out, err)
}

func run(ctx context.Context, repo string, args ...string) ([]byte, error) {
	out, err := gitrepo.Run(ctx, repo, append([]string{"--literal-pathspecs"}, args...)...)
	return out, gitError(out, err)
}

func gitError(out []byte, err error) error {
	ee, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return err
	}
	for _, msg := range [][]byte{ee.Stderr, out} {
		if text := strings.TrimSpace(string(msg)); text != "" {
			return errors.New(text)
		}
	}
	return err
}

func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	dir, base := filepath.Split(filepath.Clean(path))
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(real, base)
	}
	return filepath.Clean(path)
}

func containing(roots []string, path string) bool {
	return slices.ContainsFunc(roots, func(r string) bool { return path == r || strings.HasPrefix(path, r+"/") })
}

func decodeEvent(ev pluginsdk.Event, v any) bool {
	if err := json.Unmarshal(ev.Payload, v); err != nil {
		logError(ev.Topic, err)
		return false
	}
	return true
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "git:", what+":", err)
	}
}
