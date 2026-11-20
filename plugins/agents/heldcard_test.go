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

func (f *fixture) toolCard(t *testing.T, workspace, session string, holds int) record {
	t.Helper()
	return f.waitRecord(t, workspace, session, "the card", func(r record) bool {
		return len(r.Calls) == holds && !slices.ContainsFunc(r.Calls, func(c callRow) bool { return c.Request == "" || c.Request != r.Calls[0].Request })
	})
}

func (f *fixture) cardNote(t *testing.T, request string) {
	t.Helper()
	testutil.WaitEvents(t, f.h.Bus, "notify.posted", func(events []bus.Event) bool {
		return slices.ContainsFunc(events, func(e bus.Event) bool {
			var p struct{ Kind, Request string }
			require.NoError(t, json.Unmarshal(e.Payload, &p))
			return p == struct{ Kind, Request string }{"needs-input", request}
		})
	})
}

func resolvedRequests(t *testing.T, events []bus.Event, session string) []string {
	var out []string
	for _, e := range events {
		var c struct{ Session, Kind, Request, RequestKind string }
		require.NoError(t, json.Unmarshal(e.Payload, &c))
		if c.Session == session && c.Kind == "resolved" && c.RequestKind == "gate" {
			out = append(out, c.Request)
		}
	}
	return out
}

func (f *fixture) gateVerdicts(t *testing.T, workspace, gate string) [][2]string {
	t.Helper()
	var h gateHistory
	require.NoError(t, f.run(t, workspace, "agents.gate-verdicts", map[string]any{"id": gate}, &h))
	out := make([][2]string, 0, len(h.Verdicts))
	for _, v := range h.Verdicts {
		out = append(out, [2]string{v.Outcome, v.By})
	}
	return out
}

func (f *fixture) twoGateCard(t *testing.T) (string, string, string, string, record) {
	t.Helper()
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	push := f.addPushGate(t, workspace, repo).ID
	origin := f.addContainsGate(t, workspace, repo, "Bash", "origin").ID
	f.gateRounds(t, workspace, push, 1)
	f.gateRounds(t, workspace, origin, 2)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "alpha")
	f.waitHeldCall(t, workspace, session, 2)
	f.callChanges(t, workspace, f.callReviewer(t, workspace, session, push, ""), "p.go:1 — p")
	f.callChanges(t, workspace, f.callReviewer(t, workspace, session, origin, ""), "o.go:1 — o")
	return repo, workspace, session, push + " " + origin, f.toolCard(t, workspace, session, 2)
}

func TestToolGateCardAtCap(t *testing.T) {
	t.Parallel()
	f := start(t, callScript(bashSteps("alpha", "a1", "git push", false)))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gate := f.addPushGate(t, workspace, repo).ID
	f.gateRounds(t, workspace, gate, 1)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "alpha")
	f.waitHeldCall(t, workspace, session, 1)

	f.callChanges(t, workspace, f.callReviewer(t, workspace, session, gate, ""), "main.go:1 — breaks")

	req := f.toolCard(t, workspace, session, 1).Calls[0].Request
	rec := f.session(t, workspace, session)
	assert.Equal(t, []any{"running", true, []string{req}, true}, []any{rec.Status, rec.NeedsYou, rec.Pending, rec.Calls[0].Held})
	card := f.phoneTranscript(t, workspace, session).Request.Props
	require.NotNil(t, rec.PendingSummary)
	assert.Equal(t, card["title"], rec.PendingSummary.Text)
	assert.True(t, strings.HasSuffix(card["title"].(string), " still asks for changes after 1 round"), card["title"])
	assert.True(t, strings.HasPrefix(card["caption"].(string), filepath.Base(repo)+" · Tool call · git push · held since "), card["caption"])
	assert.Equal(t, []any{"git push\n\n- main.go:1 — breaks", "Run it without another review?"}, []any{card["detail"], card["question"]})
	options, _ := card["options"].([]any)
	var got [][3]string
	for _, o := range options {
		m, _ := o.(map[string]any)
		got = append(got, [3]string{m["id"].(string), m["label"].(string), m["shortcut"].(string)})
	}
	assert.Equal(t, [][3]string{{"merge", "Run anyway", "opt+1"}, {"more", "One more round", "opt+2"}, {"leave", "Stop", "opt+3"}}, got)
	testutil.Eventually(t, func() bool { return f.sidebarItem(t, workspace, session).Detail == "asking" }, wait)
	f.cardNote(t, req)
	require.Error(t, f.runAs(t, workspace, session, "agents.gate-answer", map[string]any{"request": req, "action": "accept"}, nil))
}

func TestTwoGatesAtCapShareOneCard(t *testing.T) {
	t.Parallel()
	f := start(t, callScript(bashSteps("alpha", "a1", "git push origin main", false)))

	_, workspace, session, _, card := f.twoGateCard(t)

	assert.Equal(t, []string{card.Calls[0].Request}, f.session(t, workspace, session).Pending)
	props := f.phoneTranscript(t, workspace, session).Request.Props
	assert.Equal(t, []any{"2 gates still ask for changes", "git push · 1 of 1 round · origin · 1 of 2 rounds"}, []any{props["title"], props["caption"]})
	detail, _ := props["detail"].(string)
	parts := strings.Split(detail, "\n\n")
	require.Len(t, parts, 3, detail)
	assert.Equal(t, "git push origin main", parts[0])
	assert.True(t, strings.HasPrefix(parts[1], "git push · ") && strings.HasSuffix(parts[1], "\n- p.go:1 — p"), parts[1])
	assert.True(t, strings.HasPrefix(parts[2], "origin · ") && strings.HasSuffix(parts[2], "\n- o.go:1 — o"), parts[2])
}

func TestOneMoreRoundRaisesOnlyTheCappedGatesCap(t *testing.T) {
	t.Parallel()
	f := start(t, callScript(bashSteps("alpha", "a1", "git push origin main", false)))
	_, workspace, session, gates, card := f.twoGateCard(t)
	push, origin, _ := strings.Cut(gates, " ")

	require.NoError(t, f.run(t, workspace, "agents.gate-answer", map[string]any{"request": card.Calls[0].Request, "action": "more"}, nil))

	testutil.Eventually(t, func() bool { return f.refused(t) == 1 }, wait)
	got := f.waitRecord(t, workspace, session, "cards answered", func(r record) bool {
		return len(r.Calls) == 2 && r.Calls[0].Request == "" && r.Calls[1].Request == ""
	})
	of := map[string]int{}
	for _, c := range got.Calls {
		of[c.Gate] = c.Of
	}
	assert.Equal(t, map[string]int{push: 2, origin: 2}, of)
	assert.Empty(t, f.session(t, workspace, session).Pending)
}

func TestRunAnywayOverridesEveryGateOnTheCall(t *testing.T) {
	t.Parallel()
	f := start(t, callScript(bashSteps("alpha", "a1", "git push origin main", false)))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	push := f.addPushGate(t, workspace, repo).ID
	origin := f.addContainsGate(t, workspace, repo, "Bash", "origin").ID
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "alpha")
	f.waitHeldCall(t, workspace, session, 2)
	first := f.callReviewer(t, workspace, session, push, "")
	require.NoError(t, f.run(t, workspace, "agents.graph-stop", map[string]any{"id": first.GateFor.Run}, nil))
	second := f.callReviewer(t, workspace, session, push, first.ID)
	require.NoError(t, f.run(t, workspace, "agents.graph-stop", map[string]any{"id": second.GateFor.Run}, nil))
	reviewing := f.callReviewer(t, workspace, session, origin, "")
	card := f.waitRecord(t, workspace, session, "push's card", func(r record) bool {
		return slices.ContainsFunc(r.Calls, func(c callRow) bool { return c.Gate == push && c.Request != "" })
	})
	assert.Equal(t, "Fable review ended without a verdict", f.phoneTranscript(t, workspace, session).Request.Props["title"])
	request := card.Calls[slices.IndexFunc(card.Calls, func(c callRow) bool { return c.Gate == push })].Request

	require.NoError(t, f.run(t, workspace, "agents.gate-answer", map[string]any{"request": request, "action": "accept"}, nil))

	testutil.Eventually(t, func() bool { return f.allowed(t) == 1 }, wait)
	f.waitRecord(t, workspace, session, "no hold", func(r record) bool { return len(r.Calls) == 0 })
	f.waitStatus(t, workspace, reviewing.ID, "closed")
	assert.Contains(t, f.gateVerdicts(t, workspace, origin), [2]string{"approved", "user"})
	assert.Contains(t, f.gateVerdicts(t, workspace, push), [2]string{"approved", "user"})
}

func TestStopClosesTheSessionsOtherCards(t *testing.T) {
	t.Parallel()
	script := pairScript()
	script["resume"] = true
	f := start(t, script)
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gates := f.heldPair(t, workspace, repo)
	for _, g := range gates {
		f.gateRounds(t, workspace, g, 1)
	}
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "push")
	f.waitHeldCall(t, workspace, session, 2)
	for _, g := range gates {
		f.callChanges(t, workspace, f.callReviewer(t, workspace, session, g, ""), "x")
	}
	cards := f.waitRecord(t, workspace, session, "two cards", func(r record) bool { return len(r.Pending) == 2 }).Pending
	var raw []byte
	testutil.Eventually(t, func() bool {
		stored, _, err := f.h.KV.Get(t.Context(), "agents", "session/"+workspace+"/"+session)
		require.NoError(t, err)
		raw = stored
		return strings.Contains(string(raw), cards[0]) && strings.Contains(string(raw), cards[1])
	}, wait)
	var rec map[string]any
	require.NoError(t, json.Unmarshal(raw, &rec))
	rec["landReviews"] = []map[string]any{{"into": "main", "source": "feat", "repo": repo, "request": "lg_0123456789abcdef", "reqOld": "a", "reqTree": "b", "since": time.Now().UTC()}}
	rec["updatedAt"] = time.Now().Add(time.Hour).UTC()
	raw, err := json.Marshal(rec)
	require.NoError(t, err)
	require.NoError(t, f.h.KV.Set(t.Context(), "agents", "session/"+session, raw))
	f.restartAgents(t, workspace)
	f.waitRecord(t, workspace, session, "three cards", func(r record) bool { return len(r.Pending) == 3 })

	require.NoError(t, f.run(t, workspace, "agents.gate-answer", map[string]any{"request": cards[0], "action": "stop"}, nil))

	testutil.WaitEvents(t, f.h.Bus, "agents.changed", func(events []bus.Event) bool {
		got := resolvedRequests(t, events, session)
		return slices.Contains(got, cards[0]) && slices.Contains(got, cards[1]) && slices.Contains(got, "lg_0123456789abcdef")
	})
	assert.Empty(t, f.session(t, workspace, session).Pending)
}

func TestCardResolveNamesItsRequest(t *testing.T) {
	t.Parallel()
	f := start(t, pairScript())
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gates := f.heldPair(t, workspace, repo)
	for _, g := range gates {
		f.gateRounds(t, workspace, g, 1)
	}
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "push")
	f.waitHeldCall(t, workspace, session, 2)
	f.callChanges(t, workspace, f.callReviewer(t, workspace, session, gates[0], ""), "x")
	f.callChanges(t, workspace, f.callReviewer(t, workspace, session, gates[1], ""), "y")
	got := f.waitRecord(t, workspace, session, "two cards", func(r record) bool { return len(r.Pending) == 2 })
	push := got.Calls[slices.IndexFunc(got.Calls, func(c callRow) bool { return c.Gate == gates[0] })].Request
	other := got.Calls[slices.IndexFunc(got.Calls, func(c callRow) bool { return c.Gate == gates[1] })].Request
	f.cardNote(t, push)
	f.cardNote(t, other)

	require.NoError(t, f.run(t, workspace, "agents.gate-answer", map[string]any{"request": push, "action": "accept"}, nil))

	testutil.WaitEvents(t, f.h.Bus, "agents.changed", func(events []bus.Event) bool {
		return slices.Contains(resolvedRequests(t, events, session), push)
	})
	assert.Equal(t, []string{other}, f.session(t, workspace, session).Pending)
	testutil.Never(t, func() bool {
		return slices.Contains(resolvedRequests(t, testutil.WaitEvents(t, f.h.Bus, "agents.changed", func([]bus.Event) bool { return true }), session), other)
	}, quiet)
}

func TestRunAnywayWithNobodyWaitingServesTheRetry(t *testing.T) {
	t.Parallel()
	first, w := withdrawn(t, "alpha", "a1", "git push")
	f := start(t, callScript(first, bashSteps("beta", "b1", "git push", false)))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gate := f.addPushGate(t, workspace, repo).ID
	f.gateRounds(t, workspace, gate, 1)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "alpha")
	f.waitHeldCall(t, workspace, session, 1)
	f.callChanges(t, workspace, f.callReviewer(t, workspace, session, gate, ""), "x")
	card := f.toolCard(t, workspace, session, 1)
	write(t, w, "")
	f.waitRecord(t, workspace, session, "the card without its call", func(r record) bool { return len(r.Calls) == 1 && !r.Calls[0].Held })

	require.NoError(t, f.run(t, workspace, "agents.gate-answer", map[string]any{"request": card.Calls[0].Request, "action": "accept"}, nil))

	f.waitRecord(t, workspace, session, "no hold", func(r record) bool { return len(r.Calls) == 0 && len(r.Pending) == 0 })
	assert.Equal(t, 0, f.allowed(t))
	f.agentPrompt(t, workspace, session, "beta")
	testutil.Eventually(t, func() bool { return f.allowed(t) == 1 }, wait)
	assert.Len(t, f.reviewPrompts(t), 1)
}

func TestChangesGateJoinsAnOpenCard(t *testing.T) {
	t.Parallel()
	f := start(t, callScript(bashSteps("alpha", "a1", "git push origin main", false)))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	push := f.addPushGate(t, workspace, repo).ID
	origin := f.addContainsGate(t, workspace, repo, "Bash", "origin").ID
	f.gateRounds(t, workspace, origin, 2)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "alpha")
	f.waitHeldCall(t, workspace, session, 2)
	reviewing := f.callReviewer(t, workspace, session, origin, "")
	first := f.callReviewer(t, workspace, session, push, "")
	require.NoError(t, f.run(t, workspace, "agents.graph-stop", map[string]any{"id": first.GateFor.Run}, nil))
	second := f.callReviewer(t, workspace, session, push, first.ID)
	require.NoError(t, f.run(t, workspace, "agents.graph-stop", map[string]any{"id": second.GateFor.Run}, nil))
	f.waitRecord(t, workspace, session, "push's card", func(r record) bool {
		return slices.ContainsFunc(r.Calls, func(c callRow) bool { return c.Gate == push && c.Request != "" })
	})

	f.callChanges(t, workspace, reviewing, "o.go:1 — o")

	card := f.toolCard(t, workspace, session, 2)
	assert.Len(t, card.Pending, 1)
	assert.Equal(t, "2 gates need you on this call", f.phoneTranscript(t, workspace, session).Request.Props["title"])
	assert.Equal(t, 0, f.refused(t))
}

func TestRetryBehindAnOpenCardShowsOnIt(t *testing.T) {
	t.Parallel()
	first, w := withdrawn(t, "alpha", "a1", "git push")
	f := start(t, callScript(first, bashSteps("beta", "b1", "git push origin feat", false)))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gate := f.addPushGate(t, workspace, repo).ID
	f.gateRounds(t, workspace, gate, 1)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "alpha")
	f.waitHeldCall(t, workspace, session, 1)
	f.callChanges(t, workspace, f.callReviewer(t, workspace, session, gate, ""), "x")
	card := f.toolCard(t, workspace, session, 1)
	write(t, w, "")
	f.waitRecord(t, workspace, session, "the card without its call", func(r record) bool { return len(r.Calls) == 1 && !r.Calls[0].Held })

	f.agentPrompt(t, workspace, session, "beta")

	got := f.waitRecord(t, workspace, session, "the retry on the card", func(r record) bool {
		return len(r.Calls) == 1 && r.Calls[0].Held && r.Calls[0].Request != ""
	})
	assert.NotEqual(t, card.Calls[0].Request, got.Calls[0].Request)
	assert.Equal(t, []string{got.Calls[0].Request}, got.Pending)
	detail, _ := f.phoneTranscript(t, workspace, session).Request.Props["detail"].(string)
	assert.True(t, strings.HasPrefix(detail, "git push origin feat\n\n"), detail)
	require.NoError(t, f.run(t, workspace, "agents.gate-answer", map[string]any{"request": got.Calls[0].Request, "action": "accept"}, nil))
	testutil.Eventually(t, func() bool { return f.allowed(t) == 1 }, wait)
}

func TestStopCancelsTheCallAndEndsTheTurn(t *testing.T) {
	t.Parallel()
	f := start(t, callScript(bashSteps("alpha", "a1", "git push", false)))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	gate := f.addPushGate(t, workspace, repo).ID
	f.gateRounds(t, workspace, gate, 1)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "alpha")
	f.waitHeldCall(t, workspace, session, 1)
	f.callChanges(t, workspace, f.callReviewer(t, workspace, session, gate, ""), "x")
	card := f.toolCard(t, workspace, session, 1)
	assert.Equal(t, "needs-you", f.session(t, workspace, session).Gate)

	require.NoError(t, f.run(t, workspace, "agents.gate-answer", map[string]any{"request": card.Calls[0].Request, "action": "stop"}, nil))

	f.answered(t, "cancelled", 1)
	f.waitCall(t, "session/cancel", func(fakeCall) bool { return true })
	got := f.waitRecord(t, workspace, session, "the turn ended", func(r record) bool { return r.Status != "running" })
	assert.Empty(t, got.Calls)
	assert.Empty(t, got.Pending)
	assert.Equal(t, 0, f.allowed(t))
	assert.Equal(t, [2]string{"stopped", "user"}, f.gateVerdicts(t, workspace, gate)[0])
}
