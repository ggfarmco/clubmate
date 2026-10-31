package main

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"clubmate/pkg/pluginsdk"
)

const (
	importTitle     = "Import nvim config"
	importEvent     = "import"
	importOfferedID = "importOffered"
)

var (
	errImportHome   = errors.New("open a project workspace first")
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

func (a *app) importConfig(ctx context.Context, h *pluginsdk.Host, projectID string) (agentsNewResult, error) {
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return agentsNewResult{}, err
	}
	if p.Home {
		return agentsNewResult{}, errImportHome
	}
	source := importSource()
	if source == "" {
		return agentsNewResult{}, errImportSource
	}
	var out agentsNewResult
	args := agentsNewArgs{Prompt: importPrompt(source, userLayer(), nvimGuide()), Title: importTitle}
	if err := h.RunCommand(ctx, "agents.new", projectID, args, &out); err != nil {
		return agentsNewResult{}, err
	}
	if err := h.Focus(ctx, projectID, "agents", ""); err != nil {
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

func (a *app) offerImport(h *pluginsdk.Host, e *editor, c *client) {
	ctx := context.Background()
	if !distroInUse() || importSource() == "" {
		return
	}
	p, err := h.GetProject(ctx, e.id)
	if err != nil || p.Home {
		return
	}
	a.offerMu.Lock()
	defer a.offerMu.Unlock()
	var offered bool
	if found, err := h.StorageGet(ctx, importOfferedID, &offered); err != nil || found && offered {
		return
	}
	if err := h.StorageSet(ctx, importOfferedID, true); err != nil {
		logError(importEvent, err)
		return
	}
	if err := c.exec(offerLua, nil); err != nil {
		logError(importEvent, err)
	}
}
