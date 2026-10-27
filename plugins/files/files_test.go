package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	sidebarSlot    = "editor-nvim.sidebar"
	editorManifest = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"sections":[{"id":"editor-nvim","title":"Code","icon":"chevron.left.forwardslash.chevron.right","order":10}]}}`
)

func items(t *testing.T, n sdui.Node) []sdui.TreeItem {
	t.Helper()
	raw, err := json.Marshal(n.Props["items"])
	require.NoError(t, err)
	var out []sdui.TreeItem
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func find(list []sdui.TreeItem, id string) (sdui.TreeItem, bool) {
	for _, it := range list {
		if it.ID == id {
			return it, true
		}
		if got, ok := find(it.Children, id); ok {
			return got, true
		}
	}
	return sdui.TreeItem{}, false
}

func TestBufferEventReveals(t *testing.T) {
	h := testutil.NewPluginHarness(t,
		testutil.BuildPluginDir(t, "clubmate/plugins/files", "plugins/files/plugin.json"),
		testutil.EchoPluginDirAs(t, editorManifest),
	)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	nested := filepath.Join(repo, "x", "y")
	require.NoError(t, os.MkdirAll(nested, 0o700))
	file := filepath.Join(nested, "z.go")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &sub))
	h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool { return n.Type == "tree" })

	payload, err := json.Marshal(map[string]string{"projectId": p.ID, "path": file})
	require.NoError(t, err)
	_, err = h.Bus.Publish(t.Context(), "editor.buffer", "editor-nvim", payload)
	require.NoError(t, err)

	tree := h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool { return n.Props["selected"] == file })
	for _, dir := range []string{filepath.Join(repo, "x"), nested} {
		it, ok := find(items(t, tree), dir)
		require.True(t, ok, dir)
		assert.True(t, it.Expanded, dir)
	}

	created := filepath.Join(nested, "new.go")
	require.NoError(t, os.WriteFile(created, nil, 0o600))
	h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool {
		_, ok := find(items(t, n), created)
		return ok
	})
}
