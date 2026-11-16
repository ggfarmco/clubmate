package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
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

	attachedImageSide = 1568
	maxShrinkPixels   = 64 << 20
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
	return readImage(args.Path)
}

func readImage(path string) (imageResult, error) {
	data, mime, err := imageBytes(path)
	if err != nil {
		return imageResult{}, err
	}
	return imageResult{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}, nil
}

func attachedImage(path string) (attachment, error) {
	data, mime, err := imageBytes(path)
	if err != nil {
		return attachment{}, err
	}
	data, mime = shrinkImage(data, mime)
	return attachment{Kind: "image", Path: path, Source: path, Name: filepath.Base(path), MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}, nil
}

func imageBytes(path string) ([]byte, string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, "", err
	}
	if !isImage(resolved) {
		return nil, "", errNotImage
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, "", errNotImage
	}
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxImageBytes {
		return nil, "", errImageLarge
	}
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return nil, "", errNotImage
	}
	return data, mime, nil
}

func shrinkImage(data []byte, mime string) ([]byte, string) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || max(cfg.Width, cfg.Height) <= attachedImageSide || cfg.Width*cfg.Height > maxShrinkPixels {
		return data, mime
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, mime
	}
	b := src.Bounds()
	side := max(b.Dx(), b.Dy())
	w, h := max(1, b.Dx()*attachedImageSide/side), max(1, b.Dy()*attachedImageSide/side)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := b.Min.Y+y*b.Dy()/h, b.Min.Y+(y+1)*b.Dy()/h
		for x := range w {
			x0, x1 := b.Min.X+x*b.Dx()/w, b.Min.X+(x+1)*b.Dx()/w
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(bl/n>>8), uint8(a/n>>8)
		}
	}
	var buf bytes.Buffer
	if format == "jpeg" {
		if jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}) != nil {
			return data, mime
		}
		return buf.Bytes(), "image/jpeg"
	}
	if png.Encode(&buf, dst) != nil {
		return data, mime
	}
	return buf.Bytes(), "image/png"
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
