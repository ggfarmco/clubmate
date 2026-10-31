package rpc_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

func newTestApp(t *testing.T, h *testutil.PluginHarness, handles ...string) chan json.RawMessage {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server := h.Srv.ServeConn(context.Background(), jsonrpc2.NewPlainObjectStream(serverSide), rpc.Caller{})
	seen := make(chan json.RawMessage, 4)
	handler := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		seen <- *req.Params
		return map[string]any{"windows": []any{}, "summary": "0 problems"}, nil
	})
	conn := jsonrpc2.NewConn(context.Background(), jsonrpc2.NewPlainObjectStream(clientSide), jsonrpc2.AsyncHandler(handler))
	t.Cleanup(func() {
		conn.Close()
		server.Close()
	})
	require.NoError(t, conn.Call(t.Context(), "client.register", map[string]any{"handles": handles}, nil))
	return seen
}

func TestSnapshotForwardedToTestClient(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t)
	seen := newTestApp(t, h, "ui.confirm", "ui.snapshot")
	var res struct {
		Summary string `json:"summary"`
	}
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.snapshot", map[string]any{"out": "/tmp/x"}, &res))
	assert.Equal(t, "0 problems", res.Summary)
	assert.JSONEq(t, `{"out":"/tmp/x"}`, string(<-seen))
}

func TestKeyForwardedToTestClient(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t)
	seen := newTestApp(t, h, "ui.key")
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.key", map[string]any{"keys": []string{"opt+return"}}, nil))
	assert.JSONEq(t, `{"keys":["opt+return"]}`, string(<-seen))
}

func TestSnapshotWithoutTestClient(t *testing.T) {
	h := testutil.NewHarness(t)
	h.Start(t)
	newTestApp(t, h, "ui.confirm")
	err := h.Client(t).Call(t.Context(), "ui.snapshot", map[string]any{"out": "/tmp/x"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot is only available in test mode")
}
