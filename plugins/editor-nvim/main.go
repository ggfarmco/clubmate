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
	"clubmate/pkg/sdui"
)

const (
	mainSlot      = "editor-nvim.main"
	statusSlot    = "editor-nvim.status"
	sessionTitle  = "nvim"
	stateRunning  = "running"
	renderDelay   = 30 * time.Millisecond
	branchTimeout = 2 * time.Second
)

var (
	errNvimNotInPATH  = errors.New("nvim not found in PATH")
	errNoCurrentFile  = errors.New("no file in the current buffer")
	errNoPath         = errors.New("open: path is required")
	errUnknownCommand = errors.New("unknown command")
	errUnknownAction  = errors.New("unknown action")
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

type terminalArgs struct {
	Cwd string `json:"cwd"`
}

type app struct {
	nvimPath string
	mu       sync.Mutex
	editors  map[string]*editor
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
	a := &app{editors: map[string]*editor{}}
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
	for _, pattern := range []string{"ui.opened", "session.exited"} {
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
	}
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	if ev.Action != "restart" {
		return nil, fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
	}
	return nil, a.restart(ctx, h, a.editor(ev.ProjectID))
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	e := a.editor(cmd.ProjectID)
	switch strings.TrimPrefix(cmd.ID, h.PluginID()+".") {
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
		return nil, a.restart(ctx, h, e)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (a *app) editor(projectID string) *editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.editors[projectID]
	if !ok {
		e = &editor{id: projectID}
		a.editors[projectID] = e
	}
	return e
}

func (a *app) ensureLogged(ctx context.Context, h *pluginsdk.Host, projectID string) {
	if _, err := a.ensure(ctx, h, a.editor(projectID)); err != nil && !errors.Is(err, errNvimNotInPATH) {
		logError("start "+projectID, err)
	}
}

func (a *app) ensure(ctx context.Context, h *pluginsdk.Host, e *editor) (*client, error) {
	e.life.Lock()
	defer e.life.Unlock()
	return a.ensureLocked(ctx, h, e)
}

func (a *app) ensureLocked(ctx context.Context, h *pluginsdk.Host, e *editor) (*client, error) {
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
		return nil, err
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
	return c, nil
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

func (a *app) restart(ctx context.Context, h *pluginsdk.Host, e *editor) error {
	e.life.Lock()
	defer e.life.Unlock()
	if c := e.reset(); c != nil {
		c.close()
	}
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

func (a *app) exited(ctx context.Context, h *pluginsdk.Host, ev sessionExited) {
	e := a.editor(ev.ProjectID)
	c, ok := e.exited(ev.ID)
	if !ok {
		return
	}
	if c != nil {
		go c.close()
	}
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
	c := e.client
	e.client, e.session = nil, ""
	return c
}

func (e *editor) exited(sessionID string) (*client, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session != sessionID {
		return nil, false
	}
	c := e.client
	e.client, e.session = nil, ""
	return c, true
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
	if len(p.Repos) > 0 {
		return p.Repos[0], nil
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
	ctx, cancel := context.WithTimeout(ctx, branchTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
