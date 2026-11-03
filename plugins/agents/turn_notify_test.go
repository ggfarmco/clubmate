package main_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

type notePost struct {
	Workspace, Session, Title, Message, Kind string
}

func (f *fixture) notes(t *testing.T) []notePost {
	t.Helper()
	events, err := f.h.Bus.Read(t.Context(), "notify.posted", 0, 100)
	require.NoError(t, err)
	var out []notePost
	for _, e := range events {
		var p notePost
		require.NoError(t, json.Unmarshal(e.Payload, &p))
		out = append(out, p)
	}
	return out
}

func TestTurnEndPostsNotification(t *testing.T) {
	t.Parallel()
	crash := []map[string]any{{"child": true}, {"exit": 3}}
	for _, tc := range []struct {
		name, text, message, kind string
		script                    []map[string]any
	}{
		{"done", "go", "Finished", "done", []map[string]any{{"echo": true}}},
		{"asks", "Ship it? ", "Asks you a question", "needs-input", []map[string]any{{"echo": true}}},
		{"stuck", "go", "Stopped with an error", "stuck", crash},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := start(t, map[string]any{"prompt": tc.script})
			workspace := f.workspace(t, newRepo(t))
			session := f.newSession(t, workspace)

			f.prompt(t, workspace, session, tc.text)

			testutil.Eventually(t, func() bool { return len(f.notes(t)) > 0 }, wait)
			testutil.Never(t, func() bool { return len(f.notes(t)) > 1 }, 300*time.Millisecond)
			assert.Equal(t, []notePost{{workspace, session, f.session(t, workspace, session).Title, tc.message, tc.kind}}, f.notes(t))
		})
	}
}

func TestChildTurnEndPostsNothing(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)

	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "go"}).Session

	f.waitStatus(t, workspace, kid, "done")
	testutil.Never(t, func() bool {
		return slices.ContainsFunc(f.notes(t), func(p notePost) bool { return p.Session == kid })
	}, 1500*time.Millisecond)
}

func TestCancelledTurnPostsNothing(t *testing.T) {
	t.Parallel()
	f, workspace, session := startQueued(t)

	require.NoError(t, f.run(t, workspace, "agents.cancel", map[string]any{"session": session}, nil))

	f.waitStatus(t, workspace, session, "done")
	testutil.Never(t, func() bool { return len(f.notes(t)) > 0 }, 300*time.Millisecond)
}

func TestDoneWaitsForChildren(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "work", "sleep": 3000}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "work"}).Session
	f.waitStatus(t, workspace, kid, "running")

	f.prompt(t, workspace, parent, "go")

	f.waitStatus(t, workspace, parent, "done")
	testutil.Never(t, func() bool { return len(f.notes(t)) > 0 }, 500*time.Millisecond)
	f.waitStatus(t, workspace, kid, "done")
	testutil.Eventually(t, func() bool {
		return slices.ContainsFunc(f.notes(t), func(p notePost) bool { return p.Session == parent && p.Kind == "done" })
	}, wait)
}
