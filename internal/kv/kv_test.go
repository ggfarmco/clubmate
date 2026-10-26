package kv_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/kv"
)

func openKV(t *testing.T) *kv.Store {
	t.Helper()
	s, err := kv.Open(filepath.Join(t.TempDir(), "plugins.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestKVNamespacesByPlugin(t *testing.T) {
	s := openKV(t)
	require.NoError(t, s.Set(t.Context(), "a", "selected/prj_1", json.RawMessage(`"ses_1"`)))
	_, found, err := s.Get(t.Context(), "b", "selected/prj_1")
	require.NoError(t, err)
	assert.False(t, found)
	keys, err := s.List(t.Context(), "a", "selected/")
	require.NoError(t, err)
	assert.Equal(t, []string{"selected/prj_1"}, keys)
}

func TestKVSetOverwritesAndDeletes(t *testing.T) {
	s := openKV(t)
	require.NoError(t, s.Set(t.Context(), "a", "k", json.RawMessage(`1`)))
	require.NoError(t, s.Set(t.Context(), "a", "k", json.RawMessage(`{"n":2}`)))
	v, found, err := s.Get(t.Context(), "a", "k")
	require.NoError(t, err)
	assert.True(t, found)
	assert.JSONEq(t, `{"n":2}`, string(v))
	require.NoError(t, s.Delete(t.Context(), "a", "k"))
	_, found, err = s.Get(t.Context(), "a", "k")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestKVListIsSortedAndPrefixed(t *testing.T) {
	s := openKV(t)
	for _, k := range []string{"b/2", "a/1", "b/1", "%b"} {
		require.NoError(t, s.Set(t.Context(), "a", k, json.RawMessage(`null`)))
	}
	keys, err := s.List(t.Context(), "a", "b/")
	require.NoError(t, err)
	assert.Equal(t, []string{"b/1", "b/2"}, keys)
	all, err := s.List(t.Context(), "a", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"%b", "a/1", "b/1", "b/2"}, all)
}

func TestKVValidation(t *testing.T) {
	s := openKV(t)
	ctx := t.Context()
	assert.ErrorIs(t, s.Set(ctx, "a", "", json.RawMessage(`1`)), kv.ErrInvalidKey)
	assert.ErrorIs(t, s.Set(ctx, "a", "a\x00b", json.RawMessage(`1`)), kv.ErrInvalidKey)
	assert.ErrorIs(t, s.Set(ctx, "a", strings.Repeat("k", 257), json.RawMessage(`1`)), kv.ErrInvalidKey)
	assert.ErrorIs(t, s.Set(ctx, "a", "k", json.RawMessage(`{bad`)), kv.ErrInvalidValue)
	big := json.RawMessage(`"` + strings.Repeat("x", 256<<10) + `"`)
	assert.ErrorIs(t, s.Set(ctx, "a", "k", big), kv.ErrValueTooLarge)
}
