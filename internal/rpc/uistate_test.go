package rpc_test

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/kv"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
	"clubmate/internal/workspace"
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
	srv := rpc.NewServer(svc, b)
	client, plug := connectAs(t, srv, rpc.Caller{}), connectAs(t, srv, rpc.Caller{PluginID: "a"})
	var p workspace.Workspace
	require.NoError(t, client.Call(t.Context(), "workspace.create", map[string]any{"name": "demo", "repos": []string{t.TempDir()}}, &p))

	var got uiStateResult
	require.NoError(t, client.Call(t.Context(), "ui.state.get", map[string]any{"workspaceId": p.ID}, &got))
	assert.Nil(t, got.State)

	require.NoError(t, client.Call(t.Context(), "ui.state.set", map[string]any{"workspaceId": p.ID, "state": map[string]any{"section": "terminals"}}, nil))
	require.NoError(t, client.Call(t.Context(), "ui.state.set", map[string]any{"workspaceId": p.ID, "state": map[string]any{"section": "terminals", "sidebarWidth": 200}}, nil))
	require.NoError(t, client.Call(t.Context(), "ui.state.get", map[string]any{"workspaceId": p.ID}, &got))
	assert.Equal(t, map[string]any{"section": "terminals", "sidebarWidth": float64(200)}, got.State)
	var section struct {
		Section string `json:"section"`
	}
	require.NoError(t, plug.Call(t.Context(), "ui.state.section", map[string]any{"workspaceId": p.ID}, &section))
	assert.Equal(t, "terminals", section.Section)
	events, err := b.Read(t.Context(), "ui.section", 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.JSONEq(t, `{"workspace":"`+p.ID+`","section":"terminals"}`, string(events[0].Payload))

	err = plug.Call(t.Context(), "ui.state.set", map[string]any{"workspaceId": p.ID, "state": map[string]any{}}, nil)
	assert.EqualValues(t, -32602, rpcCode(t, err))

	for name, state := range map[string]any{
		"array":   []int{1},
		"scalar":  "x",
		"null":    nil,
		"too big": map[string]string{"k": strings.Repeat("x", 64<<10)},
	} {
		err = client.Call(t.Context(), "ui.state.set", map[string]any{"workspaceId": p.ID, "state": state}, nil)
		assert.EqualValues(t, -32602, rpcCode(t, err), name)
	}

	err = client.Call(t.Context(), "ui.state.set", map[string]any{"workspaceId": "prj_" + strings.Repeat("0", 16), "state": map[string]any{}}, nil)
	assert.EqualValues(t, -32004, rpcCode(t, err))

	require.NoError(t, client.Call(t.Context(), "workspace.delete", map[string]any{"id": p.ID}, nil))
	err = client.Call(t.Context(), "ui.state.get", map[string]any{"workspaceId": p.ID}, nil)
	assert.EqualValues(t, -32004, rpcCode(t, err))
}

func TestUIStatePatchMergesConcurrentKeys(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	srv := rpc.NewServer(svc, b)
	client := connectAs(t, srv, rpc.Caller{})
	var p workspace.Workspace
	require.NoError(t, client.Call(t.Context(), "workspace.create", map[string]any{"name": "demo", "repos": []string{t.TempDir()}}, &p))
	patch := func(key string, value any) error {
		return client.Call(t.Context(), "ui.state.patch", map[string]any{"workspaceId": p.ID, "key": key, "value": value}, nil)
	}

	want := map[string]any{"section": "terminals"}
	var wg sync.WaitGroup
	for i := range 20 {
		key := "k" + strconv.Itoa(i)
		want[key] = float64(i)
		wg.Go(func() { assert.NoError(t, patch(key, i)) })
	}
	wg.Go(func() { assert.NoError(t, patch("section", "terminals")) })
	wg.Wait()
	var got uiStateResult
	require.NoError(t, client.Call(t.Context(), "ui.state.get", map[string]any{"workspaceId": p.ID}, &got))
	assert.Equal(t, want, got.State)
	events, err := b.Read(t.Context(), "ui.section", 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.JSONEq(t, `{"workspace":"`+p.ID+`","section":"terminals"}`, string(events[0].Payload))

	require.NoError(t, patch("k0", nil))
	var deleted uiStateResult
	require.NoError(t, client.Call(t.Context(), "ui.state.get", map[string]any{"workspaceId": p.ID}, &deleted))
	assert.NotContains(t, deleted.State, "k0")

	assert.EqualValues(t, -32602, rpcCode(t, patch("", 1)))
	assert.EqualValues(t, -32602, rpcCode(t, patch("big", strings.Repeat("x", 64<<10))))
}

func TestMigrateUIStateMovesClientKV(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	store, err := kv.Open(filepath.Join(t.TempDir(), "plugins.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	srv := rpc.NewServer(svc, b, rpc.WithKV(store))
	client := connectAs(t, srv, rpc.Caller{})
	var moved, kept workspace.Workspace
	require.NoError(t, client.Call(t.Context(), "workspace.create", map[string]any{"name": "moved"}, &moved))
	require.NoError(t, client.Call(t.Context(), "workspace.create", map[string]any{"name": "kept"}, &kept))
	require.NoError(t, client.Call(t.Context(), "ui.state.set", map[string]any{"workspaceId": kept.ID, "state": map[string]any{"section": "agents"}}, nil))
	orphan := "prj_" + strings.Repeat("0", 16)
	for id, state := range map[string]string{moved.ID: `{"section":"terminals"}`, kept.ID: `{"section":"stale"}`, orphan: `{}`} {
		require.NoError(t, store.Set(t.Context(), "@client", id, json.RawMessage(state)))
	}

	require.NoError(t, srv.MigrateUIState(t.Context()))
	require.NoError(t, srv.MigrateUIState(t.Context()))

	for id, want := range map[string]string{moved.ID: "terminals", kept.ID: "agents"} {
		var got uiStateResult
		require.NoError(t, client.Call(t.Context(), "ui.state.get", map[string]any{"workspaceId": id}, &got))
		assert.Equal(t, map[string]any{"section": want}, got.State)
	}
	keys, err := store.List(t.Context(), "@client", "")
	require.NoError(t, err)
	assert.Empty(t, keys)
}
