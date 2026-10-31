package rpc_test

import (
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

func TestDebugPprofIsClientOnly(t *testing.T) {
	srv, _ := testutil.NewRPCServer(t)
	params := map[string]any{"profile": "goroutine", "debug": 1}
	var res struct {
		Data []byte `json:"data"`
	}
	require.NoError(t, connectAs(t, srv, rpc.Caller{}).Call(t.Context(), "debug.pprof", params, &res))
	assert.Contains(t, string(res.Data), "goroutine profile:")
	var rpcErr *jsonrpc2.Error
	require.ErrorAs(t, connectAs(t, srv, rpc.Caller{PluginID: "terminals"}).Call(t.Context(), "debug.pprof", params, nil), &rpcErr)
	assert.EqualValues(t, jsonrpc2.CodeInvalidParams, rpcErr.Code)
}
