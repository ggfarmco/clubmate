package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/toolgate"
)

func TestGateNodeAndNightSessionAreNeverHeld(t *testing.T) {
	t.Parallel()
	req := acp.RequestPermissionRequest{ToolCall: []byte(`{"toolCallId":"c","name":"Bash","rawInput":{"command":"git push"}}`)}
	for _, rec := range []record{{ID: "node", GateFor: &gateFor{Gate: "g"}}, {ID: "night", Night: "n"}} {
		s := newSession(rec, newEventLog(filepath.Join(t.TempDir(), rec.ID+".jsonl")), true, func(string, bool) {})
		_, held := (&app{}).holdCall(t.Context(), s, nil, "1", req)
		assert.False(t, held, rec.ID)
	}
}

func TestHeldCallIsAnsweredOnlyWithOnceOptions(t *testing.T) {
	t.Parallel()
	options := []acp.PermissionOption{{OptionID: "always", Kind: "allow_always"}, {OptionID: "once", Kind: "allow_once"}, {OptionID: "no", Kind: "reject_once"}}

	assert.Equal(t, acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "once"}, callOutcome(options, callAllow))
	assert.Equal(t, acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "no"}, callOutcome(options, callRefuse))
	assert.Equal(t, acp.OutcomeCancelled, callOutcome(options[:1], callAllow).Outcome)
	assert.Equal(t, acp.OutcomeCancelled, callOutcome(options, callCancelled).Outcome)
}

func TestCallHoldIsAHoldOfItsGate(t *testing.T) {
	t.Parallel()
	done, push, fetch := &heldTurn{Gate: "d"}, &heldTurn{Gate: "p", Round: 1}, &heldTurn{Gate: "f", Round: 2}
	rec := record{ID: "s", Held: done, Calls: []*callHold{{Hold: push, Call: "c1"}, {Hold: fetch}}}
	s := newSession(rec, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})

	assert.Equal(t, map[string]heldTurn{"": *done, "p": *push, "f": *fetch}, heldCopies(rec))
	assert.Len(t, s.holds(), 3)
	r := holdRef{s: s, gate: "p"}
	assert.Equal(t, gatePointTool, r.point())
	next := &heldTurn{Gate: "p", Round: 2}
	require.True(t, r.cas(push, next))
	assert.False(t, r.cas(push, next))
	assert.Same(t, next, r.get())
	assert.Equal(t, "c1", s.snapshot().call("p").Call)
	assert.Same(t, push, rec.Calls[0].Hold)
	require.True(t, r.cas(next, nil))
	assert.Nil(t, s.snapshot().call("p"))
	assert.Same(t, done, s.doneHold().get())
}

func TestNoLaterCallOvertakesOnAGate(t *testing.T) {
	t.Parallel()
	on := func(id string, gates ...string) *heldCall {
		c := &heldCall{id: id}
		for _, g := range gates {
			c.hits = append(c.hits, toolgate.Hit{Rule: toolgate.Rule{ID: g}})
		}
		return c
	}
	a, b, c, d := on("a", "x"), on("b", "x", "y"), on("c", "y"), on("d", "z")

	assert.Equal(t, []*heldCall{a, d}, nextCalls([]*heldCall{a, b, c, d}))
	a.taken = true
	assert.Empty(t, nextCalls([]*heldCall{a, b, c}))
	assert.Equal(t, []*heldCall{b}, nextCalls([]*heldCall{b, c}))
}

func TestGateTakenOffACallNotYetFilledInLeavesItToItsTake(t *testing.T) {
	t.Parallel()
	s := newSession(record{ID: "s"}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	c := &heldCall{id: "c", taken: true, reply: make(chan callAnswer, 1)}
	for _, g := range []string{"g", "h"} {
		c.hits = append(c.hits, toolgate.Hit{Rule: toolgate.Rule{ID: g}})
	}
	s.calls = []*heldCall{c}

	(&app{}).regateCalls(t.Context(), s, nil, "g", byGateDeleted)

	assert.Equal(t, []toolgate.Hit{{Rule: toolgate.Rule{ID: "h"}}}, c.hits)
	assert.Equal(t, []*heldCall{c}, s.calls)
	assert.Empty(t, c.reply)
}

func TestHeldCallKeepsTheSessionFromDetaching(t *testing.T) {
	t.Parallel()
	s := newSession(record{ID: "s", Status: statusDone}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	s.hookOwed, s.cycle = true, true
	free := []bool{s.idleLocked(0), s.hookCycleReadyLocked()}
	s.cycle = false
	free = append(free, s.quietLocked())
	require.Equal(t, []bool{true, true, true}, free)

	s.calls = []*heldCall{{id: "c"}}
	assert.False(t, s.idleLocked(0))
	assert.False(t, s.quietLocked())
	s.cycle = true
	assert.False(t, s.hookCycleReadyLocked())
}

func TestNoTurnRefusalReadsRefusedInTheHistory(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "refused: no turn", verdictLook(gateVerdict{Outcome: gateRefused, By: byNoTurn}, gatePointTool).Word)
}

func TestCallThatRanWithNoGatesReadIsJudgedByTheSavedRules(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t)
	h := scriptedHost(t, func(method, key string) any {
		if method == "storage.get" && key == gatesKey {
			return map[string]any{"found": true, "value": "unreadable"}
		}
		return nil
	})
	a := &app{host: h, stateDir: t.TempDir(), registry: newRegistry()}
	rules := filepath.Join(toolHookPath(a.toolHooksRoot(), repo), toolHookRules)
	write(t, rules, `[{"id":"gt_1","tool":"Bash","how":"command","text":"git push"}]`)
	past := time.Now().Add(-time.Minute)
	require.NoError(t, os.Chtimes(rules, past, past))
	s := newSession(record{ID: "agt_1", Workspace: "ws", Worktree: repo}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})

	a.judgeCall(s, endedCall{id: "c1", tool: "Bash", input: json.RawMessage(`{"command":"git push"}`), at: time.Now(), unhooked: true})

	history, err := a.readGateHistory(t.Context(), "gt_1")
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, []string{gateUngated, "git push"}, []string{history[0].Outcome, history[0].Call})

	a.judgeCall(s, endedCall{id: "c2", tool: "Bash", input: json.RawMessage(`{"command":"git push"}`), at: past.Add(-time.Minute), unhooked: true})

	history, err = a.readGateHistory(t.Context(), "gt_1")
	require.NoError(t, err)
	assert.Len(t, history, 1)
}

func TestCompletedCallIsNotJudgedWhileNoToolGateExists(t *testing.T) {
	t.Parallel()
	s := newSession(record{ID: "agt_1"}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	done := json.RawMessage(`{"toolCallId":"c1","status":"completed"}`)
	gated := false
	s.toolGated = func() bool { return gated }

	assert.Nil(t, s.endedCallLocked("", acp.UpdateToolCallUpdate, "", done))
	gated = true
	assert.NotNil(t, s.endedCallLocked("", acp.UpdateToolCallUpdate, "", done))
}

func TestUngatedCallDropsItsMark(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mark := filepath.Join(dir, toolHookMarks, "c1")
	require.NoError(t, os.MkdirAll(filepath.Dir(mark), 0o700))
	require.NoError(t, os.WriteFile(mark, nil, 0o600))
	s := newSession(record{ID: "agt_1"}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	s.toolGated = func() bool { return false }
	s.hookDir = dir

	c := s.endedCallLocked("", acp.UpdateToolCallUpdate, "", json.RawMessage(`{"toolCallId":"c1","status":"completed"}`))
	require.NotNil(t, c)
	(&app{}).judgeCall(s, *c)

	assert.NoFileExists(t, mark)
}

func owedSession(t *testing.T) (*session, gateMessage) {
	t.Helper()
	msg := gateMessage{Call: "c1", Tail: "again"}
	rec := record{ID: "s", Calls: []*callHold{{Hold: &heldTurn{Gate: "g", Last: gateChanges}, Sent: "c1", Message: &msg}}}
	return newSession(rec, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {}), msg
}

func queuedCalls(s *session) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, q := range s.queue {
		if q.Message != nil {
			out = append(out, q.Message.Call)
		}
	}
	return out
}

func TestUnqueuedGateMessageIsQueuedAgain(t *testing.T) {
	t.Parallel()
	s, _ := owedSession(t)
	s.running = true
	a := &app{}
	require.True(t, a.tellFindings(s, false))
	s.mu.Lock()
	s.unqueueMessageLocked("")
	s.mu.Unlock()
	require.Empty(t, queuedCalls(s))

	a.tellFindings(s, false)

	assert.Equal(t, []string{"c1"}, queuedCalls(s))
}

func TestCancelDuringAFailedAttachKeepsTheFindingsOwed(t *testing.T) {
	t.Parallel()
	s, msg := owedSession(t)
	s.mu.Lock()
	s.sending = &queuedPrompt{Text: msg.text(), Message: &msg}
	_, epoch := s.beginTurnLocked()
	s.cancelRequested = true

	stale := s.attachFailedLocked(epoch)

	got := *s.rec.Calls[0]
	s.mu.Unlock()
	assert.False(t, stale)
	assert.Empty(t, queuedCalls(s))
	assert.Equal(t, "c1", got.Sent)
	assert.False(t, got.Told)
	assert.True(t, s.tellOnce("c1"))
}

func TestRefusalWhileTheSameKeyMessageIsSendingDoesNotSendItTwice(t *testing.T) {
	t.Parallel()
	h := scriptedHost(t, func(string, string) any { return nil })
	a := &app{host: h, stateDir: t.TempDir(), registry: newRegistry()}
	sent := gateMessage{Call: "a1", Groups: []gateFindings{{Head: "A", Findings: []string{"finding A"}}}}
	rec := record{ID: "s", Calls: []*callHold{
		{Hold: &heldTurn{Gate: "g", Last: gateChanges, Findings: []string{"finding A"}}, Call: "b1", Sent: "a1", Message: &sent},
		{Hold: &heldTurn{Gate: "h", Last: gateChanges, Findings: []string{"finding B"}}, Call: "b1"},
	}}
	s := newSession(rec, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	s.sending = &queuedPrompt{Text: sent.text(), Message: &sent}
	c := &heldCall{id: "b1", request: "r1", taken: true, reply: make(chan callAnswer, 1), hits: []toolgate.Hit{{Rule: toolgate.Rule{ID: "g"}}}, decided: map[string]*callHold{"g": {Hold: &heldTurn{Gate: "g", Last: gateChanges, Findings: []string{"finding B"}}}}}
	s.calls = []*heldCall{c}

	a.refuseCall(s, c)

	s.mu.Lock()
	defer s.mu.Unlock()
	require.Len(t, s.gateMsgs["b1"].Groups, 1)
	assert.Equal(t, []string{"finding B"}, s.gateMsgs["b1"].Groups[0].Findings)
	assert.Equal(t, "a1", s.sending.Message.Call)
	assert.Equal(t, []string{"a1", "b1"}, []string{s.rec.Calls[0].Sent, s.rec.Calls[1].Sent})
	assert.False(t, s.rec.Calls[0].Told)
}

func TestRefusalMergesTheUnsentMessageOfTheSameKeyOnce(t *testing.T) {
	t.Parallel()
	h := scriptedHost(t, func(string, string) any { return nil })
	a := &app{host: h, stateDir: t.TempDir(), registry: newRegistry()}
	unsent := gateMessage{Call: "a1", Groups: []gateFindings{{Head: "A", Findings: []string{"finding A"}}}}
	rec := record{ID: "s", Calls: []*callHold{{Hold: &heldTurn{Gate: "g", Last: gateChanges, Findings: []string{"finding A"}}, Call: "b1", Sent: "a1", Message: &unsent}}}
	s := newSession(rec, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	s.gateMsgs = map[string]gateMessage{"a1": unsent}
	c := &heldCall{id: "b1", request: "r1", taken: true, reply: make(chan callAnswer, 1), hits: []toolgate.Hit{{Rule: toolgate.Rule{ID: "g"}}}, decided: map[string]*callHold{"g": {Hold: &heldTurn{Gate: "g", Last: gateChanges, Findings: []string{"finding B"}}}}}
	s.calls = []*heldCall{c}

	a.refuseCall(s, c)

	s.mu.Lock()
	defer s.mu.Unlock()
	assert.NotContains(t, s.gateMsgs, "a1")
	require.Len(t, s.gateMsgs["b1"].Groups, 2)
	assert.Equal(t, []string{"finding A"}, s.gateMsgs["b1"].Groups[0].Findings)
}

func TestGateTakenOffItsCallsLastMarkClearsTheMark(t *testing.T) {
	t.Parallel()
	s := newSession(record{ID: "s"}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	c := &heldCall{id: "c", taken: true, waiting: []string{"g", "h"}, reply: make(chan callAnswer, 1)}
	for _, g := range []string{"g", "h"} {
		c.hits = append(c.hits, toolgate.Hit{Rule: toolgate.Rule{ID: g}})
	}
	s.calls = []*heldCall{c}
	s.mu.Lock()
	s.remarkLocked(c, []callMark{{Gate: "g"}})
	s.mu.Unlock()

	(&app{}).regateCalls(t.Context(), s, nil, "g", byGateDeleted)

	s.mu.Lock()
	defer s.mu.Unlock()
	assert.Equal(t, []string{"h"}, c.waiting)
	en := s.toolEntryLocked("", "c")
	require.NotNil(t, en)
	assert.JSONEq(t, `null`, string(en.Tool["gate"]))
}

func TestStopWithNothingHeldSavesNothing(t *testing.T) {
	t.Parallel()
	saves := 0
	s := newSession(record{ID: "s", Status: statusDone}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(_ string, save bool) {
		if save {
			saves++
		}
	})

	(&app{}).endCalls(s, true)

	assert.Zero(t, saves)
}

func gitRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	out, err := exec.Command("git", "-C", repo, "init", "-q", "-b", "main").CombinedOutput()
	require.NoError(t, err, string(out))
	return repo
}

func TestPlaceOfAWorktreeIsItsRepo(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t)
	tree := filepath.Join(repo, ".worktrees", "feat")
	out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "--orphan", "-b", "feat", tree).CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.MkdirAll(filepath.Join(tree, "sub"), 0o700))

	primary, err := primaryRead(t.Context(), tree)
	require.NoError(t, err)
	got, ok := toolgate.PlaceOf(filepath.Join(tree, "sub"))
	require.True(t, ok)
	assert.Equal(t, toolgate.Place{Repo: primary, Tree: tree}, got)
}
