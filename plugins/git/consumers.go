package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/pluginsdk"
)

const (
	editorOwner           = "editor-nvim"
	editorStopRoot        = "editor-nvim.stop-root"
	editorReleaseRoot     = "editor-nvim.release-root"
	editorUnsaved         = "editor-nvim.unsaved"
	editorChecktime       = "editor-nvim.checktime"
	agentsStopRoot        = "agents.stop-root"
	codeNotFound          = -32004
	codePluginUnavailable = -32012
)

var errUnsavedSwitch = errors.New("Save or discard unsaved changes first")

type consumers struct {
	h         *pluginsdk.Host
	projectID string
}

type rootArgs struct {
	Root  string `json:"root"`
	Force bool   `json:"force,omitempty"`
}

func (c consumers) checkUnsaved(ctx context.Context, root string) error {
	var names []string
	err := c.h.RunCommand(ctx, editorUnsaved, c.projectID, rootArgs{Root: root}, &names)
	if rpcErr, ok := errors.AsType[*jsonrpc2.Error](err); ok && (rpcErr.Code == codeNotFound || rpcErr.Code == codePluginUnavailable) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %s", errUnsavedSwitch, editorError(err))
	}
	if len(names) > 0 {
		return fmt.Errorf("%w: %s", errUnsavedSwitch, strings.Join(names, ", "))
	}
	return nil
}

func (c consumers) checktime(ctx context.Context, root string) error {
	return c.h.RunCommand(ctx, editorChecktime, c.projectID, rootArgs{Root: root}, nil)
}

func (c consumers) stopEditor(ctx context.Context, root string, force bool) error {
	return c.h.RunCommand(ctx, editorStopRoot, c.projectID, rootArgs{Root: root, Force: force}, nil)
}

func (c consumers) releaseEditor(ctx context.Context, root string) error {
	return c.h.RunCommand(ctx, editorReleaseRoot, c.projectID, rootArgs{Root: root}, nil)
}

func (c consumers) stopAgents(ctx context.Context, root string) error {
	return c.h.RunCommand(ctx, agentsStopRoot, c.projectID, rootArgs{Root: root}, nil)
}

func editorError(err error) string {
	if _, ok := errors.AsType[*jsonrpc2.Error](err); ok {
		return strings.TrimPrefix(rpcMessage(err), "plugin "+editorOwner+" command.run: ")
	}
	return err.Error()
}
