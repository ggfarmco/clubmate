package main_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

type gatedDocs struct {
	*fixture
	ws, sid, gate, spec, hold string
}

func startGatedDocs(t *testing.T, sessions []string, steps ...map[string]any) gatedDocs {
	t.Helper()
	g := gatedDocs{hold: filepath.Join(t.TempDir(), "hold")}
	script := append(append([]map[string]any{{"run": writesNote}}, steps...), map[string]any{"wait": g.hold})
	g.fixture = start(t, map[string]any{"resume": true, "prompt": script}, testutil.BuildPluginDir(t, "clubmate/plugins/docs", "plugins/docs/plugin.json"))
	repo := newRepo(t)
	g.spec = filepath.Join(repo, "spec.md")
	write(t, g.spec, "# Spec\n")
	g.ws = g.workspace(t, repo)
	g.gate = g.doneGate(t, g.ws, repo, map[string]any{"sessions": sessions})
	g.sid = g.newSession(t, g.ws)
	g.prompt(t, g.ws, g.sid, "write")
	testutil.Eventually(t, func() bool { return len(g.calls(t, "run")) > 0 }, wait)
	return g
}

func (g gatedDocs) agentDoc(t *testing.T, command string) string {
	t.Helper()
	var res struct {
		Note string `json:"note"`
	}
	require.NoError(t, g.runAs(t, g.ws, g.sid, command, map[string]any{"path": g.spec}, &res))
	return res.Note
}

func (g gatedDocs) shown(t *testing.T, topic string) bool {
	t.Helper()
	return slices.ContainsFunc(testutil.WaitEvents(t, g.h.Bus, topic, func([]bus.Event) bool { return true }), func(e bus.Event) bool {
		var p struct {
			Path string `json:"path"`
		}
		require.NoError(t, json.Unmarshal(e.Payload, &p))
		return p.Path == g.spec
	})
}

func (g gatedDocs) queued(t *testing.T) bool {
	t.Helper()
	raw, _, err := g.h.KV.Get(t.Context(), "agents", "session/"+g.ws+"/"+g.sid)
	require.NoError(t, err)
	return strings.Contains(string(raw), `"gatedDocs"`)
}

const queuedNote = "queued: shown when this turn is reviewed"

func TestDocOpenWaitsForRelease(t *testing.T) {
	t.Parallel()
	reply := map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "Done."}}}
	g := startGatedDocs(t, nil, reply)

	assert.Equal(t, []string{queuedNote, queuedNote}, []string{g.agentDoc(t, "docs.open"), g.agentDoc(t, "docs.present")})
	write(t, g.hold, "")
	g.waitHeld(t, g.ws, g.sid)
	g.restartAgents(t, g.ws)
	g.waitHeld(t, g.ws, g.sid)
	cards := func() []sdui.DocCard {
		agent := ofKind(g.transcript(t, g.ws, g.sid).Entries, "agent")
		require.Len(t, agent, 1)
		return agent[0].DocCards
	}
	testutil.Never(t, func() bool { return len(cards()) > 0 || g.shown(t, "docs.opened") || g.shown(t, "docs.presented") }, quiet)

	g.pauseGate(t, g.ws, g.gate)

	testutil.Eventually(t, func() bool { c := cards(); return g.shown(t, "docs.opened") && len(c) == 1 && c[0].Presented }, wait)
}

func TestDocOpenFlushedWhenTurnNotHeld(t *testing.T) {
	t.Parallel()
	g := startGatedDocs(t, nil)
	require.Equal(t, queuedNote, g.agentDoc(t, "docs.open"))

	require.NoError(t, g.run(t, g.ws, "agents.cancel", map[string]any{"session": g.sid}, nil))

	g.waitStatus(t, g.ws, g.sid, "done")
	testutil.Eventually(t, func() bool { return g.shown(t, "docs.opened") }, wait)
}

func TestDocOpenWithoutDoneGateOpensAtOnce(t *testing.T) {
	t.Parallel()
	g := startGatedDocs(t, []string{"child"})

	assert.Empty(t, g.agentDoc(t, "docs.open"))

	testutil.Eventually(t, func() bool { return g.shown(t, "docs.opened") && g.session(t, g.ws, g.sid).Status == "running" }, wait)
	write(t, g.hold, "")
}

func TestClosingSessionDropsQueuedDocs(t *testing.T) {
	t.Parallel()
	g := startGatedDocs(t, nil)
	require.Equal(t, queuedNote, g.agentDoc(t, "docs.open"))

	require.NoError(t, g.run(t, g.ws, "agents.close", map[string]any{"session": g.sid}, nil))

	g.waitStatus(t, g.ws, g.sid, "closed")
	testutil.Never(t, func() bool { return g.shown(t, "docs.opened") }, quiet)
}

func TestQueuedDocWaitsForTheDocsPlugin(t *testing.T) {
	t.Parallel()
	g := startGatedDocs(t, nil)
	require.Equal(t, queuedNote, g.agentDoc(t, "docs.open"))
	require.NoError(t, g.h.Sup.SetEnabled(t.Context(), "docs", false))
	require.NoError(t, g.run(t, g.ws, "agents.cancel", map[string]any{"session": g.sid}, nil))
	g.waitStatus(t, g.ws, g.sid, "done")
	testutil.Never(t, func() bool { return !g.queued(t) }, quiet)

	require.NoError(t, g.h.Sup.SetEnabled(t.Context(), "docs", true))

	testutil.Eventually(t, func() bool { return g.shown(t, "docs.opened") && !g.queued(t) }, wait)
}

func TestDocQueuedWhenGitFails(t *testing.T) {
	t.Parallel()
	g := startGatedDocs(t, nil)
	write(t, filepath.Join(filepath.Dir(g.spec), ".git", "HEAD"), "garbage\n")

	assert.Equal(t, queuedNote, g.agentDoc(t, "docs.open"))
}

func TestQueuedDocShowsWhenTheAgentAsks(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		step  map[string]any
		shown bool
	}{
		"question":   {map[string]any{"elicitation": askScript(false)}, true},
		"permission": {map[string]any{"permission": permissionScript()}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ask := filepath.Join(t.TempDir(), "ask")
			g := startGatedDocs(t, nil, map[string]any{"wait": ask}, tc.step)
			require.Equal(t, queuedNote, g.agentDoc(t, "docs.open"))

			write(t, ask, "")

			g.waitStatus(t, g.ws, g.sid, "waiting")
			if tc.shown {
				testutil.Eventually(t, func() bool { return g.shown(t, "docs.opened") }, wait)
			} else {
				testutil.Never(t, func() bool { return g.shown(t, "docs.opened") }, quiet)
			}
		})
	}
}

func TestReplayedDocCardStaysOnItsTurn(t *testing.T) {
	t.Parallel()
	reply := map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "Done."}}}
	g := startGatedDocs(t, nil, reply)
	require.Equal(t, queuedNote, g.agentDoc(t, "docs.present"))
	g.prompt(t, g.ws, g.sid, "again")
	g.pauseGate(t, g.ws, g.gate)

	write(t, g.hold, "")

	onFirstTurn := func() bool {
		agent := ofKind(g.transcript(t, g.ws, g.sid).Entries, "agent")
		return len(agent) == 2 && len(agent[0].DocCards) == 1 && agent[0].DocCards[0].Presented && len(agent[1].DocCards) == 0
	}
	testutil.Eventually(t, onFirstTurn, wait)
	g.restartAgents(t, g.ws)
	testutil.Eventually(t, onFirstTurn, wait)
}

func TestDocOfAnAFKParentsChildOpensAtOnce(t *testing.T) {
	t.Parallel()
	hold := filepath.Join(t.TempDir(), "hold")
	f := start(t, map[string]any{"prompt": []map[string]any{{"wait": hold}}}, testutil.BuildPluginDir(t, "clubmate/plugins/docs", "plugins/docs/plugin.json"))
	t.Cleanup(func() { write(t, hold, "") })
	repo := newRepo(t)
	spec := filepath.Join(repo, "spec.md")
	write(t, spec, "# Spec\n")
	ws := f.workspace(t, repo)
	f.doneGate(t, ws, repo, map[string]any{"sessions": []string{"child"}})
	parent := f.newSession(t, ws)
	require.NoError(t, f.run(t, ws, "agents.afk", map[string]any{"session": parent, "on": true}, nil))
	kid := f.newChild(t, ws, parent, map[string]any{"title": "Kid", "prompt": "work"}).Session
	f.waitStatus(t, ws, kid, "running")
	var res struct {
		Note string `json:"note"`
	}

	require.NoError(t, f.runAs(t, ws, kid, "docs.open", map[string]any{"path": spec}, &res))

	assert.Empty(t, res.Note)
}
