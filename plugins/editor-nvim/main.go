package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

const (
	mainSlot      = "editor-nvim.main"
	statusSlot    = "editor-nvim.status"
	sessionTitle  = "nvim"
	stateRunning  = "running"
	renderDelay   = 30 * time.Millisecond
	branchTimeout = 2 * time.Second
	noName        = "[No Name]"
	sidebarSlot   = "editor-nvim.sidebar"
	edgeEvent     = "edge"
	revPrefix     = "rev:"
	shortSHA      = 7
	navigationKey = "navigation"
)

var (
	errNvimNotInPATH  = errors.New("nvim not found in PATH")
	errNoCurrentFile  = errors.New("no file in the current buffer")
	errNoPath         = errors.New("open: path is required")
	errUnknownCommand = errors.New("unknown command")
	errUnknownAction  = errors.New("unknown action")
	errUnsaved        = errors.New("unsaved buffers")
	errDeleted        = errors.New("project deleted")
	errNotInRepo      = errors.New("not in a git repository")
	errRelativePath   = errors.New("diff: path must be absolute")
	errDiffSides      = errors.New("diff: base must be head, index or rev:<sha>, target index, worktree or rev:<sha>, and differ")
	errReviewArgs     = errors.New("review: repo must be absolute and base and head refs")
)

type projectEvent struct {
	ProjectID string `json:"projectId"`
}

type sessionExited struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Owner     string `json:"owner"`
	ExitCode  *int   `json:"exitCode"`
}

type openArgs struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

type pathEvent struct {
	ProjectID string `json:"projectId"`
	Path      string `json:"path"`
}

type diffArgs struct {
	Path   string `json:"path"`
	Base   string `json:"base"`
	Target string `json:"target"`
}

type reviewArgs struct {
	Repo      string `json:"repo"`
	Base      string `json:"base"`
	Head      string `json:"head"`
	MergeBase string `json:"mergeBase"`
}

type reviewResult struct {
	Mode string `json:"mode"`
}

type terminalArgs struct {
	Cwd string `json:"cwd"`
}

type navigationArgs struct {
	Enabled bool `json:"enabled"`
}

type navigationResult struct {
	Skipped []string `json:"skipped"`
}

type restartArgs struct {
	Force bool `json:"force"`
}

type projectPayload struct {
	Project pluginsdk.Project `json:"project"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

type app struct {
	nvimPath string
	navMu    sync.Mutex
	mu       sync.Mutex
	editors  map[string]*editor
	deleted  map[string]bool
}

type editor struct {
	id       string
	life     sync.Mutex
	renderMu sync.Mutex
	mu       sync.Mutex
	repos    []string
	root     string
	session  string
	client   *client
	status   status
	pending  bool
}

func main() {
	a := &app{editors: map[string]*editor{}, deleted: map[string]bool{}}
	a.nvimPath, _ = exec.LookPath("nvim")
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	sessions, err := h.ListSessions(ctx, "", true)
	if err != nil {
		return err
	}
	present, err := h.Presence(ctx)
	if err != nil {
		return err
	}
	for _, pattern := range []string{"ui.opened", "session.exited", "project.updated", "project.deleted"} {
		if _, err := h.Subscribe(ctx, pattern, ready.Seq); err != nil {
			return err
		}
	}
	projects := present
	for _, s := range sessions {
		if s.State == stateRunning && s.Title == sessionTitle {
			projects = append(projects, s.ProjectID)
		}
	}
	slices.Sort(projects)
	for _, id := range slices.Compact(projects) {
		go a.ensureLogged(ctx, h, id)
	}
	return nil
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case "ui.opened":
		var opened projectEvent
		if err := json.Unmarshal(ev.Payload, &opened); err != nil {
			logError("ui.opened", err)
			return
		}
		go a.ensureLogged(ctx, h, opened.ProjectID)
	case "session.exited":
		var exited sessionExited
		if err := json.Unmarshal(ev.Payload, &exited); err != nil {
			logError("session.exited", err)
			return
		}
		if exited.Owner == h.PluginID() {
			a.exited(ctx, h, exited)
		}
	case "project.updated":
		var p projectPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			logError("project.updated", err)
			return
		}
		a.projectUpdated(h, p.Project)
	case "project.deleted":
		var p deletedPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			logError("project.deleted", err)
			return
		}
		a.drop(p.ID)
	}
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	if ev.Action != "restart" {
		return nil, fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
	}
	e := a.editor(ev.ProjectID)
	if e == nil {
		return nil, nil
	}
	return nil, a.restart(ctx, h, e, false)
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	name := strings.TrimPrefix(cmd.ID, h.PluginID()+".")
	if name == "navigation" {
		var args navigationArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("navigation args: %w", err)
		}
		return a.navigation(ctx, h, args.Enabled)
	}
	e := a.editor(cmd.ProjectID)
	if e == nil {
		return nil, nil
	}
	switch name {
	case "open":
		var args openArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("open args: %w", err)
		}
		if args.Path == "" {
			return nil, errNoPath
		}
		c, err := a.ensure(ctx, h, e)
		if err != nil {
			return nil, err
		}
		return nil, c.open(args.Path, args.Line)
	case "reveal":
		file := e.current().File
		if file == "" {
			return nil, errNoCurrentFile
		}
		return nil, exec.CommandContext(ctx, "open", "-R", file).Run()
	case "terminal":
		return nil, a.terminal(ctx, h, e)
	case "restart":
		var args restartArgs
		if len(cmd.Args) > 0 {
			if err := json.Unmarshal(cmd.Args, &args); err != nil {
				return nil, fmt.Errorf("restart args: %w", err)
			}
		}
		return nil, a.restart(ctx, h, e, args.Force)
	case "diff":
		var args diffArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("diff args: %w", err)
		}
		return nil, a.diff(ctx, h, e, args)
	case "review":
		var args reviewArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("review args: %w", err)
		}
		return a.review(ctx, h, e, args)
	case "review-close":
		if c := e.connected(); c != nil {
			return nil, c.closeReview()
		}
		return nil, nil
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (a *app) editor(projectID string) *editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deleted[projectID] {
		return nil
	}
	e, ok := a.editors[projectID]
	if !ok {
		e = &editor{id: projectID}
		a.editors[projectID] = e
	}
	return e
}

func (a *app) lookup(projectID string) *editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.editors[projectID]
}

func (a *app) projectUpdated(h *pluginsdk.Host, p pluginsdk.Project) {
	e := a.lookup(p.ID)
	if e == nil {
		return
	}
	root, err := projectRoot(p)
	if err != nil {
		logError("project.updated", err)
		return
	}
	e.setProject(p.Repos, root)
	a.scheduleStatus(h, e)
}

func (a *app) drop(projectID string) {
	a.mu.Lock()
	e := a.editors[projectID]
	delete(a.editors, projectID)
	a.deleted[projectID] = true
	a.mu.Unlock()
	if e == nil {
		return
	}
	go func() {
		e.life.Lock()
		defer e.life.Unlock()
		if c := e.reset(); c != nil {
			c.close()
		}
	}()
}

func (a *app) isDeleted(projectID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deleted[projectID]
}

func (a *app) ensureLogged(ctx context.Context, h *pluginsdk.Host, projectID string) {
	e := a.editor(projectID)
	if e == nil {
		return
	}
	if _, err := a.ensure(ctx, h, e); err != nil && !errors.Is(err, errNvimNotInPATH) && !errors.Is(err, errDeleted) {
		logError("start "+projectID, err)
	}
}

func (a *app) ensure(ctx context.Context, h *pluginsdk.Host, e *editor) (*client, error) {
	e.life.Lock()
	defer e.life.Unlock()
	return a.ensureLocked(ctx, h, e)
}

func (a *app) ensureLocked(ctx context.Context, h *pluginsdk.Host, e *editor) (*client, error) {
	if a.isDeleted(e.id) {
		return nil, errDeleted
	}
	if a.nvimPath == "" {
		return nil, errors.Join(errNvimNotInPATH, renderMain(ctx, h, e.id, missingView()))
	}
	if c := e.connected(); c != nil {
		return c, nil
	}
	p, err := h.GetProject(ctx, e.id)
	if err != nil {
		return nil, err
	}
	root, err := projectRoot(p)
	if err != nil {
		return nil, err
	}
	sock, err := socketPath(pluginsdk.RuntimeDir(), e.id)
	if err != nil {
		return nil, err
	}
	e.setProject(p.Repos, root)
	s, err := a.session(ctx, h, e.id, root, sock)
	if err != nil {
		return nil, errors.Join(err, renderMain(ctx, h, e.id, failedView(err)))
	}
	e.setSession(s)
	if err := renderMain(ctx, h, e.id, runningView(s)); err != nil {
		return nil, err
	}
	c, err := connect(ctx, sock, func(ev nvimEvent) { a.nvimEvent(h, e, ev) })
	if err != nil {
		return nil, err
	}
	e.setClient(c)
	a.applyNavigation(ctx, h, c)
	return c, nil
}

func (a *app) navigation(ctx context.Context, h *pluginsdk.Host, enabled bool) (navigationResult, error) {
	a.navMu.Lock()
	defer a.navMu.Unlock()
	if err := h.StorageSet(ctx, navigationKey, enabled); err != nil {
		return navigationResult{}, err
	}
	a.mu.Lock()
	editors := slices.Collect(maps.Values(a.editors))
	a.mu.Unlock()
	res := navigationResult{Skipped: []string{}}
	var errs []error
	for _, e := range editors {
		c := e.connected()
		if c == nil {
			continue
		}
		skipped, err := c.navigation(enabled)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		res.Skipped = append(res.Skipped, skipped...)
	}
	slices.Sort(res.Skipped)
	res.Skipped = slices.Compact(res.Skipped)
	return res, errors.Join(errs...)
}

func (a *app) applyNavigation(ctx context.Context, h *pluginsdk.Host, c *client) {
	a.navMu.Lock()
	defer a.navMu.Unlock()
	var enabled bool
	found, err := h.StorageGet(ctx, navigationKey, &enabled)
	if err == nil && found {
		_, err = c.navigation(enabled)
	}
	if err != nil {
		logError(navigationKey, err)
	}
}

func (a *app) session(ctx context.Context, h *pluginsdk.Host, projectID, root, sock string) (string, error) {
	sessions, err := h.ListSessions(ctx, projectID, true)
	if err != nil {
		return "", err
	}
	for _, s := range sessions {
		if s.State == stateRunning && s.Title == sessionTitle {
			return s.ID, nil
		}
	}
	if err := renderMain(ctx, h, projectID, startingView()); err != nil {
		return "", err
	}
	for _, s := range sessions {
		if s.State != stateRunning {
			if err := h.RemoveSession(ctx, s.ID); err != nil {
				return "", err
			}
		}
	}
	if err := os.Remove(sock); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove stale socket: %w", err)
	}
	s, err := h.CreateSession(ctx, pluginsdk.SessionRequest{
		ProjectID: projectID,
		Title:     sessionTitle,
		Command:   []string{a.nvimPath, "--listen", sock},
		Cwd:       root,
	})
	return s.ID, err
}

func (a *app) restart(ctx context.Context, h *pluginsdk.Host, e *editor, force bool) error {
	e.life.Lock()
	defer e.life.Unlock()
	if c := e.connected(); c != nil && !force {
		if names, err := c.modified(); err == nil && len(names) > 0 {
			return fmt.Errorf("%w: %s", errUnsaved, e.relativeNames(names))
		}
	}
	if c := e.reset(); c != nil {
		c.close()
	}
	a.scheduleStatus(h, e)
	sessions, err := h.ListSessions(ctx, e.id, true)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		if s.State == stateRunning {
			if err := h.KillSession(ctx, s.ID); err != nil {
				return err
			}
		}
	}
	_, err = a.ensureLocked(ctx, h, e)
	return err
}

func (a *app) diff(ctx context.Context, h *pluginsdk.Host, e *editor, args diffArgs) error {
	if args.Path == "" {
		return errNoPath
	}
	if !filepath.IsAbs(args.Path) {
		return fmt.Errorf("%w: %s", errRelativePath, args.Path)
	}
	args.Path = filepath.Clean(args.Path)
	label, ok := baseLabel(args.Base)
	target, targetOK := targetLabel(args.Target)
	if !ok || !targetOK || label == target {
		return errDiffSides
	}
	repo, ok := repoRoot(filepath.Dir(args.Path))
	if !ok {
		return fmt.Errorf("%w: %s", errNotInRepo, args.Path)
	}
	rel, err := filepath.Rel(repo, args.Path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	left, err := gitShow(ctx, repo, args.Base, rel)
	if err != nil {
		return err
	}
	right := ""
	if args.Target != "worktree" {
		if right, err = gitShow(ctx, repo, args.Target, rel); err != nil {
			return err
		}
	}
	c, err := a.ensure(ctx, h, e)
	if err != nil {
		return err
	}
	return c.diff(args.Path, rel, label, target, left, right)
}

func (a *app) review(ctx context.Context, h *pluginsdk.Host, e *editor, args reviewArgs) (reviewResult, error) {
	if args.Head == "" {
		args.Head = "HEAD"
	}
	if !filepath.IsAbs(args.Repo) || !isRef(args.Base) || !isRef(args.Head) {
		return reviewResult{}, errReviewArgs
	}
	c, err := a.ensure(ctx, h, e)
	if err != nil {
		return reviewResult{}, err
	}
	mode, err := c.review(filepath.Clean(args.Repo), args.Base, args.Head)
	return reviewResult{Mode: mode}, err
}

func (a *app) exited(ctx context.Context, h *pluginsdk.Host, ev sessionExited) {
	e := a.lookup(ev.ProjectID)
	if e == nil {
		return
	}
	c, ok := e.exited(ev.ID)
	if !ok {
		return
	}
	if c != nil {
		go c.close()
	}
	a.scheduleStatus(h, e)
	if err := renderMain(ctx, h, ev.ProjectID, exitedView(ev.ExitCode)); err != nil {
		logError("render", err)
	}
}

func (a *app) terminal(ctx context.Context, h *pluginsdk.Host, e *editor) error {
	cwd := ""
	if file := e.current().File; file != "" {
		cwd = filepath.Dir(file)
	} else {
		p, err := h.GetProject(ctx, e.id)
		if err != nil {
			return err
		}
		if cwd, err = projectRoot(p); err != nil {
			return err
		}
	}
	return h.RunCommand(ctx, "terminals.new", e.id, terminalArgs{Cwd: cwd}, nil)
}

func (a *app) nvimEvent(h *pluginsdk.Host, e *editor, ev nvimEvent) {
	ctx := context.Background()
	if ev.Event == edgeEvent {
		if ev.Dir == "h" || ev.Dir == "l" {
			if err := h.FocusEdge(ctx, e.id, h.PluginID(), sidebarSlot, ev.Dir); err != nil {
				logError("focus", err)
			}
		}
		return
	}
	branch, refresh := "", ev.Event == "BufEnter" || ev.Event == "DirChanged"
	if refresh {
		branch = gitBranch(ctx, branchDir(ev.File, e.projectRoot()))
	}
	e.update(ev, branch, refresh)
	if topic := fileTopic(ev.Event); topic != "" && ev.File != "" {
		if _, err := h.Publish(ctx, topic, pathEvent{ProjectID: e.id, Path: ev.File}); err != nil {
			logError("publish "+topic, err)
		}
	}
	a.scheduleStatus(h, e)
}

func (a *app) scheduleStatus(h *pluginsdk.Host, e *editor) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pending {
		return
	}
	e.pending = true
	time.AfterFunc(renderDelay, func() {
		e.renderMu.Lock()
		defer e.renderMu.Unlock()
		e.mu.Lock()
		e.pending = false
		view := statusView(e.status, e.repos)
		e.mu.Unlock()
		if _, err := h.Render(context.Background(), e.id, statusSlot, view); err != nil {
			logError("render status", err)
		}
	})
}

func (e *editor) connected() *client {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.client != nil && e.client.alive() {
		return e.client
	}
	return nil
}

func (e *editor) setProject(repos []string, root string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.repos, e.root = repos, root
}

func (e *editor) setSession(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.session = id
}

func (e *editor) setClient(c *client) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.client = c
}

func (e *editor) reset() *client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.detachLocked()
}

func (e *editor) exited(sessionID string) (*client, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session != sessionID {
		return nil, false
	}
	return e.detachLocked(), true
}

func (e *editor) detachLocked() *client {
	c := e.client
	e.client, e.session, e.status = nil, "", status{}
	return c
}

func (e *editor) current() status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

func (e *editor) projectRoot() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.root
}

func (e *editor) relativeNames(names []string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	shown := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" {
			n = noName
		}
		shown = append(shown, relative(n, e.repos))
	}
	return strings.Join(shown, ", ")
}

func (e *editor) update(ev nvimEvent, branch string, refresh bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status.File, e.status.Mode, e.status.LSP = ev.File, ev.Mode, ev.LSP
	if refresh {
		e.status.Branch = branch
	}
}

func renderMain(ctx context.Context, h *pluginsdk.Host, projectID string, n sdui.Node) error {
	_, err := h.Render(ctx, projectID, mainSlot, n)
	return err
}

func projectRoot(p pluginsdk.Project) (string, error) {
	for _, r := range p.Repos {
		if info, err := os.Stat(r); err == nil && info.IsDir() {
			return r, nil
		}
	}
	return os.UserHomeDir()
}

func branchDir(file, root string) string {
	if file != "" {
		return filepath.Dir(file)
	}
	return root
}

func gitBranch(ctx context.Context, dir string) string {
	repo, ok := repoRoot(dir)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, branchTimeout)
	defer cancel()
	branch, err := gitrepo.Branch(ctx, repo)
	if err != nil {
		return ""
	}
	return branch
}

func baseLabel(base string) (string, bool) {
	sha, ok := strings.CutPrefix(base, revPrefix)
	if !ok {
		return base, base == "head" || base == "index"
	}
	if sha == "" || strings.Trim(sha, "0123456789abcdef") != "" {
		return "", false
	}
	return sha[:min(shortSHA, len(sha))], true
}

func targetLabel(target string) (string, bool) {
	if strings.HasPrefix(target, revPrefix) {
		return baseLabel(target)
	}
	return target, target == "index" || target == "worktree"
}

func isRef(ref string) bool {
	return ref != "" && !strings.HasPrefix(ref, "-") && !strings.ContainsAny(ref, " \t\n\r\v\f")
}

func gitShow(ctx context.Context, repo, side, rel string) (string, error) {
	var (
		rev    string
		exists bool
		err    error
	)
	switch sha, isRev := strings.CutPrefix(side, revPrefix); {
	case isRev:
		rev = sha + ":" + rel
		exists, err = inTree(ctx, repo, sha, rel)
	case side == "head":
		rev = "HEAD:" + rel
		exists, err = inHead(ctx, repo, rel)
	default:
		rev = ":" + rel
		exists, err = inIndex(ctx, repo, rel)
	}
	if err != nil || !exists {
		return "", err
	}
	out, err := gitrepo.Run(ctx, repo, "show", rev)
	return string(out), err
}

func inHead(ctx context.Context, repo, rel string) (bool, error) {
	if _, err := gitrepo.Run(ctx, repo, "rev-parse", "-q", "--verify", "HEAD^{commit}"); err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok && ee.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return inTree(ctx, repo, "HEAD", rel)
}

func inTree(ctx context.Context, repo, rev, rel string) (bool, error) {
	out, err := gitrepo.Run(ctx, repo, "--literal-pathspecs", "ls-tree", "-z", rev, "--", rel)
	return len(out) > 0, err
}

func inIndex(ctx context.Context, repo, rel string) (bool, error) {
	out, err := gitrepo.Run(ctx, repo, "--literal-pathspecs", "ls-files", "-z", "--stage", "--", rel)
	return bytes.Contains(out, []byte(" 0\t")), err
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

func fileTopic(event string) string {
	switch event {
	case "BufEnter":
		return "editor.buffer"
	case "BufWritePost":
		return "editor.saved"
	}
	return ""
}

func logError(what string, err error) {
	fmt.Fprintln(os.Stderr, "editor-nvim:", what+":", err)
}
