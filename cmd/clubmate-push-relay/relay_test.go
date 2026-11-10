package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/apns"
)

const validPush = `{"token":"aa01","environment":"development","type":"alert","collapseID":"ntf_1","payload":{"aps":{"alert":{"body":"secret text"}}}}`

type fixture struct {
	url       string
	apns      *fakeAPNS
	closeAPNS func()
	tokens    string
	studio    string
	logs      *bytes.Buffer
	limiter   *limiter
	devices   string
}

type fakeAPNS struct {
	status int
	reason string

	mu   sync.Mutex
	reqs []apnsRequest
}

type apnsRequest struct {
	path   string
	header http.Header
	body   string
}

func TestPushForwardsTheAPNSReply(t *testing.T) {
	f := newFixture(t)
	f.apns.status, f.apns.reason = http.StatusGone, "Unregistered"

	status, body := f.post(t, f.studio, validPush)

	assert.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{"status":410,"reason":"Unregistered"}`, body)
	reqs := f.apns.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "/dev/3/device/aa01", reqs[0].path)
	assert.Equal(t, "alert", reqs[0].header.Get("apns-push-type"))
	assert.Equal(t, "10", reqs[0].header.Get("apns-priority"))
	assert.Equal(t, "ntf_1", reqs[0].header.Get("apns-collapse-id"))
	assert.JSONEq(t, `{"aps":{"alert":{"body":"secret text"}}}`, reqs[0].body)
	assert.Equal(t, "push studio alert 410 Unregistered\n", f.logs.String())
}

func TestBackgroundPushHasLowPriority(t *testing.T) {
	f := newFixture(t)

	status, _ := f.post(t, f.studio, `{"token":"aa01","environment":"production","type":"background","payload":{"aps":{"content-available":1}}}`)

	assert.Equal(t, http.StatusOK, status)
	reqs := f.apns.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "/prod/3/device/aa01", reqs[0].path)
	assert.Equal(t, "5", reqs[0].header.Get("apns-priority"))
}

func TestPushNeedsAMacToken(t *testing.T) {
	f := newFixture(t)

	noToken, _ := f.post(t, "", validPush)
	wrong, _ := f.post(t, "cmr_wrong", validPush)

	assert.Equal(t, http.StatusUnauthorized, noToken)
	assert.Equal(t, http.StatusUnauthorized, wrong)
	assert.Empty(t, f.apns.requests())
}

func TestPushValidatesTheRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not hex", `{"token":"zz","environment":"development","type":"alert","payload":{}}`},
		{"no token", `{"environment":"development","type":"alert","payload":{}}`},
		{"environment", `{"token":"aa01","environment":"staging","type":"alert","payload":{}}`},
		{"type", `{"token":"aa01","environment":"development","type":"voip","payload":{}}`},
		{"payload not object", `{"token":"aa01","environment":"development","type":"alert","payload":"x"}`},
		{"payload too big", `{"token":"aa01","environment":"development","type":"alert","payload":{"x":"` + strings.Repeat("a", apns.MaxPayload) + `"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)

			status, _ := f.post(t, f.studio, tt.body)

			assert.Equal(t, http.StatusBadRequest, status)
			assert.Empty(t, f.apns.requests())
		})
	}
}

func TestPushIsRateLimitedPerMac(t *testing.T) {
	f := newFixture(t)
	f.limiter.burst = 1
	air, err := addToken(f.tokens, "air")
	require.NoError(t, err)
	touch(t, f.tokens, time.Minute)

	first, _ := f.post(t, f.studio, validPush)
	second, _ := f.post(t, f.studio, validPush)
	other, _ := f.post(t, air, validPush)

	assert.Equal(t, http.StatusOK, first)
	assert.Equal(t, http.StatusTooManyRequests, second)
	assert.Equal(t, http.StatusOK, other)
}

func TestUnreachableAPNSIsABadGateway(t *testing.T) {
	f := newFixture(t)
	f.closeAPNS()

	status, _ := f.post(t, f.studio, validPush)

	assert.Equal(t, http.StatusBadGateway, status)
}

func TestTokensFileIsReReadWhenItChanges(t *testing.T) {
	f := newFixture(t)
	air, err := addToken(f.tokens, "air")
	require.NoError(t, err)
	touch(t, f.tokens, time.Minute)
	added, _ := f.post(t, air, validPush)

	require.NoError(t, removeToken(f.tokens, "studio"))
	touch(t, f.tokens, 2*time.Minute)
	removed, _ := f.post(t, f.studio, validPush)
	names, err := tokenNames(f.tokens)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(air, "cmr_"))
	assert.Equal(t, http.StatusOK, added)
	assert.Equal(t, http.StatusUnauthorized, removed)
	assert.Equal(t, []string{"air"}, names)
}

func TestPushGoesOnlyToRegisteredDevicesOnceTheMacRegistered(t *testing.T) {
	f := newFixture(t)
	push := func(token string) int {
		status, _ := f.post(t, f.studio, strings.Replace(validPush, `"aa01"`, `"`+token+`"`, 1))
		return status
	}
	assert.Equal(t, http.StatusOK, push("aa01"))

	status, _ := f.do(t, http.MethodPut, "/v1/devices", f.studio, `{"tokens":["BB02"]}`)
	require.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, http.StatusForbidden, push("aa01"))
	assert.Equal(t, http.StatusOK, push("bb02"))
	assert.Len(t, f.apns.requests(), 2)
	raw, err := os.ReadFile(f.devices)
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(string(raw)), "bb02")
	reloaded, err := loadBindings(f.devices)
	require.NoError(t, err)
	assert.Equal(t, [2]bool{false, true}, [2]bool{reloaded.allowed("studio", "aa01"), reloaded.allowed("studio", "bb02")})
	assert.True(t, reloaded.allowed("mini", "aa01"))

	status, _ = f.do(t, http.MethodPut, "/v1/devices", f.studio, `{"tokens":[]}`)
	require.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, http.StatusForbidden, push("bb02"))
}

func TestPutDevicesChecksTheMacAndTheTokens(t *testing.T) {
	f := newFixture(t)
	many, _ := json.Marshal(devicesRequest{Tokens: strings.Fields(strings.Repeat("aa ", maxDevicesPerMac+1))})
	tests := []struct {
		name, token, body string
		want              int
	}{
		{"no token", "", `{"tokens":["aa01"]}`, http.StatusUnauthorized},
		{"not hex", f.studio, `{"tokens":["zz"]}`, http.StatusBadRequest},
		{"too many", f.studio, string(many), http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _ := f.do(t, http.MethodPut, "/v1/devices", tt.token, tt.body)
			assert.Equal(t, tt.want, status)
		})
	}
	status, _ := f.post(t, f.studio, validPush)
	assert.Equal(t, http.StatusOK, status)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tokens")
	studio, err := addToken(path, "studio")
	require.NoError(t, err)
	toks, err := loadTokens(path)
	require.NoError(t, err)
	fake := &fakeAPNS{status: http.StatusOK}
	apnsServer := httptest.NewServer(fake)
	t.Cleanup(apnsServer.Close)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	client := apns.NewClient(key, "ABC123", "TEAM")
	client.Hosts = map[string]string{apns.EnvDevelopment: apnsServer.URL + "/dev", apns.EnvProduction: apnsServer.URL + "/prod"}
	now := time.Unix(1_800_000_000, 0)
	devicesPath := filepath.Join(t.TempDir(), "devices.json")
	devices, err := loadBindings(devicesPath)
	require.NoError(t, err)
	f := &fixture{
		apns:      fake,
		closeAPNS: apnsServer.Close,
		tokens:    path,
		studio:    studio,
		logs:      &bytes.Buffer{},
		limiter:   newLimiter(pushRate, pushBurst, func() time.Time { return now }),
		devices:   devicesPath,
	}
	r := &relay{tokens: toks, devices: devices, apns: client, limiter: f.limiter, log: log.New(f.logs, "", 0)}
	ts := httptest.NewServer(r.handler())
	t.Cleanup(ts.Close)
	f.url = ts.URL
	return f
}

func (f *fixture) post(t *testing.T, token, body string) (int, string) {
	t.Helper()
	return f.do(t, http.MethodPost, "/v1/push", token, body)
}

func (f *fixture) do(t *testing.T, method, path, token, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, f.url+path, strings.NewReader(body))
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res.StatusCode, string(raw)
}

func (a *fakeAPNS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	a.reqs = append(a.reqs, apnsRequest{path: r.URL.Path, header: r.Header.Clone(), body: string(raw)})
	status, reason := a.status, a.reason
	a.mu.Unlock()
	w.WriteHeader(status)
	if reason != "" {
		_ = json.NewEncoder(w).Encode(map[string]string{"reason": reason})
	}
}

func (a *fakeAPNS) requests() []apnsRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reqs
}

func touch(t *testing.T, path string, ahead time.Duration) {
	t.Helper()
	at := time.Now().Add(ahead)
	require.NoError(t, os.Chtimes(path, at, at))
}
