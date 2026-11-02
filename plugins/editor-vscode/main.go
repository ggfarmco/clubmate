package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/editorroot"
)

const (
	mainSlot     = "editor-vscode.main"
	statusSlot   = "editor-vscode.status"
	settingsKey  = "settings"
	themeTopic   = "theme.changed"
	themeSeqKey  = "theme.seq"
	themeDarkKey = "theme.dark"
	legacyPlugin = "editor-nvim"
)

var (
	errUnknownAction  = errors.New("unknown action")
	errUnknownCommand = errors.New("unknown command")
	errNoPath         = errors.New("open: path is required")
	errBadPath        = errors.New("open: path must not contain line breaks")
	errNoStateDir     = errors.New("vscode: no plugin state dir")
)

type app struct {
	mu     sync.Mutex
	slotMu sync.Mutex
	vsMu   sync.Mutex
	roots  editorroot.Roots
	heads  *editorroot.Heads
	opened map[string]vscodeOpened
	srv    *vscodeServer
	srvErr error
	rev    int
	dark   bool
}

type settings struct {
	Accepted bool `json:"accepted"`
}

type openArgs struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Col   int    `json:"col"`
	Focus bool   `json:"focus"`
}

type terminalArgs struct {
	Cwd string `json:"cwd"`
}

type themeDark struct {
	Dark bool `json:"dark"`
}

func main() {
	a := &app{roots: editorroot.NewRoots(), opened: map[string]vscodeOpened{}, dark: true}
	a.heads = editorroot.NewHeads(a.isDeleted)
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, FSChanged: a.fsChanged, Shutdown: a.shutdown})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	if err := adoptLegacyState(pluginsdk.StateDir()); err != nil {
		logError("adopt state", err)
	}
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	present, err := h.Presence(ctx)
	if err != nil {
		return err
	}
	for _, pattern := range []string{contract.TopicUIOpened, contract.TopicWorkspaceUpdated, contract.TopicWorkspaceDeleted} {
		if _, err := h.Subscribe(ctx, pattern, ready.Seq); err != nil {
			return err
		}
	}
	dark := true
	if _, err := h.StorageGet(ctx, themeDarkKey, &dark); err != nil {
		return err
	}
	a.mu.Lock()
	a.dark = dark
	a.mu.Unlock()
	var themeSeq int64
	if _, err := h.StorageGet(ctx, themeSeqKey, &themeSeq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, themeTopic, themeSeq); err != nil {
		return err
	}
	for _, id := range present {
		go a.showLogged(ctx, h, id)
	}
	return nil
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case contract.TopicUIOpened:
		var opened contract.UIPresence
		if err := json.Unmarshal(ev.Payload, &opened); err != nil {
			logError(ev.Topic, err)
			return
		}
		go a.showLogged(ctx, h, opened.Workspace)
	case contract.TopicWorkspaceUpdated:
		var p contract.WorkspaceChanged
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			logError(ev.Topic, err)
			return
		}
		a.workspaceUpdated(ctx, h, p.Info)
	case contract.TopicWorkspaceDeleted:
		var p contract.WorkspaceDeleted
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			logError(ev.Topic, err)
			return
		}
		a.drop(p.Workspace)
		if err := a.heads.Unwatch(ctx, h, p.Workspace); err != nil {
			logError("unwatch "+p.Workspace, err)
		}
	case themeTopic:
		var t themeDark
		if err := json.Unmarshal(ev.Payload, &t); err != nil {
			logError(themeTopic, err)
			return
		}
		a.themeChanged(ctx, h, t.Dark)
		if err := h.StorageSet(ctx, themeSeqKey, ev.Seq); err != nil {
			logError(themeTopic, err)
		}
	}
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	switch ev.Action {
	case "vscode-accept":
		if _, err := a.server(h); err != nil || ev.Slot != mainSlot || !a.isTracked(ev.WorkspaceID) {
			return nil, errNoConsent
		}
		return nil, a.accept(ctx, h)
	case "vscode-retry":
		a.retry(ctx, h)
		return nil, nil
	}
	return nil, fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	name := strings.TrimPrefix(cmd.ID, h.PluginID()+".")
	if name == "settings" {
		return a.loadSettings(ctx, h)
	}
	if a.isDeleted(cmd.WorkspaceID) {
		return nil, nil
	}
	switch name {
	case "open":
		var args openArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("open args: %w", err)
		}
		return nil, a.open(ctx, h, cmd.WorkspaceID, args)
	case "terminal":
		root, ok, err := a.active(ctx, h, cmd.WorkspaceID)
		if !ok || err != nil {
			return nil, err
		}
		return nil, h.RunCommand(ctx, "terminals.new", cmd.WorkspaceID, terminalArgs{Cwd: root}, nil)
	case "restart":
		a.restart(ctx, h)
		return nil, nil
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (a *app) loadSettings(ctx context.Context, h *pluginsdk.Host) (settings, error) {
	var s settings
	_, err := h.StorageGet(ctx, settingsKey, &s)
	return s, err
}

func adoptLegacyState(stateDir string) error {
	if stateDir == "" {
		return errNoStateDir
	}
	own := filepath.Join(stateDir, "vscode")
	legacy := filepath.Join(filepath.Dir(stateDir), legacyPlugin, "vscode")
	if _, err := os.Stat(own); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(legacy); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return os.Rename(legacy, own)
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "editor-vscode:", what+":", err)
	}
}
