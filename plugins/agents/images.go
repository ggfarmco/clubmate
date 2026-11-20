package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	maxImages     = 4
	maxImageBytes = 8 << 20

	attachedImageSide = 1568
	maxShrinkPixels   = 64 << 20
	minThumbSide      = 64
	maxThumbSide      = 1024
	maxThumbs         = 64
	maxThumbBytes     = 32 << 20
	jpegQuality       = 85
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
	Side    int    `json:"side,omitempty"`
}

type imageResult struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
	Scaled   bool   `json:"scaled,omitempty"`
}

type thumbKey struct {
	path  string
	mtime time.Time
	size  int64
	side  int
}

type thumbCache struct {
	mu     sync.Mutex
	shrink sync.Mutex
	items  map[thumbKey]imageResult
	order  []thumbKey
	bytes  int
}

type fitFunc func(w, h int) (int, int)

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
	f, err := os.Open(path)
	if err != nil {
		return ref
	}
	defer f.Close()
	var head bytes.Buffer
	cfg, format, err := image.DecodeConfig(io.TeeReader(f, &head))
	if err != nil {
		return ref
	}
	ref.Width, ref.Height = cfg.Width, cfg.Height
	if format == "jpeg" && exifOrientation(head.Bytes()) >= 5 {
		ref.Width, ref.Height = cfg.Height, cfg.Width
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
	if args.Side <= 0 {
		return readImage(args.Path)
	}
	return a.thumbs.image(args.Path, min(max(args.Side, minThumbSide), maxThumbSide))
}

func (c *thumbCache) image(path string, side int) (imageResult, error) {
	key, keyed := thumbKeyFor(path, side)
	if res, ok := c.get(key); ok {
		return res, nil
	}
	data, mime, err := imageBytes(path)
	if err != nil {
		return imageResult{}, err
	}
	c.shrink.Lock()
	defer c.shrink.Unlock()
	if res, ok := c.get(key); ok {
		return res, nil
	}
	dst, _, ok := scaleImage(data, thumbFit(side), false)
	if !ok {
		return encodeResult(data, mime), nil
	}
	out, outMime, err := encodeImage(dst, !dst.Opaque())
	if err != nil {
		return encodeResult(data, mime), nil
	}
	res := encodeResult(out, outMime)
	res.Scaled = true
	if keyed {
		c.put(key, res)
	}
	return res, nil
}

func (c *thumbCache) get(key thumbKey) (imageResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	res, ok := c.items[key]
	if ok {
		c.order = append(slices.DeleteFunc(c.order, func(k thumbKey) bool { return k == key }), key)
	}
	return res, ok
}

func (c *thumbCache) put(key thumbKey, res imageResult) {
	if len(res.Data) > maxThumbBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[thumbKey]imageResult{}
	}
	if old, ok := c.items[key]; ok {
		c.bytes -= len(old.Data)
	} else {
		c.order = append(c.order, key)
	}
	c.items[key] = res
	c.bytes += len(res.Data)
	for len(c.order) > maxThumbs || c.bytes > maxThumbBytes {
		c.bytes -= len(c.items[c.order[0]].Data)
		delete(c.items, c.order[0])
		c.order = c.order[1:]
	}
}

func readImage(path string) (imageResult, error) {
	data, mime, err := imageBytes(path)
	if err != nil {
		return imageResult{}, err
	}
	return encodeResult(data, mime), nil
}

func attachedImage(path string) (attachment, error) {
	data, mime, err := imageBytes(path)
	if err != nil {
		return attachment{}, err
	}
	if dst, format, ok := scaleImage(data, longFit(attachedImageSide), true); ok {
		if out, outMime, err := encodeImage(dst, format != "jpeg"); err == nil {
			data, mime = out, outMime
		}
	}
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

func thumbKeyFor(path string, side int) (thumbKey, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return thumbKey{}, false
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return thumbKey{}, false
	}
	return thumbKey{path: resolved, mtime: info.ModTime(), size: info.Size(), side: side}, true
}

func encodeResult(data []byte, mime string) imageResult {
	return imageResult{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}
}

func thumbFit(side int) fitFunc {
	return func(w, h int) (int, int) {
		f := min(1, float64(side)/float64(min(w, h)), float64(4*side)/float64(max(w, h)))
		return max(1, int(float64(w)*f+0.5)), max(1, int(float64(h)*f+0.5))
	}
}

func longFit(side int) fitFunc {
	return func(w, h int) (int, int) {
		long := max(w, h)
		if long <= side {
			return w, h
		}
		return max(1, w*side/long), max(1, h*side/long)
	}
}

func scaleImage(data []byte, fit fitFunc, reorient bool) (*image.RGBA, string, bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width*cfg.Height > maxShrinkPixels {
		return nil, "", false
	}
	orientation := exifOrientation(data)
	w, h := cfg.Width, cfg.Height
	if orientation >= 5 {
		w, h = h, w
	}
	tw, th := fit(w, h)
	if tw >= w && th >= h && !(reorient && orientation >= 2) {
		return nil, "", false
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", false
	}
	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Rect, src, b.Min, draw.Src)
	if orientation >= 5 {
		tw, th = th, tw
	}
	return orient(boxShrink(rgba, tw, th), orientation), format, true
}

func boxShrink(src *image.RGBA, w, h int) *image.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := y*sh/h, (y+1)*sh/h
		for x := range w {
			x0, x1 := x*sw/w, (x+1)*sw/w
			var sum [4]uint64
			for sy := y0; sy < y1; sy++ {
				row := src.Pix[sy*src.Stride+x0*4 : sy*src.Stride+x1*4]
				for i := 0; i < len(row); i += 4 {
					sum[0], sum[1], sum[2], sum[3] = sum[0]+uint64(row[i]), sum[1]+uint64(row[i+1]), sum[2]+uint64(row[i+2]), sum[3]+uint64(row[i+3])
				}
			}
			n := uint64((x1 - x0) * (y1 - y0))
			i := dst.PixOffset(x, y)
			for c := range sum {
				dst.Pix[i+c] = uint8(sum[c] / n)
			}
		}
	}
	return dst
}

func orient(src *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return src
	}
	w, h := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		for x := range dw {
			sx, sy := x, y
			switch orientation {
			case 2:
				sx = w - 1 - x
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sy = h - 1 - y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			copy(dst.Pix[dst.PixOffset(x, y):][:4], src.Pix[src.PixOffset(sx, sy):][:4])
		}
	}
	return dst
}

func encodeImage(img *image.RGBA, keepPNG bool) ([]byte, string, error) {
	var buf bytes.Buffer
	if keepPNG {
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/png", nil
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xFF {
			i++
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		if n < 2 || i+2+n > len(data) {
			return 1
		}
		if marker == 0xE1 {
			if o, ok := tiffOrientation(data[i+4 : i+2+n]); ok {
				return o
			}
		}
		i += 2 + n
	}
	return 1
}

func tiffOrientation(seg []byte) (int, bool) {
	tiff, ok := bytes.CutPrefix(seg, []byte("Exif\x00\x00"))
	if !ok || len(tiff) < 8 {
		return 0, false
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, false
	}
	off := int(order.Uint32(tiff[4:]))
	if off < 8 || off+2 > len(tiff) {
		return 0, false
	}
	for k := range int(order.Uint16(tiff[off:])) {
		e := off + 2 + k*12
		if e+12 > len(tiff) {
			return 0, false
		}
		if order.Uint16(tiff[e:]) != 0x0112 {
			continue
		}
		o := int(order.Uint16(tiff[e+8:]))
		return o, o >= 1 && o <= 8
	}
	return 0, false
}
