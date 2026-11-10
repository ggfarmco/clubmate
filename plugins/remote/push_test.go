package main

import (
	"cmp"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/apns"
	"clubmate/pkg/pluginsdk"
)

const relayToken = "cmr_test"

type fakeRelay struct {
	reply   string
	devices int
	refuse  int

	mu         sync.Mutex
	reqs       []relayCall
	registered [][]string
}

type relayCall struct {
	path string
	auth string
	body map[string]any
}

func TestPushGoesToTokensWithoutAnOpenStream(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	first, streaming, taker := f.pairNamed(t, "phone-a"), f.pairNamed(t, "phone-b"), f.pairNamed(t, "phone-c")
	f.setPushToken(t, first, "aa01", apns.EnvProduction)
	f.setPushToken(t, streaming, "bb02", apns.EnvProduction)
	_, reply := f.call(t, taker, "pushToken", pushTokenRequest{Token: "aa01", Environment: apns.EnvDevelopment})
	assert.JSONEq(t, `{"push":true}`, string(reply.Result))
	f.openEvents(t, streaming, "")

	res := f.pushNotification(t, `{"id":"ntf_1","workspace":"prj_1","session":"agt_1","message":"Run tests?","kind":"permission"}`)

	assert.Equal(t, "phone-b skipped, stream open · phone-c 200", res.Summary)
	reqs := relay.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "/v1/push", reqs[0].path)
	assert.Equal(t, "Bearer "+relayToken, reqs[0].auth)
	assert.Equal(t, map[string]any{
		"token":       "aa01",
		"environment": "development",
		"type":        "alert",
		"collapseID":  "ntf_1",
		"payload": map[string]any{
			"aps": map[string]any{
				"alert":     map[string]any{"title": "Mac · demo", "subtitle": "Permission needed", "body": "Run tests?"},
				"sound":     "default",
				"thread-id": "cmp",
			},
			"id":        "cmp|ntf_1",
			"computer":  "cmp",
			"workspace": "prj_1",
			"session":   "agt_1",
			"kind":      "permission",
		},
	}, reqs[0].body)
}

func TestRelayKnowsWhichDevicesThisMacPushesTo(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	a, b := f.pairNamed(t, "phone-a"), f.pairNamed(t, "phone-b")
	f.setPushToken(t, a, "AA01", apns.EnvProduction)
	f.setPushToken(t, b, "bb02", apns.EnvProduction)
	f.setPushToken(t, b, "", "")
	f.setPushToken(t, b, "bb03", apns.EnvProduction)
	require.NoError(t, f.srv.revoke(t.Context(), "phone-a"))

	assert.Equal(t, [][]string{{}, {"aa01"}, {"aa01", "bb02"}, {"aa01"}, {"aa01", "bb03"}, {"bb03"}}, relay.registrations())
	f.pushNotification(t, `{"id":"ntf_1","message":"m","kind":"done"}`)
	assert.Len(t, relay.registrations(), 6)
}

func TestPushRefusedForAnUnregisteredDeviceRegistersAgainAndRetriesOnce(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)
	relay.refuse = 1

	res := f.pushNotification(t, `{"id":"ntf_1","message":"m","kind":"done"}`)

	assert.Equal(t, "phone 200", res.Summary)
	assert.Len(t, relay.requests(), 2)
	assert.Equal(t, []string{"aa01"}, relay.registrations()[len(relay.registrations())-1])
	assert.Len(t, relay.registrations(), 3)
	assert.Empty(t, f.notifyPosts())
}

func TestPushWorksWithARelayWithoutDeviceRegistration(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	relay.devices = http.StatusNotFound
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)

	res := f.pushNotification(t, `{"id":"ntf_1","message":"m","kind":"done"}`)

	assert.Equal(t, "phone 200", res.Summary)
	assert.Empty(t, f.notifyPosts())
}

func TestReplayMarksWhatWasPushedToThatDevice(t *testing.T) {
	f := newFixture(t)
	f.withRelay(t)
	pushed, silent := f.pair(t, "phone-a"), f.pair(t, "phone-b")
	f.setPushToken(t, pushed, "aa01", apns.EnvProduction)
	ev := notifyEvent(`{"id":"ntf_1","message":"m","kind":"done"}`)
	ev.Seq = 1
	f.backend.events = []pluginsdk.Event{ev}
	f.pushNotification(t, string(ev.Payload))

	for token, want := range map[string]string{pushed: `{"id":"ntf_1","message":"m","kind":"done","pushed":true}`, silent: `{"id":"ntf_1","message":"m","kind":"done"}`} {
		_, r := f.openEvents(t, token, "0")
		frame(t, r)
		streamID(t, r)
		var got wireEvent
		require.NoError(t, json.Unmarshal([]byte(strings.SplitN(frame(t, r), "data: ", 2)[1]), &got))
		assert.JSONEq(t, want, string(got.Payload))
	}
}

func TestPushFollowsTheSessionPolicy(t *testing.T) {
	tests := []struct {
		policy  string
		kind    string
		summary string
	}{
		{"muted", "permission", "held: session muted"},
		{"needs-input", "done", "held: session needs-input only"},
		{"needs-input", "question", "phone 200"},
		{"muted", "done always", "phone 200"},
	}
	for _, tt := range tests {
		t.Run(tt.policy+" "+tt.kind, func(t *testing.T) {
			f := newFixture(t)
			relay := f.withRelay(t)
			f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)
			f.backend.sessions = json.RawMessage(`[{"id":"agt_1","notifyEffective":"` + tt.policy + `"}]`)

			kind, always := strings.CutSuffix(tt.kind, " always")
			res := f.pushNotification(t, `{"id":"ntf_1","session":"agt_1","message":"m","kind":"`+kind+`","always":`+strconv.FormatBool(always)+`}`)

			assert.Equal(t, tt.summary, res.Summary)
			assert.Equal(t, tt.summary == "phone 200", len(relay.requests()) == 1)
			assert.Equal(t, []runCall{{id: sessionsCommand, workspace: "prj_1", args: `{"session":"agt_1"}`}}, f.backend.calls())
		})
	}
}

func TestPushCommandGates(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		relay   bool
		token   bool
		ts      time.Duration
		want    string
	}{
		{"access off", false, true, true, 0, "companion access off"},
		{"no relay", true, false, true, 0, "no relay set"},
		{"stale", true, true, true, 2 * time.Hour, "held: older than 1h"},
		{"no token", true, true, false, 0, "no device with a token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.relay {
				f.withRelay(t)
			}
			phone := f.pairNamed(t, "phone")
			if tt.token {
				f.setPushToken(t, phone, "aa01", apns.EnvProduction)
			}
			ts := f.clock.Now().Add(-tt.ts).Format(time.RFC3339)

			res, err := f.srv.pushCommand(t.Context(), "prj_1", tt.enabled, pushArgs{Notification: `{"id":"ntf_1","message":"m","kind":"done","ts":"` + ts + `"}`})

			require.NoError(t, err)
			assert.Equal(t, pushResult{Summary: tt.want}, res)
		})
	}
}

func TestPushCommandPushesAnyKind(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)

	f.pushNotification(t, `{"id":"ntf_1","message":"m","kind":"info"}`)

	reqs := relay.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "Info", alertOf(reqs[0])["subtitle"])
}

func TestPushCommandWithoutSession(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)

	res := f.pushNotification(t, `{"id":"ntf_1","workspace":"prj_1","title":"Hook","message":"m","kind":"done"}`)

	assert.Equal(t, "phone 200", res.Summary)
	reqs := relay.requests()
	require.Len(t, reqs, 1)
	assert.NotContains(t, reqs[0].body["payload"], "session")
	assert.Empty(t, f.backend.calls())
}

func TestPushCollapsesOnTheNotificationsGroup(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)

	f.pushNotification(t, `{"id":"ntf_1","session":"agt_1","collapse":"agt_1","message":"Has a question","kind":"question"}`)
	f.pushNotification(t, `{"id":"ntf_2","session":"agt_1","collapse":"agt_1","message":"Finished","kind":"done"}`)

	reqs := relay.requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "agt_1", reqs[0].body["collapseID"])
	assert.Equal(t, "agt_1", reqs[1].body["collapseID"])
	assert.Equal(t, "cmp|ntf_2", reqs[1].body["payload"].(map[string]any)["id"])
}

func TestQuietPushMakesNoSound(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)

	f.pushNotification(t, `{"id":"ntf_1","message":"Finished","kind":"done","quiet":true}`)

	reqs := relay.requests()
	require.Len(t, reqs, 1)
	aps := reqs[0].body["payload"].(map[string]any)["aps"].(map[string]any)
	assert.NotContains(t, aps, "sound")
	assert.Equal(t, "passive", aps["interruption-level"])
}

func TestPushIgnoresACollapseOfAnotherSession(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)

	f.pushNotification(t, `{"id":"ntf_1","session":"agt_2","collapse":"agt_1","message":"m","kind":"done"}`)

	reqs := relay.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "ntf_1", reqs[0].body["collapseID"])
}

func TestFreePush(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)

	res, err := f.srv.pushCommand(t.Context(), "prj_1", true, pushArgs{Title: "Build", Message: "green"})

	require.NoError(t, err)
	assert.Equal(t, pushResult{Summary: "phone 200", Devices: []devicePush{{Device: "phone", Name: "phone", Outcome: "200"}}}, res)
	reqs := relay.requests()
	require.Len(t, reqs, 1)
	assert.NotContains(t, reqs[0].body, "collapseID")
	assert.Equal(t, map[string]any{"title": "Mac · demo", "subtitle": "Build", "body": "green"}, alertOf(reqs[0]))
	assert.Equal(t, "info", reqs[0].body["payload"].(map[string]any)["kind"])
}

func TestPushCommandRejectsBadArgs(t *testing.T) {
	for name, args := range map[string]pushArgs{
		"empty":         {Title: "t"},
		"not an object": {Notification: `"x"`},
		"no id":         {Notification: `{"message":"m","kind":"done"}`},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)

			_, err := f.srv.pushCommand(t.Context(), "prj_1", true, args)

			assert.Error(t, err)
		})
	}
}

func TestResolvedSendsABackgroundPush(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pair(t, "phone"), "aa01", apns.EnvProduction)

	f.srv.push(t.Context(), pluginsdk.Event{Topic: agentsChangedTopic, Workspace: "prj_1", Payload: json.RawMessage(`{"session":"agt_1","kind":"resolved","request":"r1","requestKind":"elicitation"}`)})

	reqs := relay.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, map[string]any{
		"token":       "aa01",
		"environment": "production",
		"type":        "background",
		"payload": map[string]any{
			"aps":      map[string]any{"content-available": float64(1)},
			"resolved": map[string]any{"computer": "cmp", "session": "agt_1", "requestKind": "elicitation"},
		},
	}, reqs[0].body)
}

func TestNotifyPostedNoLongerPushesByItself(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	f.setPushToken(t, f.pair(t, "phone"), "aa01", apns.EnvProduction)
	a := &app{ctx: t.Context(), srv: f.srv, enabled: true}

	a.busEvent(t.Context(), nil, "", notifyEvent(`{"id":"ntf_1","message":"m","kind":"done"}`))

	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, relay.requests())
}

func TestRelayedGoneTokenIsDropped(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	relay.reply = `{"status":410,"reason":"Unregistered"}`
	f.setPushToken(t, f.pairNamed(t, "phone"), "aa01", apns.EnvProduction)

	res := f.pushNotification(t, `{"id":"ntf_1","message":"m","kind":"done"}`)

	assert.Equal(t, "phone 410 Unregistered, token dropped", res.Summary)
	require.Len(t, relay.requests(), 1)
	assert.Empty(t, f.srv.devices.list()[0].PushToken)
	var stored device
	require.NoError(t, json.Unmarshal([]byte(f.kv.get(deviceKey("phone"))), &stored))
	assert.Empty(t, stored.PushToken)
}

func TestRelayErrorKeepsTokensAndGoesOn(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	relay.reply = ""
	f.setPushToken(t, f.pairNamed(t, "phone-a"), "aa01", apns.EnvProduction)
	f.setPushToken(t, f.pairNamed(t, "phone-b"), "bb02", apns.EnvProduction)

	res := f.pushNotification(t, `{"id":"ntf_1","message":"m","kind":"done"}`)

	assert.Equal(t, "phone-a relay unreachable · phone-b relay unreachable", res.Summary)
	assert.Len(t, relay.requests(), 2)
	for _, d := range f.srv.devices.list() {
		assert.NotEmpty(t, d.PushToken)
	}
}

func TestRelayDownAlertsOnceAnHour(t *testing.T) {
	f := newFixture(t)
	f.withRelay(t)
	f.setPushToken(t, f.pairNamed(t, "phone-a"), "aa01", apns.EnvProduction)
	f.closeRelay()

	for range 2 {
		assert.Equal(t, "phone-a relay unreachable", f.pushNotification(t, `{"id":"ntf_1","message":"m","kind":"done"}`).Summary)
	}
	alerts := f.notifyPosts()
	require.Len(t, alerts, 1)
	var post map[string]string
	require.NoError(t, json.Unmarshal([]byte(alerts[0].args), &post))
	assert.Equal(t, "prj_1", alerts[0].workspace)
	assert.Equal(t, "stuck", post["kind"])
	assert.Equal(t, "Push relay unreachable", post["title"])
	assert.NotEmpty(t, post["message"])

	f.clock.advance(61 * time.Minute)
	f.pushNotification(t, `{"id":"ntf_2","message":"m","kind":"done"}`)
	assert.Len(t, f.notifyPosts(), 2)
}

func TestSetPushRelay(t *testing.T) {
	f := newFixture(t)
	f.kv.data["apnsKey"] = []byte(`"old"`)

	assert.ErrorIs(t, f.srv.relay.configure(t.Context(), "http://relay.example:8000", relayToken), errRelayNotHTTPS)
	assert.ErrorIs(t, f.srv.relay.configure(t.Context(), "https://relay.example:8000", ""), errNoRelayToken)
	require.NoError(t, f.srv.relay.configure(t.Context(), "https://relay.example:8000/", relayToken))
	reloaded := newRelayClient(f.kv, http.DefaultClient)
	require.NoError(t, reloaded.load(t.Context()))
	assert.True(t, reloaded.configured())
	assert.Equal(t, `"https://relay.example:8000"`, f.kv.get(pushRelayURLKey))
	assert.Empty(t, f.kv.get("apnsKey"))

	require.NoError(t, f.srv.relay.configure(t.Context(), "", relayToken))
	assert.False(t, f.srv.relay.configured())
	assert.Equal(t, `""`, f.kv.get(pushRelayTokenKey))
}

func TestSetPushRelayTakesLoopbackHTTPInTestModeOnly(t *testing.T) {
	f := newFixture(t)

	assert.ErrorIs(t, f.srv.relay.configure(t.Context(), "http://127.0.0.1:8000", relayToken), errRelayNotHTTPS)
	t.Setenv(testModeEnv, "1")
	assert.ErrorIs(t, f.srv.relay.configure(t.Context(), "http://relay.example:8000", relayToken), errRelayNotHTTPS)
	require.NoError(t, f.srv.relay.configure(t.Context(), "http://127.0.0.1:8000", relayToken))
	assert.Equal(t, `"http://127.0.0.1:8000"`, f.kv.get(pushRelayURLKey))
}

func (f *fixture) withRelay(t *testing.T) *fakeRelay {
	t.Helper()
	relay := &fakeRelay{reply: `{"status":200,"reason":""}`}
	ts := httptest.NewTLSServer(relay)
	t.Cleanup(ts.Close)
	f.relayServer = ts
	f.srv.relay.client = ts.Client()
	require.NoError(t, f.srv.relay.configure(t.Context(), ts.URL, relayToken))
	return relay
}

func (f *fixture) closeRelay() {
	f.relayServer.Close()
}

func (f *fixture) pairNamed(t *testing.T, name string) string {
	t.Helper()
	return f.pairDevice(t, deviceInfo{ID: name, Name: name, Model: "iPhone"}).Token
}

func (f *fixture) pushNotification(t *testing.T, payload string) pushResult {
	t.Helper()
	res, err := f.srv.pushCommand(t.Context(), "prj_1", true, pushArgs{Notification: payload})
	require.NoError(t, err)
	return res
}

func (f *fixture) notifyPosts() []runCall {
	return slices.DeleteFunc(f.backend.calls(), func(c runCall) bool { return c.id != notifyPostCommand })
}

func (f *fixture) setPushToken(t *testing.T, token, push, env string) {
	t.Helper()
	_, reply := f.call(t, token, "pushToken", pushTokenRequest{Token: push, Environment: env})
	require.Nil(t, reply.Error)
}

func (r *fakeRelay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	raw, _ := io.ReadAll(req.Body)
	if req.URL.Path == relayDevicesPath {
		var d relayDevices
		_ = json.Unmarshal(raw, &d)
		r.mu.Lock()
		r.registered = append(r.registered, d.Tokens)
		status := cmp.Or(r.devices, http.StatusNoContent)
		r.mu.Unlock()
		w.WriteHeader(status)
		return
	}
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	r.mu.Lock()
	r.reqs = append(r.reqs, relayCall{path: req.URL.Path, auth: req.Header.Get("Authorization"), body: body})
	reply := r.reply
	refuse := r.refuse > 0
	r.refuse = max(0, r.refuse-1)
	r.mu.Unlock()
	if refuse {
		http.Error(w, "device token not registered for this Mac", http.StatusForbidden)
		return
	}
	if reply == "" {
		http.Error(w, "too many pushes", http.StatusTooManyRequests)
		return
	}
	_, _ = io.WriteString(w, reply)
}

func (r *fakeRelay) registrations() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registered
}

func (r *fakeRelay) requests() []relayCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reqs
}

func alertOf(c relayCall) map[string]any {
	return c.body["payload"].(map[string]any)["aps"].(map[string]any)["alert"].(map[string]any)
}

func notifyEvent(payload string) pluginsdk.Event {
	return pluginsdk.Event{Topic: notifyTopic, Workspace: "prj_1", Payload: json.RawMessage(payload)}
}
