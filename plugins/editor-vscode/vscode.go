package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/editorroot"
	"clubmate/plugins/internal/gitrepo"
)

const (
	vscodeKey         = "vscode"
	vscodeIcon        = "curlybraces"
	vscodeRetryWindow = 30 * time.Second
	vscodeLicenseURL  = "https://aka.ms/vscode-server-license"
	vscodeConsent     = "VS Code needs Microsoft's VS Code Server: about 600 MB, downloaded once per version, under the VS Code Server License Terms."
)

var errNoConsent = errors.New("vscode-accept: the consent is not shown")

type vscodeOpened struct {
	root string
	open openArgs
}

func (a *app) showLogged(ctx context.Context, h *pluginsdk.Host, workspaceID string) {
	if a.isDeleted(workspaceID) {
		return
	}
	root, ok, err := a.active(ctx, h, workspaceID)
	if ok && err == nil {
		err = a.show(ctx, h, workspaceID, root, nil)
	}
	logError("show "+workspaceID, err)
}

func (a *app) active(ctx context.Context, h *pluginsdk.Host, workspaceID string) (string, bool, error) {
	p, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return "", false, err
	}
	root, _, ok, err := a.selectRoot(p, "")
	if !ok || err != nil {
		return "", false, err
	}
	logError("watch "+p.ID, a.heads.Watch(ctx, h, p))
	return root, true, nil
}

func (a *app) selectRoot(p pluginsdk.Workspace, want string) (string, bool, bool, error) {
	existing := editorroot.Existing(p)
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.roots.Select(p, existing, want)
}

func (a *app) workspaceUpdated(ctx context.Context, h *pluginsdk.Host, p pluginsdk.Workspace) {
	if !a.isTracked(p.ID) {
		return
	}
	root, moved, ok, err := a.selectRoot(p, "")
	if !ok || err != nil {
		logError("workspace.updated", err)
		return
	}
	logError("watch "+p.ID, a.heads.Watch(ctx, h, p))
	if !moved {
		go a.renderStatus(ctx, h, p.ID, root)
		return
	}
	go func() { logError("switch "+p.ID, a.show(ctx, h, p.ID, root, nil)) }()
}

func (a *app) fsChanged(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.FSChange) {
	if !a.heads.Moved(ev) {
		return
	}
	if root, ok := a.current(ev.WorkspaceID); ok {
		go a.renderStatus(ctx, h, ev.WorkspaceID, root)
	}
}

func (a *app) open(ctx context.Context, h *pluginsdk.Host, workspaceID string, args openArgs) error {
	if args.Path == "" {
		return errNoPath
	}
	if strings.ContainsAny(args.Path, "\n\r") {
		return errBadPath
	}
	t, err := editorroot.Resolve(ctx, h, workspaceID, args.Path)
	if err != nil {
		return err
	}
	root, _, ok, err := a.selectRoot(t.Workspace, t.Want)
	if !ok || err != nil {
		return err
	}
	logError("watch "+workspaceID, a.heads.Watch(ctx, h, t.Workspace))
	args.Path = gitrepo.Canonical(args.Path)
	if err := a.show(ctx, h, workspaceID, root, &args); err != nil {
		return err
	}
	if args.Focus || t.Switched {
		if err := h.Focus(ctx, workspaceID, h.PluginID(), mainSlot); err != nil {
			logError("focus", err)
		}
	}
	return nil
}

func (a *app) show(ctx context.Context, h *pluginsdk.Host, workspaceID, root string, open *openArgs) error {
	srv, view, started, err := a.ready(ctx, h)
	if cur, ok := a.current(workspaceID); ok {
		root = cur
	}
	if view.Type == "" {
		port, token, _ := srv.Base()
		view = sdui.WebView(vscodeKey, workbenchURL(port, token, root, a.lastOpen(workspaceID, root, open)), a.revision())
	}
	err = errors.Join(err, a.render(ctx, h, workspaceID, root, mainSlot, view))
	a.renderStatus(ctx, h, workspaceID, root)
	if started {
		go a.rerender(ctx, h, workspaceID)
	}
	return err
}

func (a *app) ready(ctx context.Context, h *pluginsdk.Host) (*vscodeServer, sdui.Node, bool, error) {
	a.vsMu.Lock()
	defer a.vsMu.Unlock()
	srv, err := a.server(h)
	if errors.Is(err, errCodeMissing) {
		return nil, codeMissingView(), false, nil
	}
	if err != nil {
		return nil, failedView(err), false, err
	}
	if err := a.failure(); err != nil {
		return nil, failedView(err), false, nil
	}
	s, err := a.loadSettings(ctx, h)
	if err != nil {
		return nil, failedView(err), false, err
	}
	if !s.Accepted {
		return nil, consentView(), false, nil
	}
	if !srv.Downloaded() {
		if err := a.download(ctx, h, srv); err != nil {
			return nil, failedView(err), false, err
		}
	}
	if _, _, ok := srv.Base(); ok {
		return srv, sdui.Node{}, false, nil
	}
	if err := srv.Start(ctx, a.isDark()); err != nil {
		a.setFailure(err)
		return nil, failedView(err), false, err
	}
	return srv, sdui.Node{}, true, nil
}

func (a *app) download(ctx context.Context, h *pluginsdk.Host, srv *vscodeServer) error {
	a.setDownloading(true)
	a.renderAll(ctx, h, downloadingView())
	a.renderSettings(ctx, h)
	err := srv.Download(ctx)
	a.setDownloading(false)
	if err != nil {
		a.setFailure(err)
	}
	a.renderSettings(ctx, h)
	return err
}

func (a *app) prepare(ctx context.Context, h *pluginsdk.Host) {
	a.vsMu.Lock()
	defer a.vsMu.Unlock()
	srv, err := a.server(h)
	if err != nil || a.failure() != nil || srv.Downloaded() {
		return
	}
	s, err := a.loadSettings(ctx, h)
	if err != nil || !s.Accepted {
		logError("prepare", err)
		return
	}
	logError("download", a.download(ctx, h, srv))
}

func (a *app) render(ctx context.Context, h *pluginsdk.Host, workspaceID, root, slot string, n sdui.Node) error {
	a.slotMu.Lock()
	defer a.slotMu.Unlock()
	if cur, ok := a.current(workspaceID); !ok || cur != root {
		return nil
	}
	_, err := h.Render(ctx, workspaceID, slot, n)
	return err
}

func (a *app) renderStatus(ctx context.Context, h *pluginsdk.Host, workspaceID, root string) {
	logError("render status", a.render(ctx, h, workspaceID, root, statusSlot, statusView(gitrepo.BranchAt(ctx, root))))
}

func (a *app) renderAll(ctx context.Context, h *pluginsdk.Host, n sdui.Node) {
	for _, id := range a.tracked() {
		if root, ok := a.current(id); ok {
			logError("render", a.render(ctx, h, id, root, mainSlot, n))
		}
	}
}

func (a *app) rerender(ctx context.Context, h *pluginsdk.Host, except string) {
	for _, id := range a.tracked() {
		if id != except {
			a.showLogged(ctx, h, id)
		}
	}
}

func (a *app) lastOpen(workspaceID, root string, open *openArgs) *openArgs {
	a.mu.Lock()
	defer a.mu.Unlock()
	if open != nil {
		a.opened[workspaceID] = vscodeOpened{root: root, open: *open}
		return open
	}
	if last, ok := a.opened[workspaceID]; ok && last.root == root {
		return &last.open
	}
	return nil
}

func (a *app) server(h *pluginsdk.Host) (*vscodeServer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.srv != nil {
		return a.srv, nil
	}
	dir := pluginsdk.StateDir()
	if dir == "" {
		return nil, errNoStateDir
	}
	code, err := findCode(exec.LookPath, fileExists)
	if err != nil {
		return nil, err
	}
	a.srv = newVSCodeServer(dir, code, kvPorts{h})
	go a.watchServer(h, a.srv)
	return a.srv, nil
}

func (a *app) runningServer() *vscodeServer {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.srv
}

func (a *app) watchServer(h *pluginsdk.Host, srv *vscodeServer) {
	var last time.Time
	for err := range srv.Exited() {
		logError("vscode server", err)
		if time.Since(last) < vscodeRetryWindow {
			a.setFailure(err)
			a.renderSettings(context.Background(), h)
		}
		last = time.Now()
		a.rerender(context.Background(), h, "")
	}
}

func (a *app) accept(ctx context.Context, h *pluginsdk.Host) error {
	if err := h.StorageSet(ctx, settingsKey, settings{Accepted: true}); err != nil {
		return err
	}
	go a.rerender(ctx, h, "")
	go a.prepare(context.WithoutCancel(ctx), h)
	return nil
}

func (a *app) retry(ctx context.Context, h *pluginsdk.Host) {
	a.setFailure(nil)
	a.renderSettings(ctx, h)
	go a.rerender(ctx, h, "")
	go a.prepare(context.WithoutCancel(ctx), h)
}

func (a *app) restart(ctx context.Context, h *pluginsdk.Host) {
	a.setFailure(nil)
	a.renderSettings(ctx, h)
	if srv := a.runningServer(); srv != nil {
		srv.Stop()
	}
	a.rerender(ctx, h, "")
}

func (a *app) themeChanged(ctx context.Context, h *pluginsdk.Host, dark bool) {
	a.mu.Lock()
	a.dark = dark
	srv := a.srv
	a.mu.Unlock()
	if err := h.StorageSet(ctx, themeDarkKey, dark); err != nil {
		logError(themeTopic, err)
	}
	if srv == nil {
		return
	}
	if err := writeMachineSettings(srv.serverDir(), dark); err != nil {
		logError(themeTopic, err)
	}
	a.mu.Lock()
	a.rev++
	a.mu.Unlock()
	go a.rerender(ctx, h, "")
}

func (a *app) shutdown(context.Context, *pluginsdk.Host) {
	if srv := a.runningServer(); srv != nil {
		srv.Stop()
	}
}

func (a *app) drop(workspaceID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.roots.Drop(workspaceID)
	delete(a.opened, workspaceID)
}

func (a *app) current(workspaceID string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.roots.Current(workspaceID)
}

func (a *app) tracked() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.roots.Tracked()
}

func (a *app) isTracked(workspaceID string) bool {
	_, ok := a.current(workspaceID)
	return ok
}

func (a *app) isDeleted(workspaceID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.roots.Deleted(workspaceID)
}

func (a *app) isDark() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dark
}

func (a *app) failure() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.srvErr
}

func (a *app) setFailure(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.srvErr = err
}

func (a *app) setDownloading(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.downloading = on
}

func (a *app) revision() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rev
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func consentView() sdui.Node {
	return sdui.VStack(sdui.Card("", vscodeIcon, sdui.Markdown(consentText()), acceptButton()))
}

func consentText() string {
	return strings.Replace(vscodeConsent, "VS Code Server License Terms", "[VS Code Server License Terms]("+vscodeLicenseURL+")", 1)
}

func acceptButton() sdui.Node {
	return sdui.WithConfirm(sdui.Button("vscode-accept", "Accept & Download", "arrow.down.circle"), "Accept the VS Code Server License Terms and download the VS Code server?")
}

func downloadingView() sdui.Node {
	return sdui.Empty("Downloading the VS Code server…")
}

func failedView(err error) sdui.Node {
	retry := sdui.Button("vscode-retry", "Retry", "arrow.clockwise")
	return sdui.EmptyState(err.Error(), sdui.EmptyProps{Title: "VS Code could not start", Icon: "exclamationmark.triangle", Action: &retry})
}

func codeMissingView() sdui.Node {
	return sdui.Empty(errCodeMissing.Error())
}

func statusView(branch string) sdui.Node {
	items := []sdui.Node{sdui.MutedText("VS Code")}
	if branch != "" {
		items = append(items, sdui.MutedText(branch))
	}
	return sdui.HStack(items...)
}
