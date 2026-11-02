package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

const (
	vscodeKey         = "vscode"
	vscodeRetryWindow = 30 * time.Second
	vscodeLicenseURL  = "https://aka.ms/vscode-server-license"
	vscodeConsent     = "VS Code needs Microsoft's VS Code Server: about 600 MB, downloaded once per version, under the VS Code Server License Terms."
)

var (
	errNotInVSCode = errors.New("Not available with VS Code yet")
	errNoConsent   = errors.New("vscode-accept: the consent is not shown")
)

type vscodeOpened struct {
	root string
	open openArgs
}

func (a *app) vscodeShown(workspaceID string) bool {
	if !a.usesVSCode() {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.nvimShown[workspaceID]
}

func (a *app) setNvimShown(workspaceID string, on bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	was := a.nvimShown[workspaceID]
	if on {
		a.nvimShown[workspaceID] = true
	} else {
		delete(a.nvimShown, workspaceID)
	}
	return was
}

func (a *app) vscodeShow(ctx context.Context, h *pluginsdk.Host, e *editor, open *openArgs) error {
	srv, view, started, err := a.vscodeReady(ctx, h)
	if cur := a.currentEditor(e.id); cur != nil {
		e = cur
	}
	if view.Type == "" {
		port, token, _ := srv.Base()
		view = sdui.WebView(vscodeKey, workbenchURL(port, token, e.root, a.lastOpen(e, open)), a.rev())
	}
	a.scheduleStatus(h, e)
	err = errors.Join(err, a.render(ctx, h, e, mainSlot, view))
	if started {
		go a.vscodeRerender(ctx, h, e.id)
	}
	return err
}

func (a *app) vscodeReady(ctx context.Context, h *pluginsdk.Host) (*vscodeServer, sdui.Node, bool, error) {
	a.vsMu.Lock()
	defer a.vsMu.Unlock()
	srv, err := a.server(h)
	if errors.Is(err, errCodeMissing) {
		return nil, codeMissingView(), false, nil
	}
	if err != nil {
		return nil, vscodeFailedView(err), false, err
	}
	if err := a.failure(); err != nil {
		return nil, vscodeFailedView(err), false, nil
	}
	s, err := a.loadSettings(ctx, h)
	if err != nil {
		return nil, vscodeFailedView(err), false, err
	}
	if !s.VSCodeAccepted {
		return nil, consentView(), false, nil
	}
	if !srv.Downloaded() {
		a.renderVSCode(ctx, h, downloadingView())
		if err := srv.Download(ctx); err != nil {
			a.setFailure(err)
			return nil, vscodeFailedView(err), false, err
		}
	}
	if _, _, ok := srv.Base(); ok {
		return srv, sdui.Node{}, false, nil
	}
	if err := srv.Start(ctx, themeDark()); err != nil {
		a.setFailure(err)
		return nil, vscodeFailedView(err), false, err
	}
	return srv, sdui.Node{}, true, nil
}

func (a *app) lastOpen(e *editor, open *openArgs) *openArgs {
	a.mu.Lock()
	defer a.mu.Unlock()
	if open != nil {
		a.vsOpen[e.id] = vscodeOpened{root: e.root, open: *open}
		return open
	}
	if last, ok := a.vsOpen[e.id]; ok && last.root == e.root {
		return &last.open
	}
	return nil
}

func (a *app) renderVSCode(ctx context.Context, h *pluginsdk.Host, n sdui.Node) {
	for _, id := range a.trackedWorkspaces() {
		if e := a.currentEditor(id); e != nil && a.vscodeShown(id) {
			logError("render", a.render(ctx, h, e, mainSlot, n))
		}
	}
}

func (a *app) server(h *pluginsdk.Host) (*vscodeServer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vsSrv != nil {
		return a.vsSrv, nil
	}
	dir := pluginsdk.StateDir()
	if dir == "" {
		return nil, errNoStateDir
	}
	code, err := findCode(exec.LookPath, fileExists)
	if err != nil {
		return nil, err
	}
	a.vsSrv = newVSCodeServer(dir, code, kvPorts{h})
	go a.watchServer(h, a.vsSrv)
	return a.vsSrv, nil
}

func (a *app) runningServer() *vscodeServer {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.vsSrv
}

func (a *app) watchServer(h *pluginsdk.Host, srv *vscodeServer) {
	var last time.Time
	for err := range srv.Exited() {
		if !a.usesVSCode() {
			continue
		}
		logError("vscode server", err)
		if time.Since(last) < vscodeRetryWindow {
			a.setFailure(err)
		}
		last = time.Now()
		a.vscodeRerender(context.Background(), h, "")
	}
}

func (a *app) vscodeRerender(ctx context.Context, h *pluginsdk.Host, except string) {
	for _, id := range a.trackedWorkspaces() {
		if id == except || !a.vscodeShown(id) {
			continue
		}
		e, err := a.active(ctx, h, id)
		if e != nil && err == nil {
			err = a.vscodeShow(ctx, h, e, nil)
		}
		if err != nil && !quiet(err) {
			logError("vscode "+id, err)
		}
	}
}

func (a *app) editorChanged(ctx context.Context, h *pluginsdk.Host) {
	a.mu.Lock()
	clear(a.nvimShown)
	a.vsErr = nil
	srv := a.vsSrv
	a.mu.Unlock()
	if !a.usesVSCode() && srv != nil {
		srv.Stop()
	}
	go a.showAll(ctx, h)
}

func (a *app) showAll(ctx context.Context, h *pluginsdk.Host) {
	present, err := h.Presence(ctx)
	if err != nil {
		logError("presence", err)
	}
	ids := append(a.trackedWorkspaces(), present...)
	slices.Sort(ids)
	for _, id := range slices.Compact(ids) {
		e, err := a.active(ctx, h, id)
		if e != nil && err == nil {
			err = a.show(ctx, h, e)
		}
		if err != nil && !quiet(err) {
			logError("show "+id, err)
		}
	}
}

func (a *app) acceptVSCode(ctx context.Context, h *pluginsdk.Host) error {
	accepted := true
	_, err := a.setSettings(ctx, h, editorSettingsArgs{VSCodeAccepted: &accepted})
	return err
}

func (a *app) vscodeRetry(ctx context.Context, h *pluginsdk.Host) {
	a.setFailure(nil)
	go a.vscodeRerender(ctx, h, "")
}

func (a *app) backToVSCode(ctx context.Context, h *pluginsdk.Host, workspaceID string) error {
	a.setNvimShown(workspaceID, false)
	e, err := a.active(ctx, h, workspaceID)
	if e == nil || err != nil {
		return err
	}
	return a.vscodeShow(ctx, h, e, nil)
}

func (a *app) vscodeOpen(ctx context.Context, h *pluginsdk.Host, workspaceID string, args openArgs) error {
	a.setNvimShown(workspaceID, false)
	e, err := a.targetWith(ctx, h, workspaceID, args.Path, func(context.Context, *pluginsdk.Host, *editor) error { return nil })
	if e == nil || err != nil {
		return err
	}
	args.Path = gitrepo.Canonical(args.Path)
	if err := a.vscodeShow(ctx, h, e, &args); err != nil {
		return err
	}
	focusMain(ctx, h, workspaceID)
	return nil
}

func (a *app) vscodeRestart(ctx context.Context, h *pluginsdk.Host) {
	a.setFailure(nil)
	if srv := a.runningServer(); srv != nil {
		srv.Stop()
	}
	a.vscodeRerender(ctx, h, "")
}

func (a *app) vscodeTheme(ctx context.Context, h *pluginsdk.Host, dark bool) {
	srv := a.runningServer()
	if !a.usesVSCode() || srv == nil {
		return
	}
	if err := writeMachineSettings(srv.serverDir(), dark); err != nil {
		logError(themeTopic, err)
	}
	a.mu.Lock()
	a.vsRev++
	a.mu.Unlock()
	go a.vscodeRerender(ctx, h, "")
}

func (a *app) shutdown(context.Context, *pluginsdk.Host) {
	if srv := a.runningServer(); srv != nil {
		srv.Stop()
	}
}

func (a *app) failure() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.vsErr
}

func (a *app) setFailure(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.vsErr = err
}

func (a *app) rev() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.vsRev
}

func (a *app) trackedWorkspaces() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]string, 0, len(a.current))
	for id := range a.current {
		ids = append(ids, id)
	}
	return ids
}

func (a *app) currentEditor(workspaceID string) *editor {
	a.mu.Lock()
	defer a.mu.Unlock()
	root, ok := a.current[workspaceID]
	if !ok || a.deleted[workspaceID] {
		return nil
	}
	return a.editors[editorKey{workspace: workspaceID, root: root}]
}

func themeDark() bool {
	path := themeFile()
	if path == "" {
		return true
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	t, err := decodeTheme(payload)
	return err != nil || t.Dark
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func consentView() sdui.Node {
	terms := "[VS Code Server License Terms](" + vscodeLicenseURL + ")"
	accept := sdui.WithConfirm(sdui.Button("vscode-accept", "Accept & Download", "arrow.down.circle"), "Accept the VS Code Server License Terms and download the VS Code server?")
	return sdui.VStack(sdui.Card("", "chevron.left.forwardslash.chevron.right", sdui.Markdown(strings.Replace(vscodeConsent, "VS Code Server License Terms", terms, 1)), accept))
}

func downloadingView() sdui.Node {
	return sdui.Empty("Downloading the VS Code server…")
}

func vscodeFailedView(err error) sdui.Node {
	retry := sdui.Button("vscode-retry", "Retry", "arrow.clockwise")
	return sdui.EmptyState(err.Error(), sdui.EmptyProps{Title: "VS Code could not start", Icon: "exclamationmark.triangle", Action: &retry})
}

func codeMissingView() sdui.Node {
	return sdui.Empty(errCodeMissing.Error())
}

func vscodeStatusView(branch string) sdui.Node {
	items := []sdui.Node{sdui.MutedText("VS Code")}
	if branch != "" {
		items = append(items, sdui.MutedText(branch))
	}
	return sdui.HStack(append(items, sdui.MutedText(chatHint))...)
}

func backToVSCodeButton() sdui.Node {
	return sdui.Button("back-to-vscode", "Back to VS Code", "arrow.uturn.backward")
}
