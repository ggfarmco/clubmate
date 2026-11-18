package main_test

import (
	"encoding/json"
	"os/exec"
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

func TestDoneOnlyRepoGetsNoHookEnv(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "turn"}, nil))

	var res struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"prompt": "go"}, &res))
	call := f.waitCall(t, "session/new", func(c fakeCall) bool { return strings.Contains(string(c.Params), res.Session) })
	var meta struct {
		Meta struct {
			ClaudeCode struct {
				Options struct {
					Env map[string]string `json:"env"`
				} `json:"options"`
			} `json:"claudeCode"`
		} `json:"_meta"`
	}
	require.NoError(t, json.Unmarshal(call.Params, &meta))

	assert.NotContains(t, meta.Meta.ClaudeCode.Options.Env, "CLUBMATE_LAND_PROTECT")
}

func TestDoneGatesRefuseDuplicatesAndNameTheirMatch(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	var plans gateListed
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "turn", "wrote": []string{"**/plans/*.md", "**/specs/*.md"}}, &plans))
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "turn"}, nil))
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "turn", "sessions": []string{"child"}, "wrote": []string{"docs/*.md"}}, nil))

	err := f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "turn", "sessions": []string{"afk", "top"}, "wrote": []string{"**/specs/*.md", "**/plans/*.md"}}, nil)

	assert.ErrorContains(t, err, plans.ID)
	var names []string
	for _, g := range f.gates(t, workspace) {
		names = append(names, g.Name)
	}
	assert.Equal(t, []string{"Done · plans and specs", "Done · any turn", "Done · docs/*.md"}, names)
}

func TestGateSinceStaysAtTheFirstTurnAfterAnErrorWhereADoneGateHolds(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "first", "fail": "Internal error: max_output_tokens", "errorKind": "max_output_tokens"}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "turn"}, nil))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "first")
	f.waitStatus(t, workspace, session, "error")

	f.prompt(t, workspace, session, "second")
	f.waitStatus(t, workspace, session, "done")

	assert.Equal(t, git(t, repo, "rev-parse", "refs/clubmate/cp/"+session+"/1"), f.gateSince(t, workspace, session))
}

func TestDoneClearsGateSinceOnceTheOnlyDoneGateIsPaused(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "first", "fail": "Internal error: max_output_tokens", "errorKind": "max_output_tokens"}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gate := f.doneGate(t, workspace, repo, map[string]any{})
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "first")
	f.waitStatus(t, workspace, session, "error")
	require.NotEmpty(t, f.gateSince(t, workspace, session))
	f.pauseGate(t, workspace, gate)

	f.prompt(t, workspace, session, "second")
	f.waitStatus(t, workspace, session, "done")

	assert.Empty(t, f.gateSince(t, workspace, session))
	assert.Nil(t, f.held(t, workspace, session))
}

func TestDoneClearsGateSinceWithoutADoneGateForItsKind(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "turn", "sessions": []string{"child"}}, nil))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "done")

	assert.Empty(t, f.gateSince(t, workspace, session))
}

func TestDoneGateEditSavesFromTheSheet(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	var gate gateListed
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "turn"}, &gate))

	f.h.Event(t, workspace, "agents.gates.main", gate.ID+"/gateEdit", "gateEdit", nil)
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 1 })
	f.h.Event(t, workspace, "agents.sheet", "gateSheet/rounds", "gateRounds", "3")
	f.h.Event(t, workspace, "agents.sheet", "gateSheet/save", "gateSave", nil)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 0 })
	got := f.gates(t, workspace)[0]
	assert.Equal(t, []any{3, "turn"}, []any{got.Rounds, got.Point})
}

func (f *fixture) gateSince(t *testing.T, workspace, session string) string {
	t.Helper()
	var list []struct {
		GateSince string `json:"gateSince"`
	}
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"session": session}, &list))
	require.Len(t, list, 1)
	return list[0].GateSince
}

const writesNote = "echo x > note.md"

type heldTurn struct {
	Gate    string `json:"gate"`
	Key     string `json:"key"`
	Run     string `json:"run"`
	Round   int    `json:"round"`
	Last    string `json:"last"`
	Request string `json:"request"`
}

func (f *fixture) held(t *testing.T, workspace, session string) *heldTurn {
	t.Helper()
	var list []struct {
		Held *heldTurn `json:"held"`
	}
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"session": session}, &list))
	require.Len(t, list, 1)
	return list[0].Held
}

func (f *fixture) doneGate(t *testing.T, workspace, repo string, args map[string]any) string {
	t.Helper()
	var g gateListed
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "turn", "sessions": args["sessions"], "wrote": args["wrote"], "rounds": args["rounds"], "graph": args["graph"]}, &g))
	return g.ID
}

func (f *fixture) waitHeld(t *testing.T, workspace, session string) {
	t.Helper()
	testutil.Eventually(t, func() bool { return f.held(t, workspace, session) != nil }, wait)
	assert.Equal(t, "running", f.session(t, workspace, session).Status)
}

func (f *fixture) pauseGate(t *testing.T, workspace, id string) {
	t.Helper()
	require.NoError(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": id, "paused": true}, nil))
}

func doneTurns(t *testing.T, events []bus.Event, session string) int {
	t.Helper()
	n := 0
	for _, e := range events {
		var p struct {
			Session string `json:"session"`
			Status  string `json:"status"`
		}
		require.NoError(t, json.Unmarshal(e.Payload, &p))
		if p.Session == session && p.Status == "done" {
			n++
		}
	}
	return n
}

func TestDoneGateHoldsTurnUntilReleased(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"run": writesNote}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gate := f.doneGate(t, workspace, repo, nil)
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "write")
	f.waitHeld(t, workspace, session)
	testutil.Never(t, func() bool {
		return doneTurns(t, testutil.WaitEvents(t, f.h.Bus, "agents.turn", func([]bus.Event) bool { return true }), session) > 0
	}, quiet)
	f.pauseGate(t, workspace, gate)

	f.waitStatus(t, workspace, session, "done")
	testutil.WaitEvents(t, f.h.Bus, "agents.turn", func(events []bus.Event) bool { return doneTurns(t, events, session) == 1 })
	assert.Nil(t, f.held(t, workspace, session))
	assert.Empty(t, f.gateSince(t, workspace, session))
}

func TestHeldChildDoesNotWakeParent(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "work", "run": writesNote}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gate := f.doneGate(t, workspace, repo, map[string]any{"sessions": []string{"child"}})
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "plan")
	parentID := f.waitStatus(t, workspace, parent, "done").AgentSessionID
	kid := f.newChild(t, workspace, parent, map[string]any{"title": "Kid", "prompt": "work"}).Session
	f.waitHeld(t, workspace, kid)

	var waited waitedAll
	require.NoError(t, f.runAs(t, workspace, parent, "agents.wait", map[string]any{"sessions": []string{kid}, "timeout": 2}, &waited))
	assert.Equal(t, "running", waited.Sessions[0].Status)
	assert.ErrorContains(t, f.runAs(t, workspace, parent, "agents.cancel", map[string]any{"session": kid}, nil), "only the user can stop it")
	assert.NotNil(t, f.held(t, workspace, kid))
	assert.Empty(t, f.promptsTo(t, parentID, "[Clubmate] Child"))
	f.pauseGate(t, workspace, gate)

	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, "[Clubmate] Child")) > 0 }, wait)
	testutil.Never(t, func() bool { return len(f.promptsTo(t, parentID, "[Clubmate] Child")) > 1 }, quiet)
	assert.Equal(t, []string{`[Clubmate] Child "Kid" (` + kid + ") is done."}, f.promptsTo(t, parentID, "[Clubmate] Child"))
}

func TestHeldTurnSurvivesRestart(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"resume": true, "prompt": []map[string]any{
		{"match": "work", "run": writesNote},
		{"match": "work", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "Plan ready"}}},
	}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gate := f.doneGate(t, workspace, repo, map[string]any{"sessions": []string{"child"}})
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "plan")
	parentID := f.waitStatus(t, workspace, parent, "done").AgentSessionID
	kid := f.newChild(t, workspace, parent, map[string]any{"title": "Kid", "prompt": "work"}).Session
	kidID := f.agentID(t, workspace, kid)
	f.waitHeld(t, workspace, kid)

	f.restartPlugin(t, workspace)
	f.waitHeld(t, workspace, kid)
	f.pauseGate(t, workspace, gate)

	f.waitStatus(t, workspace, kid, "done")
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, "[Clubmate] Child")) == 1 }, wait)
	assert.Empty(t, f.promptsTo(t, kidID, cutOffText))
	type turnEvent struct {
		Session string `json:"session"`
		Turn    int    `json:"turn"`
		Status  string `json:"status"`
		Reply   string `json:"reply"`
	}
	want := turnEvent{Session: kid, Turn: 1, Status: "done", Reply: "Plan ready"}
	testutil.WaitEvents(t, f.h.Bus, "agents.turn", func(events []bus.Event) bool {
		return slices.ContainsFunc(events, func(e bus.Event) bool {
			var got turnEvent
			return json.Unmarshal(e.Payload, &got) == nil && got == want
		})
	})
}

func TestPromptThenNoPlanStillGated(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "plan", "run": "mkdir -p docs/plans && echo x > docs/plans/a.md"}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.doneGate(t, workspace, repo, map[string]any{"wrote": []string{"**/plans/*.md"}})
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "plan")
	f.waitHeld(t, workspace, session)
	since := f.gateSince(t, workspace, session)

	f.prompt(t, workspace, session, "talk")

	testutil.Eventually(t, func() bool {
		turns := f.transcript(t, workspace, session).Turns
		return len(turns) == 2 && turns[1].StopReason != "" && f.held(t, workspace, session) != nil
	}, wait)
	assert.Equal(t, "running", f.session(t, workspace, session).Status)
	assert.Equal(t, since, f.gateSince(t, workspace, session))
}

func TestBackgroundSettleIsGated(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{
		"prompt": []map[string]any{{"run": writesNote}, backgroundCall("b1"), {"shell": "sleep 30; :"}},
		"after":  followUp(1500),
	})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.doneGate(t, workspace, repo, nil)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "watch CI")
	require.Equal(t, "running", endedTurn(t, f, workspace, session).Status)
	require.Nil(t, f.held(t, workspace, session))

	f.waitHeld(t, workspace, session)
	testutil.Never(t, func() bool { return f.session(t, workspace, session).Status != "running" }, quiet)
}

func TestCancelledTurnNotHeld(t *testing.T) {
	t.Parallel()
	hold := filepath.Join(t.TempDir(), "hold")
	f := start(t, map[string]any{"prompt": []map[string]any{{"run": writesNote}, {"wait": hold}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.doneGate(t, workspace, repo, nil)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "write")
	testutil.Eventually(t, func() bool { return len(f.calls(t, "run")) > 0 }, wait)

	require.NoError(t, f.run(t, workspace, "agents.cancel", map[string]any{"session": session}, nil))

	f.waitStatus(t, workspace, session, "done")
	assert.Nil(t, f.held(t, workspace, session))
	assert.NotEmpty(t, f.gateSince(t, workspace, session))
}

func TestTurnWithRunningChildrenNotHeld(t *testing.T) {
	t.Parallel()
	hold, kidHold := filepath.Join(t.TempDir(), "hold"), filepath.Join(t.TempDir(), "kid")
	f := start(t, map[string]any{"prompt": []map[string]any{
		{"match": "lead", "run": writesNote},
		{"match": "lead", "wait": hold},
		{"match": "kid", "wait": kidHold},
	}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.doneGate(t, workspace, repo, nil)
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "lead")
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "kid"}).Session
	f.waitStatus(t, workspace, kid, "running")

	write(t, hold, "")

	f.waitStatus(t, workspace, parent, "done")
	assert.Nil(t, f.held(t, workspace, parent))
	write(t, kidHold, "")
}

func TestUnchangedTurnPasses(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.doneGate(t, workspace, repo, nil)
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "talk")

	f.waitStatus(t, workspace, session, "done")
	assert.Nil(t, f.held(t, workspace, session))
}

func TestReleaseRunsReview(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"run": writesNote}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gate := f.doneGate(t, workspace, repo, nil)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "write")
	f.waitHeld(t, workspace, session)
	require.False(t, f.session(t, workspace, session).Review)

	f.pauseGate(t, workspace, gate)

	testutil.Eventually(t, func() bool { return f.session(t, workspace, session).Review }, wait)
}

func TestCancelledTurnKeepsGateSinceWithoutADoneGateForItsKind(t *testing.T) {
	t.Parallel()
	hold := filepath.Join(t.TempDir(), "hold")
	f := start(t, map[string]any{"prompt": []map[string]any{{"run": "true"}, {"wait": hold}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.doneGate(t, workspace, repo, map[string]any{"sessions": []string{"child"}})
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	testutil.Eventually(t, func() bool { return len(f.calls(t, "run")) > 0 }, wait)

	require.NoError(t, f.run(t, workspace, "agents.cancel", map[string]any{"session": session}, nil))

	f.waitStatus(t, workspace, session, "done")
	assert.NotEmpty(t, f.gateSince(t, workspace, session))
}

func TestStopDropsHoldWithoutRelease(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"run": writesNote}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.doneGate(t, workspace, repo, nil)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "write")
	f.waitHeld(t, workspace, session)

	require.NoError(t, f.run(t, workspace, "agents.cancel", map[string]any{"session": session, "turn": 1}, nil))

	f.waitStatus(t, workspace, session, "idle")
	assert.Nil(t, f.held(t, workspace, session))
	assert.NotEmpty(t, f.gateSince(t, workspace, session))
	assert.Zero(t, doneTurns(t, testutil.WaitEvents(t, f.h.Bus, "agents.turn", func([]bus.Event) bool { return true }), session))
}

func TestCloseDropsHold(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"run": writesNote}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.doneGate(t, workspace, repo, nil)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "write")
	f.waitHeld(t, workspace, session)

	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": session}, nil))

	f.waitStatus(t, workspace, session, "closed")
	assert.Nil(t, f.held(t, workspace, session))
}

func TestGateNoLongerMatchingReleasesHold(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"run": writesNote}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gate := f.doneGate(t, workspace, repo, nil)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "write")
	f.waitHeld(t, workspace, session)

	require.NoError(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": gate, "wrote": []string{"**/plans/*.md"}}, nil))

	f.waitStatus(t, workspace, session, "done")
	assert.Nil(t, f.held(t, workspace, session))
}

func TestHeldSessionThatErrorsStaysErrorAfterRestart(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{
		"prompt": []map[string]any{{"run": writesNote}},
		"after":  []map[string]any{{"sleep": 1500}, {"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "more"}}}, {"exit": 1}},
	})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.doneGate(t, workspace, repo, nil)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "write")
	f.waitHeld(t, workspace, session)
	f.waitStatus(t, workspace, session, "error")
	testutil.Eventually(t, func() bool {
		raw, _, err := f.h.KV.Get(t.Context(), "agents", "session/"+workspace+"/"+session)
		require.NoError(t, err)
		return strings.Contains(string(raw), `"status":"error"`) && !strings.Contains(string(raw), `"held"`)
	}, wait)

	f.restartPlugin(t, workspace)

	testutil.Never(t, func() bool { return f.session(t, workspace, session).Status != "error" }, quiet)
	assert.Nil(t, f.held(t, workspace, session))
}

func TestStopAfterAPermissionOutlivedItsTurnIsNotHeld(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"run": writesNote}}, "after": []map[string]any{{"permission": permissionScript()}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.doneGate(t, workspace, repo, nil)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "write")
	testutil.Eventually(t, func() bool {
		turns := f.transcript(t, workspace, session).Turns
		return len(turns) == 1 && turns[0].StopReason != "" && f.session(t, workspace, session).Status == "waiting"
	}, wait)

	require.NoError(t, f.run(t, workspace, "agents.cancel", map[string]any{"session": session}, nil))

	f.waitStatus(t, workspace, session, "done")
	testutil.Never(t, func() bool { return f.held(t, workspace, session) != nil }, quiet)
}

const doneReview = "Clubmate Done review"

type doneSetup struct {
	f         *fixture
	repo      string
	workspace string
	session   string
	gate      string
}

func startDone(t *testing.T, gate map[string]any, script map[string]any, review map[string]any) doneSetup {
	t.Helper()
	return startDoneIn(t, nil, "", gate, script, review)
}

func startDoneIn(t *testing.T, env []string, graph string, gate map[string]any, script map[string]any, review map[string]any) doneSetup {
	t.Helper()
	if review == nil {
		review = map[string]any{"match": doneReview, "wait": filepath.Join(t.TempDir(), "never")}
	}
	steps := []map[string]any{review, {"match": "WORK", "run": writesNote}}
	if extra, ok := script["prompt"].([]map[string]any); ok {
		steps = append(steps, extra...)
	}
	script["prompt"] = steps
	f := startEnv(t, env, script, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	repo := newRepo(t)
	if graph != "" {
		writeGraph(t, repo, "held", graph)
		git(t, repo, "add", ".")
		git(t, repo, "commit", "-qm", "graph held")
		gate["graph"] = "held"
	}
	workspace := f.workspace(t, repo)
	d := doneSetup{f: f, repo: repo, workspace: workspace, gate: f.doneGate(t, workspace, repo, gate)}
	var res struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"title": "Planner"}, &res))
	d.session = res.Session
	f.prompt(t, workspace, d.session, "WORK")
	f.waitHeld(t, workspace, d.session)
	return d
}

func (d doneSetup) reviewer(t *testing.T) record {
	t.Helper()
	var found record
	testutil.Eventually(t, func() bool {
		var list []record
		require.NoError(t, d.f.run(t, d.workspace, "agents.sessions", map[string]any{}, &list))
		i := slices.IndexFunc(list, func(r record) bool {
			return r.GateFor != nil && r.GateFor.Lander == d.session && r.GateFor.Verdict && !r.GateFor.Done && r.Status != "closed"
		})
		if i >= 0 {
			found = list[i]
		}
		return i >= 0
	}, wait)
	return found
}

func (d doneSetup) verdict(t *testing.T, r record, outcome string, findings ...string) error {
	t.Helper()
	return d.f.runAs(t, d.workspace, r.ID, "agents.land-verdict", map[string]any{"key": r.GateFor.Key, "outcome": outcome, "findings": findings}, nil)
}

func (d doneSetup) held(t *testing.T) *heldTurn {
	t.Helper()
	return d.f.held(t, d.workspace, d.session)
}

func (d doneSetup) gatePrompts(t *testing.T) []string {
	t.Helper()
	return d.f.promptsTo(t, d.f.agentID(t, d.workspace, d.session), "[Clubmate] From the gate · ")
}

func TestDoneGateApprovedReleases(t *testing.T) {
	t.Parallel()
	d := startDone(t, nil, map[string]any{}, nil)
	r := d.reviewer(t)

	require.NoError(t, d.verdict(t, r, "approved"))

	d.f.waitStatus(t, d.workspace, d.session, "done")
	d.f.waitStatus(t, d.workspace, r.ID, "closed")
	assert.Nil(t, d.held(t))
	testutil.WaitEvents(t, d.f.h.Bus, "agents.turn", func(events []bus.Event) bool { return doneTurns(t, events, d.session) == 1 })
}

func laterCycle(release string) map[string]any {
	return map[string]any{"after": []map[string]any{
		{"match": "WORK", "run": "while [ ! -e " + release + " ]; do sleep 0.05; done; echo later > later.md"},
		{"match": "WORK", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "CI is green"}}},
		{"match": "WORK", "sleep": 1500},
		{"match": "WORK", "update": map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}},
	}}
}

func approveThenCycle(t *testing.T, gate map[string]any) (doneSetup, string) {
	t.Helper()
	cycle := filepath.Join(t.TempDir(), "cycle")
	d := startDone(t, gate, laterCycle(cycle), nil)
	require.NoError(t, d.verdict(t, d.reviewer(t), "approved"))
	d.f.waitStatus(t, d.workspace, d.session, "done")
	reviewed := d.f.gateSince(t, d.workspace, d.session)
	write(t, cycle, "")
	d.f.waitHeld(t, d.workspace, d.session)
	return d, reviewed
}

func TestCycleAfterAReviewedDoneIsHeldByAWroteGate(t *testing.T) {
	t.Parallel()
	d, reviewed := approveThenCycle(t, map[string]any{"wrote": []string{"**/*.md"}})

	assert.Equal(t, d.gate, d.held(t).Gate)
	assert.Equal(t, reviewed, d.f.gateSince(t, d.workspace, d.session))
	assert.Equal(t, git(t, d.repo, "rev-parse", "refs/clubmate/cp/"+d.session+"/reviewed"), reviewed)
}

func TestCycleAfterAReviewedDoneIsReviewedFromThatDone(t *testing.T) {
	t.Parallel()
	d, reviewed := approveThenCycle(t, nil)
	key := d.held(t).Key

	var prompts []string
	testutil.Eventually(t, func() bool {
		r := d.reviewer(t)
		if r.GateFor.Key != key {
			return false
		}
		prompts = d.f.promptsTo(t, d.f.agentID(t, d.workspace, r.ID), doneReview)
		return len(prompts) > 0
	}, wait)
	assert.Contains(t, prompts[0], "`git diff "+reviewed+" ")
	assert.Contains(t, prompts[0], "later.md")
	assert.NotContains(t, prompts[0], "note.md")
}

func TestDoneGateChangesPromptsWithFindings(t *testing.T) {
	t.Parallel()
	d := startDone(t, nil, map[string]any{"prompt": []map[string]any{{"match": "From the gate", "run": "echo y > note.md"}}}, nil)
	r := d.reviewer(t)

	require.NoError(t, d.verdict(t, r, "changes", "note.md:1 — says x"))

	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Round == 2 && h.Key != r.GateFor.Key }, wait)
	prompts := d.gatePrompts(t)
	require.Len(t, prompts, 1)
	assert.True(t, strings.HasPrefix(prompts[0], "[Clubmate] From the gate · "+filepath.Base(d.repo)+" · Done · any turn · round 1 of 2 · changes"), prompts[0])
	assert.Contains(t, prompts[0], "note.md:1 — says x")
	next := d.reviewer(t)
	assert.Equal(t, []string{r.GateFor.Run, d.held(t).Key}, []string{next.GateFor.Run, next.GateFor.Key})
}

func TestSameKeyAfterChangesCountsRound(t *testing.T) {
	t.Parallel()
	d := startDone(t, map[string]any{"rounds": 3}, map[string]any{}, nil)
	r := d.reviewer(t)

	require.NoError(t, d.verdict(t, r, "changes", "note.md:1 — says x"))

	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Request != "" }, wait)
	h := d.held(t)
	assert.Equal(t, []any{3, r.GateFor.Key, "changes"}, []any{h.Round, h.Key, h.Last})
	prompts := d.gatePrompts(t)
	require.Len(t, prompts, 2)
	assert.Contains(t, prompts[1], "round 2 of 3 · changes")
	assert.Contains(t, prompts[1], "note.md:1 — says x")
	assert.Equal(t, r.GateFor.Run, h.Run)
}

func TestWorktreeMovedWhileHeldReruns(t *testing.T) {
	t.Parallel()
	d := startDone(t, nil, map[string]any{}, nil)
	r := d.reviewer(t)
	write(t, filepath.Join(d.repo, "note.md"), "moved\n")

	err := d.verdict(t, r, "approved")

	assert.ErrorContains(t, err, "changed")
	moved := d.held(t)
	require.NotNil(t, moved)
	assert.NotEqual(t, r.GateFor.Key, moved.Key)
	next := d.reviewer(t)
	assert.Equal(t, []string{r.ID, r.GateFor.Run, moved.Key}, []string{next.ID, next.GateFor.Run, next.GateFor.Key})
}

func TestInterruptedRunRetriedOnce(t *testing.T) {
	t.Parallel()
	d := startDone(t, nil, map[string]any{}, nil)
	first := d.reviewer(t).GateFor.Run

	require.NoError(t, d.f.run(t, d.workspace, "agents.graph-stop", map[string]any{"id": first}, nil))
	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Run != "" && h.Run != first }, wait)
	require.NoError(t, d.f.run(t, d.workspace, "agents.graph-stop", map[string]any{"id": d.held(t).Run}, nil))

	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Request != "" }, wait)
	assert.Empty(t, d.held(t).Run)
	assert.Equal(t, "running", d.f.session(t, d.workspace, d.session).Status)
}

func TestDeleteGateReleasesHeldTurn(t *testing.T) {
	t.Parallel()
	d := startDone(t, nil, map[string]any{}, nil)
	r := d.reviewer(t)

	require.NoError(t, d.f.run(t, d.workspace, "agents.gate-delete", map[string]any{"id": d.gate}, nil))

	d.f.waitStatus(t, d.workspace, d.session, "done")
	d.f.waitStatus(t, d.workspace, r.ID, "closed")
	assert.Equal(t, "stopped", gateSetup{f: d.f, workspace: d.workspace}.runStatus(t, r.GateFor.Run))
}

func TestHeldCycleRecomputesKey(t *testing.T) {
	t.Parallel()
	cycle := filepath.Join(t.TempDir(), "cycle")
	d := startDone(t, nil, map[string]any{"after": []map[string]any{
		{"match": "WORK", "wait": cycle},
		{"match": "WORK", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "one more thing"}}},
		{"match": "WORK", "run": "echo z > note.md"},
		{"match": "WORK", "update": map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}},
	}}, nil)
	r := d.reviewer(t)

	write(t, cycle, "")

	testutil.Eventually(t, func() bool {
		h := d.held(t)
		return h != nil && h.Key != r.GateFor.Key && d.reviewer(t).GateFor.Key == h.Key
	}, wait)
	assert.Equal(t, r.GateFor.Run, d.held(t).Run)
}

func TestPauseWhileHeldCycleRunsReleasesAfterIt(t *testing.T) {
	t.Parallel()
	cycle, settle := filepath.Join(t.TempDir(), "cycle"), filepath.Join(t.TempDir(), "settle")
	d := startDone(t, nil, map[string]any{"after": []map[string]any{
		{"match": "WORK", "wait": cycle},
		{"match": "WORK", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "one more thing"}}},
		{"match": "WORK", "wait": settle},
		{"match": "WORK", "update": map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}},
	}}, nil)
	write(t, cycle, "")
	testutil.Eventually(t, func() bool {
		return slices.ContainsFunc(d.f.transcript(t, d.workspace, d.session).Entries, func(e entry) bool { return e.Text == "one more thing" })
	}, wait)

	d.f.pauseGate(t, d.workspace, d.gate)
	testutil.Never(t, func() bool { return d.f.session(t, d.workspace, d.session).Status != "running" }, quiet)
	write(t, settle, "")

	d.f.waitStatus(t, d.workspace, d.session, "done")
	assert.Nil(t, d.held(t))
}

func TestGateEditReholdsUnderAnotherMatchingGate(t *testing.T) {
	t.Parallel()
	d := startDone(t, map[string]any{"wrote": []string{"**/*.md"}}, map[string]any{}, nil)
	other := d.f.doneGate(t, d.workspace, d.repo, nil)
	require.Equal(t, d.gate, d.held(t).Gate)

	require.NoError(t, d.f.run(t, d.workspace, "agents.gate-update", map[string]any{"id": d.gate, "wrote": []string{"**/plans/*.md"}}, nil))

	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Gate == other }, wait)
	assert.Equal(t, "running", d.f.session(t, d.workspace, d.session).Status)
}

func TestReviewerTurnEndedThroughBackgroundIsNudged(t *testing.T) {
	t.Parallel()
	follow := filepath.Join(t.TempDir(), "follow")
	review := backgroundCall("b1")
	review["match"] = doneReview
	d := startDone(t, nil, map[string]any{
		"prompt": []map[string]any{{"match": doneReview, "shell": "sleep 30; :"}},
		"after": []map[string]any{
			{"match": doneReview, "wait": follow},
			{"match": doneReview, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "tests pass"}}},
			{"match": doneReview, "update": map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}},
		},
	}, review)
	r := d.reviewer(t)
	require.Equal(t, "running", endedTurn(t, d.f, d.workspace, r.ID).Status)

	write(t, follow, "")

	testutil.Eventually(t, func() bool {
		return len(d.f.promptsTo(t, d.f.agentID(t, d.workspace, r.ID), "Done review ended without a verdict")) == 1
	}, wait)
}

func TestStopHeldMidCycleThenPermissionSettlesIdle(t *testing.T) {
	t.Parallel()
	cycle, after := filepath.Join(t.TempDir(), "cycle"), filepath.Join(t.TempDir(), "after")
	d := startDone(t, nil, map[string]any{"after": []map[string]any{
		{"match": "WORK", "wait": cycle},
		{"match": "WORK", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "one more thing"}}},
		{"match": "WORK", "wait": after},
		{"match": "WORK", "permission": permissionScript()},
	}}, nil)
	write(t, cycle, "")
	testutil.Eventually(t, func() bool {
		return slices.ContainsFunc(d.f.transcript(t, d.workspace, d.session).Entries, func(e entry) bool { return e.Text == "one more thing" })
	}, wait)
	require.NoError(t, d.f.run(t, d.workspace, "agents.cancel", map[string]any{"session": d.session}, nil))
	write(t, after, "")

	waiting := d.f.waitStatus(t, d.workspace, d.session, "waiting")
	d.f.answerPermission(t, d.workspace, d.session, waiting.Pending[0])

	d.f.waitStatus(t, d.workspace, d.session, "idle")
	testutil.Never(t, func() bool { return d.f.session(t, d.workspace, d.session).Status != "idle" }, quiet)
}

const heldChecked = `{"nodes":[{"id":"lint","prompt":"Lint. {{input}}","start":true},{"id":"check","run":"cat note.md"},{"id":"review","prompt":"Review. {{input}}","outcomes":["approved","changes"]}],"edges":[{"from":"lint","to":"check"},{"from":"check","to":"review"}]}`

func TestDoneGateStepRunsOnHeldTree(t *testing.T) {
	t.Parallel()
	d := startDoneIn(t, allowHome(t, "Bash(cat note.md)"), heldChecked, map[string]any{}, map[string]any{}, map[string]any{"match": "Review.", "wait": filepath.Join(t.TempDir(), "never")})
	g := gateSetup{f: d.f, repo: d.repo, workspace: d.workspace, gate: d.gate}
	testutil.Eventually(t, func() bool { return d.held(t).Run != "" }, wait)

	assert.Equal(t, "x\n", g.stepOutput(t, d.held(t).Run, "check"))
	require.Len(t, g.gateTrees(t), 1)
	d.f.pauseGate(t, d.workspace, d.gate)

	d.f.waitStatus(t, d.workspace, d.session, "done")
	testutil.Eventually(t, func() bool { return len(g.gateTrees(t)) == 0 }, wait)
}

func TestDoneReviewNodeHasNoRow(t *testing.T) {
	t.Parallel()
	d := startDone(t, nil, map[string]any{}, nil)

	r := d.reviewer(t)

	testutil.Never(t, func() bool { return !d.f.session(t, d.workspace, r.ID).Hidden }, quiet)
}

func TestPauseReleasesAsFinished(t *testing.T) {
	t.Parallel()
	d := startDone(t, nil, map[string]any{}, nil)
	d.reviewer(t)

	d.f.pauseGate(t, d.workspace, d.gate)

	testutil.WaitEvents(t, d.f.h.Bus, "agents.turn", func(events []bus.Event) bool {
		return slices.ContainsFunc(events, func(e bus.Event) bool {
			var p struct {
				Session  string `json:"session"`
				Status   string `json:"status"`
				Finished bool   `json:"finished"`
			}
			return json.Unmarshal(e.Payload, &p) == nil && p.Session == d.session && p.Status == "done" && p.Finished
		})
	})
}

func TestCancelledReviewerSettlingStopsItsRun(t *testing.T) {
	t.Parallel()
	review := backgroundCall("b1")
	review["match"] = doneReview
	d := startDone(t, nil, map[string]any{"prompt": []map[string]any{{"match": doneReview, "shell": "sleep 30; :"}}}, review)
	r := d.reviewer(t)
	require.Equal(t, "running", endedTurn(t, d.f, d.workspace, r.ID).Status)

	require.NoError(t, d.f.run(t, d.workspace, "agents.cancel", map[string]any{"session": r.ID}, nil))

	testutil.Eventually(t, func() bool { return gateSetup{f: d.f, workspace: d.workspace}.runStatus(t, r.GateFor.Run) == "stopped" }, wait)
	assert.Empty(t, d.f.promptsTo(t, d.f.agentID(t, d.workspace, r.ID), "Done review ended without a verdict"))
}

func TestFixTurnWithoutASnapshotCountsRound(t *testing.T) {
	t.Parallel()
	d := startDone(t, map[string]any{"rounds": 3}, map[string]any{"prompt": []map[string]any{{"match": "From the gate", "run": "mv .git .git-away || true"}}}, nil)
	r := d.reviewer(t)

	require.NoError(t, d.verdict(t, r, "changes", "note.md:1 — says x"))

	testutil.Eventually(t, func() bool { return len(d.gatePrompts(t)) == 2 }, wait)
	assert.Contains(t, d.gatePrompts(t)[1], "round 2 of 3 · changes")
}

const heldReviewOnly = `{"nodes":[{"id":"review","prompt":"Review. {{input}}","start":true,"outcomes":["approved","changes"]}]}`

func TestNewGraphStartsAHoldWaitingForTrust(t *testing.T) {
	t.Parallel()
	d := startDoneIn(t, nil, heldReviewOnly, map[string]any{}, map[string]any{"prompt": []map[string]any{{"match": "WORK", "run": "sed -i '' 's/Review\\./Check./' .clubmate/graphs/held.json"}}}, nil)
	var waiting struct {
		Held *struct {
			Wait string `json:"wait"`
			Run  string `json:"run"`
		} `json:"held"`
		Landing *struct {
			Words string `json:"words"`
		} `json:"landing"`
	}
	testutil.Eventually(t, func() bool {
		var list []json.RawMessage
		require.NoError(t, d.f.run(t, d.workspace, "agents.sessions", map[string]any{"session": d.session}, &list))
		require.NoError(t, json.Unmarshal(list[0], &waiting))
		return waiting.Held != nil && waiting.Held.Wait != "" && waiting.Landing != nil && waiting.Landing.Words == "waits for trust"
	}, wait)

	require.NoError(t, d.f.run(t, d.workspace, "agents.gate-update", map[string]any{"id": d.gate, "graph": "fable-review"}, nil))

	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Run != "" }, wait)
}

func TestReheldTurnDoesNotReplayTheOldGatesVerdict(t *testing.T) {
	t.Parallel()
	d := startDone(t, map[string]any{"wrote": []string{"**/*.md"}, "rounds": 1}, map[string]any{}, nil)
	other := d.f.doneGate(t, d.workspace, d.repo, nil)
	require.NoError(t, d.verdict(t, d.reviewer(t), "changes", "note.md:1 — says x"))
	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Request != "" }, wait)
	write(t, filepath.Join(d.repo, "fresh.txt"), "new\n")
	objects := filepath.Join(d.repo, ".git", "objects")
	t.Cleanup(func() { _ = exec.Command("chmod", "-R", "u+w", objects).Run() })
	require.NoError(t, exec.Command("chmod", "-R", "a-w", objects).Run())

	d.f.pauseGate(t, d.workspace, d.gate)
	require.NoError(t, exec.Command("chmod", "-R", "u+w", objects).Run())

	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Gate == other && h.Run != "" }, wait)
	assert.Empty(t, d.held(t).Last)
	assert.Empty(t, d.gatePrompts(t))
}

func TestDoneGateCreatedFromTheSheetEditsWithoutInto(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	open := func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 1 }

	f.h.Event(t, workspace, "agents.gates.main", "help/gateNew", "gateNew", nil)
	f.h.WaitSlot(t, workspace, "agents.sheet", open)
	f.h.Event(t, workspace, "agents.sheet", "gateSheet/holds", "gateHolds", "turn")
	f.h.Event(t, workspace, "agents.sheet", "gateSheet/session/child", "gateSession", true)
	f.h.Event(t, workspace, "agents.sheet", "gateSheet/save", "gateSave", nil)
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 0 })
	got := f.gates(t, workspace)[0]
	f.h.Event(t, workspace, "agents.gates.main", got.ID+"/gateEdit", "gateEdit", nil)
	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", open)

	assert.Equal(t, []any{"turn", []string{"afk", "child", "top"}, []string{"**/plans/*.md", "**/specs/*.md"}}, []any{got.Point, got.Sessions, got.Wrote})
	assert.Equal(t, "Edit gate · Done · plans and specs", sheetNodes(sheet, "sheet")[0].Props["title"])
	assert.False(t, slices.ContainsFunc(sheetNodes(sheet, "select"), func(n sdui.Node) bool { return n.ID == "gateSheet/into" }))
}

func TestHeldSessionRowReadsHeld(t *testing.T) {
	t.Parallel()
	d := startDone(t, nil, map[string]any{}, nil)
	r := d.reviewer(t)

	testutil.Eventually(t, func() bool {
		var list []struct {
			Landing *struct {
				Words string `json:"words"`
				Badge string `json:"badge"`
				Line  string `json:"line"`
			} `json:"landing"`
		}
		require.NoError(t, d.f.run(t, d.workspace, "agents.sessions", map[string]any{"session": d.session}, &list))
		l := list[0].Landing
		return l != nil && l.Words == "held" && l.Badge == "held · any turn" && strings.HasPrefix(l.Line, "Held · any turn · ")
	}, wait)
	testutil.Eventually(t, func() bool { return d.f.sidebarItem(t, d.workspace, d.session).Detail == "held" }, wait)
	testutil.Eventually(t, func() bool {
		var h gateHistory
		require.NoError(t, d.f.run(t, d.workspace, "agents.gate-verdicts", map[string]any{"id": d.gate}, &h))
		return len(h.Landings) == 1 && h.Landings[0].State == "reviewing" && h.Landings[0].Reviewer == r.ID
	}, wait)
}

func TestHeldTurnWorkingItsChangesIsNotShownHeld(t *testing.T) {
	t.Parallel()
	d := startDone(t, map[string]any{"rounds": 2}, map[string]any{"prompt": []map[string]any{{"match": "From the gate", "wait": filepath.Join(t.TempDir(), "never")}}}, nil)
	r := d.reviewer(t)

	require.NoError(t, d.verdict(t, r, "changes", "plans/x.md:1 — say who owns it"))

	testutil.Eventually(t, func() bool { return len(d.gatePrompts(t)) == 1 }, wait)
	testutil.Eventually(t, func() bool {
		var list []struct {
			Landing *struct {
				Words string `json:"words"`
			} `json:"landing"`
		}
		require.NoError(t, d.f.run(t, d.workspace, "agents.sessions", map[string]any{"session": d.session}, &list))
		var h gateHistory
		require.NoError(t, d.f.run(t, d.workspace, "agents.gate-verdicts", map[string]any{"id": d.gate}, &h))
		return list[0].Landing == nil && len(h.Landings) == 0
	}, wait)
}
