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
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.zip"), []byte("PK\x03\x04\xff\xfe"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o600))
	args := promptArgs{Attachments: []attachment{{Kind: "file", Path: "a.zip"}, {Kind: "file", Path: "a.txt"}}}

	blocks := promptBlocks(args, dir, acp.PromptCapabilities{EmbeddedContext: true})
	require.Len(t, blocks, 3)
	assert.Equal(t, acp.ContentBlock{Type: "resource_link", URI: "file://" + filepath.Join(dir, "a.zip"), Name: "a.zip"}, blocks[1])
	assert.Equal(t, "resource", blocks[2].Type)
}
