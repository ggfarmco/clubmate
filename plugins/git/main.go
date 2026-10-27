package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	editorSection = "editor-nvim"
	editorSlot    = "editor-nvim.main"
	mutateTimeout = 10 * time.Minute
	committing    = "Committing…"
	staging       = "Staging…"
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
	commits  int
	busy     string
	failure  string
	watch    string
}

type app struct {
	mu       sync.Mutex
	projects map[string]*state
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
	for _, pattern := range []string{"ui.*", "project.*"} {
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
		return nil, nil
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
		s = &state{}
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
	repos := make([]string, 0, len(p.Repos))
	for _, r := range p.Repos {
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
	defer s.mu.Unlock()
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
	s.mu.Lock()
	repos := slices.Clone(s.repos)
	s.mu.Unlock()
	views := make(map[string]repoView, len(repos))
	for _, repo := range repos {
		_, entries, err := gitrepo.Status(ctx, repo, false)
		if err != nil {
			logError("status "+repo, err)
			views[repo] = repoView{Path: repo, Problem: problem(ctx, repo, err)}
			continue
		}
		branch, err := gitrepo.Branch(ctx, repo)
		logError("branch "+repo, err)
		views[repo] = repoView{Path: repo, Branch: branch, Entries: entries}
	}
	s.mu.Lock()
	s.views = views
	s.selected = s.follow(s.selected)
	s.mu.Unlock()
	return a.render(ctx, h, id, s)
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
	sel := s.leaf(s.selected)
	m := mainModel{Sel: sel, Repos: live, FormID: fmt.Sprintf("commit-%d", s.commits), Busy: s.busy, Failure: s.failure}
	s.mu.Unlock()
	selected := ""
	if sel != nil {
		selected = sel.id()
		if info, err := os.Lstat(sel.Path); err == nil && sel.Group == groupUntracked {
			sel.Dir = info.IsDir()
		}
		text, err := bodyFor(ctx, *sel)
		m.Body = bodyNode(text, sel.Dir, err)
	}
	if _, err := h.Render(ctx, id, sidebarSlot, sidebarView(views, selected)); err != nil {
		return err
	}
	_, err := h.Render(ctx, id, mainSlot, mainView(m))
	return err
}

func (a *app) selectItem(ctx context.Context, h *pluginsdk.Host, id string, s *state, item string) error {
	s.mu.Lock()
	if s.leaf(item) != nil {
		s.selected = item
	} else {
		s.selected = ""
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
	if sel.Group == groupStaged {
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
	if v, ok := s.views[repo]; ok {
		return v
	}
	return repoView{Path: repo}
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

func bodyFor(ctx context.Context, l leaf) (string, error) {
	rel := l.rel()
	switch {
	case l.Group == groupStaged:
		args := []string{"diff", "--no-color", "--no-ext-diff", "--cached", "--", rel}
		if l.Orig != "" {
			args = append(args, relPath(l.Repo, l.Orig))
		}
		out, err := run(ctx, l.Repo, args...)
		return string(out), err
	case l.Group == groupChanges:
		out, err := run(ctx, l.Repo, "diff", "--no-color", "--no-ext-diff", "--", rel)
		return string(out), err
	case l.Dir:
		out, err := run(ctx, l.Repo, "ls-files", "--others", "--exclude-standard", "-z", "--", rel+"/")
		return strings.ReplaceAll(strings.TrimSuffix(string(out), "\x00"), "\x00", "\n"), err
	}
	out, err := gitrepo.Run(ctx, l.Repo, "diff", "--no-color", "--no-ext-diff", "--no-index", "--", "/dev/null", rel)
	if ee, ok := errors.AsType[*exec.ExitError](err); ok && ee.ExitCode() == 1 && len(ee.Stderr) == 0 {
		return string(out), nil
	}
	return string(out), gitError(out, err)
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
