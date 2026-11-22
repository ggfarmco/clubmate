package main_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
)

type notePost struct {
	Workspace, Session, Title, Message, Kind string
}

func (f *fixture) notes(t *testing.T) []notePost {
	t.Helper()
	return f.waitNotes(t, func([]notePost) bool { return true })
}

func (f *fixture) waitNotes(t *testing.T, cond func([]notePost) bool) []notePost {
	t.Helper()
	var out []notePost
	testutil.WaitEvents(t, f.h.Bus, "notify.posted", func(events []bus.Event) bool {
		out = out[:0]
		for _, e := range events {
			var p notePost
			require.NoError(t, json.Unmarshal(e.Payload, &p))
			out = append(out, p)
		}
		return cond(out)
	})
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

			f.waitNotes(t, func(n []notePost) bool { return len(n) > 0 })
			testutil.Never(t, func() bool { return len(f.notes(t)) > 1 }, 300*time.Millisecond)
			assert.Equal(t, []notePost{{workspace, session, f.session(t, workspace, session).Title, tc.message, tc.kind}}, f.notes(t))
		})
	}
}

func TestNextTurnOrQuestionReplacesAHeldEnd(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_END_HOLD=3s"}, map[string]any{"prompt": []map[string]any{{"echo": true}, {"match": "third", "elicitation": askScript(false)}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "first")
	f.waitStatus(t, workspace, session, "done")
	f.prompt(t, workspace, session, "second")
	f.waitStatus(t, workspace, session, "done")
	f.prompt(t, workspace, session, "third")
	f.waitStatus(t, workspace, session, "waiting")

	f.waitNotes(t, func(n []notePost) bool { return len(n) > 0 })
	testutil.Never(t, func() bool { return len(f.notes(t)) > 1 }, 3500*time.Millisecond)
	assert.Equal(t, "question", f.notes(t)[0].Kind)
}

func TestFinishAfterAQuestionOrWithOpenItemsIsQuiet(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "ask", "elicitation": askScript(false)}, {"echo": true}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	quiet := func(n int) []bool {
		var got []bool
		testutil.WaitEvents(t, f.h.Bus, "notify.posted", func(events []bus.Event) bool {
			got = got[:0]
			for _, e := range events {
				var p struct {
					Kind  string
					Quiet bool
				}
				require.NoError(t, json.Unmarshal(e.Payload, &p))
				if p.Kind == "done" {
					got = append(got, p.Quiet)
				}
			}
			return len(got) == n
		})
		return got
	}

	f.prompt(t, workspace, session, "ask")
	f.waitStatus(t, workspace, session, "waiting")
	request := ofKind(f.transcript(t, workspace, session).Entries, "elicitation")[0].ID
	require.NoError(t, f.h.PluginCall(t, "remote", "command.run", map[string]any{"id": "agents.elicitation", "workspaceId": workspace, "args": map[string]any{"session": session, "request": request, "action": "decline"}}))
	f.waitStatus(t, workspace, session, "done")
	assert.Equal(t, []bool{true}, quiet(1))

	f.prompt(t, workspace, session, "go")
	assert.Equal(t, []bool{true, false}, quiet(2))

	f.postItems(t, workspace, session, offer("add a screen"))
	f.prompt(t, workspace, session, "go on")
	assert.Equal(t, []bool{true, false, true}, quiet(3))
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

func TestDoneWaitsForGrandchildren(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "work", "sleep": 3000}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{}).Session
	grandkid := f.newChild(t, workspace, kid, map[string]any{"prompt": "work"}).Session
	f.waitStatus(t, workspace, grandkid, "running")

	f.prompt(t, workspace, parent, "go")

	f.waitStatus(t, workspace, parent, "done")
	testutil.Never(t, func() bool { return len(f.notes(t)) > 0 }, 500*time.Millisecond)
	f.waitStatus(t, workspace, grandkid, "done")
	f.waitNotes(t, func(n []notePost) bool {
		return slices.ContainsFunc(n, func(p notePost) bool { return p.Session == parent && p.Kind == "done" })
	})
}

func TestDoneComesAfterAGrandchildUnderAVanishedChild(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "work", "sleep": 3000}}}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"branch": "kid", "prompt": "hi"})
	f.waitStatus(t, workspace, kid.Session, "done")
	grandkid := f.newChild(t, workspace, kid.Session, map[string]any{"prompt": "work"}).Session
	f.waitStatus(t, workspace, grandkid, "running")
	require.NoError(t, os.RemoveAll(kid.Worktree))

	f.prompt(t, workspace, parent, "go")

	f.waitStatus(t, workspace, grandkid, "done")
	f.waitNotes(t, func(n []notePost) bool {
		return slices.ContainsFunc(n, func(p notePost) bool { return p.Session == parent && p.Kind == "done" })
	})
}

func TestDoneWaitsForLandReview(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	require.ErrorContains(t, g.land(t), "review pending")
	landerDone := func(n []notePost) bool {
		return slices.ContainsFunc(n, func(p notePost) bool { return p.Session == g.lander && p.Kind == "done" })
	}

	g.f.prompt(t, g.workspace, g.lander, "go")

	g.f.waitStatus(t, g.workspace, g.lander, "done")
	testutil.Never(t, func() bool { return landerDone(g.f.notes(t)) }, 500*time.Millisecond)
	g.verdict(t, "approved")
	g.f.waitNotes(t, landerDone)
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
	f.waitNotes(t, func(n []notePost) bool {
		return slices.ContainsFunc(n, func(p notePost) bool { return p.Session == parent && p.Kind == "done" })
	})
}
