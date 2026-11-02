package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	settingsKey   = "settings"
	settingsSlot  = "editor-nvim.settings"
	settingsTopic = "editor-nvim.settings-changed"
	editorNvim    = "nvim"
	editorVSCode  = "vscode"
)

var (
	errUnsavedToVSCode = errors.New("Save or discard unsaved changes first")
	errUnknownEditor   = errors.New("unknown editor")

	editorOptions = []sdui.Option{{Value: editorNvim, Label: "Neovim"}, {Value: editorVSCode, Label: "VS Code"}}
)

type editorSettings struct {
	Editor         string `json:"editor"`
	VSCodeAccepted bool   `json:"vscodeAccepted"`
}

type editorSettingsArgs struct {
	Editor         *string `json:"editor"`
	VSCodeAccepted *bool   `json:"-"`
}

func (a *app) loadSettings(ctx context.Context, h *pluginsdk.Host) (editorSettings, error) {
	s := editorSettings{Editor: editorNvim}
	if _, err := h.StorageGet(ctx, settingsKey, &s); err != nil {
		return editorSettings{Editor: editorNvim}, err
	}
	if !knownEditor(s.Editor) {
		s.Editor = editorNvim
	}
	return s, nil
}

func (a *app) setSettings(ctx context.Context, h *pluginsdk.Host, args editorSettingsArgs) (editorSettings, error) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	s, err := a.loadSettings(ctx, h)
	if err != nil {
		return editorSettings{}, err
	}
	next := s
	if args.Editor != nil {
		if !knownEditor(*args.Editor) {
			return s, errors.Join(fmt.Errorf("%w %q", errUnknownEditor, *args.Editor), a.renderSettings(ctx, h, s))
		}
		next.Editor = *args.Editor
	}
	if args.VSCodeAccepted != nil {
		next.VSCodeAccepted = *args.VSCodeAccepted
	}
	var stopped []editorKey
	if s.Editor != editorVSCode && next.Editor == editorVSCode {
		stopped, err = a.stopNvims(ctx, h)
	}
	if err == nil {
		err = h.StorageSet(ctx, settingsKey, next)
	}
	if err != nil {
		a.release(stopped)
		return s, errors.Join(err, a.renderSettings(ctx, h, s))
	}
	a.vscode.Store(next.Editor == editorVSCode)
	a.release(stopped)
	logError("render settings", a.renderSettings(ctx, h, next))
	_, err = h.Publish(ctx, settingsTopic, next)
	logError("publish "+settingsTopic, err)
	a.editorChanged(ctx, h)
	return next, nil
}

func (a *app) usesVSCode() bool {
	return a.vscode.Load()
}

func (a *app) renderSettings(ctx context.Context, h *pluginsdk.Host, s editorSettings) error {
	_, err := h.Render(ctx, "", settingsSlot, sdui.SettingRow("Editor", "Neovim or your installed VS Code in the Code section.", sdui.WithAction(sdui.Select("editor", editorOptions, s.Editor), "editor")))
	return err
}

func (a *app) settingsEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	if ev.Action != "editor" {
		return nil, fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
	}
	var editor string
	if err := json.Unmarshal(ev.Value, &editor); err != nil {
		return nil, fmt.Errorf("editor: %w", err)
	}
	return a.setSettings(ctx, h, editorSettingsArgs{Editor: &editor})
}

func (a *app) stopNvims(ctx context.Context, h *pluginsdk.Host) ([]editorKey, error) {
	keys, err := a.nvimRoots(ctx, h)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, k := range keys {
		unsaved, err := a.unsaved(k.workspace, k.root)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errUnsavedToVSCode, err)
		}
		names = append(names, unsaved...)
	}
	if len(names) > 0 {
		slices.Sort(names)
		return nil, fmt.Errorf("%w: %s", errUnsavedToVSCode, strings.Join(slices.Compact(names), ", "))
	}
	var errs []error
	for _, k := range keys {
		errs = append(errs, a.stopRoot(ctx, h, k.workspace, k.root, false))
	}
	return keys, errors.Join(errs...)
}

func (a *app) nvimRoots(ctx context.Context, h *pluginsdk.Host) ([]editorKey, error) {
	sessions, err := h.ListSessions(ctx, "", true)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	keys := make([]editorKey, 0, len(a.editors)+len(sessions))
	for k := range a.editors {
		keys = append(keys, k)
	}
	a.mu.Unlock()
	for _, s := range sessions {
		if s.State == contract.SessionStateRunning && s.Title == sessionTitle {
			keys = append(keys, editorKey{workspace: s.WorkspaceID, root: s.Cwd})
		}
	}
	slices.SortFunc(keys, func(x, y editorKey) int {
		return strings.Compare(x.workspace+"\x00"+x.root, y.workspace+"\x00"+y.root)
	})
	return slices.Compact(keys), nil
}

func (a *app) release(keys []editorKey) {
	for _, k := range keys {
		a.setStopping(k.workspace, k.root, false)
	}
}

func knownEditor(id string) bool {
	return slices.ContainsFunc(editorOptions, func(o sdui.Option) bool { return o.Value == id })
}
