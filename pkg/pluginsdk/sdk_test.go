package pluginsdk_test

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/kv"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

type initResult struct {
	Protocol int `json:"protocol"`
}

func TestSDKInitCanCallCoreBeforeAndAfterReplying(t *testing.T) {
	srv, _ := testutil.NewRPCServer(t)
	coreSide, pluginSide := net.Pipe()
	conn := srv.ServeConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), rpc.Caller{PluginID: "echo"})
	inited := make(chan []pluginsdk.Workspace, 1)
	go func() {
		_ = pluginsdk.RunConn(t.Context(), pluginSide, pluginsdk.Plugin{
			Init: func(ctx context.Context, h *pluginsdk.Host) error {
				ps, err := h.ListWorkspaces(ctx)
				inited <- ps
				return err
			},
		})
	}()
	t.Cleanup(func() { conn.Close() })
	var res initResult
	require.NoError(t, conn.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": 1, "pluginId": "echo"}, &res))
	assert.Equal(t, 1, res.Protocol)
	select {
	case ps := <-inited:
		assert.Empty(t, ps)
	case <-time.After(3 * time.Second):
		t.Fatal("Init did not reach the core")
	}
}

func TestSDKRejectsProtocolMismatch(t *testing.T) {
	conn := startPlugin(t, pluginsdk.Plugin{}, nil)
	var res initResult
	err := conn.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": 2, "pluginId": "echo"}, &res)
	var rpcErr *jsonrpc2.Error
	require.ErrorAs(t, err, &rpcErr)
}

func TestSDKUnsetHandlersAreMethodNotFound(t *testing.T) {
	conn := startPlugin(t, pluginsdk.Plugin{}, nil)
	for _, method := range []string{"ui.event", "command.run", "nope"} {
		var rpcErr *jsonrpc2.Error
		require.ErrorAs(t, conn.Call(t.Context(), method, map[string]any{}, nil), &rpcErr, method)
		assert.EqualValues(t, -32601, rpcErr.Code, method)
	}
}

func TestSDKDispatchesRequests(t *testing.T) {
	conn := startPlugin(t, pluginsdk.Plugin{
		UIEvent: func(_ context.Context, _ *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
			return map[string]string{"action": ev.Action, "value": string(ev.Value)}, nil
		},
	}, nil)
	var pong string
	require.NoError(t, conn.Call(t.Context(), "plugin.ping", nil, &pong))
	assert.Equal(t, "pong", pong)
	var got map[string]string
	require.NoError(t, conn.Call(t.Context(), "ui.event", map[string]any{"action": "click", "value": 7}, &got))
	assert.Equal(t, map[string]string{"action": "click", "value": "7"}, got)
}

func TestSDKDeliversBusEventWithSubscription(t *testing.T) {
	got := make(chan string, 1)
	conn := startPlugin(t, pluginsdk.Plugin{
		BusEvent: func(_ context.Context, _ *pluginsdk.Host, sub string, ev pluginsdk.Event) {
			got <- sub + ":" + ev.Topic
		},
	}, nil)
	require.NoError(t, conn.Notify(t.Context(), "bus.event", map[string]any{"subscription": "sub_x", "event": map[string]any{"topic": "a.b"}}))
	select {
	case v := <-got:
		assert.Equal(t, "sub_x:a.b", v)
	case <-time.After(3 * time.Second):
		t.Fatal("no bus event")
	}
}

func TestSDKDeliversUIUpdated(t *testing.T) {
	got := make(chan pluginsdk.UIUpdate, 1)
	conn := startPlugin(t, pluginsdk.Plugin{
		UIUpdated: func(_ context.Context, _ *pluginsdk.Host, ev pluginsdk.UIUpdate) {
			got <- ev
		},
	}, nil)
	view := map[string]any{"workspaceId": "prj_1", "slot": "cron.main", "owner": "cron", "rev": 3, "tree": map[string]any{"type": "empty"}}
	require.NoError(t, conn.Notify(t.Context(), "ui.updated", map[string]any{"subscription": "sub_x", "view": view}))
	select {
	case ev := <-got:
		assert.Equal(t, "sub_x", ev.Subscription)
		assert.Equal(t, pluginsdk.SlotView{WorkspaceID: "prj_1", Slot: "cron.main", Owner: "cron", Rev: 3, Tree: json.RawMessage(`{"type":"empty"}`)}, ev.View)
	case <-time.After(3 * time.Second):
		t.Fatal("no ui.updated")
	}
}

func TestSDKDeliversFSChanged(t *testing.T) {
	got := make(chan pluginsdk.FSChange, 1)
	conn := startPlugin(t, pluginsdk.Plugin{
		FSChanged: func(_ context.Context, _ *pluginsdk.Host, ev pluginsdk.FSChange) { got <- ev },
	}, nil)
	require.NoError(t, conn.Notify(t.Context(), "fs.changed", map[string]any{"watch": "sub_w", "workspaceId": "prj_x", "paths": []string{"/r/a"}, "overflow": true}))
	select {
	case ev := <-got:
		assert.Equal(t, pluginsdk.FSChange{Watch: "sub_w", WorkspaceID: "prj_x", Paths: []string{"/r/a"}, Overflow: true}, ev)
	case <-time.After(3 * time.Second):
		t.Fatal("no fs change")
	}
}

func TestSDKShutdownEndsRun(t *testing.T) {
	shut := make(chan struct{}, 1)
	done := make(chan error, 1)
	conn := startPlugin(t, pluginsdk.Plugin{
		Shutdown: func(context.Context, *pluginsdk.Host) { shut <- struct{}{} },
	}, done)
	var res json.RawMessage
	require.NoError(t, conn.Call(t.Context(), "plugin.shutdown", nil, &res))
	assert.JSONEq(t, `{}`, string(res))
	select {
	case <-shut:
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown not called")
	}
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestSDKHostStorageAndSessions(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	store, err := kv.Open(filepath.Join(t.TempDir(), "plugins.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	srv := rpc.NewServer(svc, b, rpc.WithKV(store))
	coreSide, pluginSide := net.Pipe()
	conn := srv.ServeConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), rpc.Caller{PluginID: "echo"})
	t.Cleanup(func() { conn.Close() })
	ready := make(chan *pluginsdk.Host, 1)
	go func() {
		_ = pluginsdk.RunConn(t.Context(), pluginSide, pluginsdk.Plugin{
			Init: func(_ context.Context, h *pluginsdk.Host) error {
				ready <- h
				return nil
			},
		})
	}()
	require.NoError(t, conn.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": 1, "pluginId": "echo"}, nil))
	var h *pluginsdk.Host
	select {
	case h = <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("Init not called")
	}
	ctx := t.Context()
	assert.Equal(t, "echo", h.PluginID())

	require.NoError(t, h.StorageSet(ctx, "k", map[string]int{"n": 1}))
	var v map[string]int
	found, err := h.StorageGet(ctx, "k", &v)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 1, v["n"])
	keys, err := h.StorageList(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"k"}, keys)
	require.NoError(t, h.StorageDelete(ctx, "k"))
	found, err = h.StorageGet(ctx, "k", &v)
	require.NoError(t, err)
	assert.False(t, found)

	sub, err := h.Subscribe(ctx, "*", 0)
	require.NoError(t, err)
	assert.NotEmpty(t, sub)
	ev, err := h.Publish(ctx, "echo.hi", map[string]string{"a": "b"})
	require.NoError(t, err)
	assert.Equal(t, "plugin:echo", ev.Source)

	sessions, err := h.ListSessions(ctx, "", true)
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

func startPlugin(t *testing.T, p pluginsdk.Plugin, done chan<- error) *jsonrpc2.Conn {
	t.Helper()
	coreSide, pluginSide := net.Pipe()
	conn := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), noopHandler{})
	t.Cleanup(func() { conn.Close() })
	go func() {
		err := pluginsdk.RunConn(t.Context(), pluginSide, p)
		if done != nil {
			done <- err
		}
	}()
	return conn
}

type noopHandler struct{}

func (noopHandler) Handle(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) {}

func TestSDKRejectsSecondInitialize(t *testing.T) {
	conn := startPlugin(t, pluginsdk.Plugin{}, nil)
	params := map[string]any{"protocol": 1, "pluginId": "echo"}
	require.NoError(t, conn.Call(t.Context(), "plugin.initialize", params, nil))
	var rpcErr *jsonrpc2.Error
	require.ErrorAs(t, conn.Call(t.Context(), "plugin.initialize", params, nil), &rpcErr)
}

func TestSDKDeliversBusClosed(t *testing.T) {
	got := make(chan string, 1)
	conn := startPlugin(t, pluginsdk.Plugin{
		BusClosed: func(_ context.Context, _ *pluginsdk.Host, sub string, err error) {
			got <- sub + ":" + err.Error()
		},
	}, nil)
	require.NoError(t, conn.Notify(t.Context(), "bus.closed", map[string]any{"subscription": "sub_x", "error": "slow"}))
	select {
	case v := <-got:
		assert.Equal(t, "sub_x:slow", v)
	case <-time.After(3 * time.Second):
		t.Fatal("no bus.closed")
	}
}

func TestSDKBusEventsAreOrderedAndMayCallCore(t *testing.T) {
	const n = 50
	seqs := make(chan int64, n)
	h := startWithCore(t, pluginsdk.Plugin{
		BusEvent: func(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
			_, _ = h.ListWorkspaces(ctx)
			seqs <- ev.Seq
		},
	})
	_, err := h.Subscribe(t.Context(), "*", 0)
	require.NoError(t, err)
	for range n {
		_, err := h.Publish(t.Context(), "t.x", nil)
		require.NoError(t, err)
	}
	var last int64
	for range n {
		select {
		case seq := <-seqs:
			assert.Greater(t, seq, last)
			last = seq
		case <-time.After(5 * time.Second):
			t.Fatal("bus events stalled")
		}
	}
}

func startWithCore(t *testing.T, p pluginsdk.Plugin) *pluginsdk.Host {
	t.Helper()
	srv, _ := testutil.NewRPCServer(t)
	coreSide, pluginSide := net.Pipe()
	conn := srv.ServeConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), rpc.Caller{PluginID: "echo"})
	t.Cleanup(func() { conn.Close() })
	ready := make(chan *pluginsdk.Host, 1)
	p.Init = func(_ context.Context, h *pluginsdk.Host) error {
		ready <- h
		return nil
	}
	go func() { _ = pluginsdk.RunConn(t.Context(), pluginSide, p) }()
	require.NoError(t, conn.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": 1, "pluginId": "echo"}, nil))
	select {
	case h := <-ready:
		return h
	case <-time.After(3 * time.Second):
		t.Fatal("Init not called")
		return nil
	}
}

func TestWatchedSectionsKeepsLatestPerWorkspace(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	srv := rpc.NewServer(svc, b)
	coreSide, pluginSide := net.Pipe()
	conn := srv.ServeConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), rpc.Caller{PluginID: "remote"})
	t.Cleanup(func() { conn.Close() })
	ready := make(chan *pluginsdk.Host, 1)
	go func() {
		_ = pluginsdk.RunConn(t.Context(), pluginSide, pluginsdk.Plugin{
			Init: func(_ context.Context, h *pluginsdk.Host) error {
				ready <- h
				return nil
			},
		})
	}()
	require.NoError(t, conn.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": 1, "pluginId": "remote"}, nil))
	h := <-ready
	ctx := t.Context()
	var until int64
	for _, w := range []contract.RemoteWatching{
		{Workspace: "a", Sections: []string{"processes"}},
		{Workspace: "b", Sections: []string{"agents"}},
		{Workspace: "a", Sections: []string{}},
		{Workspace: "b", Sections: []string{"agents", "processes"}},
		{Workspace: "c", Sections: []string{"cron"}},
	} {
		ev, err := h.Publish(ctx, contract.TopicRemoteWatching, w)
		require.NoError(t, err)
		if w.Workspace != "c" {
			until = ev.Seq
		}
	}

	got, err := h.WatchedSections(ctx, until)

	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"b": {"agents", "processes"}}, got)
}

func TestSDKRenderSendsDeltaAndFallsBackToFullTree(t *testing.T) {
	var renders []map[string]json.RawMessage
	reject := false
	h := renderHost(t, func(p map[string]json.RawMessage) (any, error) {
		renders = append(renders, p)
		if reject && p["delta"] != nil {
			return nil, &jsonrpc2.Error{Code: -32016, Message: "stale base"}
		}
		return map[string]int{"rev": len(renders)}, nil
	})
	tree := func(text string) json.RawMessage {
		return json.RawMessage(`{"type":"stack","props":{"direction":"vertical"},"children":[{"type":"text","props":{"text":"a line that stays the same"}},{"type":"text","props":{"text":"` + text + `"}}]}`)
	}
	ctx := t.Context()
	_, err := h.RenderJSON(ctx, "prj_1", "a.main", tree("a first line that keeps growing"))
	require.NoError(t, err)
	rev, err := h.RenderJSON(ctx, "prj_1", "a.main", tree("a first line that keeps growing, then more"))
	require.NoError(t, err)
	assert.Equal(t, int64(2), rev)
	assert.Nil(t, renders[1]["tree"])
	assert.JSONEq(t, `1`, string(renders[1]["base"]))
	assert.JSONEq(t, `{"type":"stack","props":{"direction":"vertical"},"children":[{"ref":[0]},{"type":"text","props":{"text":{"append":", then more"}}}]}`, string(renders[1]["delta"]))
	reject = true
	rev, err = h.RenderJSON(ctx, "prj_1", "a.main", tree("a first line that keeps growing, then more and more"))
	require.NoError(t, err)
	assert.Equal(t, int64(4), rev)
	assert.JSONEq(t, string(tree("a first line that keeps growing, then more and more")), string(renders[3]["tree"]))
}

func TestSDKRenderForgetsLeastRecentlyRenderedSlots(t *testing.T) {
	var renders []map[string]json.RawMessage
	h := renderHost(t, func(p map[string]json.RawMessage) (any, error) {
		renders = append(renders, p)
		return map[string]int{"rev": len(renders)}, nil
	})
	tree := json.RawMessage(`{"type":"text","props":{"text":"a line long enough to go as a ref"}}`)
	ctx := t.Context()
	_, err := h.RenderJSON(ctx, "prj_a", "a.main", tree)
	require.NoError(t, err)
	for i := range 64 {
		_, err := h.RenderJSON(ctx, "prj_"+strconv.Itoa(i), "a.main", tree)
		require.NoError(t, err)
	}
	_, err = h.RenderJSON(ctx, "prj_a", "a.main", tree)
	require.NoError(t, err)
	last := renders[len(renders)-1]
	assert.JSONEq(t, string(tree), string(last["tree"]))
	assert.NotContains(t, last, "base")
	assert.NotContains(t, last, "delta")
}

func renderHost(t *testing.T, render func(map[string]json.RawMessage) (any, error)) *pluginsdk.Host {
	t.Helper()
	core := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		if req.Method != "ui.render" {
			return nil, nil
		}
		var p map[string]json.RawMessage
		if err := json.Unmarshal(*req.Params, &p); err != nil {
			return nil, err
		}
		return render(p)
	})
	coreSide, pluginSide := net.Pipe()
	conn := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), jsonrpc2.AsyncHandler(core))
	t.Cleanup(func() { conn.Close() })
	ready := make(chan *pluginsdk.Host, 1)
	go func() {
		_ = pluginsdk.RunConn(t.Context(), pluginSide, pluginsdk.Plugin{Init: func(_ context.Context, h *pluginsdk.Host) error {
			ready <- h
			return nil
		}})
	}()
	require.NoError(t, conn.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": 1, "pluginId": "echo"}, nil))
	return <-ready
}
