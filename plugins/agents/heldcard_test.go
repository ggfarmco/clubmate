package main_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
)

const fixesNote = "echo y > note.md"

func (d doneSetup) cardAtCap(t *testing.T) string {
	t.Helper()
	require.NoError(t, d.verdict(t, d.reviewer(t), "changes", "note.md:1 — says x"))
	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Request != "" }, wait)
	return d.held(t).Request
}

func (d doneSetup) answer(t *testing.T, request, action string) {
	t.Helper()
	require.NoError(t, d.f.run(t, d.workspace, "agents.gate-answer", map[string]any{"request": request, "action": action}, nil))
}

func (d doneSetup) card(t *testing.T) map[string]any {
	t.Helper()
	return d.f.phoneTranscript(t, d.workspace, d.session).Request.Props
}

func TestDoneGateCardAtCap(t *testing.T) {
	t.Parallel()
	d := startDone(t, map[string]any{"rounds": 1}, map[string]any{}, nil)

	req := d.cardAtCap(t)

	rec := d.f.session(t, d.workspace, d.session)
	assert.Equal(t, []any{"running", true, []string{req}, "needsYou", true}, []any{rec.Status, rec.NeedsYou, rec.Pending, rec.Group, rec.TurnHeld})
	require.NotNil(t, rec.HeldRound)
	assert.Equal(t, "Fable review · round 1 changes", rec.HeldRound.Text)
	assert.Equal(t, d.held(t).Run, rec.HeldRound.Run)
	var list []record
	require.NoError(t, d.f.run(t, d.workspace, "agents.sessions", map[string]any{}, &list))
	assert.Len(t, slices.DeleteFunc(list, func(r record) bool { return !r.NeedsYou }), 1)
	testutil.Eventually(t, func() bool { return d.f.sidebarItem(t, d.workspace, d.session).Detail == "asking" }, wait)
	assert.Equal(t, []string{req}, d.f.phoneSession(t, d.workspace, d.session).Requests)
	card := d.card(t)
	assert.Equal(t, "Fable review still asks for changes after 1 round", card["title"])
	assert.True(t, strings.HasPrefix(card["caption"].(string), filepath.Base(d.repo)+" · Done · any turn · held since "), card["caption"])
	assert.Contains(t, card["detail"], "note.md:1 — says x")
	options, _ := card["options"].([]any)
	var got [][3]string
	for _, o := range options {
		m, _ := o.(map[string]any)
		got = append(got, [3]string{m["id"].(string), m["label"].(string), m["shortcut"].(string)})
	}
	assert.Equal(t, [][3]string{{"merge", "Accept as done", "opt+1"}, {"more", "One more round", "opt+2"}, {"leave", "Stop", "opt+3"}}, got)
}

func TestOneMoreRoundAfterChangesPromptsTheFindings(t *testing.T) {
	t.Parallel()
	d := startDone(t, map[string]any{"rounds": 1}, map[string]any{"prompt": []map[string]any{{"match": "From the gate", "run": fixesNote}}}, nil)
	req := d.cardAtCap(t)

	d.answer(t, req, "more")

	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Round == 2 && h.Request == "" }, wait)
	prompts := d.gatePrompts(t)
	require.Len(t, prompts, 1)
	assert.Contains(t, prompts[0], "round 1 of 2 · changes")
	assert.Contains(t, prompts[0], "note.md:1 — says x")
	assert.False(t, d.f.session(t, d.workspace, d.session).NeedsYou)
}

func TestNoVerdictCardOneMoreRoundReruns(t *testing.T) {
	t.Parallel()
	d := startDone(t, nil, map[string]any{}, nil)
	stopped := []string{d.reviewer(t).GateFor.Run}
	require.NoError(t, d.f.run(t, d.workspace, "agents.graph-stop", map[string]any{"id": stopped[0]}, nil))
	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Run != "" && h.Run != stopped[0] }, wait)
	stopped = append(stopped, d.held(t).Run)
	require.NoError(t, d.f.run(t, d.workspace, "agents.graph-stop", map[string]any{"id": stopped[1]}, nil))
	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Request != "" }, wait)
	assert.Equal(t, "Fable review ended without a verdict", d.card(t)["title"])

	d.answer(t, d.held(t).Request, "more")

	testutil.Eventually(t, func() bool {
		h := d.held(t)
		return h != nil && h.Request == "" && h.Run != "" && !slices.Contains(stopped, h.Run)
	}, wait)
	assert.Equal(t, 1, d.held(t).Round)
	assert.Empty(t, d.gatePrompts(t))
}

func TestAcceptAsDoneReleasesAndApprovesTheKey(t *testing.T) {
	t.Parallel()
	edit := map[string]any{"sessionUpdate": "tool_call", "toolCallId": "w2", "title": "Write note.md", "kind": "edit", "status": "completed", "locations": []map[string]any{{"path": "note.md"}}}
	d := startDone(t, map[string]any{"rounds": 1}, map[string]any{"prompt": []map[string]any{{"match": "AGAIN", "update": edit}}}, nil)
	req := d.cardAtCap(t)

	require.NoError(t, d.f.cardEvent(t, d.workspace, d.session+"/landgate/"+req, "landAnswer", map[string]any{"request": req, "action": "merge"}))

	d.f.waitStatus(t, d.workspace, d.session, "done")
	assert.Nil(t, d.held(t))
	assert.False(t, d.f.session(t, d.workspace, d.session).NeedsYou)
	testutil.WaitEvents(t, d.f.h.Bus, "agents.turn", func(events []bus.Event) bool { return doneTurns(t, events, d.session) == 1 })
	var h gateHistory
	require.NoError(t, d.f.run(t, d.workspace, "agents.gate-verdicts", map[string]any{"id": d.gate}, &h))
	require.NotEmpty(t, h.Verdicts)
	assert.Equal(t, []string{"approved", "user"}, []string{h.Verdicts[0].Outcome, h.Verdicts[0].By})
	assert.Error(t, d.f.run(t, d.workspace, "agents.gate-answer", map[string]any{"request": req, "action": "accept"}, nil))

	d.f.prompt(t, d.workspace, d.session, "WORK AGAIN")

	testutil.WaitEvents(t, d.f.h.Bus, "agents.turn", func(events []bus.Event) bool { return doneTurns(t, events, d.session) == 2 })
}

func TestStopLeavesSessionIdle(t *testing.T) {
	t.Parallel()
	d := startDone(t, map[string]any{"rounds": 1}, map[string]any{}, nil)
	reviewer := d.reviewer(t)
	req := d.cardAtCap(t)
	require.Error(t, d.f.runAs(t, d.workspace, d.session, "agents.gate-answer", map[string]any{"request": req, "action": "stop"}, nil))

	d.answer(t, req, "stop")

	d.f.waitStatus(t, d.workspace, d.session, "idle")
	d.f.waitStatus(t, d.workspace, reviewer.ID, "closed")
	assert.Nil(t, d.held(t))
	assert.NotEmpty(t, d.f.gateSince(t, d.workspace, d.session))
	testutil.Never(t, func() bool {
		return doneTurns(t, testutil.WaitEvents(t, d.f.h.Bus, "agents.turn", func([]bus.Event) bool { return true }), d.session) > 0
	}, quiet)
}

func TestDoneGateStepApprovalAsksUnderTheHeldSession(t *testing.T) {
	t.Parallel()
	release := filepath.Join(t.TempDir(), "release")
	write(t, release, "")
	d := startDoneIn(t, nil, gateWhere, map[string]any{}, map[string]any{}, map[string]any{"match": doneReview, "wait": release})

	held, need := gateSetup{f: d.f, workspace: d.workspace, lander: d.session}.waitAsking(t)

	assert.Equal(t, []any{"where wants to run pwd", d.session, "Round 1 review"}, []any{held.Landing.Ask, need.RowUnder, need.Title})
	var history gateHistory
	require.NoError(t, d.f.run(t, d.workspace, "agents.gate-verdicts", map[string]any{"id": d.gate}, &history))
	require.Len(t, history.Landings, 1)
	assert.Equal(t, []string{d.session, "needs-you"}, []string{history.Landings[0].Lander, history.Landings[0].State})
}

func TestQueuedGatePromptKeepsTheHoldAcrossARestart(t *testing.T) {
	t.Parallel()
	d := startDone(t, nil, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "From the gate", "run": fixesNote}}}, nil)
	raw, _, err := d.f.h.KV.Get(t.Context(), "agents", "session/"+d.workspace+"/"+d.session)
	require.NoError(t, err)
	var rec map[string]any
	require.NoError(t, json.Unmarshal(raw, &rec))
	h, _ := rec["held"].(map[string]any)
	require.NotNil(t, h)
	h["last"], h["findings"] = "changes", []string{"note.md:1 — says x"}
	text := "[Clubmate] From the gate · " + filepath.Base(d.repo) + " · Done · any turn · round 1 of 2 · changes\n- note.md:1 — says x"
	rec["queue"] = []map[string]any{{"id": "q1", "text": text, "gate": true}}
	rec["updatedAt"] = time.Now().Add(time.Hour).UTC()
	raw, err = json.Marshal(rec)
	require.NoError(t, err)
	require.NoError(t, d.f.h.KV.Set(t.Context(), "agents", "session/"+d.session, raw))

	d.f.restartAgents(t, d.workspace)

	testutil.Eventually(t, func() bool { h := d.held(t); return h != nil && h.Round == 2 }, wait)
	prompts := d.gatePrompts(t)
	require.Len(t, prompts, 1)
	assert.Equal(t, 1, strings.Count(prompts[0], "From the gate"))
}

func TestDocsQueuedByACutOffTurnShowAfterRestart(t *testing.T) {
	t.Parallel()
	g := startGatedDocs(t, nil)
	require.Equal(t, queuedNote, g.agentDoc(t, "docs.open"))
	testutil.Eventually(t, func() bool { return g.queued(t) }, wait)

	g.restartAgents(t, g.ws)

	testutil.Eventually(t, func() bool { return g.shown(t, "docs.opened") }, wait)
}

func TestDocsStayQueuedAcrossALimitWait(t *testing.T) {
	t.Parallel()
	g := gatedDocs{hold: filepath.Join(t.TempDir(), "hold")}
	script := []map[string]any{{"run": writesNote}, {"wait": g.hold}, {"fail": limitError()}}
	g.fixture = start(t, map[string]any{"resume": true, "prompt": script},
		testutil.BuildPluginDir(t, "clubmate/plugins/docs", "plugins/docs/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"))
	repo := newRepo(t)
	g.spec = filepath.Join(repo, "spec.md")
	write(t, g.spec, "# Spec\n")
	g.ws = g.workspace(t, repo)
	g.gate = g.doneGate(t, g.ws, repo, nil)
	g.sid = g.newSession(t, g.ws)
	g.prompt(t, g.ws, g.sid, "write")
	testutil.Eventually(t, func() bool { return len(g.calls(t, "run")) > 0 }, wait)
	require.Equal(t, queuedNote, g.agentDoc(t, "docs.open"))

	write(t, g.hold, "")

	g.waitRecord(t, g.ws, g.sid, "a scheduled wait", func(r record) bool { return r.LimitWait != nil && r.LimitWait.Job != "" })
	testutil.Never(t, func() bool { return g.shown(t, "docs.opened") }, quiet)
}
