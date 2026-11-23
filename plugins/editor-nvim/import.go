package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

const (
	importTitle     = "Import nvim config"
	importEvent     = "import"
	importStateKey  = "nvimImport"
	importOfferedID = "importOffered"
	importUnasked   = "unasked"
	importImported  = "imported"
	importDeclined  = "declined"
	importTopic     = "editor-nvim.import"
)

var (
	errImportSource = errors.New("no ~/.config/nvim to import")

	//go:embed import_prompt.md
	importTemplate string
)

type agentsNewArgs struct {
	Prompt string `json:"prompt"`
	Title  string `json:"title"`
}

type agentsNewResult struct {
	Session  string `json:"session"`
	Worktree string `json:"worktree"`
}

func importPrompt(source, layer, guide string) string {
	return strings.NewReplacer("{{source}}", source, "{{layer}}", layer, "{{guide}}", guide).Replace(importTemplate)
}

func importSource() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".config", "nvim")
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return ""
	}
	return path
}

func userLayer() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", appName())
}

func nvimGuide() string {
	return filepath.Join(pluginsdk.ResourcesDir(), "nvim-distro", "skill", "SKILL.md")
}

func (a *app) importConfig(ctx context.Context, h *pluginsdk.Host, workspaceID string) (agentsNewResult, error) {
	source := importSource()
	if source == "" {
		return agentsNewResult{}, errImportSource
	}
	var out agentsNewResult
	args := agentsNewArgs{Prompt: importPrompt(source, userLayer(), nvimGuide()), Title: importTitle}
	if err := h.RunCommand(ctx, "agents.new", workspaceID, args, &out); err != nil {
		return agentsNewResult{}, err
	}
	if err := a.setImportState(ctx, h, importImported); err != nil {
		logError(importEvent, err)
	}
	if err := h.Focus(ctx, workspaceID, "agents", ""); err != nil {
		logError("focus", err)
	}
	return out, nil
}

func (a *app) importFromEditor(h *pluginsdk.Host, e *editor) {
	_, err := a.importConfig(context.Background(), h, e.id)
	if err == nil {
		return
	}
	logError(importEvent, err)
	if c := e.connected(); c != nil {
		_ = c.exec(importErrorLua, nil, "Import: "+err.Error())
	}
}

func (a *app) sectionShown(h *pluginsdk.Host, ev contract.UISection) {
	if ev.Section != h.PluginID() || ev.OpenedFor != contract.OpenedForEdit {
		return
	}
	a.offerImport(h, ev.Workspace)
}

func (a *app) offerImport(h *pluginsdk.Host, workspaceID string) {
	ctx := context.Background()
	if !distroInUse() || importSource() == "" {
		return
	}
	a.offerMu.Lock()
	defer a.offerMu.Unlock()
	if err := a.askImport(ctx, h, workspaceID); err != nil {
		logError(importEvent, err)
	}
}

func (a *app) askImport(ctx context.Context, h *pluginsdk.Host, workspaceID string) error {
	state, err := importState(ctx, h)
	if err != nil || state != importUnasked {
		return err
	}
	e, err := a.active(ctx, h, workspaceID)
	if e == nil || err != nil {
		return err
	}
	c, err := a.ensure(ctx, h, e)
	if err != nil {
		return err
	}
	if err := a.setImportState(ctx, h, importDeclined); err != nil {
		return err
	}
	if err := c.exec(offerLua, nil); err != nil {
		return fmt.Errorf("ask failed, %s stored %s: %w", importStateKey, importDeclined, err)
	}
	return nil
}

func importState(ctx context.Context, h *pluginsdk.Host) (string, error) {
	var state string
	found, err := h.StorageGet(ctx, importStateKey, &state)
	if err != nil || found {
		return state, err
	}
	var offered bool
	if _, err := h.StorageGet(ctx, importOfferedID, &offered); err != nil {
		return "", err
	}
	if offered {
		return importDeclined, nil
	}
	return importUnasked, nil
}

func (a *app) setImportState(ctx context.Context, h *pluginsdk.Host, state string) error {
	if err := h.StorageSet(ctx, importStateKey, state); err != nil {
		return err
	}
	_, err := h.Publish(ctx, importTopic, map[string]string{"state": state})
	return err
}
