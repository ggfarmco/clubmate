package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

const (
	mainSlot       = "editor-nvim.main"
	statusSlot     = "editor-nvim.status"
	sessionTitle   = "nvim"
	stateRunning   = "running"
	renderDelay    = 30 * time.Millisecond
	branchTimeout  = 2 * time.Second
	noName         = "[No Name]"
	sidebarSlot    = "editor-nvim.sidebar"
	edgeEvent      = "edge"
	openErrorEvent = "open_error"
	openErrorMax   = 80
	revPrefix      = "rev:"
	shortSHA       = 7
	lightMaxSize   = 5 * 1024 * 1024
	binarySniff    = 8000
	navigationKey  = "navigation"
	quickfixMax    = 10000
	quickfixText   = 512
	quickfixFiles  = 1000
	quickfixPath   = 4096
	quickfixBytes  = 2 << 20
)

var (
	errNvimNotInPATH  = errors.New("nvim not found in PATH")
	errNoCurrentFile  = errors.New("no file in the current buffer")
	errBadPath        = errors.New("open: path must not contain line breaks")
	errNoPath         = errors.New("open: path is required")
	errUnknownCommand = errors.New("unknown command")
	errUnknownAction  = errors.New("unknown action")
	errUnsaved        = errors.New("unsaved buffers")
	errUnsavedUnknown = fmt.Errorf("%w: unknown (nvim not responding)", errUnsaved)
	errDeleted        = errors.New("project deleted")
	errNotInRepo      = errors.New("not in a git repository")
	errRelativePath   = errors.New("diff: path must be absolute")
	errDiffSides      = errors.New("diff: base must be head, index or rev:<sha>, target index, worktree or rev:<sha>, and differ")
	errReviewArgs     = errors.New("review: repo must be absolute and base and head refs")
	errStopRoot       = errors.New("stop-root: root must be absolute")
	errNavigationSide = errors.New("navigation: side must be left or right")
	errQuickfixCount  = fmt.Errorf("quickfix: items must hold 1 to %d entries", quickfixMax)
	errQuickfixTitle  = fmt.Errorf("quickfix: title must be at most %d chars without line breaks or NUL", quickfixText)
	errQuickfixPath   = fmt.Errorf("quickfix: path must be absolute, at most %d bytes, without line breaks or NUL", quickfixPath)
	errQuickfixLine   = fmt.Errorf("quickfix: line must be 1 to %d", math.MaxInt32)
	errQuickfixCol    = fmt.Errorf("quickfix: col must be 0 (none) or 1 to %d", math.MaxInt32)
	errQuickfixText   = fmt.Errorf("quickfix: text must be at most %d chars without NUL", quickfixText)
	errQuickfixSize   = fmt.Errorf("quickfix: items hold more than %d MiB of paths and text; send fewer", quickfixBytes>>20)
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
	Col  int    `json:"col"`
}

type quickfixArgs struct {
	Title string         `json:"title"`
	Items []quickfixItem `json:"items"`
}

type quickfixItem struct {
	Path string `json:"path" msgpack:"filename"`
	Line int    `json:"line" msgpack:"lnum"`
	Col  int    `json:"col" msgpack:"col"`
	Text string `json:"text" msgpack:"text"`
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
	Enabled bool   `json:"enabled"`
	Side    string `json:"side"`
}

type navigationResult struct {
	Skipped []string `json:"skipped"`
}

type restartArgs struct {
	Force bool `json:"force"`
}

type stopRootArgs struct {
	Root  string `json:"root"`
	Force bool   `json:"force"`
}

type projectPayload struct {
	Project pluginsdk.Project `json:"project"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

type editorKey struct {
	project string
	root    string
}

type app struct {
	copies  *copies
	navMu   sync.Mutex
	slotMu  sync.Mutex
	mu      sync.Mutex
	editors map[editorKey]*editor
	current map[string]string
	folders map[string][]string
	deleted map[string]bool
}

type editor struct {
	id       string
	root     string
	life     sync.Mutex
	renderMu sync.Mutex
	mu       sync.Mutex
	session  string
	client   *client
	status   status
	pending  bool
}

func main() {
	a := &app{
		copies:  newCopies(),
		editors: map[editorKey]*editor{},
		current: map[string]string{},
		folders: map[string][]string{},
		deleted: map[string]bool{},
	}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, Shutdown: a.shutdown})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	go a.sweepTemps()
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
		a.projectUpdated(ctx, h, p.Project)
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
	e, err := a.active(ctx, h, ev.ProjectID)
	if e == nil || err != nil {
		return nil, err
	}
	return nil, a.restart(ctx, h, e, false, false)
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	name := strings.TrimPrefix(cmd.ID, h.PluginID()+".")
	switch name {
	case "navigation":
		var args navigationArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("navigation args: %w", err)
		}
		if args.Side != "left" && args.Side != "right" {
			return nil, errNavigationSide
		}
		return a.navigation(ctx, h, args)
	case "config":
		var args configArgs
		if len(cmd.Args) > 0 {
			if err := json.Unmarshal(cmd.Args, &args); err != nil {
				return nil, fmt.Errorf("config args: %w", err)
			}
		}
		return a.config(ctx, h, args)
	case "config-copy":
		var args copyArgs
		if len(cmd.Args) > 0 {
			if err := json.Unmarshal(cmd.Args, &args); err != nil {
				return nil, fmt.Errorf("config-copy args: %w", err)
			}
		}
		return a.configCopy(args)
	case "config-copy-status":
		var args copyIDArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("config-copy-status args: %w", err)
		}
		return a.copies.status(ctx, args.ID)
	case "config-copy-cancel":
		var args copyIDArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("config-copy-cancel args: %w", err)
		}
		return nil, a.copies.cancel(args.ID)
	}
	if a.isDeleted(cmd.ProjectID) {
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
		if strings.ContainsAny(args.Path, "\n\r") {
			return nil, errBadPath
		}
		e, err := a.target(ctx, h, cmd.ProjectID, args.Path)
		if e == nil || err != nil {
			return nil, err
		}
		c, err := a.ensure(ctx, h, e)
		if err != nil {
			return nil, err
		}
		return nil, c.open(args.Path, args.Line, args.Col, lightFile(args.Path))
	case "quickfix":
		var args quickfixArgs
		dec := json.NewDecoder(bytes.NewReader(cmd.Args))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&args); err != nil {
			return nil, fmt.Errorf("quickfix args: %w", err)
		}
		return nil, a.quickfix(ctx, h, cmd.ProjectID, args)
	case "diff":
		var args diffArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("diff args: %w", err)
		}
		return nil, a.diff(ctx, h, cmd.ProjectID, args)
	case "review":
		var args reviewArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("review args: %w", err)
		}
		return a.review(ctx, h, cmd.ProjectID, args)
	case "stop-root":
		var args stopRootArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("stop-root args: %w", err)
		}
		if !filepath.IsAbs(args.Root) {
			return nil, errStopRoot
		}
		return nil, a.stopRoot(ctx, h, cmd.ProjectID, filepath.Clean(args.Root), args.Force)
	}
	e, err := a.active(ctx, h, cmd.ProjectID)
	if e == nil || err != nil {
		return nil, err
	}
	switch name {
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
		return nil, a.restart(ctx, h, e, true, args.Force)
	case "review-close":
		if c := e.connected(); c != nil {
			return nil, c.closeReview()
		}
		return nil, nil
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (a *app) active(ctx context.Context, h *pluginsdk.Host, projectID string) (*editor, error) {
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	e, moved, err := a.selectRoot(p)
	if e == nil || err != nil || !moved {
		return e, err
	}
	return e, a.show(ctx, h, e)
}

func (a *app) target(ctx context.Context, h *pluginsdk.Host, projectID, path string) (*editor, error) {
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	repo, root, switched := owner(ctx, p, path)
	if switched {
		if p, err = h.SetWorktree(ctx, projectID, repo, root); err != nil {
			return nil, err
		}
	}
	e, moved, err := a.selectRoot(p)
	if e == nil || err != nil || !moved && !switched {
		return e, err
	}
	if err := a.show(ctx, h, e); err != nil {
		return nil, err
	}
	if switched {
		if err := h.Focus(ctx, projectID, h.PluginID(), mainSlot); err != nil {
			logError("focus", err)
		}
	}
	return e, nil
}

func (a *app) selectRoot(p pluginsdk.Project) (*editor, bool, error) {
	root, err := projectRoot(p)
	if err != nil {
		return nil, false, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deleted[p.ID] {
		return nil, false, nil
	}
	prev, known := a.current[p.ID]
	a.current[p.ID] = root
	a.folders[p.ID] = slices.Concat(p.Repos, p.Roots)
	return a.editorLocked(p.ID, root), known && prev != root, nil
}

func (a *app) editorAt(projectID, root string) *editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deleted[projectID] {
		return nil
	}
	return a.editorLocked(projectID, root)
}

func (a *app) editorLocked(projectID, root string) *editor {
	key := editorKey{project: projectID, root: root}
	e, ok := a.editors[key]
	if !ok {
		e = &editor{id: projectID, root: root}
		a.editors[key] = e
	}
	return e
}

func (a *app) lookup(projectID, root string) *editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.editors[editorKey{project: projectID, root: root}]
}

func (a *app) forget(e *editor) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := editorKey{project: e.id, root: e.root}
	if a.editors[key] == e {
		delete(a.editors, key)
	}
}

func (a *app) projectEditors(projectID string) []*editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*editor
	for k, e := range a.editors {
		if k.project == projectID {
			out = append(out, e)
		}
	}
	return out
}

func (a *app) isCurrent(e *editor) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.deleted[e.id] && a.current[e.id] == e.root && a.editors[editorKey{project: e.id, root: e.root}] == e
}

func (a *app) projectFolders(projectID string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.folders[projectID]
}

func (a *app) tracked(projectID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.current[projectID]
	return ok
}

func (a *app) projectUpdated(ctx context.Context, h *pluginsdk.Host, p pluginsdk.Project) {
	if !a.tracked(p.ID) {
		return
	}
	e, moved, err := a.selectRoot(p)
	if err != nil {
		logError("project.updated", err)
		return
	}
	if e == nil {
		return
	}
	a.scheduleStatus(h, e)
	if moved {
		go func() {
			if err := a.show(ctx, h, e); err != nil && !errors.Is(err, errNvimNotInPATH) && !errors.Is(err, errDeleted) {
				logError("switch "+p.ID, err)
			}
		}()
	}
}

func (a *app) show(ctx context.Context, h *pluginsdk.Host, e *editor) error {
	e.life.Lock()
	defer e.life.Unlock()
	if !a.isCurrent(e) {
		return nil
	}
	if _, err := a.ensureLocked(ctx, h, e); err != nil {
		return err
	}
	a.scheduleStatus(h, e)
	return a.render(ctx, h, e, mainSlot, runningView(e.sessionID()))
}

func (a *app) render(ctx context.Context, h *pluginsdk.Host, e *editor, slot string, n sdui.Node) error {
	a.slotMu.Lock()
	defer a.slotMu.Unlock()
	if !a.isCurrent(e) {
		return nil
	}
	_, err := h.Render(ctx, e.id, slot, n)
	return err
}

func (a *app) drop(projectID string) {
	a.mu.Lock()
	var editors []*editor
	for k, e := range a.editors {
		if k.project == projectID {
			editors = append(editors, e)
			delete(a.editors, k)
		}
	}
	delete(a.current, projectID)
	delete(a.folders, projectID)
	a.deleted[projectID] = true
	a.mu.Unlock()
	for _, e := range editors {
		go func() {
			e.life.Lock()
			defer e.life.Unlock()
			if c := e.reset(); c != nil {
				c.close()
			}
		}()
	}
}

func (a *app) isDeleted(projectID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deleted[projectID]
}

func (a *app) ensureLogged(ctx context.Context, h *pluginsdk.Host, projectID string) {
	if a.isDeleted(projectID) {
		return
	}
	e, err := a.active(ctx, h, projectID)
	if e != nil && err == nil {
		_, err = a.ensure(ctx, h, e)
	}
	if err != nil && !errors.Is(err, errNvimNotInPATH) && !errors.Is(err, errDeleted) {
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
	if c := e.connected(); c != nil {
		return c, nil
	}
	s, sock, err := a.session(ctx, h, e)
	if errors.Is(err, errNvimNotInPATH) {
		return nil, errors.Join(err, a.render(ctx, h, e, mainSlot, missingView(errors.Is(err, errSystemNvimMissing))))
	}
	if err != nil {
		return nil, errors.Join(err, a.render(ctx, h, e, mainSlot, failedView(err)))
	}
	e.setSession(s)
	if err := a.render(ctx, h, e, mainSlot, runningView(s)); err != nil {
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

func (a *app) navigation(ctx context.Context, h *pluginsdk.Host, nav navigationArgs) (navigationResult, error) {
	a.navMu.Lock()
	defer a.navMu.Unlock()
	if err := h.StorageSet(ctx, navigationKey, nav); err != nil {
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
		skipped, err := c.navigation(nav)
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
	var nav navigationArgs
	found, err := h.StorageGet(ctx, navigationKey, &nav)
	if err == nil && found {
		_, err = c.navigation(nav)
	}
	if err != nil {
		logError(navigationKey, err)
	}
}

func (a *app) session(ctx context.Context, h *pluginsdk.Host, e *editor) (string, string, error) {
	sessions, err := h.ListSessions(ctx, e.id, true)
	if err != nil {
		return "", "", err
	}
	for _, s := range sessions {
		if sock, ok := ownedSocket(s, e.id, e.root); ok {
			return s.ID, sock, nil
		}
	}
	sock, err := socketPath(pluginsdk.RuntimeDir(), e.id, e.root)
	if err != nil {
		return "", "", err
	}
	path, env, unset, pathAppend, err := a.nvimCommand(ctx, h)
	if err != nil {
		return "", "", err
	}
	if err := a.render(ctx, h, e, mainSlot, startingView()); err != nil {
		return "", "", err
	}
	for _, s := range sessions {
		if s.State != stateRunning {
			if err := h.RemoveSession(ctx, s.ID); err != nil {
				return "", "", err
			}
		}
	}
	if err := os.Remove(sock); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("remove stale socket: %w", err)
	}
	s, err := h.CreateSession(ctx, pluginsdk.SessionRequest{
		ProjectID:  e.id,
		Title:      sessionTitle,
		Command:    []string{path, "--listen", sock},
		Cwd:        e.root,
		Env:        env,
		UnsetEnv:   unset,
		PathAppend: pathAppend,
	})
	return s.ID, sock, err
}

func ownedSocket(s pluginsdk.Session, projectID, root string) (string, bool) {
	if s.State != stateRunning || s.Cwd != root || len(s.Command) != 3 || s.Command[1] != "--listen" {
		return "", false
	}
	sock := s.Command[2]
	rt := pluginsdk.RuntimeDir()
	want, err := socketPath(rt, projectID, root)
	return sock, err == nil && sock == want || sock == legacySocketPath(rt, projectID)
}

func (a *app) restart(ctx context.Context, h *pluginsdk.Host, cur *editor, all, force bool) error {
	sessions, err := h.ListSessions(ctx, cur.id, true)
	if err != nil {
		return err
	}
	running := map[*editor]bool{}
	var kill []string
	for _, s := range sessions {
		if s.State != stateRunning {
			continue
		}
		_, owned := ownedSocket(s, cur.id, s.Cwd)
		if !all && (!owned || s.Cwd != cur.root) {
			continue
		}
		kill = append(kill, s.ID)
		if !owned {
			continue
		}
		e := a.editorAt(cur.id, s.Cwd)
		if e == nil {
			return errDeleted
		}
		running[e] = true
	}
	editors := slices.Collect(maps.Keys(running))
	if !running[cur] {
		editors = append(editors, cur)
	}
	defer lockAll(editors)()
	if !force {
		if err := a.checkUnsaved(ctx, h, editors, running); err != nil {
			return err
		}
	}
	for _, e := range editors {
		if c := e.reset(); c != nil {
			c.close()
		}
	}
	a.scheduleStatus(h, cur)
	for _, id := range kill {
		if err := h.KillSession(ctx, id); err != nil {
			return err
		}
	}
	var errs []error
	for _, e := range editors {
		if e == cur || running[e] {
			_, err := a.ensureLocked(ctx, h, e)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (a *app) stopRoot(ctx context.Context, h *pluginsdk.Host, projectID, root string, force bool) error {
	sessions, err := h.ListSessions(ctx, projectID, true)
	if err != nil {
		return err
	}
	var kill []string
	for _, s := range sessions {
		if _, ok := ownedSocket(s, projectID, root); ok {
			kill = append(kill, s.ID)
		}
	}
	e := a.lookup(projectID, root)
	if e == nil && len(kill) == 0 {
		return nil
	}
	if e == nil {
		if e = a.editorAt(projectID, root); e == nil {
			return nil
		}
	}
	e.life.Lock()
	defer e.life.Unlock()
	if !force {
		if err := a.checkUnsaved(ctx, h, []*editor{e}, map[*editor]bool{e: len(kill) > 0}); err != nil {
			return err
		}
	}
	if c := e.reset(); c != nil {
		c.close()
	}
	a.forget(e)
	for _, id := range kill {
		if err := h.KillSession(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func lockAll(editors []*editor) func() {
	slices.SortFunc(editors, func(x, y *editor) int { return strings.Compare(x.root, y.root) })
	for _, e := range editors {
		e.life.Lock()
	}
	return func() {
		for _, e := range editors {
			e.life.Unlock()
		}
	}
}

func (a *app) checkUnsaved(ctx context.Context, h *pluginsdk.Host, editors []*editor, running map[*editor]bool) error {
	var names []string
	for _, e := range editors {
		c := e.connected()
		if c == nil {
			if !running[e] {
				continue
			}
			var err error
			if c, err = a.ensureLocked(ctx, h, e); err != nil {
				return errUnsavedUnknown
			}
		}
		modified, err := c.modified()
		if err != nil {
			return errUnsavedUnknown
		}
		names = append(names, relativeNames(modified, a.projectFolders(e.id))...)
	}
	if len(names) > 0 {
		return fmt.Errorf("%w: %s", errUnsaved, strings.Join(names, ", "))
	}
	return nil
}

func (a *app) diff(ctx context.Context, h *pluginsdk.Host, projectID string, args diffArgs) error {
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
	e, err := a.target(ctx, h, projectID, args.Path)
	if e == nil || err != nil {
		return err
	}
	c, err := a.ensure(ctx, h, e)
	if err != nil {
		return err
	}
	return c.diff(args.Path, rel, label, target, left, right)
}

func (a *app) review(ctx context.Context, h *pluginsdk.Host, projectID string, args reviewArgs) (reviewResult, error) {
	if args.Head == "" {
		args.Head = "HEAD"
	}
	if !filepath.IsAbs(args.Repo) || !isRef(args.Base) || !isRef(args.Head) {
		return reviewResult{}, errReviewArgs
	}
	repo := filepath.Clean(args.Repo)
	e, err := a.target(ctx, h, projectID, repo)
	if e == nil || err != nil {
		return reviewResult{}, err
	}
	c, err := a.ensure(ctx, h, e)
	if err != nil {
		return reviewResult{}, err
	}
	mode, err := c.review(repo, args.Base, args.Head)
	return reviewResult{Mode: mode}, err
}

func (a *app) quickfix(ctx context.Context, h *pluginsdk.Host, projectID string, args quickfixArgs) error {
	if err := validQuickfix(args); err != nil {
		return err
	}
	e, err := a.target(ctx, h, projectID, args.Items[0].Path)
	if e == nil || err != nil {
		return err
	}
	c, err := a.ensure(ctx, h, e)
	if err != nil {
		return err
	}
	title, items := capFiles(args)
	if err := c.quickfix(title, items); err != nil {
		return err
	}
	if err := h.Focus(ctx, e.id, h.PluginID(), mainSlot); err != nil {
		logError("focus", err)
	}
	return nil
}

func validQuickfix(args quickfixArgs) error {
	if len(args.Items) == 0 || len(args.Items) > quickfixMax {
		return errQuickfixCount
	}
	if utf8.RuneCountInString(args.Title) > quickfixText || strings.ContainsAny(args.Title, "\n\r\x00") {
		return errQuickfixTitle
	}
	size := 0
	for i, it := range args.Items {
		size += len(it.Path) + len(it.Text)
		if size > quickfixBytes {
			return errQuickfixSize
		}
		var err error
		switch {
		case !filepath.IsAbs(it.Path) || len(it.Path) > quickfixPath || strings.ContainsAny(it.Path, "\n\r\x00"):
			err = errQuickfixPath
		case it.Line < 1 || it.Line > math.MaxInt32:
			err = errQuickfixLine
		case it.Col < 0 || it.Col > math.MaxInt32:
			err = errQuickfixCol
		case utf8.RuneCountInString(it.Text) > quickfixText || strings.ContainsRune(it.Text, 0):
			err = errQuickfixText
		}
		if err != nil {
			return fmt.Errorf("%w: items[%d]", err, i)
		}
	}
	return nil
}

func capFiles(args quickfixArgs) (string, []quickfixItem) {
	files := map[string]bool{}
	items := make([]quickfixItem, 0, len(args.Items))
	for _, it := range args.Items {
		if !files[it.Path] {
			if len(files) == quickfixFiles {
				continue
			}
			files[it.Path] = true
		}
		items = append(items, it)
	}
	if len(items) < len(args.Items) {
		return fmt.Sprintf("%s (first %d files)", args.Title, quickfixFiles), items
	}
	return args.Title, items
}

func (a *app) exited(ctx context.Context, h *pluginsdk.Host, ev sessionExited) {
	for _, e := range a.projectEditors(ev.ProjectID) {
		c, ok := e.exited(ev.ID)
		if !ok {
			continue
		}
		if c != nil {
			go c.close()
		}
		a.scheduleStatus(h, e)
		if err := a.render(ctx, h, e, mainSlot, exitedView(ev.ExitCode)); err != nil {
			logError("render", err)
		}
		return
	}
}

func (a *app) terminal(ctx context.Context, h *pluginsdk.Host, e *editor) error {
	cwd := e.root
	if file := e.current().File; file != "" {
		cwd = filepath.Dir(file)
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
	if ev.Event == openErrorEvent {
		e.setOpenError(shortError(ev.Message))
		a.scheduleStatus(h, e)
		return
	}
	branch, refresh := "", ev.Event == "BufEnter" || ev.Event == "DirChanged"
	if refresh {
		branch = gitBranch(ctx, branchDir(ev.File, e.root))
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
		st := e.status
		e.mu.Unlock()
		if err := a.render(context.Background(), h, e, statusSlot, statusView(st, a.projectFolders(e.id))); err != nil {
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

func (e *editor) setSession(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.session = id
}

func (e *editor) sessionID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.session
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

func relativeNames(names, folders []string) []string {
	shown := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" {
			n = noName
		}
		shown = append(shown, relative(n, folders))
	}
	return shown
}

func (e *editor) setOpenError(msg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status.OpenError = msg
}

func shortError(msg string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(msg), "\n")
	runes := []rune(first)
	if len(runes) > openErrorMax {
		return string(runes[:openErrorMax]) + "..."
	}
	return first
}

func (e *editor) update(ev nvimEvent, branch string, refresh bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status.File, e.status.Mode, e.status.LSP, e.status.Light = ev.File, ev.Mode, ev.LSP, ev.Light
	if ev.Event == "BufEnter" {
		e.status.OpenError = ""
	}
	if refresh {
		e.status.Branch = branch
	}
}

func projectRoot(p pluginsdk.Project) (string, error) {
	for _, r := range p.Roots {
		if info, err := os.Stat(r); err == nil && info.IsDir() {
			return r, nil
		}
	}
	return os.UserHomeDir()
}

func lightFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if info.Size() > lightMaxSize {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, binarySniff)
	n, _ := io.ReadFull(f, buf)
	return bytes.IndexByte(buf[:n], 0) >= 0
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
