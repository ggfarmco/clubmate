package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/apns"
	"clubmate/pkg/pluginsdk"
)

func TestNewPresence(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want presence
	}{
		{"test mode here", map[string]string{testModeEnv: "1", presenceEnv: "here"}, fixedPresence(true)},
		{"test mode default", map[string]string{testModeEnv: "1"}, fixedPresence(false)},
		{"default", nil, macPresence{awayAfter: defaultAwayAfter}},
		{"override", map[string]string{awayAfterEnv: "10s"}, macPresence{awayAfter: 10e9}},
		{"bad override", map[string]string{awayAfterEnv: "soon"}, macPresence{awayAfter: defaultAwayAfter}},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, newPresence(func(k string) string { return c.env[k] }), c.name)
	}
}

func TestAtTheMacPushIsQuiet(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)
	f.srv.presence = fixedPresence(true)

	res := f.pushNotification(t, `{"id":"ntf_1","message":"m","kind":"question"}`)

	assert.Equal(t, "phone 200 quiet, at the Mac", res.Summary)
	aps := relay.requests()[0].body["payload"].(map[string]any)["aps"].(map[string]any)
	assert.NotContains(t, aps, "sound")
	assert.Equal(t, "passive", aps["interruption-level"])
}

func TestQuietDecisionHoldsForLiveAndReplay(t *testing.T) {
	f := newFixture(t)
	f.withRelay(t)
	streaming := f.pair(t, "phone-a")
	f.setPushToken(t, f.pairNamed(t, "phone-b"), "bb02", apns.EnvProduction)
	ev := notifyEvent(`{"id":"ntf_1","message":"m","kind":"done"}`)
	ev.Seq = 1
	f.backend.events = []pluginsdk.Event{ev}
	_, live := f.openEvents(t, streaming, "")
	frame(t, live)
	streamID(t, live)
	f.srv.presence = fixedPresence(true)

	(&app{ctx: t.Context(), srv: f.srv}).busEvent(t.Context(), nil, "", ev)
	f.srv.presence = fixedPresence(false)
	res := f.pushNotification(t, string(ev.Payload))

	want := `{"id":"ntf_1","message":"m","kind":"done","quiet":true}`
	assert.JSONEq(t, want, string(payloadOf(t, frame(t, live))))
	assert.Equal(t, "phone-b 200 quiet, at the Mac", res.Summary)
	_, replay := f.openEvents(t, streaming, "0")
	frame(t, replay)
	streamID(t, replay)
	assert.JSONEq(t, want, string(payloadOf(t, frame(t, replay))))
}

func TestFreePushDecidesEachTime(t *testing.T) {
	f := newFixture(t)
	f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)
	f.srv.presence = fixedPresence(true)
	_, err := f.srv.pushCommand(t.Context(), "prj_1", true, pushArgs{Message: "m"})
	require.NoError(t, err)
	f.srv.presence = fixedPresence(false)

	res, err := f.srv.pushCommand(t.Context(), "prj_1", true, pushArgs{Message: "m"})

	require.NoError(t, err)
	assert.Equal(t, "phone 200", res.Summary)
}

func TestLeavingEscalatesAPendingQuestionOnce(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	token := f.pairNamed(t, "phone")
	f.setPushToken(t, token, "aa01", apns.EnvProduction)
	f.backend.sessions = json.RawMessage(`[{"id":"agt_1","needsYou":true}]`)
	f.srv.presence = fixedPresence(true)
	f.pushNotification(t, `{"id":"ntf_1","session":"agt_1","collapse":"agt_1","message":"m","kind":"question"}`)
	f.srv.quiet = newQuietLog(f.kv, f.clock.Now)
	f.openEvents(t, token, "")

	f.srv.escalate(t.Context(), true)
	f.srv.presence = fixedPresence(false)
	f.srv.escalate(t.Context(), true)
	f.srv.escalate(t.Context(), true)

	reqs := relay.requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "agt_1", reqs[1].body["collapseID"])
	assert.Equal(t, "default", reqs[1].body["payload"].(map[string]any)["aps"].(map[string]any)["sound"])
}

func TestLeavingDoesNotEscalate(t *testing.T) {
	cases := []struct {
		name     string
		session  string
		kind     string
		sessions string
		enabled  bool
		before   func(f *fixture)
	}{
		{"done", "agt_1", "done", `[{"id":"agt_1","needsYou":true}]`, true, nil},
		{"answered", "agt_1", "question", `[{"id":"agt_1","needsYou":true}]`, true, func(f *fixture) {
			f.srv.quiet.resolved(t.Context(), agentsChanged{Session: "agt_1", Kind: changeResolved, RequestKind: "elicitation"})
		}},
		{"no longer needs you", "agt_1", "question", `[{"id":"agt_1","needsYou":false}]`, true, nil},
		{"muted since", "agt_1", "question", `[{"id":"agt_1","needsYou":true}]`, true, func(f *fixture) {
			f.backend.sessions = json.RawMessage(`[{"id":"agt_1","needsYou":true,"notifyEffective":"muted"}]`)
		}},
		{"access off", "agt_1", "question", `[{"id":"agt_1","needsYou":true}]`, false, nil},
		{"terminal session", "trm_1", "needs-input", `[]`, true, nil},
		{"gate no longer waits", "gt_1", "needs-input", `{"sessions":[{"id":"gt_1","needsYou":false}]}`, true, nil},
		{"gate trusted", "gt_1", "needs-input", `[]`, true, func(f *fixture) {
			f.srv.quiet.resolved(t.Context(), agentsChanged{Session: "gt_1", Kind: changeResolved, Request: "trust:gt_1", RequestKind: requestGate})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			relay := f.withRelay(t)
			f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)
			f.backend.sessions = json.RawMessage(c.sessions)
			f.srv.presence = fixedPresence(true)
			f.pushNotification(t, `{"id":"ntf_1","session":"`+c.session+`","message":"m","kind":"`+c.kind+`"}`)
			if c.before != nil {
				c.before(f)
			}
			f.srv.presence = fixedPresence(false)

			f.srv.escalate(t.Context(), c.enabled)

			assert.Len(t, relay.requests(), 1)
			assert.Empty(t, f.srv.quiet.pending(t.Context()))
		})
	}
}

func TestLeavingEscalatesOnlyTheNewestOfASession(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)
	f.backend.sessions = json.RawMessage(`[{"id":"agt_1","needsYou":true}]`)
	f.srv.presence = fixedPresence(true)
	f.pushNotification(t, `{"id":"ntf_1","session":"agt_1","collapse":"agt_1","message":"Asks you a question","kind":"needs-input"}`)
	f.clock.advance(time.Minute)
	f.pushNotification(t, `{"id":"ntf_2","session":"agt_1","collapse":"agt_1","message":"Has a question","kind":"question"}`)
	f.srv.presence = fixedPresence(false)

	f.srv.escalate(t.Context(), true)

	reqs := relay.requests()
	require.Len(t, reqs, 3)
	assert.Equal(t, "cmp|ntf_2", reqs[2].body["payload"].(map[string]any)["id"])
	assert.Empty(t, f.srv.quiet.pending(t.Context()))
}

func TestFailedEscalationIsTriedAgain(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)
	f.backend.sessions = json.RawMessage(`[{"id":"agt_1","needsYou":true}]`)
	f.srv.presence = fixedPresence(true)
	f.pushNotification(t, `{"id":"ntf_1","session":"agt_1","message":"m","kind":"question"}`)
	f.srv.presence = fixedPresence(false)
	relay.reply = `{"status":500,"reason":"InternalServerError"}`

	f.srv.escalate(t.Context(), true)
	relay.reply = `{"status":200,"reason":""}`
	f.srv.escalate(t.Context(), true)
	assert.Len(t, relay.requests(), 2, "waits before it tries again")
	f.clock.advance(20 * time.Second)
	f.srv.escalate(t.Context(), true)

	assert.Len(t, relay.requests(), 3)
	assert.Empty(t, f.srv.quiet.pending(t.Context()))
}

func TestFailedEscalationKeepsADroppedEntryGone(t *testing.T) {
	f := newFixture(t)
	f.srv.quiet.hold(t.Context(), notifyPayload{ID: "ntf_1", Session: "agt_1", Kind: "question"})
	e := f.srv.quiet.pending(t.Context())[0]
	f.srv.quiet.drop(t.Context(), "ntf_1")

	f.srv.quiet.failed(t.Context(), e)

	assert.Empty(t, f.srv.quiet.pending(t.Context()))
}

func TestQuietDecisionSurvivesARestart(t *testing.T) {
	f := newFixture(t)
	f.srv.quiet.decide(t.Context(), "ntf_1", func() bool { return true })
	restarted := newQuietLog(f.kv, f.clock.Now)

	assert.True(t, restarted.quieted(t.Context(), "ntf_1"))
	assert.True(t, restarted.decide(t.Context(), "ntf_1", func() bool { return false }))
	f.clock.advance(pushMaxAge + time.Second)
	assert.False(t, restarted.quieted(t.Context(), "ntf_1"))
	assert.False(t, restarted.decide(t.Context(), "ntf_1", func() bool { return false }))
}

func TestLiveEventStaysLoudWithoutARelay(t *testing.T) {
	f := newFixture(t)
	_, live := f.openEvents(t, f.pair(t, "phone"), "")
	frame(t, live)
	streamID(t, live)
	f.srv.presence = fixedPresence(true)
	ev := notifyEvent(`{"id":"ntf_1","message":"m","kind":"question"}`)
	ev.Seq = 1

	(&app{ctx: t.Context(), srv: f.srv}).busEvent(t.Context(), nil, "", ev)

	assert.JSONEq(t, `{"id":"ntf_1","message":"m","kind":"question"}`, string(payloadOf(t, frame(t, live))))
}

func payloadOf(t *testing.T, fr string) json.RawMessage {
	t.Helper()
	var got wireEvent
	require.NoError(t, json.Unmarshal([]byte(strings.SplitN(fr, "data: ", 2)[1]), &got))
	return got.Payload
}
