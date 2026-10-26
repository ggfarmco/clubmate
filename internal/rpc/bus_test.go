package rpc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

func TestRPCBusSubscribePushesEvents(t *testing.T) {
	c := startServer(t)
	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, c.Call(t.Context(), "bus.subscribe", map[string]any{"pattern": "demo.*"}, &sub))
	assert.Regexp(t, `^sub_[0-9a-f]{16}$`, sub.Subscription)
	var ev bus.Event
	require.NoError(t, c.Call(t.Context(), "bus.publish", map[string]any{"topic": "demo.hello", "payload": map[string]int{"n": 1}}, &ev))
	assert.Equal(t, "cli", ev.Source)
	n := recvNotification(t, c)
	assert.Equal(t, "bus.event", n.Method)
	assert.Contains(t, string(n.Params), `"topic":"demo.hello"`)
}

func TestRPCBusClosedNotifiesSubscriber(t *testing.T) {
	c, b := startServerWithBus(t)
	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, c.Call(t.Context(), "bus.subscribe", map[string]any{"pattern": "*"}, &sub))
	require.NoError(t, b.Close())
	n := recvNotification(t, c)
	assert.Equal(t, "bus.closed", n.Method)
	assert.JSONEq(t, `{"subscription":"`+sub.Subscription+`","error":"bus closed"}`, string(n.Params))
}

func TestRPCBusUnsubscribeStopsDelivery(t *testing.T) {
	c := startServer(t)
	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, c.Call(t.Context(), "bus.subscribe", map[string]any{"pattern": "demo.*"}, &sub))
	require.NoError(t, c.Call(t.Context(), "bus.unsubscribe", sub, nil))
	require.NoError(t, c.Call(t.Context(), "bus.publish", map[string]any{"topic": "demo.hello"}, nil))
	select {
	case n := <-c.Notifications():
		assert.Failf(t, "unexpected notification", "%s %s", n.Method, n.Params)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestRPCBusSubscriptionReleasedWhenForwardingFails(t *testing.T) {
	srv, b := testutil.NewRPCServer(t)
	for range 1000 {
		_, err := b.Publish(t.Context(), "demo.x", "test", nil)
		require.NoError(t, err)
	}
	coreSide, clientSide := net.Pipe()
	srv.ServeConn(t.Context(), failingNotifyStream{jsonrpc2.NewPlainObjectStream(coreSide)}, rpc.Caller{})
	client := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(clientSide), jsonrpc2.HandlerWithError(
		func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (any, error) { return nil, nil }))
	t.Cleanup(func() { client.Close() })
	require.NoError(t, client.Call(t.Context(), "bus.subscribe", map[string]any{"pattern": "demo.*"}, nil))
	testutil.Eventually(t, func() bool { return !strings.Contains(goroutines(), "bus.(*Subscription).backfill") }, 2*time.Second)
}

func TestRPCReentrantCallsDoNotDeadlock(t *testing.T) {
	srv, _ := testutil.NewRPCServer(t)
	coreSide, pluginSide := net.Pipe()
	conn := srv.ServeConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), rpc.Caller{PluginID: "echo"})
	var pluginConn *jsonrpc2.Conn
	pluginConn = jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(pluginSide), jsonrpc2.AsyncHandler(jsonrpc2.HandlerWithError(
		func(ctx context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
			var projects []project.Project
			err := pluginConn.Call(ctx, "project.list", nil, &projects)
			return len(projects), err
		})))
	t.Cleanup(func() { pluginConn.Close(); conn.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var n int
	require.NoError(t, conn.Call(ctx, "plugin.ping", nil, &n))
	assert.Equal(t, 0, n)
}

func TestRPCBusCursorsAreScopedPerPlugin(t *testing.T) {
	srv, _ := testutil.NewRPCServer(t)
	conns := []*jsonrpc2.Conn{connectAs(t, srv, rpc.Caller{PluginID: "a"}), connectAs(t, srv, rpc.Caller{PluginID: "b"}), connectAs(t, srv, rpc.Caller{})}
	for i, c := range conns {
		require.NoError(t, c.Call(t.Context(), "bus.cursor.set", map[string]any{"consumer": "x", "seq": i + 1}, nil))
	}
	for i, c := range conns {
		var got struct {
			Seq int64 `json:"seq"`
		}
		require.NoError(t, c.Call(t.Context(), "bus.cursor.get", map[string]any{"consumer": "x"}, &got))
		assert.EqualValues(t, i+1, got.Seq)
	}
}

type failingNotifyStream struct {
	jsonrpc2.ObjectStream
}

func (s failingNotifyStream) WriteObject(v any) error {
	raw, err := json.Marshal(v)
	if err == nil && bytes.Contains(raw, []byte(`"method":"bus.event"`)) {
		return errors.New("write failed")
	}
	return s.ObjectStream.WriteObject(v)
}

func goroutines() string {
	buf := make([]byte, 1<<20)
	return string(buf[:runtime.Stack(buf, true)])
}

func recvNotification(t *testing.T, c *rpc.Client) rpc.Notification {
	t.Helper()
	select {
	case n := <-c.Notifications():
		return n
	case <-time.After(2 * time.Second):
		require.FailNow(t, "no notification within 2s")
		return rpc.Notification{}
	}
}

func connectAs(t *testing.T, srv *rpc.Server, caller rpc.Caller) *jsonrpc2.Conn {
	t.Helper()
	coreSide, peerSide := net.Pipe()
	conn := srv.ServeConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), caller)
	client := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(peerSide), nil)
	t.Cleanup(func() { client.Close(); conn.Close() })
	return client
}
