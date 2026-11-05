package rpc_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/session"
)

func TestNotifyPostValidatesAndPublishes(t *testing.T) {
	c, b := startServerWithBus(t)
	p, other := createWorkspace(t, c), createWorkspace(t, c)
	var s session.Session
	require.NoError(t, c.Call(t.Context(), "session.create", map[string]any{"workspaceId": other.ID, "command": []string{"sh", "-c", "exec cat"}}, &s))
	t.Cleanup(func() { c.Call(context.Background(), "session.kill", map[string]string{"id": s.ID}, nil) })

	for name, params := range map[string]map[string]any{
		"unknown kind":      {"workspaceId": p.ID, "message": "hi", "kind": "urgent"},
		"empty message":     {"workspaceId": p.ID, "message": "  "},
		"foreign session":   {"workspaceId": p.ID, "message": "hi", "session": s.ID},
		"malformed session": {"workspaceId": p.ID, "message": "hi", "session": "../x"},
		"foreign collapse":  {"workspaceId": p.ID, "message": "hi", "collapse": "agt_0123456789abcdef"},
	} {
		assert.ErrorIs(t, c.Call(t.Context(), "notify.post", params, nil), rpc.ErrInvalidParams, name)
	}

	var res struct {
		ID string `json:"id"`
	}
	require.NoError(t, c.Call(t.Context(), "notify.post", map[string]any{"workspaceId": other.ID, "session": s.ID, "collapse": s.ID, "title": "Claude", "message": strings.Repeat("x", 1500)}, &res))
	events, err := b.Read(t.Context(), "notify.posted", 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	var got map[string]any
	require.NoError(t, json.Unmarshal(events[0].Payload, &got))
	assert.Equal(t, res.ID, got["id"])
	assert.Equal(t, other.ID, got["workspace"])
	assert.Equal(t, s.ID, got["session"])
	assert.Equal(t, s.ID, got["collapse"])
	assert.Equal(t, "Claude", got["title"])
	assert.Equal(t, "info", got["kind"])
	assert.Len(t, []rune(got["message"].(string)), 1000)
	assert.NotEmpty(t, got["ts"])
}

func TestNotifyPostRateLimitsPerWorkspaceAndInAll(t *testing.T) {
	c := startServer(t)
	post := func(workspaceID string) error {
		return c.Call(t.Context(), "notify.post", map[string]any{"workspaceId": workspaceID, "message": "hi"}, nil)
	}
	first := createWorkspace(t, c)
	for range 5 {
		require.NoError(t, post(first.ID))
	}
	assert.ErrorIs(t, post(first.ID), rpc.ErrRateLimited)
	for range 3 {
		p := createWorkspace(t, c)
		for range 5 {
			require.NoError(t, post(p.ID))
		}
	}
	assert.ErrorIs(t, post(createWorkspace(t, c).ID), rpc.ErrRateLimited)
}
