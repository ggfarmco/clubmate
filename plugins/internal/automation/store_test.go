package automation_test

import (
	"context"
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
	"clubmate/plugins/internal/automation"
)

type note struct {
	ID        string `json:"id"`
	Workspace string `json:"workspace"`
	Text      string `json:"text"`
}

func (n *note) StorageKey() (workspace, id string) {
	return n.Workspace, n.ID
}

type job struct {
	ID        string `json:"id"`
	Workspace string `json:"workspace"`
	Command   string `json:"command"`
}

func (j *job) StorageKey() (workspace, id string) {
	return j.Workspace, j.ID
}

func (j *job) Action() automation.Action {
	return automation.Action{ID: j.ID, Workspace: j.Workspace, Command: j.Command}
}

func (j *job) SetCommand(id string) {
	j.Command = id
}

func TestStoreKeepsKeyedValuesUnderWorkspaceAndID(t *testing.T) {
	h, store := storeHost(t)
	st := &automation.Store[note, *note]{Host: h, Prefix: "note/", Noun: "note", Log: func(err error) { t.Error(err) }}

	n := &note{ID: "n1", Workspace: "ws1", Text: "hi"}
	require.NoError(t, st.Save(t.Context(), n))
	_, found, err := store.Get(t.Context(), "test", "note/ws1/n1")
	require.NoError(t, err)
	assert.True(t, found)
	loaded, err := st.Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []*note{n}, loaded)

	require.NoError(t, st.Remove(t.Context(), n))
	loaded, err = st.Load(t.Context())
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

func TestStoreMigratesLegacyCommandsOfEntries(t *testing.T) {
	h, _ := storeHost(t)
	st := &automation.Store[job, *job]{Host: h, Prefix: "job/", Noun: "job", Log: func(err error) { t.Error(err) }}
	require.NoError(t, h.StorageSet(t.Context(), "job/ws1/j1", job{ID: "j1", Workspace: "ws1", Command: "remote.cancelPair"}))

	loaded, err := st.Load(t.Context())
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "remote.cancel-pair", loaded[0].Command)
	var stored job
	_, err = h.StorageGet(t.Context(), "job/ws1/j1", &stored)
	require.NoError(t, err)
	assert.Equal(t, "remote.cancel-pair", stored.Command)
}

func storeHost(t *testing.T) (*pluginsdk.Host, *kv.Store) {
	t.Helper()
	svc, b := testutil.NewServiceWithBus(t)
	store, err := kv.Open(filepath.Join(t.TempDir(), "plugins.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	srv := rpc.NewServer(svc, b, rpc.WithKV(store))
	coreSide, pluginSide := net.Pipe()
	conn := srv.ServeConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), rpc.Caller{PluginID: "test"})
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
	require.NoError(t, conn.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": 1, "pluginId": "test"}, nil))
	select {
	case h := <-ready:
		return h, store
	case <-time.After(3 * time.Second):
		t.Fatal("Init not called")
		return nil, nil
	}
}
