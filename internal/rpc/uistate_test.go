package rpc_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/kv"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

type uiStateResult struct {
	State map[string]any `json:"state"`
}

func rpcCode(t *testing.T, err error) int64 {
	t.Helper()
	var rpcErr *jsonrpc2.Error
	require.ErrorAs(t, err, &rpcErr)
	return rpcErr.Code
}

func TestUIStateRoundTripAndDelete(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	store, err := kv.Open(filepath.Join(t.TempDir(), "plugins.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	srv := rpc.NewServer(svc, b, rpc.WithKV(store))
	client, plug := connectAs(t, srv, rpc.Caller{}), connectAs(t, srv, rpc.Caller{PluginID: "a"})
	var p project.Project
	require.NoError(t, client.Call(t.Context(), "project.create", map[string]any{"name": "demo", "repos": []string{t.TempDir()}}, &p))

	var got uiStateResult
	require.NoError(t, client.Call(t.Context(), "ui.state.get", map[string]any{"projectId": p.ID}, &got))
	assert.Nil(t, got.State)

	require.NoError(t, client.Call(t.Context(), "ui.state.set", map[string]any{"projectId": p.ID, "state": map[string]any{"section": "terminals"}}, nil))
	require.NoError(t, client.Call(t.Context(), "ui.state.set", map[string]any{"projectId": p.ID, "state": map[string]any{"section": "terminals", "sidebarWidth": 200}}, nil))
	require.NoError(t, client.Call(t.Context(), "ui.state.get", map[string]any{"projectId": p.ID}, &got))
	assert.Equal(t, map[string]any{"section": "terminals", "sidebarWidth": float64(200)}, got.State)
	var section struct {
		Section string `json:"section"`
	}
	require.NoError(t, plug.Call(t.Context(), "ui.state.section", map[string]any{"projectId": p.ID}, &section))
	assert.Equal(t, "terminals", section.Section)
	events, err := b.Read(t.Context(), "ui.section", 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.JSONEq(t, `{"projectId":"`+p.ID+`","section":"terminals"}`, string(events[0].Payload))

	err = plug.Call(t.Context(), "ui.state.set", map[string]any{"projectId": p.ID, "state": map[string]any{}}, nil)
	assert.EqualValues(t, -32602, rpcCode(t, err))

	for name, state := range map[string]any{
		"array":   []int{1},
		"scalar":  "x",
		"null":    nil,
		"too big": map[string]string{"k": strings.Repeat("x", 64<<10)},
	} {
		err = client.Call(t.Context(), "ui.state.set", map[string]any{"projectId": p.ID, "state": state}, nil)
		assert.EqualValues(t, -32602, rpcCode(t, err), name)
	}

	err = client.Call(t.Context(), "ui.state.set", map[string]any{"projectId": "prj_" + strings.Repeat("0", 16), "state": map[string]any{}}, nil)
	assert.EqualValues(t, -32004, rpcCode(t, err))

	require.NoError(t, client.Call(t.Context(), "project.delete", map[string]any{"id": p.ID}, nil))
	err = client.Call(t.Context(), "ui.state.get", map[string]any{"projectId": p.ID}, nil)
	assert.EqualValues(t, -32004, rpcCode(t, err))
	_, found, err := store.Get(t.Context(), "@client", p.ID)
	require.NoError(t, err)
	assert.False(t, found)
}
