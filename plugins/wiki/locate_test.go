package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/sdui"
)

func TestLocateFindsPageWithAnchor(t *testing.T) {
	e := newPluginEnv(t)
	var written struct {
		Path string `json:"path"`
	}
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "guides/a.md", "content": "# Aye\n\n## Two\n", "summary": "x"}, &written))

	var got map[string]string
	require.NoError(t, e.run("wiki.locate", "", map[string]any{"path": written.Path + "#two"}, &got))

	assert.Equal(t, map[string]string{"id": "app/guides/a.md", "path": written.Path, "title": "Aye", "anchor": "two"}, got)
}

func TestLocateOutsideWikiIsEmpty(t *testing.T) {
	e := newPluginEnv(t)
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "a.md", "content": "# A\n", "summary": "x"}, nil))

	var inRepo, missingPage map[string]string
	require.NoError(t, e.run("wiki.locate", "", map[string]any{"path": filepath.Join(e.repo, "a.go")}, &inRepo))
	var written struct {
		Path string `json:"path"`
	}
	require.NoError(t, e.run("wiki.read", "", map[string]any{"page": "a.md"}, &written))
	require.NoError(t, e.run("wiki.locate", "", map[string]any{"path": filepath.Join(filepath.Dir(written.Path), "gone.md")}, &missingPage))

	assert.Empty(t, inRepo)
	assert.Empty(t, missingPage)
}

func TestLocateOutsideScopeFails(t *testing.T) {
	e := newPluginEnv(t)

	for _, path := range []string{"/etc/hosts", "a.md"} {
		err := e.run("wiki.locate", "", map[string]any{"path": path}, nil)

		require.Error(t, err, path)
		assert.Contains(t, err.Error(), "not in workspace", path)
	}
}

func TestLocateLeavesSelectionAlone(t *testing.T) {
	e := newPluginEnv(t)
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "b.md", "content": "# Bee\n", "summary": "x"}, nil))
	var a struct {
		Path string `json:"path"`
	}
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "a.md", "content": "# Aye\n", "summary": "x"}, &a))
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.ws}, nil))
	title := func() string {
		var got string
		e.h.WaitSlot(t, e.ws, mainSlot, func(n sdui.Node) bool {
			h, ok := findNode(n, headerNode)
			got, _ = h.Props["title"].(string)
			return ok
		})
		return got
	}
	require.Equal(t, "Aye", title())

	require.NoError(t, e.run("wiki.locate", "", map[string]any{"path": filepath.Join(filepath.Dir(a.Path), "b.md")}, nil))
	e.h.Event(t, e.ws, sidebarSlot, searchNode, "search", "")

	assert.Equal(t, "Aye", title())
}

func TestLocateFlagsHistoryAnchor(t *testing.T) {
	e := newPluginEnv(t)
	var written struct {
		Path string `json:"path"`
	}
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "a.md", "content": "# Aye\n", "summary": "x"}, &written))

	var got map[string]any
	require.NoError(t, e.run("wiki.locate", "", map[string]any{"path": written.Path + "#" + historyAnchor}, &got))

	assert.Equal(t, true, got["history"])
}

func TestLocateHomeIsRootIndexElseFirstTreePage(t *testing.T) {
	e := newPluginEnv(t)
	var empty, first, index map[string]string
	require.NoError(t, e.run("wiki.locate", "", map[string]any{"home": true}, &empty))
	for _, page := range []string{"guides/a.md", "b.md", "c.md"} {
		require.NoError(t, e.run("wiki.write", "", map[string]any{"page": page, "content": "# " + page + "\n", "summary": "x"}, nil))
	}
	require.NoError(t, e.run("wiki.locate", "", map[string]any{"home": true}, &first))
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "README.md", "content": "# Home\n", "summary": "x"}, nil))
	require.NoError(t, e.run("wiki.locate", "", map[string]any{"home": true}, &index))

	assert.Empty(t, empty)
	assert.Equal(t, "app/b.md", first["id"])
	assert.Equal(t, map[string]string{"id": "app/README.md", "path": filepath.Join(filepath.Dir(first["path"]), "README.md"), "title": "Home"}, index)
}
