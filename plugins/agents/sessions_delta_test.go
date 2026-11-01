package main_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

type sessionsPage struct {
	Sessions []struct {
		ID     string `json:"id"`
		Notify string `json:"notify"`
	} `json:"sessions"`
	Removed []string `json:"removed"`
	Cursor  string   `json:"cursor"`
	Delta   bool     `json:"delta"`
}

func (p sessionsPage) ids() []string {
	out := []string{}
	for _, s := range p.Sessions {
		out = append(out, s.ID)
	}
	return out
}

func TestSessionsSinceReturnsOnlyChangedAndRemovedSessions(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	kept := f.newSession(t, workspace)
	muted := f.newSession(t, workspace)
	deleted := f.newSession(t, workspace)
	for _, id := range []string{kept, muted, deleted} {
		testutil.Eventually(t, func() bool { return f.session(t, workspace, id).AgentSessionID != "" }, wait)
	}
	page := func(since string) sessionsPage {
		var p sessionsPage
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"since": since}, &p))
		return p
	}
	full := page("")
	assert.False(t, full.Delta)
	assert.Equal(t, []string{kept, muted, deleted}, full.ids())
	assert.Empty(t, page(full.Cursor).ids())

	require.NoError(t, f.run(t, workspace, "agents.notify", map[string]any{"session": muted, "policy": "muted"}, nil))
	require.NoError(t, f.run(t, workspace, "agents.delete", map[string]any{"session": deleted}, nil))

	changed := page(full.Cursor)
	assert.True(t, changed.Delta)
	assert.Equal(t, []string{muted}, changed.ids())
	assert.Equal(t, "muted", changed.Sessions[0].Notify)
	assert.Equal(t, []string{deleted}, changed.Removed)
	assert.Empty(t, page(changed.Cursor).ids())
}

func TestSessionsSinceACursorFromBeforeARestartReturnsTheFullList(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	id := f.newSession(t, workspace)

	var p sessionsPage
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"since": "before-restart.1"}, &p))

	assert.False(t, p.Delta)
	assert.Equal(t, []string{id}, p.ids())
	assert.NotEmpty(t, p.Cursor)
}
