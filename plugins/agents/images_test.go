package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
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
	chunk, err := json.Marshal(map[string]any{"turn": 1, "kind": kindUpdate, "payload": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Look: `" + named + "`"}}})
	require.NoError(t, err)
	lines := []string{`{"turn":1,"kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"go"}]}}`, string(chunk), `{"turn":1,"kind":"turn_end","payload":{"stopReason":"end_turn"}}`}
	path := filepath.Join(dir, "s1.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	a := docsApp(t, newSession(record{ID: "s1", Workspace: "ws", Worktree: dir}, newEventLog(path), false, func(string, bool) {}))
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
