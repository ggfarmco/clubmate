package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/plugin"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

type pluginEnv struct {
	h    *testutil.PluginHarness
	ws   string
	repo string
	t    *testing.T
}

type readResult struct {
	Page    string   `json:"page"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	By      string   `json:"by"`
	Stale   bool     `json:"stale"`
	Path    string   `json:"path"`
	Sources []string `json:"sources"`
}

func newPluginEnv(t *testing.T) pluginEnv {
	t.Helper()
	t.Setenv("CLUBMATE_WIKI_DIR", tempDir(t))
	repo := newRepo(t)
	sessions, err := json.Marshal([]map[string]string{{"id": "agt_1", "title": "Fix SDUI", "worktree": repo}})
	require.NoError(t, err)
	agents := `{"id":"agents","name":"Agents","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"agents.sessions","title":"Sessions","hidden":true}]},"echoResults":{"agents.sessions":` + string(sessions) + `}}`
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/wiki", "plugins/wiki/plugin.json"), testutil.EchoPluginDirAs(t, agents))
	testutil.WaitEvents(t, h.Bus, "wiki.ready", func(events []bus.Event) bool { return len(events) > 0 })
	ws, err := h.Svc.CreateWorkspace(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	e := pluginEnv{h: h, ws: ws.ID, repo: repo, t: t}
	testutil.Eventually(t, func() bool { return e.run("agents.sessions", "", map[string]any{}, nil) == nil }, 10*time.Second)
	return e
}

func (e pluginEnv) run(id, caller string, args map[string]any, out any) error {
	return e.runIn(e.ws, id, caller, args, out)
}

func (e pluginEnv) runIn(ws, id, caller string, args map[string]any, out any) error {
	return e.h.Client(e.t).Call(e.t.Context(), "command.run", map[string]any{"id": id, "workspaceId": ws, "args": args, "agent": caller != "", "caller": caller}, out)
}

func TestAgentWriteThenRead(t *testing.T) {
	e := newPluginEnv(t)

	require.NoError(t, e.run("wiki.write", "agt_1", map[string]any{"page": "sdui/nodes.md", "content": "# Nodes\n\nA tree of nodes.\n", "summary": "explain", "sources": []string{"pkg/sdui/node.go"}}, nil))

	var got readResult
	require.NoError(t, e.run("wiki.read", "agt_1", map[string]any{"page": "sdui/nodes.md"}, &got))
	assert.Equal(t, "# Nodes\n\nA tree of nodes.\n", got.Content)
	assert.Equal(t, "Fix SDUI", got.By)
	assert.Equal(t, "Nodes", got.Title)
	assert.Equal(t, []string{"pkg/sdui/node.go"}, got.Sources)
	testutil.WaitEvents(t, e.h.Bus, "wiki.changed", func(events []bus.Event) bool {
		return len(events) > 0 && strings.Contains(string(events[0].Payload), `"page":"sdui/nodes.md"`)
	})
	var hits struct {
		Hits []map[string]string `json:"hits"`
	}
	require.NoError(t, e.run("wiki.search", "agt_1", map[string]any{"query": "tree"}, &hits))
	require.Len(t, hits.Hits, 1)
	assert.Equal(t, "sdui/nodes.md", hits.Hits[0]["page"])
}

func TestUserWriteIsYou(t *testing.T) {
	e := newPluginEnv(t)

	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "a.md", "content": "# A\n", "summary": "x"}, nil))

	var got readResult
	require.NoError(t, e.run("wiki.read", "", map[string]any{"page": "a.md"}, &got))
	assert.Equal(t, "you", got.By)
}

func TestHomeWorkspaceHasAWiki(t *testing.T) {
	e := newPluginEnv(t)
	home, err := e.h.Svc.HomeWorkspace(t.Context())
	require.NoError(t, err)

	require.NoError(t, e.runIn(home.ID, "wiki.write", "", map[string]any{"page": "plans.md", "content": "# Plans\n", "summary": "x"}, nil))

	var got struct {
		Pages []map[string]any `json:"pages"`
	}
	require.NoError(t, e.runIn(home.ID, "wiki.list", "", map[string]any{}, &got))
	require.Len(t, got.Pages, 1)
	assert.Equal(t, "plans.md", got.Pages[0]["page"])
}

func TestDeleteIsDestructive(t *testing.T) {
	data, err := os.ReadFile("plugin.json")
	require.NoError(t, err)
	var m plugin.Manifest
	require.NoError(t, json.Unmarshal(data, &m))

	seen := 0
	for _, c := range m.Contributes.Commands {
		if strings.HasPrefix(c.ID, "wiki.") {
			seen++
			assert.Equal(t, c.ID == "wiki.delete", c.Destructive, c.ID)
		}
	}
	assert.Equal(t, 7, seen)
}

func TestSectionShowsNewestPageAndDeletes(t *testing.T) {
	e := newPluginEnv(t)
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.ws}, nil))
	slotHas := func(slot, text string) {
		e.h.WaitSlot(t, e.ws, slot, func(n sdui.Node) bool {
			raw, err := json.Marshal(n)
			return err == nil && strings.Contains(string(raw), text)
		})
	}
	slotHas(mainSlot, "No wiki pages yet")

	require.NoError(t, e.run("wiki.write", "agt_1", map[string]any{"page": "sdui/nodes.md", "content": "# Nodes\n\nA tree of nodes.\n", "summary": "explain"}, nil))
	slotHas(sidebarSlot, `"id":"app/sdui/nodes.md"`)
	slotHas(mainSlot, "A tree of nodes.")

	e.h.Event(t, e.ws, mainSlot, deleteNode, "delete", nil)
	slotHas(mainSlot, "No wiki pages yet")
}

func TestUIEventGuards(t *testing.T) {
	e := newPluginEnv(t)
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "a.md", "content": "# A\n", "summary": "x"}, nil))
	event := func(node, action string, value any, agent bool) error {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		return e.h.Client(t).Call(t.Context(), "ui.event", map[string]any{"workspaceId": e.ws, "slot": mainSlot, "nodeId": node, "action": action, "value": json.RawMessage(raw), "agent": agent}, nil)
	}

	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.ws}, nil))
	e.h.WaitSlot(t, e.ws, sidebarSlot, func(n sdui.Node) bool {
		_, ok := findNode(n, newNode)
		return ok
	})

	for _, node := range []string{newNode, restoreNode} {
		err := event(node, node, nil, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not for agents")
	}
	raw, err := json.Marshal(nil)
	require.NoError(t, err)
	err = e.h.Client(t).Call(t.Context(), "ui.event", map[string]any{"workspaceId": e.ws, "slot": mainSlot, "nodeId": newNode, "action": "new", "value": json.RawMessage(raw), "caller": "agt_1"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not for agents")
	var list struct {
		Pages []map[string]any `json:"pages"`
	}
	require.NoError(t, e.run("wiki.list", "", map[string]any{}, &list))
	assert.Len(t, list.Pages, 1)

	err = event(docNode, "openFile", "/etc/hosts", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in workspace")
}

func TestLinkToPageSelectsIt(t *testing.T) {
	e := newPluginEnv(t)
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "b.md", "content": "# Bee\n", "summary": "x"}, nil))
	var out struct {
		Path string `json:"path"`
	}
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "a.md", "content": "# A\n\n[b](b.md)\n", "summary": "x"}, &out))
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.ws}, nil))
	e.h.WaitSlot(t, e.ws, mainSlot, func(n sdui.Node) bool {
		h, ok := findNode(n, headerNode)
		return ok && h.Props["title"] == "A"
	})

	e.h.Event(t, e.ws, mainSlot, docNode, "openDoc", filepath.Join(filepath.Dir(out.Path), "b.md")+"#top")

	e.h.WaitSlot(t, e.ws, mainSlot, func(n sdui.Node) bool {
		h, ok := findNode(n, headerNode)
		return ok && h.Props["title"] == "Bee"
	})
}

func TestMarginListsBacklinksAndOpensHistory(t *testing.T) {
	e := newPluginEnv(t)
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "guides/a.md", "content": "# Aye\n\nSee [b](../b.md#two).\n", "summary": "x"}, nil))
	var out struct {
		Path string `json:"path"`
	}
	for i := range 4 {
		require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "b.md", "content": "# Bee\n\n## One\n\n## Two\n\nv" + strconv.Itoa(i) + "\n", "summary": "v" + strconv.Itoa(i)}, &out))
	}
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.ws}, nil))
	e.h.WaitSlot(t, e.ws, mainSlot, func(n sdui.Node) bool {
		doc, ok := findNode(n, docNode)
		if !ok {
			return false
		}
		html, _ := doc.Props["html"].(string)
		return strings.Contains(html, `>Aye</span>`) && strings.Contains(html, "All 4 changes") && strings.Contains(html, `href="#two"`)
	})

	e.h.Event(t, e.ws, mainSlot, docNode, "openDoc", out.Path+"#"+historyAnchor)

	e.h.WaitSlot(t, e.ws, mainSlot, func(n sdui.Node) bool {
		_, ok := findNode(n, changesNode)
		return ok
	})
}

func (e pluginEnv) slotHas(slot, text string, want bool) {
	e.h.WaitSlot(e.t, e.ws, slot, func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		return err == nil && strings.Contains(string(raw), text) == want
	})
}

func TestNewToYouClearsWhenOpened(t *testing.T) {
	e := newPluginEnv(t)
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.ws}, nil))
	e.slotHas(mainSlot, "No wiki pages yet", true)
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "a.md", "content": "# A\n", "summary": "x"}, nil))
	e.h.Event(t, e.ws, sidebarSlot, pagesNode, "select", "app/a.md")

	require.NoError(t, e.run("wiki.write", "agt_1", map[string]any{"page": "b.md", "content": "# Bee\n", "summary": "x"}, nil))
	e.slotHas(sidebarSlot, `"id":"new:app/b.md"`, true)

	e.h.Event(t, e.ws, sidebarSlot, pagesNode, "select", "app/b.md")
	e.slotHas(sidebarSlot, `"id":"new:app/b.md"`, false)
}

func TestOpeningSearchHitMarksMatches(t *testing.T) {
	e := newPluginEnv(t)
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "a.md", "content": "# A\n\nThe socket listens.\n", "summary": "x"}, nil))
	require.NoError(t, e.run("wiki.write", "", map[string]any{"page": "b.md", "content": "# B\n", "summary": "x"}, nil))
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.ws}, nil))
	e.slotHas(mainSlot, `"title":"B"`, true)

	e.h.Event(t, e.ws, sidebarSlot, searchNode, "search", "socket")
	e.h.Event(t, e.ws, sidebarSlot, pagesNode, "select", "app/a.md")

	e.h.WaitSlot(t, e.ws, mainSlot, func(n sdui.Node) bool {
		doc, ok := findNode(n, docNode)
		return ok && doc.Props["anchor"] == matchAnchor+"1" && strings.Contains(doc.Props["html"].(string), `<mark id="wiki-match-1">socket</mark>`)
	})
}
