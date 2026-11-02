package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	maxImages     = 4
	maxImageBytes = 8 << 20
)

var (
	imageExts       = []string{".png", ".jpg", ".jpeg", ".gif", ".webp"}
	errImageUnnamed = errors.New("no agent message of this session names that image")
	errImageLarge   = fmt.Errorf("image is larger than %d MiB", maxImageBytes>>20)
	errNotImage     = errors.New("not an image")
)

type imageArgs struct {
	Session string `json:"session"`
	Path    string `json:"path"`
}

type imageResult struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

func isImage(path string) bool {
	return slices.Contains(imageExts, strings.ToLower(filepath.Ext(path)))
}

func resolveImage(target, root string) (string, bool) {
	path, anchor, ok := resolveTarget(target, root)
	if !ok || anchor != "" {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !isImage(resolved) || !isFile(resolved) {
		return "", false
	}
	return path, true
}

func imageRef(path, root string) sdui.ImageRef {
	ref := sdui.ImageRef{Path: path, Rel: relTo(root, path)}
	if f, err := os.Open(path); err == nil {
		if cfg, _, err := image.DecodeConfig(f); err == nil {
			ref.Width, ref.Height = cfg.Width, cfg.Height
		}
		f.Close()
	}
	return ref
}

func (a *app) imageCommand(_ context.Context, cmd pluginsdk.CommandRun, args imageArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	if !s.namesImage(args.Path) {
		return nil, errImageUnnamed
	}
	resolved, err := filepath.EvalSymlinks(args.Path)
	if err != nil {
		return nil, err
	}
	if !isImage(resolved) {
		return nil, errNotImage
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotImage
	}
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, errImageLarge
	}
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return nil, errNotImage
	}
	return imageResult{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}, nil
}

func (s *session) namesImage(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	for _, en := range s.entries {
		if slices.ContainsFunc(en.Images, func(r sdui.ImageRef) bool { return r.Path == path }) {
			return true
		}
	}
	return false
}
