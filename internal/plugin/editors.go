package plugin

import (
	"context"

	"clubmate/internal/rpc"
)

const (
	nvimImportKey     = "nvimImport"
	nvimImportOldKey  = "importOffered"
	nvimImportUnasked = "unasked"
	nvimImportDone    = "imported"
	nvimImportNotNow  = "declined"

	reviewDiffview = "diffview"
	reviewVSCode   = "vscode"
	reviewHidden   = "hidden"

	captionNvim   = "Review changes and Full diff open diffview in Neovim, where you comment on lines."
	captionVSCode = "Files open in VS Code. Review changes and Full diff need Neovim; turn it on to review."
	captionBoth   = "Both on: Review changes and Full diff open in Neovim (diffview)."
	captionNone   = "Both off: no Code in the rail. Review changes, Full diff and Open in editor are hidden; edit cards keep their inline diff."
)

func (c *Control) Editors(ctx context.Context) (rpc.EditorState, error) {
	nvim, vscode := c.Enabled(nvimPlugin), c.Enabled(vscodePlugin)
	imported, err := c.nvimImport(ctx)
	if err != nil {
		return rpc.EditorState{}, err
	}
	st := rpc.EditorState{Enabled: []string{}, NvimImport: imported, ImportRow: nvim && imported != nvimImportDone}
	if nvim {
		st.Enabled = append(st.Enabled, nvimPlugin)
	}
	if vscode {
		st.Enabled = append(st.Enabled, vscodePlugin)
	}
	switch {
	case nvim && vscode:
		st.Review, st.Caption = reviewDiffview, captionBoth
	case nvim:
		st.Review, st.Caption = reviewDiffview, captionNvim
	case vscode:
		st.Review, st.Caption = reviewVSCode, captionVSCode
	default:
		st.Review, st.Caption = reviewHidden, captionNone
	}
	st.Commands = editorCommands(nvim, vscode)
	return st, nil
}

func editorCommands(nvim, vscode bool) []rpc.EditorCommand {
	out := []rpc.EditorCommand{}
	if vscode {
		out = append(out, rpc.EditorCommand{ID: "vscode-off", Title: "Turn off VS Code", Detail: "Editors"})
	} else {
		out = append(out, rpc.EditorCommand{ID: "vscode-on", Title: "Turn on VS Code…", Detail: "Editors"})
	}
	if nvim {
		out = append(out, rpc.EditorCommand{ID: "nvim-off", Title: "Turn off Neovim", Detail: "Editors"})
	} else {
		out = append(out, rpc.EditorCommand{ID: "nvim-on", Title: "Turn on Neovim", Detail: "Editors"})
	}
	return append(out, rpc.EditorCommand{ID: "settings", Title: "Open Settings › General › Editors", Detail: "Settings"})
}

func (c *Control) nvimImport(ctx context.Context) (string, error) {
	var state string
	found, err := c.get(ctx, nvimPlugin, nvimImportKey, &state)
	if err != nil || found {
		return state, err
	}
	var offered bool
	if _, err := c.get(ctx, nvimPlugin, nvimImportOldKey, &offered); err != nil {
		return "", err
	}
	if offered {
		return nvimImportNotNow, nil
	}
	return nvimImportUnasked, nil
}
