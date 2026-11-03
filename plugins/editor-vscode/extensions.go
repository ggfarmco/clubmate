package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	settingsSlot    = "editor-vscode.settings"
	importAction    = "import-extensions"
	downloadAction  = "vscode-download"
	extensionsHint  = "Install the extensions of your desktop VS Code into the VS Code in Clubmate."
	codeMissingHint = "VS Code isn't installed on this Mac. Install it from code.visualstudio.com: Clubmate runs its VS Code server and imports extensions from it."
)

var (
	errImportRunning = errors.New("extensions are already being imported")
	extensionID      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*\.[A-Za-z0-9][A-Za-z0-9-]*$`)
)

type importFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

type importResult struct {
	Installed []string        `json:"installed"`
	Skipped   []string        `json:"skipped"`
	Failed    []importFailure `json:"failed"`
}

type settingsState struct {
	codeMissing bool
	accepted    bool
	downloading bool
	downloaded  bool
	failure     error
	imp         importStatus
}

type importStatus struct {
	running     bool
	done, total int
	result      *importResult
	err         error
}

func syncExtensions(ctx context.Context, s *vscodeServer, progress func(done, total int)) (importResult, error) {
	bin := s.serverBin()
	if bin == "" {
		return importResult{}, errServerMissing
	}
	want, err := listExtensions(ctx, s.code)
	if err != nil {
		return importResult{}, fmt.Errorf("list VS Code extensions: %w", err)
	}
	have, err := listExtensions(ctx, bin, "--server-data-dir", s.serverDir())
	if err != nil {
		return importResult{}, fmt.Errorf("list installed extensions: %w", err)
	}
	res := importResult{Installed: []string{}, Skipped: []string{}, Failed: []importFailure{}}
	var missing []string
	for _, id := range want {
		if slices.Contains(have, id) {
			res.Skipped = append(res.Skipped, id)
		} else {
			missing = append(missing, id)
		}
	}
	progress(0, len(missing))
	for i, id := range missing {
		if err := run(ctx, bin, "--install-extension", id, "--server-data-dir", s.serverDir()); err != nil {
			res.Failed = append(res.Failed, importFailure{ID: id, Error: err.Error()})
		} else {
			res.Installed = append(res.Installed, id)
		}
		progress(i+1, len(missing))
	}
	return res, nil
}

func listExtensions(ctx context.Context, bin string, args ...string) ([]string, error) {
	out, err := exec.CommandContext(ctx, bin, append(args, "--list-extensions")...).Output()
	if err != nil {
		return nil, err
	}
	var ids []string
	for line := range strings.Lines(string(out)) {
		if id := strings.TrimSpace(line); extensionID.MatchString(id) {
			ids = append(ids, strings.ToLower(id))
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

func run(ctx context.Context, bin string, args ...string) error {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(out.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func (a *app) importExtensions(ctx context.Context, h *pluginsdk.Host) (importResult, error) {
	a.mu.Lock()
	if a.imp.running {
		a.mu.Unlock()
		return importResult{}, errImportRunning
	}
	a.imp = importStatus{running: true}
	a.mu.Unlock()
	a.renderSettings(ctx, h)
	srv, err := a.server(h)
	var res importResult
	if err == nil {
		res, err = syncExtensions(ctx, srv, func(done, total int) {
			a.mu.Lock()
			a.imp.done, a.imp.total = done, total
			a.mu.Unlock()
			a.renderSettings(ctx, h)
		})
	}
	if errors.Is(err, errServerMissing) {
		err = errors.New("download the VS Code server first: Settings → VS Code → VS Code server")
	}
	a.mu.Lock()
	a.imp = importStatus{result: &res, err: err}
	if err == nil && len(res.Installed) > 0 {
		a.rev++
	}
	a.mu.Unlock()
	a.renderSettings(ctx, h)
	if err == nil && len(res.Installed) > 0 {
		go a.rerender(context.WithoutCancel(ctx), h, "")
	}
	return res, err
}

func (a *app) renderSettings(ctx context.Context, h *pluginsdk.Host) {
	_, err := h.Render(ctx, "", settingsSlot, settingsView(a.settingsState(ctx, h)))
	logError("render settings", err)
}

func (a *app) settingsState(ctx context.Context, h *pluginsdk.Host) settingsState {
	srv, err := a.server(h)
	if errors.Is(err, errCodeMissing) {
		return settingsState{codeMissing: true}
	}
	s, serr := a.loadSettings(ctx, h)
	a.mu.Lock()
	defer a.mu.Unlock()
	st := settingsState{accepted: s.Accepted, downloading: a.downloading, failure: cmp.Or(err, serr, a.srvErr), imp: a.imp}
	if srv != nil {
		st.downloaded = srv.Downloaded()
	}
	return st
}

func settingsView(st settingsState) sdui.Node {
	const server = "VS Code server"
	var rows []sdui.Node
	switch {
	case st.codeMissing:
		return sdui.VStack(sdui.SettingRow(server, codeMissingHint, sdui.MutedText("Not found")))
	case !st.accepted:
		rows = append(rows, sdui.Markdown(consentText()), sdui.SettingRow(server, "Not downloaded.", acceptButton()))
	case st.failure != nil:
		rows = append(rows, sdui.SettingRow(server, st.failure.Error(), sdui.Button("vscode-retry", "Retry", "arrow.clockwise")))
	case st.downloading:
		rows = append(rows, sdui.SettingRow(server, "Downloading the VS Code server…", sdui.WithLoading(downloadButton(), "Downloading")))
	case !st.downloaded:
		rows = append(rows, sdui.SettingRow(server, "Not downloaded.", downloadButton()))
	default:
		rows = append(rows, sdui.SettingRow(server, "Downloaded.", sdui.MutedText("Ready")))
	}
	return sdui.VStack(append(rows, extensionsRow(st))...)
}

func downloadButton() sdui.Node {
	return sdui.Button(downloadAction, "Download", "arrow.down.circle")
}

func extensionsRow(st settingsState) sdui.Node {
	button := sdui.Button(importAction, "Import from VS Code", "square.and.arrow.down")
	hint := extensionsHint
	switch {
	case st.imp.running:
		button = sdui.WithLoading(button, "Importing")
		if st.imp.total > 0 {
			hint = fmt.Sprintf("Installing %d of %d…", st.imp.done+1, st.imp.total)
			if st.imp.done == st.imp.total {
				hint = "Finishing…"
			}
		}
	case st.imp.err != nil:
		hint = "Import failed: " + st.imp.err.Error()
	case st.imp.result != nil:
		hint = importSummary(*st.imp.result)
	case !st.downloaded:
		button = sdui.WithDisabled(button)
		hint = "Download the VS Code server first."
	}
	return sdui.SettingRow("Extensions", hint, button)
}

func importSummary(r importResult) string {
	if len(r.Installed) == 0 && len(r.Failed) == 0 {
		return "All your VS Code extensions are installed."
	}
	parts := []string{fmt.Sprintf("Installed %d", len(r.Installed))}
	if len(r.Failed) > 0 {
		ids := make([]string, 0, len(r.Failed))
		for _, f := range r.Failed {
			ids = append(ids, f.ID)
		}
		parts = append(parts, fmt.Sprintf("%d failed: %s", len(r.Failed), strings.Join(ids, ", ")))
	}
	return strings.Join(parts, " · ")
}
