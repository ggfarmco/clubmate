package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

type published struct {
	Topic   string
	Payload map[string]any
}

type fakeCore struct {
	mu        sync.Mutex
	ws        contract.Workspace
	sessions  []map[string]string
	renders   []sdui.Node
	focus     []string
	published []published
	unwatched int
}

type fakeClock struct {
	mu      sync.Mutex
	pending []func()
}

type env struct {
	t    *testing.T
	d    *docs
	core *fakeCore
	clk  *fakeClock
	h    *pluginsdk.Host
	dir  string
}

func TestOpenBackClose(t *testing.T) {
	e := newEnv(t)
	a, b := e.doc("a.md", "# Alpha"), e.doc("b.md", "# Beta")

	res, err := e.run(pluginsdk.CommandRun{ID: "docs.open", Args: e.args(map[string]any{"path": "a.md"})})
	require.NoError(t, err)
	assert.Equal(t, openResult{Path: a}, res)
	e.assertDoc(a, 1, false)
	assert.Equal(t, []string{popupSlot}, e.core.focus)

	e.event("openDoc", b+"#intro")
	e.assertDoc(b, 1, true)
	assert.Equal(t, "intro", e.anchor())

	e.event("back", nil)
	e.assertDoc(a, 1, false)
	assert.Nil(t, e.anchor())

	e.event("close", nil)
	assert.Equal(t, "empty", e.core.last().Type)
	assert.Equal(t, 1, e.core.unwatched)
}

func TestChangeRerenders(t *testing.T) {
	e := newEnv(t)
	a := e.doc("a.md", "# One")
	e.open(a)

	write(t, a, "# Two")
	e.changed(a)
	e.clk.fire()

	e.assertDoc(a, 2, false)
	assert.Equal(t, published{Topic: "docs.changed", Payload: map[string]any{"workspace": "ws", "path": a, "rev": float64(2)}}, e.core.lastPublished())
}

func TestChangeBurstRendersOnce(t *testing.T) {
	e := newEnv(t)
	a := e.doc("a.md", "# One")
	e.open(a)
	before := e.core.renderCount()

	for range 20 {
		e.changed(a)
	}
	assert.Equal(t, before, e.core.renderCount())
	e.clk.fire()

	assert.Equal(t, before+1, e.core.renderCount())
	assert.Empty(t, e.clk.pending)
}

func TestDeletedFileShowsGone(t *testing.T) {
	e := newEnv(t)
	a := e.doc("a.md", "# One")
	e.open(a)

	require.NoError(t, os.Remove(a))
	e.changed(a)
	e.clk.fire()

	empty := find(e.core.last(), func(n sdui.Node) bool { return n.Type == "empty" })
	require.NotNil(t, empty)
	assert.Equal(t, "File no longer exists", empty.Props["text"])
	assert.Equal(t, a, empty.Props["hint"])
	assert.Nil(t, find(e.core.last(), func(n sdui.Node) bool { return n.Type == "document" }))
}

func TestReadIsWatched(t *testing.T) {
	e := newEnv(t)
	a := e.doc("a.md", "# Alpha\n\ntext")

	res, err := e.run(pluginsdk.CommandRun{ID: "docs.read", Args: e.args(map[string]any{"path": a})})
	require.NoError(t, err)
	got, ok := res.(readResult)
	require.True(t, ok)
	assert.Equal(t, a, got.Path)
	assert.Equal(t, "Alpha", got.Title)
	assert.Contains(t, got.HTML, "<p>text</p>")
	assert.Equal(t, 1, got.Rev)

	write(t, a, "# Beta")
	e.changed(a)
	e.clk.fire()

	assert.Equal(t, published{Topic: "docs.changed", Payload: map[string]any{"workspace": "ws", "path": a, "rev": float64(2)}}, e.core.lastPublished())
	assert.Zero(t, e.core.renderCount())
}

func TestAgentOpenPublishesOpened(t *testing.T) {
	e := newEnv(t)
	tree := filepath.Join(e.dir, ".worktrees", "feat")
	e.core.sessions = []map[string]string{{"id": "agt_a", "worktree": tree}}
	spec := e.doc(filepath.Join(".worktrees", "feat", "spec.md"), "# Spec")

	_, err := e.run(pluginsdk.CommandRun{ID: "docs.open", Agent: true, Caller: "agt_a", Args: e.args(map[string]any{"path": "spec.md#intro"})})
	require.NoError(t, err)

	e.assertDoc(spec, 1, false)
	assert.Equal(t, "intro", e.anchor())
	assert.Equal(t, published{Topic: "docs.opened", Payload: map[string]any{"workspace": "ws", "session": "agt_a", "path": spec}}, e.core.lastPublished())
}

func TestArtifactOpens(t *testing.T) {
	e := newEnv(t)
	src := "<!doctype html><title>Q3</title><script>document.title = 'x'</script>"
	r := e.doc("report.html", src)

	e.open(r)

	e.assertDoc(r, 1, false)
	doc := find(e.core.last(), func(n sdui.Node) bool { return n.Type == "document" })
	assert.Equal(t, "artifact", doc.Props["kind"])
	assert.Equal(t, src, doc.Props["html"])
}

func TestArtifactTitle(t *testing.T) {
	e := newEnv(t)
	r := e.doc("report.htm", "<html><head><TITLE>\n  Q3 &amp; Q4\n</TITLE></head></html>")

	res, err := e.run(pluginsdk.CommandRun{ID: "docs.read", Args: e.args(map[string]any{"path": r})})
	require.NoError(t, err)
	got, ok := res.(readResult)
	require.True(t, ok)
	assert.Equal(t, "Q3 & Q4", got.Title)
	assert.Equal(t, "artifact", got.Kind)
}

func TestArtifactLinkFromDoc(t *testing.T) {
	e := newEnv(t)
	a := e.doc("a.md", "[r](out/report.html)")
	r := e.doc(filepath.Join("out", "report.html"), "<p>r</p>")
	e.open(a)
	doc := find(e.core.last(), func(n sdui.Node) bool { return n.Type == "document" })
	assert.Contains(t, doc.Props["html"], `href="clubmate-doc:`+r+`"`)
	assert.Nil(t, doc.Props["kind"])

	e.event("openDoc", r)
	e.assertDoc(r, 1, true)

	e.event("back", nil)
	e.assertDoc(a, 1, false)
}

func newEnv(t *testing.T) *env {
	dir := realDir(t)
	core := &fakeCore{ws: contract.Workspace{ID: "ws", Folders: []contract.Folder{{Path: dir}}}}
	clk := &fakeClock{}
	d := newDocs(clk, "")
	h := core.connect(t)
	d.h = h
	return &env{t: t, d: d, core: core, clk: clk, h: h, dir: dir}
}

func (e *env) doc(rel, content string) string {
	path := filepath.Join(e.dir, rel)
	write(e.t, path, content)
	return path
}

func (e *env) args(v map[string]any) json.RawMessage {
	raw, err := json.Marshal(v)
	require.NoError(e.t, err)
	return raw
}

func (e *env) run(cmd pluginsdk.CommandRun) (any, error) {
	cmd.WorkspaceID = "ws"
	return e.d.command(e.t.Context(), e.h, cmd)
}

func (e *env) open(path string) {
	_, err := e.run(pluginsdk.CommandRun{ID: "docs.open", Args: e.args(map[string]any{"path": path})})
	require.NoError(e.t, err)
}

func (e *env) event(action string, value any) {
	raw, err := json.Marshal(value)
	require.NoError(e.t, err)
	_, err = e.d.uiEvent(e.t.Context(), e.h, pluginsdk.UIEvent{WorkspaceID: "ws", Slot: popupSlot, Action: action, Value: raw})
	require.NoError(e.t, err)
}

func (e *env) changed(paths ...string) {
	e.d.fsChanged(e.t.Context(), e.h, pluginsdk.FSChange{Watch: "w1", WorkspaceID: "ws", Paths: paths})
}

func (e *env) assertDoc(path string, rev int, back bool) {
	e.t.Helper()
	last := e.core.last()
	doc := find(last, func(n sdui.Node) bool { return n.Type == "document" })
	require.NotNil(e.t, doc)
	assert.Equal(e.t, "doc", doc.ID)
	assert.Equal(e.t, path, doc.Props["path"])
	assert.Equal(e.t, float64(rev), doc.Props["rev"])
	assert.Equal(e.t, "fill", doc.Props["size"])
	assert.Equal(e.t, back, find(last, func(n sdui.Node) bool { return n.ID == "back" }) != nil)
	header := find(last, func(n sdui.Node) bool { return n.Type == "header" })
	require.NotNil(e.t, header)
	assert.Equal(e.t, filepath.Base(path), header.Props["title"])
}

func (e *env) anchor() any {
	doc := find(e.core.last(), func(n sdui.Node) bool { return n.Type == "document" })
	require.NotNil(e.t, doc)
	return doc.Props["anchor"]
}

func (c *fakeCore) connect(t *testing.T) *pluginsdk.Host {
	t.Helper()
	coreSide, pluginSide := net.Pipe()
	handler := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		return c.handle(req)
	})
	conn := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), handler)
	t.Cleanup(func() { conn.Close() })
	ready := make(chan *pluginsdk.Host, 1)
	init := func(_ context.Context, h *pluginsdk.Host) error {
		ready <- h
		return nil
	}
	go func() { _ = pluginsdk.RunConn(t.Context(), pluginSide, pluginsdk.Plugin{Init: init}) }()
	require.NoError(t, conn.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": pluginsdk.ProtocolVersion, "pluginId": "docs"}, nil))
	select {
	case h := <-ready:
		return h
	case <-time.After(3 * time.Second):
		t.Fatal("Init not called")
		return nil
	}
}

func (c *fakeCore) handle(req *jsonrpc2.Request) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var params map[string]json.RawMessage
	if req.Params != nil {
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}
	}
	switch req.Method {
	case "workspace.get":
		return c.ws, nil
	case "ui.render":
		var slot string
		var tree sdui.Node
		if err := json.Unmarshal(params["slot"], &slot); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(params["tree"], &tree); err != nil {
			return nil, err
		}
		if slot == popupSlot {
			c.renders = append(c.renders, tree)
		}
		return map[string]int{"rev": len(c.renders)}, nil
	case "ui.focus":
		var slot string
		if err := json.Unmarshal(params["slot"], &slot); err != nil {
			return nil, err
		}
		c.focus = append(c.focus, slot)
		return map[string]int{"delivered": 1}, nil
	case "bus.publish":
		var p published
		if err := json.Unmarshal(params["topic"], &p.Topic); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(params["payload"], &p.Payload); err != nil {
			return nil, err
		}
		c.published = append(c.published, p)
		return pluginsdk.Event{}, nil
	case "fs.watch":
		return map[string]string{"watch": "w1"}, nil
	case "fs.unwatch":
		c.unwatched++
		return nil, nil
	case "command.run":
		return c.sessions, nil
	}
	return nil, nil
}

func (c *fakeCore) last() sdui.Node {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.renders) == 0 {
		return sdui.Node{}
	}
	return c.renders[len(c.renders)-1]
}

func (c *fakeCore) renderCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.renders)
}

func (c *fakeCore) lastPublished() published {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.published) == 0 {
		return published{}
	}
	return c.published[len(c.published)-1]
}

func (c *fakeClock) AfterFunc(_ time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = append(c.pending, f)
	return func() bool { return true }
}

func (c *fakeClock) fire() {
	c.mu.Lock()
	fns := c.pending
	c.pending = nil
	c.mu.Unlock()
	for _, f := range fns {
		f()
	}
}

func find(n sdui.Node, match func(sdui.Node) bool) *sdui.Node {
	if match(n) {
		return &n
	}
	for _, c := range n.Children {
		if found := find(c, match); found != nil {
			return found
		}
	}
	return nil
}
