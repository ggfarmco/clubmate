package workspace_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUIStateLifecycle(t *testing.T) {
	s := openStore(t)
	p, err := s.Create(t.Context(), "demo", nil)
	require.NoError(t, err)
	_, found, err := s.UIState(t.Context(), p.ID)
	require.NoError(t, err)
	assert.False(t, found)

	require.NoError(t, s.ImportUIState(t.Context(), p.ID, json.RawMessage(`{"section":"imported"}`)))
	require.NoError(t, s.SetUIState(t.Context(), p.ID, json.RawMessage(`{"section":"terminals"}`)))
	require.NoError(t, s.ImportUIState(t.Context(), p.ID, json.RawMessage(`{"section":"stale"}`)))
	got, found, err := s.UIState(t.Context(), p.ID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.JSONEq(t, `{"section":"terminals"}`, string(got))

	require.NoError(t, s.Delete(t.Context(), p.ID))
	_, found, err = s.UIState(t.Context(), p.ID)
	require.NoError(t, err)
	assert.False(t, found)
}
