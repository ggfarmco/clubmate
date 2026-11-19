package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

func writePNG(t *testing.T, path string, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
	return buf.Bytes()
}

func TestImagesListMentionedImagesInOrder(t *testing.T) {
	t.Parallel()
	wt, other := t.TempDir(), t.TempDir()
	writePNG(t, filepath.Join(wt, "shots", "a.png"), 3, 2)
	for _, name := range []string{"b.PNG", "c.gif", "d.webp"} {
		writePNG(t, filepath.Join(wt, name), 1, 1)
	}
	outside := filepath.Join(other, "x.jpg")
	require.NoError(t, os.WriteFile(outside, []byte("not a jpeg"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(wt, "plan.md"), []byte("# Plan\n"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(wt, "plan.md"), filepath.Join(wt, "key.png")))
	text := "[shot](shots/a.png), `shots/a.png`, `" + outside + "`, `missing.png`, `b.PNG#x`, `plan.md`, `key.png`, `b.PNG`, `c.gif`, `d.webp`."

	r := docRefs(text, wt)

	assert.Equal(t, []sdui.ImageRef{
		{Path: filepath.Join(wt, "shots", "a.png"), Rel: "shots/a.png", Width: 3, Height: 2},
		{Path: outside, Rel: outside},
		{Path: filepath.Join(wt, "b.PNG"), Rel: "b.PNG", Width: 1, Height: 1},
		{Path: filepath.Join(wt, "c.gif"), Rel: "c.gif", Width: 1, Height: 1},
	}, r.images)
	assert.Equal(t, map[string]string{
		"shots/a.png": filepath.Join(wt, "shots", "a.png"),
		outside:       outside,
		"b.PNG":       filepath.Join(wt, "b.PNG"),
		"c.gif":       filepath.Join(wt, "c.gif"),
	}, r.imageLinks)
	assert.Equal(t, map[string]string{"plan.md": filepath.Join(wt, "plan.md")}, r.links)
	assert.Len(t, r.cards, 1)
}

func TestImageCommandServesOnlyNamedImages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	named, unnamed := filepath.Join(dir, "named.png"), filepath.Join(dir, "other.png")
	data := writePNG(t, named, 2, 2)
	writePNG(t, unnamed, 2, 2)
	a := namingApp(t, dir, named)
	run := pluginsdk.CommandRun{WorkspaceID: "ws"}

	res, err := a.imageCommand(t.Context(), run, imageArgs{Session: "s1", Path: named})
	require.NoError(t, err)
	assert.Equal(t, imageResult{MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(data)}, res)

	_, err = a.imageCommand(t.Context(), run, imageArgs{Session: "s1", Path: unnamed})
	assert.ErrorIs(t, err, errImageUnnamed)

	secret := filepath.Join(dir, "id_rsa")
	require.NoError(t, os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600))
	require.NoError(t, os.Remove(named))
	require.NoError(t, os.Symlink(secret, named))
	_, err = a.imageCommand(t.Context(), run, imageArgs{Session: "s1", Path: named})
	assert.ErrorIs(t, err, errNotImage)

	require.NoError(t, os.Remove(named))
	require.NoError(t, os.WriteFile(named, []byte("PRIVATE KEY"), 0o600))
	_, err = a.imageCommand(t.Context(), run, imageArgs{Session: "s1", Path: named})
	assert.ErrorIs(t, err, errNotImage)
}

func TestImageCommandShrinksToSide(t *testing.T) {
	t.Parallel()
	opaque, translucent := solidImage(800, 400, color.RGBA{R: 200, A: 255}), solidImage(800, 400, color.RGBA{R: 100, A: 100})
	tests := []struct {
		name       string
		file       string
		data       []byte
		side       int
		wantMime   string
		wantW      int
		wantH      int
		wantScaled bool
	}{
		{name: "opaque png becomes jpeg", file: "a.png", data: encodePNG(t, opaque), side: 100, wantMime: "image/jpeg", wantW: 200, wantH: 100, wantScaled: true},
		{name: "png with alpha stays png", file: "b.png", data: encodePNG(t, translucent), side: 100, wantMime: "image/png", wantW: 200, wantH: 100, wantScaled: true},
		{name: "side clamped and long side capped at four sides", file: "c.png", data: encodePNG(t, solidImage(100, 5000, color.Black)), side: 1, wantMime: "image/jpeg", wantW: 5, wantH: 256, wantScaled: true},
		{name: "gif gives its first frame", file: "d.gif", data: encodeGIF(t, opaque), side: 100, wantMime: "image/jpeg", wantW: 200, wantH: 100, wantScaled: true},
		{name: "small image as it is", file: "e.png", data: encodePNG(t, solidImage(50, 50, color.Black)), side: 100, wantMime: "image/png"},
		{name: "webp as it is", file: "f.webp", data: []byte("RIFF\x10\x00\x00\x00WEBPVP8 \x00\x00\x00\x00"), side: 100, wantMime: "image/webp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, tt.file)
			require.NoError(t, os.WriteFile(path, tt.data, 0o600))
			a := namingApp(t, dir, path)

			res, err := a.imageCommand(t.Context(), pluginsdk.CommandRun{WorkspaceID: "ws"}, imageArgs{Session: "s1", Path: path, Side: tt.side})
			require.NoError(t, err)

			r, ok := res.(imageResult)
			require.True(t, ok)
			assert.Equal(t, tt.wantMime, r.MimeType)
			assert.Equal(t, tt.wantScaled, r.Scaled)
			data, err := base64.StdEncoding.DecodeString(r.Data)
			require.NoError(t, err)
			if !tt.wantScaled {
				assert.Equal(t, tt.data, data)
				return
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
			require.NoError(t, err)
			assert.Equal(t, [2]int{tt.wantW, tt.wantH}, [2]int{cfg.Width, cfg.Height})
		})
	}
}

func TestImageCommandSideKeepsTheNamedCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	named, unnamed := filepath.Join(dir, "named.png"), filepath.Join(dir, "other.png")
	writePNG(t, named, 2, 2)
	writePNG(t, unnamed, 2, 2)
	a := namingApp(t, dir, named)

	_, err := a.imageCommand(t.Context(), pluginsdk.CommandRun{WorkspaceID: "ws"}, imageArgs{Session: "s1", Path: unnamed, Side: 100})

	assert.ErrorIs(t, err, errImageUnnamed)
}

func TestRotatedJPEGThumbIsUpright(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")
	src := solidImage(400, 200, color.RGBA{R: 255, A: 255})
	draw.Draw(src, image.Rect(200, 0, 400, 200), image.NewUniform(color.RGBA{B: 255, A: 255}), image.Point{}, draw.Src)
	require.NoError(t, os.WriteFile(path, exifJPEG(t, src, 6), 0o600))
	a := namingApp(t, dir, path)

	res, err := a.imageCommand(t.Context(), pluginsdk.CommandRun{WorkspaceID: "ws"}, imageArgs{Session: "s1", Path: path, Side: 64})
	require.NoError(t, err)

	r, ok := res.(imageResult)
	require.True(t, ok)
	data, err := base64.StdEncoding.DecodeString(r.Data)
	require.NoError(t, err)
	thumb, err := jpeg.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, image.Rect(0, 0, 64, 128), thumb.Bounds())
	top, _, _, _ := thumb.At(32, 8).RGBA()
	_, _, bottom, _ := thumb.At(32, 120).RGBA()
	assert.Greater(t, top, uint32(0xC000))
	assert.Greater(t, bottom, uint32(0xC000))
	ref := imageRef(path, dir)
	assert.Equal(t, [2]int{200, 400}, [2]int{ref.Width, ref.Height})
}

func namingApp(t *testing.T, dir string, paths ...string) *app {
	t.Helper()
	text := "Look:"
	for _, p := range paths {
		text += " `" + p + "`"
	}
	chunk, err := json.Marshal(map[string]any{"turn": 1, "kind": kindUpdate, "payload": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": text}}})
	require.NoError(t, err)
	lines := []string{`{"turn":1,"kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"go"}]}}`, string(chunk), `{"turn":1,"kind":"turn_end","payload":{"stopReason":"end_turn"}}`}
	log := filepath.Join(t.TempDir(), "s1.jsonl")
	require.NoError(t, os.WriteFile(log, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return docsApp(t, newSession(record{ID: "s1", Workspace: "ws", Worktree: dir}, newEventLog(log), false, func(string, bool) {}))
}

func solidImage(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Rect, image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func encodeGIF(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, gif.Encode(&buf, img, nil))
	return buf.Bytes()
}

func exifJPEG(t *testing.T, img image.Image, orientation byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	seg := []byte("Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00")
	seg = append(seg, orientation, 0, 0, 0, 0, 0, 0)
	n := len(seg) + 2
	out := buf.Bytes()
	return slices.Concat(out[:2], []byte{0xFF, 0xE1, byte(n >> 8), byte(n)}, seg, out[2:])
}
