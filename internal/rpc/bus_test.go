package rpc_test

import (
	"context"
	"net"
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
