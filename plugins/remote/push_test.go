package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/apns"
	"clubmate/pkg/pluginsdk"
)

const relayToken = "cmr_test"

type fakeRelay struct {
	reply string

	mu   sync.Mutex
	reqs []relayCall
}

type relayCall struct {
	path string
	auth string
	body map[string]any
}

func TestPushGoesToTokensWithoutAnOpenStream(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	first, streaming, taker := f.pair(t, "phone-a"), f.pair(t, "phone-b"), f.pair(t, "phone-c")
	f.setPushToken(t, first, "aa01", apns.EnvProduction)
	f.setPushToken(t, streaming, "bb02", apns.EnvProduction)
	_, reply := f.call(t, taker, "pushToken", pushTokenRequest{Token: "aa01", Environment: apns.EnvDevelopment})
	assert.JSONEq(t, `{"push":true}`, string(reply.Result))
	f.openEvents(t, streaming, "")

	f.srv.push(t.Context(), notifyEvent(`{"id":"ntf_1","session":"agt_1","message":"Run tests?","kind":"permission"}`))

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

func TestPushFollowsTheSessionPolicy(t *testing.T) {
	tests := []struct {
		policy string
		kind   string
		sent   bool
	}{
		{"muted", "permission", false},
		{"needs-input", "done", false},
		{"needs-input", "question", true},
	}
	for _, tt := range tests {
		t.Run(tt.policy+" "+tt.kind, func(t *testing.T) {
			f := newFixture(t)
			relay := f.withRelay(t)
			f.setPushToken(t, f.pair(t, "phone"), "aa01", apns.EnvProduction)
			f.backend.sessions = json.RawMessage(`[{"id":"agt_1","notifyEffective":"` + tt.policy + `"}]`)

			f.srv.push(t.Context(), notifyEvent(`{"id":"ntf_1","session":"agt_1","message":"m","kind":"`+tt.kind+`"}`))

			assert.Equal(t, tt.sent, len(relay.requests()) == 1)
			assert.Equal(t, []runCall{{id: sessionsCommand, workspace: "prj_1", args: `{"session":"agt_1"}`}}, f.backend.calls())
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

func TestRelayedGoneTokenIsDropped(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	relay.reply = `{"status":410,"reason":"Unregistered"}`
	f.setPushToken(t, f.pair(t, "phone"), "aa01", apns.EnvProduction)

	f.srv.push(t.Context(), notifyEvent(`{"id":"ntf_1","message":"m","kind":"done"}`))

	require.Len(t, relay.requests(), 1)
	assert.Empty(t, f.srv.devices.list()[0].PushToken)
	assert.NotContains(t, f.kv.get(deviceKey("phone")), "aa01")
}

func TestRelayErrorKeepsTokensAndGoesOn(t *testing.T) {
	f := newFixture(t)
	relay := f.withRelay(t)
	relay.reply = ""
	f.setPushToken(t, f.pair(t, "phone-a"), "aa01", apns.EnvProduction)
	f.setPushToken(t, f.pair(t, "phone-b"), "bb02", apns.EnvProduction)

	f.srv.push(t.Context(), notifyEvent(`{"id":"ntf_1","message":"m","kind":"done"}`))

	assert.Len(t, relay.requests(), 2)
	for _, d := range f.srv.devices.list() {
		assert.NotEmpty(t, d.PushToken)
	}
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

func (f *fixture) withRelay(t *testing.T) *fakeRelay {
	t.Helper()
	relay := &fakeRelay{reply: `{"status":200,"reason":""}`}
	ts := httptest.NewTLSServer(relay)
	t.Cleanup(ts.Close)
	f.srv.relay.client = ts.Client()
	require.NoError(t, f.srv.relay.configure(t.Context(), ts.URL, relayToken))
	return relay
}

func (f *fixture) setPushToken(t *testing.T, token, push, env string) {
	t.Helper()
	_, reply := f.call(t, token, "pushToken", pushTokenRequest{Token: push, Environment: env})
	require.Nil(t, reply.Error)
}

func (r *fakeRelay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	raw, _ := io.ReadAll(req.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	r.mu.Lock()
	r.reqs = append(r.reqs, relayCall{path: req.URL.Path, auth: req.Header.Get("Authorization"), body: body})
	reply := r.reply
	r.mu.Unlock()
	if reply == "" {
		http.Error(w, "too many pushes", http.StatusTooManyRequests)
		return
	}
	_, _ = io.WriteString(w, reply)
}

func (r *fakeRelay) requests() []relayCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reqs
}

func notifyEvent(payload string) pluginsdk.Event {
	return pluginsdk.Event{Topic: notifyTopic, Workspace: "prj_1", Payload: json.RawMessage(payload)}
}
