package pluginmigrate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/kv"
	"clubmate/internal/paths"
	"clubmate/internal/pluginmigrate"
)

func setup(t *testing.T) (*kv.Store, paths.Dirs, string) {
	t.Helper()
	dirs := paths.Dirs{State: t.TempDir(), Runtime: t.TempDir()}
	store, err := kv.Open(dirs.PluginsDB())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	old := filepath.Join(dirs.PluginState("graphs"), "output", "gr_1", "3.log")
	require.NoError(t, os.MkdirAll(filepath.Dir(old), 0o700))
	require.NoError(t, os.WriteFile(old, []byte("step output\n"), 0o600))
	row, err := json.Marshal(map[string]any{"id": "gr_1", "moves": []any{map[string]any{"step": map[string]any{"output": old}}}})
	require.NoError(t, err)
	require.NoError(t, store.Set(t.Context(), "graphs", "run/w/gr_1", row))
	return store, dirs, old
}

func TestMoveRewritesStepOutputPaths(t *testing.T) {
	store, dirs, _ := setup(t)
	require.NoError(t, pluginmigrate.Graphs(t.Context(), store, dirs))
	raw, found, err := store.Get(t.Context(), "agents", "graph/run/w/gr_1")
	require.NoError(t, err)
	require.True(t, found)
	var run struct {
		Moves []struct {
			Step struct {
				Output string `json:"output"`
			} `json:"step"`
		} `json:"moves"`
	}
	require.NoError(t, json.Unmarshal(raw, &run))
	moved := filepath.Join(dirs.PluginState("agents"), "graphs", "output", "gr_1", "3.log")
	assert.Equal(t, moved, run.Moves[0].Step.Output)
	data, err := os.ReadFile(moved)
	require.NoError(t, err)
	assert.Equal(t, "step output\n", string(data))
	assert.NoDirExists(t, dirs.PluginState("graphs"))
}

func TestGraphsMigrationIdempotent(t *testing.T) {
	store, dirs, _ := setup(t)
	require.NoError(t, pluginmigrate.Graphs(t.Context(), store, dirs))
	before, err := store.List(t.Context(), "agents", "")
	require.NoError(t, err)
	n, err := store.MovePlugin(t.Context(), "graphs", "agents", "graph/", func(_ string, v json.RawMessage) json.RawMessage { return v })
	require.NoError(t, err)
	assert.Zero(t, n)
	require.NoError(t, pluginmigrate.Graphs(t.Context(), store, dirs))
	after, err := store.List(t.Context(), "agents", "")
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.FileExists(t, filepath.Join(dirs.PluginState("agents"), "graphs", "output", "gr_1", "3.log"))
}
