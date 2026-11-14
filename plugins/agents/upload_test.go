package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/agents/acp"
)

func TestSaveUploadsWritesFileAndPointsAttachmentAtIt(t *testing.T) {
	t.Parallel()
	a := &app{stateDir: t.TempDir()}
	data := []byte("PK\x03\x04\xff\x00")
	atts := []attachment{
		{Kind: "file", Name: "../logs.zip", MimeType: "application/zip", Data: base64.StdEncoding.EncodeToString(data)},
		{Kind: "image", MimeType: "image/jpeg", Data: "AAAA"},
	}
	require.NoError(t, a.saveUploads("agt_1", atts))

	assert.Equal(t, "logs.zip", filepath.Base(atts[0].Path))
	assert.Equal(t, filepath.Join(a.stateDir, uploadsDir, "agt_1"), filepath.Dir(filepath.Dir(atts[0].Path)))
	got, err := os.ReadFile(atts[0].Path)
	require.NoError(t, err)
	assert.Equal(t, data, got)
	assert.Empty(t, atts[0].Data)
	assert.Equal(t, "AAAA", atts[1].Data)

	assert.ErrorIs(t, a.saveUploads("agt_1", []attachment{{Kind: "file", Name: "x", Data: "%%%"}}), errBadUpload)
}

func TestPromptBlocksLinksBinaryFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.zip"), []byte("PK\x03\x04\xff\xfe"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o600))
	args := promptArgs{Attachments: []attachment{{Kind: "file", Path: "a.zip"}, {Kind: "file", Path: "a.txt"}}}

	blocks := promptBlocks(args, dir, acp.PromptCapabilities{EmbeddedContext: true})
	require.Len(t, blocks, 3)
	assert.Equal(t, acp.ContentBlock{Type: "resource_link", URI: "file://" + filepath.Join(dir, "a.zip"), Name: "a.zip"}, blocks[1])
	assert.Equal(t, "resource", blocks[2].Type)
}

func TestQueuedImageIsReadOnlyFromTheSessionUploads(t *testing.T) {
	t.Parallel()
	a := &app{stateDir: t.TempDir()}
	inside := filepath.Join(a.stateDir, uploadsDir, "agt_1", "x", imageName)
	outside := filepath.Join(t.TempDir(), "secret")
	link := filepath.Join(a.stateDir, uploadsDir, "agt_1", "x", "link")
	require.NoError(t, os.MkdirAll(filepath.Dir(inside), 0o700))
	require.NoError(t, os.WriteFile(inside, []byte("img"), 0o600))
	require.NoError(t, os.WriteFile(outside, []byte("key"), 0o600))
	require.NoError(t, os.Symlink(outside, link))
	args := promptArgs{Attachments: []attachment{{Kind: "image", Path: inside}, {Kind: "image", Path: outside}, {Kind: "image", Path: link}}}

	got := a.withImages("agt_1", args)

	assert.Equal(t, []string{base64.StdEncoding.EncodeToString([]byte("img")), "", ""}, []string{got.Attachments[0].Data, got.Attachments[1].Data, got.Attachments[2].Data})
	assert.Empty(t, args.Attachments[0].Data)
}

func TestUnreadableSpilledQueueIsKeptAside(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := queueSpillPath(dir, "agt_1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("{broken"), 0o600))

	_, err := readSpilledQueue(dir, "agt_1")
	require.Error(t, err)
	require.NoError(t, dropSpilledQueue(dir, "agt_1"))

	got, err := os.ReadFile(path + ".bad")
	require.NoError(t, err)
	assert.Equal(t, "{broken", string(got))
}

func TestSaveOfADeletedSessionDropsItsSpilledQueue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	n := &notifier{kv: memStore{}, dir: dir}
	s := newSession(record{ID: "agt_1"}, newEventLog(logPath(dir, "agt_1")), false, func(string, bool) {})
	s.deleted = true
	require.NoError(t, os.MkdirAll(filepath.Dir(queueSpillPath(dir, "agt_1")), 0o700))
	require.NoError(t, os.WriteFile(queueSpillPath(dir, "agt_1"), []byte("[]"), 0o600))

	n.persist(t.Context(), s)

	assert.NoDirExists(t, filepath.Join(dir, uploadsDir, "agt_1"))
}
