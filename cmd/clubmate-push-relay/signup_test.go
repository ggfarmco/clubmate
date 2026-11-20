package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
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
	"clubmate/pkg/report"
)

type signupFixture struct {
	*reportFixture
	apns *fakeAPNS
}

func TestSignupNewKeyPushesOnlyToRegisteredDevices(t *testing.T) {
	f := newSignupFixture(t)

	res, reply := f.signup(t, f.key)
	before := f.push(t, reply.Token, "aa01")
	devices := f.call(t, http.MethodPut, "/v1/devices", reply.Token, `{"tokens":["aa01"]}`)
	after := f.push(t, reply.Token, "aa01")

	require.Equal(t, http.StatusCreated, res.Code)
	assert.Equal(t, macName(f.key.Public().(ed25519.PublicKey)), reply.Name)
	assert.True(t, strings.HasPrefix(reply.Token, tokenPrefix))
	assert.Equal(t, http.StatusForbidden, before)
	assert.Equal(t, http.StatusNoContent, devices.Code)
	assert.Equal(t, http.StatusOK, after)
	assert.Len(t, f.apns.requests(), 1)
	assert.Contains(t, f.logs.String(), "signup "+reply.Name+" new 192.0.2.1\n")
	assert.NotContains(t, f.logs.String(), reply.Token)
}

func TestSignupAgainReplacesTheTokenAndKeepsTheDevices(t *testing.T) {
	f := newSignupFixture(t)
	_, first := f.signup(t, f.key)
	require.Equal(t, http.StatusNoContent, f.call(t, http.MethodPut, "/v1/devices", first.Token, `{"tokens":["aa01"]}`).Code)

	res, again := f.signup(t, f.key)

	require.Equal(t, http.StatusOK, res.Code)
	assert.Equal(t, first.Name, again.Name)
	assert.NotEqual(t, first.Token, again.Token)
	assert.Equal(t, http.StatusUnauthorized, f.push(t, first.Token, "aa01"))
	assert.Equal(t, http.StatusOK, f.push(t, again.Token, "aa01"))
	entries, err := readTokens(f.relay.tokens.path)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
	assert.Contains(t, f.logs.String(), "signup "+again.Name+" again 192.0.2.1\n")
}

func TestSignupRefusals(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, f *signupFixture) *httptest.ResponseRecorder
		want int
	}{
		{"signup off", func(t *testing.T, f *signupFixture) *httptest.ResponseRecorder {
			f.relay.signup = nil
			res, _ := f.signup(t, f.key)
			return res
		}, http.StatusNotFound},
		{"banned IP", func(t *testing.T, f *signupFixture) *httptest.ResponseRecorder {
			f.writeBans(t, "ip 192.0.2.0/24\n")
			res, _ := f.signup(t, f.key)
			return res
		}, http.StatusForbidden},
		{"IP limit charged before the body", func(t *testing.T, f *signupFixture) *httptest.ResponseRecorder {
			for range 5 {
				require.Equal(t, http.StatusUnauthorized, f.serve(httptest.NewRequest(http.MethodPost, "/v1/macs", strings.NewReader("{}"))).Code)
			}
			res, _ := f.signup(t, newKey(t, 1))
			assert.NotEmpty(t, res.Header().Get("Retry-After"))
			return res
		}, http.StatusTooManyRequests},
		{"body over 1 KiB", func(t *testing.T, f *signupFixture) *httptest.ResponseRecorder {
			return f.signupWith(t, f.key, `{"x":"`+strings.Repeat("a", maxSignupBody)+`"}`, f.now)
		}, http.StatusRequestEntityTooLarge},
		{"time more than 5 min off", func(t *testing.T, f *signupFixture) *httptest.ResponseRecorder {
			return f.signupWith(t, f.key, "{}", f.now.Add(-report.MaxSkew-time.Second))
		}, http.StatusUnauthorized},
		{"replayed signature", func(t *testing.T, f *signupFixture) *httptest.ResponseRecorder {
			require.Equal(t, http.StatusCreated, f.signupWith(t, f.key, "{}", f.now).Code)
			return f.signupWith(t, f.key, "{}", f.now)
		}, http.StatusUnauthorized},
		{"key limit", func(t *testing.T, f *signupFixture) *httptest.ResponseRecorder {
			for range 3 {
				res, _ := f.signup(t, f.key)
				require.Less(t, res.Code, 300)
			}
			res, _ := f.signup(t, f.key)
			return res
		}, http.StatusTooManyRequests},
		{"global new-key limit", func(t *testing.T, f *signupFixture) *httptest.ResponseRecorder {
			f.relay.signup.newKeys = newLimiter(300.0/86400, 1, f.relay.now)
			res, _ := f.signup(t, f.key)
			require.Equal(t, http.StatusCreated, res.Code)
			res, _ = f.signup(t, newKey(t, 1))
			return res
		}, http.StatusTooManyRequests},
		{"cap of self-signed entries", func(t *testing.T, f *signupFixture) *httptest.ResponseRecorder {
			f.relay.signup.cap = 1
			res, _ := f.signup(t, f.key)
			require.Equal(t, http.StatusCreated, res.Code)
			res, _ = f.signup(t, newKey(t, 1))
			assert.Equal(t, "sign-up closed\n", res.Body.String())
			return res
		}, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSignupFixture(t)

			res := tt.run(t, f)

			assert.Equal(t, tt.want, res.Code)
		})
	}
}

func TestSignupBannedKeyLosesItsToken(t *testing.T) {
	f := newSignupFixture(t)
	_, reply := f.signup(t, f.key)
	f.writeBans(t, "key "+report.KeyID(f.key.Public().(ed25519.PublicKey))+"\n")

	res, _ := f.signup(t, f.key)

	assert.Equal(t, http.StatusForbidden, res.Code)
	assert.Equal(t, http.StatusUnauthorized, f.push(t, reply.Token, "aa01"))
	assert.Contains(t, f.logs.String(), "signup refused 403 192.0.2.1\n")
}

func TestSignupBearerLeavesAReportAnonymous(t *testing.T) {
	f := newSignupFixture(t)
	_, reply := f.signup(t, f.key)

	res := f.send(t, reportRequest{bearer: reply.Token})
	list := f.list(t, "")

	require.Equal(t, http.StatusCreated, res.Code)
	require.Len(t, list, 1)
	assert.False(t, list[0].Trusted)
	assert.Empty(t, list[0].Mac)
}

func TestSignupEntrySurvivesARestart(t *testing.T) {
	f := newSignupFixture(t)
	_, reply := f.signup(t, f.key)

	toks, err := loadTokens(f.relay.tokens.path)
	require.NoError(t, err)
	name, self, ok := toks.lookup(reply.Token, nil)
	names, err := tokenNames(toks.path)
	require.NoError(t, err)
	_, addErr := addToken(toks.path, "k-mine")

	assert.True(t, ok)
	assert.True(t, self)
	assert.Equal(t, reply.Name, name)
	require.Len(t, toks.entries, 2)
	assert.Equal(t, []byte(f.key.Public().(ed25519.PublicKey)), toks.entries[1].key)
	assert.Equal(t, f.now.Unix(), toks.entries[1].at)
	assert.Equal(t, []string{"studio", reply.Name + selfMark}, names)
	assert.ErrorIs(t, addErr, errSelfName)
}

func TestTokenAddAndSignupAtOnceLoseNoLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	toks, err := loadTokens(path)
	require.NoError(t, err)
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	for i := range n {
		wg.Go(func() {
			_, err := addToken(path, fmt.Sprintf("mac-%d", i))
			errs <- err
		})
		wg.Go(func() {
			_, _, err := toks.signup(newKey(t, i).Public().(ed25519.PublicKey), time.Now(), func(bool, int) error { return nil })
			errs <- err
		})
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	entries, err := readTokens(path)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, e := range entries {
		names[e.name] = true
	}
	assert.Len(t, entries, 2*n)
	assert.Len(t, names, 2*n)
}

func newSignupFixture(t *testing.T) *signupFixture {
	t.Helper()
	f := newReportFixture(t)
	fake := &fakeAPNS{status: http.StatusOK}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	client := apns.NewClient(key, "ABC123", "TEAM")
	client.Hosts = map[string]string{apns.EnvDevelopment: srv.URL + "/dev", apns.EnvProduction: srv.URL + "/prod"}
	devices, err := loadBindings(filepath.Join(t.TempDir(), "devices.json"))
	require.NoError(t, err)
	f.relay.apns = client
	f.relay.devices = devices
	f.relay.limiter = newLimiter(pushRate, pushBurst, f.relay.now)
	f.relay.signup = newSignupLimits(f.relay.now)
	return &signupFixture{reportFixture: f, apns: fake}
}

func (f *signupFixture) signup(t *testing.T, key ed25519.PrivateKey) (*httptest.ResponseRecorder, signupReply) {
	t.Helper()
	f.now = f.now.Add(time.Second)
	res := f.signupWith(t, key, "{}", f.now)
	var reply signupReply
	if res.Code == http.StatusCreated || res.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &reply))
	}
	return res, reply
}

func (f *signupFixture) signupWith(t *testing.T, key ed25519.PrivateKey, body string, at time.Time) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/macs", strings.NewReader(body))
	report.Sign(req, key, []byte(body), at)
	return f.serve(req)
}

func (f *signupFixture) push(t *testing.T, token, device string) int {
	t.Helper()
	return f.call(t, http.MethodPost, "/v1/push", token, strings.Replace(validPush, `"aa01"`, `"`+device+`"`, 1)).Code
}

func (f *signupFixture) call(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	return f.serve(req)
}
