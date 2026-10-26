package rpc_test

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/kv"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

type storageGet struct {
	Value any  `json:"value"`
	Found bool `json:"found"`
}

func TestRPCStorageRequiresPluginCaller(t *testing.T) {
	c := startServer(t)
	err := c.Call(t.Context(), "storage.set", map[string]any{"key": "k", "value": 1}, nil)
	assert.ErrorIs(t, err, rpc.ErrInvalidParams)
}

func TestRPCStorageIsolatedPerPlugin(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	store, err := kv.Open(filepath.Join(t.TempDir(), "plugins.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	srv := rpc.NewServer(svc, b, rpc.WithKV(store))
	connect := func(plugin string) *jsonrpc2.Conn {
		coreSide, pluginSide := net.Pipe()
		conn := srv.ServeConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), rpc.Caller{PluginID: plugin})
		client := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(pluginSide), nil)
		t.Cleanup(func() { client.Close(); conn.Close() })
		return client
	}
	a, other := connect("a"), connect("b")
	require.NoError(t, a.Call(t.Context(), "storage.set", map[string]any{"key": "k", "value": map[string]int{"n": 1}}, nil))
	var got storageGet
	require.NoError(t, a.Call(t.Context(), "storage.get", map[string]any{"key": "k"}, &got))
	assert.True(t, got.Found)
	assert.Equal(t, map[string]any{"n": float64(1)}, got.Value)
	var other2 storageGet
	require.NoError(t, other.Call(t.Context(), "storage.get", map[string]any{"key": "k"}, &other2))
	assert.False(t, other2.Found)
	assert.Nil(t, other2.Value)
	var list struct {
		Keys []string `json:"keys"`
	}
	require.NoError(t, other.Call(t.Context(), "storage.list", map[string]any{"prefix": ""}, &list))
	assert.Equal(t, []string{}, list.Keys)
	err = a.Call(t.Context(), "storage.set", map[string]any{"key": "", "value": 1}, nil)
	var rpcErr *jsonrpc2.Error
	require.ErrorAs(t, err, &rpcErr)
	assert.EqualValues(t, jsonrpc2.CodeInvalidParams, rpcErr.Code)
}
