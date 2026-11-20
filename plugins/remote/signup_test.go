package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/internal/tunnelrelay"
	"clubmate/pkg/report"
)

type signupStep struct {
	status     int
	retryAfter string
}

type fakeSignup struct {
	srv *httptest.Server

	mu      sync.Mutex
	steps   []signupStep
	keys    []string
	tokens  []string
	calls   int
	revoked map[string]bool
}

func newFakeSignup(t *testing.T, steps ...signupStep) *fakeSignup {
	t.Helper()
	s := &fakeSignup{steps: steps, revoked: map[string]bool{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *fakeSignup) handle(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == relayMacsPath:
		s.signup(w, r)
	case r.URL.Path == relayDevicesPath:
		if s.isRevoked(token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == macStreamPath:
		w.Header().Set(tunnelrelay.HeaderRelay, "1")
		if s.isRevoked(token) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set(tunnelrelay.HeaderStream, "s-"+token)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, ": ok\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	case r.URL.Path == macTicketsPath:
		w.Header().Set(tunnelrelay.HeaderRelay, "1")
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *fakeSignup) signup(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	sec, _ := strconv.ParseInt(r.Header.Get(report.HeaderTime), 10, 64)
	pub, err := report.Verify(r, body, time.Unix(sec, 0))
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.keys = append(s.keys, hex.EncodeToString(pub))
	var step signupStep
	if len(s.steps) > 0 {
		step, s.steps = s.steps[0], s.steps[1:]
	}
	if step.status != 0 {
		if step.retryAfter != "" {
			w.Header().Set("Retry-After", step.retryAfter)
		}
		http.Error(w, "refused", step.status)
		return
	}
	token := "cmr_" + strconv.Itoa(s.calls)
	s.tokens = append(s.tokens, token)
	w.WriteHeader(http.StatusCreated)
	_, _ = fmt.Fprintf(w, `{"token":%q,"name":"k-test"}`, token)
}

func (s *fakeSignup) isRevoked(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revoked[token]
}

func (s *fakeSignup) revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revoked[token] = true
}

func (s *fakeSignup) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *fakeSignup) lastToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tokens) == 0 {
		return ""
	}
	return s.tokens[len(s.tokens)-1]
}

func (s *fakeSignup) keyList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keys...)
}

func newSignupFixture(t *testing.T, steps ...signupStep) (*fixture, *fakeSignup) {
	t.Helper()
	fs := newFakeSignup(t, steps...)
	t.Setenv(testModeEnv, "1")
	t.Setenv(relayURLEnv, fs.srv.URL)
	f := newFixture(t)
	f.srv.relay.dir = t.TempDir()
	f.srv.relay.first, f.srv.relay.max = 5*time.Millisecond, 20*time.Millisecond
	f.srv.relay.setAccess(true)
	return f, fs
}

func TestSignUpIsSignedWithTheInstallKeyAndStoredInAutoMode(t *testing.T) {
	f, fs := newSignupFixture(t)
	relay := f.srv.relay

	relay.ensure()
	testutil.Eventually(t, relay.configured, 10*time.Second)
	key, err := loadReportKey(relay.dir)
	require.NoError(t, err)

	assert.Equal(t, []string{hex.EncodeToString(key.Public().(ed25519.PublicKey))}, fs.keyList())
	assert.Equal(t, `"auto"`, f.kv.get(pushRelayModeKey))
	assert.Equal(t, strconv.Quote(fs.srv.URL), f.kv.get(pushRelayURLKey))
	assert.Equal(t, strconv.Quote("cmr_1"), f.kv.get(pushRelayTokenKey))
	assert.Equal(t, relayView{Mode: relayAuto, URL: fs.srv.URL, State: relayStateReady, Detail: "Signed up to " + relayHost(fs.srv.URL)}, relay.view(true))
}

func TestSignUpRetriesAndKeepsTheNextAttemptTime(t *testing.T) {
	f, fs := newSignupFixture(t, signupStep{status: http.StatusInternalServerError}, signupStep{status: http.StatusTooManyRequests, retryAfter: "1"})
	relay := f.srv.relay
	start := time.Now()

	relay.ensure()
	testutil.Eventually(t, func() bool { return fs.callCount() == 2 }, 10*time.Second)
	testutil.Eventually(t, func() bool {
		return relay.view(true).Detail == "Too many sign-ups, trying again later" && f.kv.get(pushRelaySignupNextKey) != ""
	}, 10*time.Second)
	var next time.Time
	found, err := f.kv.StorageGet(t.Context(), pushRelaySignupNextKey, &next)
	require.NoError(t, err)
	testutil.Eventually(t, relay.configured, 10*time.Second)

	assert.True(t, found)
	assert.True(t, next.After(start.Add(500*time.Millisecond)))
	assert.GreaterOrEqual(t, time.Since(start), time.Second)
	assert.Equal(t, 3, fs.callCount())
	assert.Empty(t, f.kv.get(pushRelaySignupNextKey))
}

func TestSignupWaitStaysWithinTwentyPercent(t *testing.T) {
	for range 200 {
		got := signupWait(100 * time.Millisecond)

		require.GreaterOrEqual(t, got, 80*time.Millisecond)
		require.LessOrEqual(t, got, 120*time.Millisecond)
	}
}

func TestSignUpStopsOnForbiddenUntilTheUserActs(t *testing.T) {
	f, fs := newSignupFixture(t, signupStep{status: http.StatusForbidden})
	relay := f.srv.relay

	relay.ensure()
	testutil.Eventually(t, func() bool { return relay.view(true).Detail == "This Mac is blocked by the relay" }, 10*time.Second)
	testutil.Never(t, func() bool { return fs.callCount() > 1 }, 200*time.Millisecond)
	relay.ensure()
	testutil.Never(t, func() bool { return fs.callCount() > 1 }, 200*time.Millisecond)
	require.NoError(t, relay.setAuto(t.Context()))
	relay.ensure()

	testutil.Eventually(t, relay.configured, 10*time.Second)
	assert.Equal(t, 2, fs.callCount())
}

func TestSignUpRunsOnlyWhileCompanionAccessIsOn(t *testing.T) {
	f, fs := newSignupFixture(t, signupStep{status: http.StatusServiceUnavailable}, signupStep{status: http.StatusServiceUnavailable}, signupStep{status: http.StatusServiceUnavailable})
	relay := f.srv.relay
	relay.setAccess(false)

	relay.ensure()
	testutil.Never(t, func() bool { return fs.callCount() > 0 }, 200*time.Millisecond)
	relay.setAccess(true)
	relay.ensure()
	testutil.Eventually(t, func() bool { return fs.callCount() >= 1 }, 10*time.Second)
	relay.setAccess(false)
	var seen int
	testutil.Eventually(t, func() bool {
		n := fs.callCount()
		stable := n == seen
		seen = n
		return stable
	}, 10*time.Second)
	count := fs.callCount()

	testutil.Never(t, func() bool { return fs.callCount() > count }, 200*time.Millisecond)
	assert.Equal(t, detailAccessOff, relay.view(false).Detail)
}

func TestManualAndOffMacsNeverSignUpAndAutoSignsUp(t *testing.T) {
	f, fs := newSignupFixture(t)
	relay := f.srv.relay
	require.NoError(t, relay.configure(t.Context(), fs.srv.URL, "cmr_mine"))

	relay.ensure()
	require.NoError(t, relay.configure(t.Context(), "", ""))
	relay.ensure()
	testutil.Never(t, func() bool { return fs.callCount() > 0 }, 200*time.Millisecond)
	require.NoError(t, relay.setAuto(t.Context()))
	relay.ensure()

	testutil.Eventually(t, relay.configured, 10*time.Second)
	assert.Equal(t, 1, fs.callCount())
	assert.Equal(t, relayAuto, relay.relayMode())
}

func TestTestModeWithoutTheRelayEnvNeverSignsUp(t *testing.T) {
	f := newFixture(t)
	t.Setenv(testModeEnv, "1")
	t.Setenv(relayURLEnv, "")
	f.srv.relay.dir = t.TempDir()

	f.srv.relay.ensure()
	f.srv.relay.mu.Lock()
	running := f.srv.relay.running
	f.srv.relay.mu.Unlock()
	err := f.srv.relay.signUpNow(t.Context())

	assert.False(t, running)
	assert.ErrorContains(t, err, detailTestMode)
	assert.Equal(t, relayStateNone, f.srv.relay.view(true).State)
	assert.Equal(t, detailTestMode, f.srv.relay.view(true).Detail)
}

func TestLoadMigratesTheStoredRelayToAMode(t *testing.T) {
	tests := []struct {
		name  string
		store map[string]any
		want  string
	}{
		{"nothing stored", map[string]any{}, relayAuto},
		{"url and token without a mode", map[string]any{pushRelayURLKey: "https://relay.example", pushRelayTokenKey: "cmr_x"}, relayManual},
		{"a stored mode wins", map[string]any{pushRelayModeKey: relayOff, pushRelayURLKey: "https://relay.example", pushRelayTokenKey: "cmr_x"}, relayOff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			for k, v := range tt.store {
				require.NoError(t, f.kv.StorageSet(t.Context(), k, v))
			}
			relay := newRelayClient(f.kv, http.DefaultClient)

			require.NoError(t, relay.load(t.Context()))

			assert.Equal(t, tt.want, relay.relayMode())
		})
	}
}

func TestAProxyTokenRefusalSignsUpAgainWithTheSameKeyAtMostOncePerTenMinutes(t *testing.T) {
	f, fs := newSignupFixture(t)
	relay := f.srv.relay
	clock := &clock{now: time.Now()}
	relay.now = clock.Now
	relay.ensure()
	testutil.Eventually(t, relay.configured, 10*time.Second)

	fs.revoke("cmr_1")
	require.Error(t, relay.register(t.Context(), []string{"aa01"}))
	testutil.Eventually(t, func() bool { return fs.lastToken() == "cmr_2" && relay.view(true).State == relayStateReady }, 10*time.Second)
	fs.revoke("cmr_2")
	require.Error(t, relay.register(t.Context(), []string{"aa02"}))
	testutil.Never(t, func() bool { return fs.callCount() > 2 }, 200*time.Millisecond)
	clock.advance(11 * time.Minute)
	require.Error(t, relay.register(t.Context(), []string{"aa03"}))

	testutil.Eventually(t, func() bool { return fs.callCount() == 3 }, 10*time.Second)
	keys := fs.keyList()
	assert.Equal(t, []string{keys[0], keys[0], keys[0]}, keys)
}

func TestSetTunnelSignsUpAndAStreamTokenRefusalSignsUpAgain(t *testing.T) {
	f, fs := newSignupFixture(t)
	relay := f.srv.relay
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	relay.onSigned = func(ctx context.Context) {
		tc.defaultOn(ctx)
		tc.restart()
	}
	fs.revoke("cmr_1")

	require.NoError(t, tc.configure(t.Context(), true))
	testutil.Eventually(t, func() bool { return tc.status().Connected }, 10*time.Second)

	assert.Equal(t, 2, fs.callCount())
	assert.Equal(t, "cmr_2", fs.lastToken())
	assert.Equal(t, strconv.Quote("cmr_2"), f.kv.get(pushRelayTokenKey))
	assert.Equal(t, "true", f.kv.get(tunnelOnKey))
	assert.Equal(t, tunnelConnected, tc.status().State)
}

func TestTheFirstSignUpTurnsTheRelayPathOnOnlyWhenItWasNeverStored(t *testing.T) {
	tests := []struct {
		name   string
		stored string
		want   string
	}{
		{"never stored", "", "true"},
		{"stored false", "false", "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, fs := newSignupFixture(t)
			tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
			if tt.stored != "" {
				require.NoError(t, f.kv.StorageSet(t.Context(), tunnelOnKey, false))
			}
			var hooked atomic.Bool
			f.srv.relay.onSigned = func(ctx context.Context) {
				tc.defaultOn(ctx)
				hooked.Store(true)
			}

			f.srv.relay.ensure()
			testutil.Eventually(t, hooked.Load, 10*time.Second)
			assert.Equal(t, 1, fs.callCount())

			assert.Equal(t, tt.want, f.kv.get(tunnelOnKey))
		})
	}
}
