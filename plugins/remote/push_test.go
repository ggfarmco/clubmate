package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

type fakeAPNS struct {
	key    *ecdsa.PrivateKey
	status int
	reason string

	mu   sync.Mutex
	reqs []apnsRequest
}

type apnsRequest struct {
	path   string
	header http.Header
	body   map[string]any
}

func TestPushGoesToTokensWithoutAnOpenStream(t *testing.T) {
	f := newFixture(t)
	apns := f.withAPNS(t)
	first, streaming, taker := f.pair(t, "phone-a"), f.pair(t, "phone-b"), f.pair(t, "phone-c")
	f.setPushToken(t, first, "aa01", envProduction)
	f.setPushToken(t, streaming, "bb02", envProduction)
	_, reply := f.call(t, taker, "pushToken", pushTokenRequest{Token: "aa01", Environment: envDevelopment})
	assert.JSONEq(t, `{"push":true}`, string(reply.Result))
	f.openEvents(t, streaming, "")

	f.srv.push(t.Context(), notifyEvent(`{"id":"ntf_1","session":"agt_1","message":"Run tests?","kind":"permission"}`))

	reqs := apns.requests()
	require.Len(t, reqs, 1)
	r := reqs[0]
	assert.Equal(t, "/dev/3/device/aa01", r.path)
	assert.Equal(t, "com.clubmate.companion", r.header.Get("apns-topic"))
	assert.Equal(t, "alert", r.header.Get("apns-push-type"))
	assert.Equal(t, "10", r.header.Get("apns-priority"))
	assert.Equal(t, "ntf_1", r.header.Get("apns-collapse-id"))
	apns.verifyJWT(t, r.header.Get("authorization"))
	assert.Equal(t, map[string]any{
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
	}, r.body)
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
			apns := f.withAPNS(t)
			f.setPushToken(t, f.pair(t, "phone"), "aa01", envProduction)
			f.backend.sessions = json.RawMessage(`[{"id":"agt_1","notifyEffective":"` + tt.policy + `"}]`)

			f.srv.push(t.Context(), notifyEvent(`{"id":"ntf_1","session":"agt_1","message":"m","kind":"`+tt.kind+`"}`))

			assert.Equal(t, tt.sent, len(apns.requests()) == 1)
			assert.Equal(t, []runCall{{id: sessionsCommand, workspace: "prj_1", args: `{"session":"agt_1"}`}}, f.backend.calls())
		})
	}
}

func TestResolvedSendsABackgroundPush(t *testing.T) {
	f := newFixture(t)
	apns := f.withAPNS(t)
	f.setPushToken(t, f.pair(t, "phone"), "aa01", envProduction)

	f.srv.push(t.Context(), pluginsdk.Event{Topic: agentsChangedTopic, Workspace: "prj_1", Payload: json.RawMessage(`{"session":"agt_1","kind":"resolved","request":"r1","requestKind":"elicitation"}`)})

	reqs := apns.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "background", reqs[0].header.Get("apns-push-type"))
	assert.Equal(t, "5", reqs[0].header.Get("apns-priority"))
	assert.Equal(t, map[string]any{
		"aps":      map[string]any{"content-available": float64(1)},
		"resolved": map[string]any{"computer": "cmp", "session": "agt_1", "requestKind": "elicitation"},
	}, reqs[0].body)
}

func TestGoneTokenIsDropped(t *testing.T) {
	f := newFixture(t)
	apns := f.withAPNS(t)
	apns.status, apns.reason = http.StatusGone, "Unregistered"
	f.setPushToken(t, f.pair(t, "phone"), "aa01", envProduction)

	f.srv.push(t.Context(), notifyEvent(`{"id":"ntf_1","message":"m","kind":"done"}`))

	require.Len(t, apns.requests(), 1)
	assert.Empty(t, f.srv.devices.list()[0].PushToken)
	assert.NotContains(t, f.kv.get(deviceKey("phone")), "aa01")
}

func TestSetAPNSKey(t *testing.T) {
	f := newFixture(t)
	path, _ := writeAPNSKey(t)

	require.NoError(t, f.srv.apns.configure(t.Context(), path, "", "TEAM"))
	assert.Equal(t, `"ABC123"`, f.kv.get(apnsKeyIDKey))
	reloaded := newAPNSClient(f.kv, f.clock.Now, nil)
	require.NoError(t, reloaded.load(t.Context()))
	assert.True(t, reloaded.configured())

	bad := filepath.Join(t.TempDir(), "AuthKey_X.p8")
	require.NoError(t, os.WriteFile(bad, []byte("nope"), 0o600))
	assert.ErrorIs(t, f.srv.apns.configure(t.Context(), bad, "", "TEAM"), errBadAPNSKey)

	require.NoError(t, f.srv.apns.configure(t.Context(), "", "", ""))
	assert.False(t, f.srv.apns.configured())
}

func (f *fixture) withAPNS(t *testing.T) *fakeAPNS {
	t.Helper()
	path, key := writeAPNSKey(t)
	require.NoError(t, f.srv.apns.configure(t.Context(), path, "", "TEAM"))
	apns := &fakeAPNS{key: key, status: http.StatusOK}
	ts := httptest.NewServer(apns)
	t.Cleanup(ts.Close)
	f.srv.apns.hosts = map[string]string{envDevelopment: ts.URL + "/dev", envProduction: ts.URL + "/prod"}
	return apns
}

func (f *fixture) setPushToken(t *testing.T, token, push, env string) {
	t.Helper()
	_, reply := f.call(t, token, "pushToken", pushTokenRequest{Token: push, Environment: env})
	require.Nil(t, reply.Error)
}

func (a *fakeAPNS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	a.mu.Lock()
	a.reqs = append(a.reqs, apnsRequest{path: r.URL.Path, header: r.Header.Clone(), body: body})
	a.mu.Unlock()
	w.WriteHeader(a.status)
	if a.reason != "" {
		_ = json.NewEncoder(w).Encode(apnsErrorBody{Reason: a.reason})
	}
}

func (a *fakeAPNS) requests() []apnsRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reqs
}

func (a *fakeAPNS) verifyJWT(t *testing.T, auth string) {
	t.Helper()
	token, ok := strings.CutPrefix(auth, "bearer ")
	require.True(t, ok)
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"alg":"ES256","kid":"ABC123"}`, string(header))
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	assert.JSONEq(t, `{"iss":"TEAM","iat":1800000000}`, string(claims))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	require.Len(t, sig, 64)
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	assert.True(t, ecdsa.Verify(&a.key.PublicKey, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])))
}

func writeAPNSKey(t *testing.T) (string, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "AuthKey_ABC123.p8")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600))
	return path, key
}

func notifyEvent(payload string) pluginsdk.Event {
	return pluginsdk.Event{Topic: notifyTopic, Workspace: "prj_1", Payload: json.RawMessage(payload)}
}
