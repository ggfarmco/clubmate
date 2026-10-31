package rpc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func serveAt(t *testing.T, srv *rpc.Server, socket string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(socket), 0o700))
	l, err := net.Listen("unix", socket)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, l) }()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})
}

func startTestAppRoot(t *testing.T) (*rpc.Client, string) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "tap")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	svc, b := testutil.NewServiceWithBus(t)
	socket := filepath.Join(root, "real.sock")
	serveAt(t, rpc.NewServer(svc, b, rpc.WithTestAppRoot(root)), socket)
	c, err := rpc.Dial(t.Context(), socket)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	return c, root
}

func startTestInstance(t *testing.T, root, name string) string {
	t.Helper()
	srv, _ := testutil.NewRPCServer(t)
	sup := plugin.NewSupervisor(srv, testutil.ShortDirs(t), slog.New(slog.DiscardHandler))
	srv.SetUI(internalsdui.NewStore(), internalsdui.NewSections(nil), plugin.NewRouter(sup))
	socket := filepath.Join(root, "clubmate-test-"+name, "run", "core.sock")
	serveAt(t, srv, socket)
	return socket
}

func attachTestApp(t *testing.T, socket string) chan map[string]any {
	t.Helper()
	nc, err := net.Dial("unix", socket)
	require.NoError(t, err)
	seen := make(chan map[string]any, 1)
	handler := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		var p map[string]any
		if err := json.Unmarshal(*req.Params, &p); err != nil {
			return nil, err
		}
		seen <- p
		return map[string]any{"windows": []any{}, "summary": "0 problems"}, nil
	})
	conn := jsonrpc2.NewConn(context.Background(), jsonrpc2.NewPlainObjectStream(nc), jsonrpc2.AsyncHandler(handler))
	t.Cleanup(func() { conn.Close() })
	require.NoError(t, conn.Call(t.Context(), "client.register", map[string]any{"handles": []string{"ui.snapshot"}}, nil))
	return seen
}

func TestTestAppSnapshotDefaultsOutIntoInstance(t *testing.T) {
	c, root := startTestAppRoot(t)
	seen := attachTestApp(t, startTestInstance(t, root, "demo"))
	require.NoError(t, c.Call(t.Context(), "testapp.call", map[string]any{"instance": "demo", "method": "ui.snapshot"}, nil))
	assert.Equal(t, filepath.Join(root, "clubmate-test-demo", "snapshots"), (<-seen)["out"])
}

func TestTestAppCommandRunIsAgentCall(t *testing.T) {
	c, root := startTestAppRoot(t)
	startTestInstance(t, root, "demo")
	err := c.Call(t.Context(), "testapp.call", map[string]any{"instance": "demo", "method": "command.run", "params": map[string]any{
		"id": "workspace.worktree.set", "args": map[string]any{"id": "prj_x", "repo": "/a", "path": ""},
	}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestTestAppCallProxies(t *testing.T) {
	c, root := startTestAppRoot(t)
	startTestInstance(t, root, "demo")
	var created project.Project
	require.NoError(t, c.Call(t.Context(), "testapp.call", map[string]any{
		"instance": "demo", "method": "workspace.open", "params": map[string]any{"folders": []string{t.TempDir()}},
	}, &created))
	var list []project.Project
	require.NoError(t, c.Call(t.Context(), "testapp.call", map[string]any{"instance": "demo", "method": "project.list"}, &list))
	require.Len(t, list, 1)
	var own []project.Project
	require.NoError(t, c.Call(t.Context(), "project.list", struct{}{}, &own))
	assert.Empty(t, own)
}

func TestTestAppCallRejects(t *testing.T) {
	c, root := startTestAppRoot(t)
	startTestInstance(t, root, "demo")
	for _, tc := range []struct{ instance, method, want string }{
		{"demo", "fs.read", `method "fs.read" is not allowed`},
		{"../x", "project.list", `invalid instance name "../x"`},
		{"gone", "project.list", "instance gone is not running"},
		{"demo", "ui.snapshot", "out must be inside"},
	} {
		err := c.Call(t.Context(), "testapp.call", map[string]any{"instance": tc.instance, "method": tc.method, "params": map[string]any{"out": "/etc/x"}}, nil)
		require.Error(t, err, tc.instance)
		assert.Contains(t, err.Error(), tc.want)
	}
}

func TestTestAppRefusesSharedDir(t *testing.T) {
	c, root := startTestAppRoot(t)
	startTestInstance(t, root, "demo")
	require.NoError(t, os.Chmod(filepath.Join(root, "clubmate-test-demo"), 0o777))
	err := c.Call(t.Context(), "testapp.call", map[string]any{"instance": "demo", "method": "project.list"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instance demo is not running")
}

func TestTestAppList(t *testing.T) {
	c, root := startTestAppRoot(t)
	startTestInstance(t, root, "live")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "clubmate-test-stale", "run"), 0o700))
	var list []struct {
		Name    string `json:"name"`
		Runtime string `json:"runtime"`
	}
	require.NoError(t, c.Call(t.Context(), "testapp.list", struct{}{}, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "live", list[0].Name)
	assert.Equal(t, filepath.Join(root, "clubmate-test-live", "run"), list[0].Runtime)
}

func TestAgentUIEventRefusesConfirmedAction(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t, testutil.EchoPluginDirAs(t, manifestJSON("alpha", `{"sections":[{"id":"alpha","title":"Alpha"}]}`)))
	testutil.Eventually(t, func() bool {
		st := h.Sup.Statuses()
		return len(st) == 1 && st[0].State == plugin.StateRunning
	}, 5*time.Second)
	p := h.CreateProject(t, "demo")
	h.WaitSlot(t, p.ID, "alpha.main", func(n sdui.Node) bool { return n.Type == "text" })
	tree := map[string]any{"type": "stack", "props": map[string]any{"direction": "vertical"}, "children": []any{
		map[string]any{"type": "list", "id": "files", "props": map[string]any{"items": []any{
			map[string]any{"id": "a.go", "label": "a.go", "menu": []any{map[string]any{"action": "discard", "label": "Discard", "confirm": "Discard a.go?"}}},
		}}},
		map[string]any{"type": "button", "id": "kill", "props": map[string]any{"label": "Kill", "confirm": "Kill it?"}},
		map[string]any{"type": "form", "id": "edit", "props": map[string]any{"action": "save"}, "children": []any{
			map[string]any{"type": "button", "id": "go", "props": map[string]any{"label": "Save", "confirm": "Overwrite?"}},
		}},
	}}
	require.NoError(t, h.PluginCall(t, "alpha", "ui.render", map[string]any{"projectId": p.ID, "slot": "alpha.main", "tree": tree}))
	for _, tc := range []struct{ node, action string }{{"files", "discard"}, {"ghost", "discard"}, {"kill", "kill"}, {"edit", "save"}} {
		err := h.Client(t).Call(t.Context(), "ui.event", map[string]any{"projectId": p.ID, "slot": "alpha.main", "nodeId": tc.node, "action": tc.action, "value": "a.go", "agent": true}, nil)
		require.Error(t, err, tc.node)
		assert.Contains(t, err.Error(), fmt.Sprintf("action %q needs the user's confirmation", tc.action))
	}
}
