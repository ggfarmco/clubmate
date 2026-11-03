package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"clubmate/pkg/docext"
)

var errNotInRoot = errors.New("attachment is not a file inside the workspace folder")

const (
	opensURL    = "url"
	opensDoc    = "doc"
	opensEditor = "editor"
	opensSystem = "system"
)

type attachmentView struct {
	Attachment
	Path  string `json:"path,omitempty"`
	Opens string `json:"opens"`
}

func attachmentOf(a Attachment, root string) attachmentView {
	v := attachmentView{Attachment: a}
	if webURL(a.Ref) {
		v.Opens = opensURL
		return v
	}
	v.Path = a.Ref
	if root != "" {
		v.Path = filepath.Join(root, a.Ref)
	}
	switch {
	case docext.Is(a.Ref) || docext.IsArtifact(a.Ref):
		v.Opens = opensDoc
	case imageExt(a.Ref):
		v.Opens = opensSystem
	default:
		v.Opens = opensEditor
	}
	return v
}

func (b *backlog) root(ctx context.Context, workspaceID string) string {
	ws, err := b.h.GetWorkspace(ctx, workspaceID)
	if err != nil || len(ws.Folders) == 0 {
		return ""
	}
	return ws.Folders[0].Root()
}

func (b *backlog) openAttachment(ctx context.Context, workspaceID string, a Attachment) error {
	v := attachmentOf(a, b.root(ctx, workspaceID))
	switch v.Opens {
	case opensURL:
		return exec.CommandContext(ctx, "/usr/bin/open", a.Ref).Run()
	case opensDoc:
		return b.h.RunCommand(ctx, "docs.open", workspaceID, map[string]string{"path": v.Path}, nil)
	case opensEditor:
		return b.h.RunCommand(ctx, "editor.open", workspaceID, map[string]string{"path": v.Path}, nil)
	}
	path, err := insideRoot(b.root(ctx, workspaceID), v.Path)
	if err != nil {
		return err
	}
	return exec.CommandContext(ctx, "/usr/bin/open", "-a", "Preview", path).Run()
}

func insideRoot(root, path string) (string, error) {
	if root == "" {
		return "", errNotInRoot
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return "", errNotInRoot
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errNotInRoot
	}
	return real, nil
}
