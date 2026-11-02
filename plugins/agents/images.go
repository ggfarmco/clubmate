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
	if !ok || anchor != "" || !isImage(path) || !isFile(path) {
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
	info, err := os.Stat(args.Path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxImageBytes {
		return nil, errImageLarge
	}
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return nil, err
	}
	return imageResult{MimeType: http.DetectContentType(data), Data: base64.StdEncoding.EncodeToString(data)}, nil
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
