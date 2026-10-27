package pluginsdk_test

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/kv"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
	"clubmate/pkg/pluginsdk"
)

type initResult struct {
	Protocol int `json:"protocol"`
}

func TestSDKInitCanCallCoreBeforeAndAfterReplying(t *testing.T) {
	srv, _ := testutil.NewRPCServer(t)
	coreSide, pluginSide := net.Pipe()
	conn := srv.ServeConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), rpc.Caller{PluginID: "echo"})
	inited := make(chan []pluginsdk.Project, 1)
	go func() {
		_ = pluginsdk.RunConn(t.Context(), pluginSide, pluginsdk.Plugin{
			Init: func(ctx context.Context, h *pluginsdk.Host) error {
				ps, err := h.ListProjects(ctx)
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

func TestSDKDeliversFSChanged(t *testing.T) {
	got := make(chan pluginsdk.FSChange, 1)
	conn := startPlugin(t, pluginsdk.Plugin{
		FSChanged: func(_ context.Context, _ *pluginsdk.Host, ev pluginsdk.FSChange) { got <- ev },
	}, nil)
	require.NoError(t, conn.Notify(t.Context(), "fs.changed", map[string]any{"watch": "sub_w", "projectId": "prj_x", "paths": []string{"/r/a"}, "overflow": true}))
	select {
	case ev := <-got:
		assert.Equal(t, pluginsdk.FSChange{Watch: "sub_w", ProjectID: "prj_x", Paths: []string{"/r/a"}, Overflow: true}, ev)
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
			_, _ = h.ListProjects(ctx)
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
