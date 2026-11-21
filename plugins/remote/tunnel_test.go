package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/internal/tunnelrelay"
	"clubmate/pkg/apns"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/tunnel"
)

const (
	testMacToken = "cmr_test"
	testTicket   = "cmk_test"
)

type testRelay struct {
	ts      *httptest.Server
	tickets string
	cur     atomic.Pointer[tunnelrelay.Relay]
	push    *fakeRelay
}

type phoneStream struct {
	ch     *tunnel.Channel
	re     uint32
	seq    uint32
	r      *bufio.Reader
	cancel context.CancelFunc
	status int
	buf    []byte
	ended  bool
}

type fakePhone struct {
	base   string
	ticket string
	ch     *tunnel.Channel
}

type logLines struct {
	mu    sync.Mutex
	lines []string
}

func TestTunnelCarriesHelloAndCommands(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	token := f.pair(t, "phone")
	phone := openPhone(t, tr, macKey(t, tc))
	big := make([]byte, 100<<10)
	_, err := rand.Read(big)
	require.NoError(t, err)

	hs, hello := phone.rpc(t, token, "hello", nil)
	cs, said := phone.rpc(t, token, "command", map[string]any{"id": "agents.sessions", "requestId": "r1"})
	bs, _ := phone.rpc(t, token, "command", map[string]any{"id": "agents.sessions", "args": map[string]string{"blob": hex.EncodeToString(big)}})

	assert.Equal(t, [3]int{200, 200, 200}, [3]int{hs.Status, cs.Status, bs.Status})
	assert.Contains(t, string(hello.Result), `"id":"cmp"`)
	assert.JSONEq(t, `{"ran":"agents.sessions"}`, string(said.Result))
	runs := f.backend.calls()
	require.Len(t, runs, 2)
	assert.Contains(t, runs[1].args, hex.EncodeToString(big))
	assert.Equal(t, tunnelStatus{On: true, URL: tr.ts.URL, Connected: true, Channels: 1, State: tunnelConnected, Detail: "Connected through " + relayHost(tr.ts.URL)}, tc.status())
}

func TestTunnelChecksTheDeviceTokenInside(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	phone := openPhone(t, tr, macKey(t, tc))

	head, body := phone.rpc(t, "cmt_wrong", "hello", nil)

	assert.Equal(t, http.StatusUnauthorized, head.Status)
	require.NotNil(t, body.Error)
	assert.Equal(t, codeUnauthorized, body.Error.Code)
}

func TestTunnelRefusesAPhoneWithAnotherMacKey(t *testing.T) {
	logs := captureLogs(t)
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	waitConnected(t, tc)
	other, err := tunnel.NewStatic()
	require.NoError(t, err)

	tr.openRefused(t, logs, other.Public(), testTicket, "err=handshake")

	assert.Zero(t, tc.status().Channels)
}

func TestTunnelKeyAndStateSurviveAPluginRestart(t *testing.T) {
	f, tr := newTunnelFixture(t)
	keys := fileStore{path: filepath.Join(t.TempDir(), "key")}
	first := newTestTunnel(t, f, keys)
	require.NoError(t, first.configure(t.Context(), true))
	key := macKey(t, first)
	first.stop()
	token := f.pair(t, "phone")

	second := newTestTunnel(t, f, keys)
	require.NoError(t, second.load(t.Context()))
	phone := openPhone(t, tr, key)
	head, _ := phone.rpc(t, token, "hello", nil)

	assert.Equal(t, key, macKey(t, second))
	assert.Equal(t, http.StatusOK, head.Status)
}

func TestTunnelReconnectsAfterTheRelayRestarts(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	token := f.pair(t, "phone")
	openPhone(t, tr, macKey(t, tc))

	tr.restart(t)
	phone := openPhone(t, tr, macKey(t, tc))
	head, _ := phone.rpc(t, token, "hello", nil)

	assert.Equal(t, http.StatusOK, head.Status)
}

func TestTunnelStreamCarriesEventsAndUIInOrder(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	token := f.pair(t, "phone")
	f.backend.events = []pluginsdk.Event{
		{Seq: 1, Topic: "agents.changed", Payload: json.RawMessage(`{}`)},
		{Seq: 2, Topic: "agents.changed", Payload: json.RawMessage(`{}`)},
		{Seq: 3, Topic: "ui.opened", Payload: json.RawMessage(`{}`)},
		{Seq: 4, Topic: "notify.posted", Payload: json.RawMessage(`{}`)},
	}
	phone := openPhone(t, tr, macKey(t, tc))

	st := phone.stream(t, token, "1", "")
	connected := st.frame(t)
	id := st.streamID(t)
	replayed := []string{st.frame(t), st.frame(t)}
	head, _ := phone.rpc(t, token, "uiWatch", map[string]any{"stream": id, "workspace": "prj_1", "slots": []string{"cron.main"}})
	f.srv.hub.publish(pluginsdk.Event{Seq: 5, Topic: "agents.changed", Payload: json.RawMessage(`{}`)})
	live := st.frame(t)
	f.srv.uiUpdated(view("cron.main", 2))
	ui := st.frame(t)
	path := f.srv.hub.path("phone")
	require.NoError(t, f.srv.revoke(t.Context(), "phone"))

	assert.Equal(t, ": connected", connected)
	assert.Equal(t, http.StatusOK, st.status)
	assert.True(t, strings.HasPrefix(replayed[0], "id: 2\n"))
	assert.True(t, strings.HasPrefix(replayed[1], "id: 4\n"))
	assert.Equal(t, http.StatusOK, head.Status)
	assert.True(t, strings.HasPrefix(live, "id: 5\n"))
	assert.Equal(t, "event: ui\ndata: {\"workspace\":\"prj_1\",\"slot\":\"cron.main\",\"rev\":2,\"tree\":{\"type\":\"empty\"}}", ui)
	assert.Equal(t, pathRelay, path)
	assert.Empty(t, st.rest(t))
}

func TestTunnelStreamReplaysABacklogLargerThanTheRelayQueue(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	token := f.pair(t, "phone")
	const backlog = 5000
	for seq := int64(1); seq <= backlog; seq++ {
		f.backend.events = append(f.backend.events, pluginsdk.Event{Seq: seq, Topic: "agents.changed", Payload: json.RawMessage(`{}`)})
	}
	phone := openPhone(t, tr, macKey(t, tc))

	st := phone.stream(t, token, "0", "")
	st.frame(t)
	st.streamID(t)
	var last string
	for range backlog {
		last = st.frame(t)
	}

	assert.True(t, strings.HasPrefix(last, "id: 5000\n"))
}

func TestANewStreamClosesTheDeviceStreamOnTheOtherPath(t *testing.T) {
	logs := captureLogs(t)
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	token := f.pair(t, "phone")
	phone := openPhone(t, tr, macKey(t, tc))
	_, direct := f.openEvents(t, token, "")
	frame(t, direct)

	relayed := phone.stream(t, token, "", "")
	relayed.frame(t)
	_, directErr := io.ReadAll(direct)
	probe := openProbe(t, f.ts.URL, token)
	frame(t, probe)
	f.srv.hub.publish(pluginsdk.Event{Seq: 9, Topic: "agents.changed", Payload: json.RawMessage(`{}`)})
	relayed.streamID(t)
	afterProbe := relayed.frame(t)
	_, again := f.openEvents(t, token, "")
	frame(t, again)
	relayed.rest(t)

	require.NoError(t, directErr)
	assert.True(t, strings.HasPrefix(afterProbe, "id: 9\n"))
	assert.Equal(t, pathTailnet, f.srv.hub.path("phone"))
	assert.Equal(t, 2, f.srv.hub.streamCount())
	assert.True(t, logs.has("path=relay"))
	assert.True(t, logs.has("path=tailnet probe=1"))
	assert.True(t, logs.has("reason=switched"))
}

func TestPushSkipsARelayStreamUntilTheRelayDropsIt(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	token := f.pair(t, "phone")
	f.setPushToken(t, token, "aa01", apns.EnvProduction)
	phone := openPhone(t, tr, macKey(t, tc))
	st := phone.stream(t, token, "", "")
	st.frame(t)
	payload := `{"id":"ntf_1","workspace":"prj_1","session":"agt_1","message":"Run tests?","kind":"permission"}`

	skipped := f.pushNotification(t, payload)
	st.cancel()
	testutil.Eventually(t, func() bool { return !f.srv.hub.connected("phone") }, 10*time.Second)
	pushed := f.pushNotification(t, payload)

	assert.Equal(t, "iPhone skipped, stream open", skipped.Summary)
	assert.Equal(t, "iPhone 200", pushed.Summary)
	assert.Len(t, tr.push.requests(), 1)
}

func TestPushBatchMergesChunksPerStreamUnderTheBodyLimit(t *testing.T) {
	a, b := [tunnel.IDSize]byte{1}, [tunnel.IDSize]byte{2}
	data := func(id [tunnel.IDSize]byte, d string) tunnel.PushItem {
		return tunnel.PushItem{Kind: tunnel.PushData, ID: id, Data: []byte(d)}
	}
	end := tunnel.PushItem{Kind: tunnel.PushEnd, ID: a, Data: []byte("e")}
	items := []tunnel.PushItem{data(a, "a1"), data(b, "b1"), data(a, "a2"), end, data(b, "b2"), data(a, "a3")}

	all, none, _ := nextBatch(items, tunnelrelay.MaxBody)
	limit := 2*pushItemHead + 6
	first, rest, taken := nextBatch(items, limit)

	assert.Equal(t, []tunnel.PushItem{data(a, "a1a2"), data(b, "b1b2"), end, data(a, "a3")}, all)
	assert.Empty(t, none)
	assert.Equal(t, []tunnel.PushItem{data(a, "a1a2"), data(b, "b1")}, first)
	assert.Equal(t, items[3:], rest)
	assert.Equal(t, 6, taken)
	assert.LessOrEqual(t, len(tunnel.AppendPush(tunnel.AppendPush(nil, first[0]), first[1])), limit)
	assert.Equal(t, "a2", string(items[2].Data))
}

func TestPushReplyGoneCancelsTheStream(t *testing.T) {
	tr := newTestRelay(t)
	s := newSessions(http.NotFoundHandler(), http.DefaultClient, time.Now)
	s.add("ch_test", nil, false)
	id := [tunnel.IDSize]byte{1}
	_, ctx, done := s.begin(t.Context(), reqFrame{ID: hex.EncodeToString(id[:]), Ch: "ch_test"})
	defer done()

	require.NoError(t, s.pushes.add(t.Context(), macConn{base: tr.ts.URL, token: testMacToken}, false, tunnel.PushItem{Kind: tunnel.PushData, ID: id, Data: []byte("x")}))

	testutil.Eventually(t, func() bool { return ctx.Err() != nil }, 10*time.Second)
}

func TestCallOnAChannelTheMacForgotIsResentOnce(t *testing.T) {
	f, tr := newTunnelFixture(t)
	keys := fileStore{path: filepath.Join(t.TempDir(), "key")}
	first := newTestTunnel(t, f, keys)
	require.NoError(t, first.configure(t.Context(), true))
	token := f.pair(t, "phone")
	phone := openPhone(t, tr, macKey(t, first))
	first.stop()
	second := newTestTunnel(t, f, keys)
	require.NoError(t, second.load(t.Context()))
	testutil.Eventually(t, func() bool { return second.status().Connected }, 10*time.Second)

	stale := phone.callStatus(t, token, "hello")
	reopened := openPhone(t, tr, macKey(t, second))
	head, _ := reopened.rpc(t, token, "hello", nil)

	assert.Equal(t, http.StatusGone, stale)
	assert.Equal(t, http.StatusOK, head.Status)
}

func TestTunnelCutsCallsItMustNotServe(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		body   []byte
		setup  func(s *sessions)
		reason string
	}{
		{"inflates over the body limit", "/v1/rpc", make([]byte, maxBodyBytes+maxTunnelHead), nil, "message_too_large"},
		{"events as a call", eventsPath, nil, nil, "as_a_call"},
		{"no reply before the call deadline", "/test/block", nil, func(s *sessions) { s.callWait = 100 * time.Millisecond }, "deadline_exceeded"},
		{"all slots busy", "/v1/rpc", nil, func(s *sessions) {
			for range cap(s.slots) {
				s.slots <- struct{}{}
			}
		}, "err=busy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			f, tr := newTunnelFixture(t)
			tun := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
			ended := make(chan struct{})
			inner := tun.sess.handler
			tun.sess.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/test/block":
					<-r.Context().Done()
					close(ended)
				default:
					inner.ServeHTTP(w, r)
				}
			})
			if tc.setup != nil {
				tc.setup(tun.sess)
			}
			require.NoError(t, tun.configure(t.Context(), true))
			phone := openPhone(t, tr, macKey(t, tun))

			got := phone.raw(t, tc.path, tc.body)

			assert.Empty(t, got)
			assert.True(t, logs.has(tc.reason), "log has %q", tc.reason)
			if tc.path == "/test/block" {
				testutil.Eventually(t, func() bool {
					select {
					case <-ended:
						return true
					default:
						return false
					}
				}, 10*time.Second)
			}
		})
	}
}

func TestAReplyOverTheRelayLimitIsASealedError(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tun := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	noise := make([]byte, tunnelrelay.MaxBody)
	_, err := rand.Read(noise)
	require.NoError(t, err)
	tun.sess.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test/zeros":
			_, _ = w.Write(make([]byte, maxBodyBytes+1))
		case "/test/noise":
			_, _ = w.Write(noise)
		case "/test/huge":
			for range tunnel.MaxMessage/maxBodyBytes + 1 {
				if _, err := w.Write(make([]byte, maxBodyBytes)); err != nil {
					return
				}
			}
		}
	})
	require.NoError(t, tun.configure(t.Context(), true))
	phone := openPhone(t, tr, macKey(t, tun))

	zerosHead, zeros := phone.call(t, "/test/zeros", "", nil)
	noiseHead, noiseBody := phone.call(t, "/test/noise", "", nil)
	hugeHead, hugeBody := phone.call(t, "/test/huge", "", nil)

	assert.Equal(t, http.StatusOK, zerosHead.Status)
	assert.Len(t, zeros, maxBodyBytes+1)
	for _, got := range []struct {
		head tunnel.ReplyHead
		body []byte
	}{{noiseHead, noiseBody}, {hugeHead, hugeBody}} {
		assert.Equal(t, http.StatusRequestEntityTooLarge, got.head.Status)
		var e errorBody
		require.NoError(t, json.Unmarshal(got.body, &e))
		assert.Equal(t, codeTooLarge, e.Error.Code)
	}
}

func TestSetTunnelSignsUpWithNoPushRelayAndStopsWhenItIsRemoved(t *testing.T) {
	t.Setenv(testModeEnv, "1")
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, f.srv.relay.configure(t.Context(), "", ""))
	f.srv.relay.setAccess(true)
	err := tc.configure(t.Context(), true)
	require.ErrorIs(t, err, errSignupFailed)
	assert.ErrorContains(t, err, detailTestMode)
	assert.Empty(t, f.kv.get(tunnelOnKey))
	require.NoError(t, f.srv.relay.configure(t.Context(), tr.ts.URL, testMacToken))
	require.NoError(t, tc.configure(t.Context(), true))
	testutil.Eventually(t, func() bool { return tc.status().Connected }, 10*time.Second)

	require.NoError(t, f.srv.relay.configure(t.Context(), "", ""))
	tc.restart()

	assert.Equal(t, tunnelStatus{On: true, Error: errTunnelNoRelay.Error(), State: tunnelError, Detail: errTunnelNoRelay.Error()}, tc.status())
	assert.Equal(t, "true", f.kv.get(tunnelOnKey))
}

func TestTunnelStopsWhileCompanionAccessIsOff(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	openPhone(t, tr, macKey(t, tc))

	tc.allow(false)
	phone := &fakePhone{base: tr.ts.URL, ticket: testTicket}

	testutil.Eventually(t, func() bool { return phone.open(t.Context(), t, macKey(t, tc)) == http.StatusServiceUnavailable }, 10*time.Second)
	assert.Equal(t, tunnelStatus{On: true, URL: tr.ts.URL, Error: errAccessOff.Error(), State: tunnelOff, Detail: "Companion access is off"}, tc.status())
}

func TestKeychainStoreKeepsTheKeyOffTheCommandLine(t *testing.T) {
	if os.Getenv("CLUBMATE_TEST_KEYCHAIN") != "1" {
		t.Skip("set CLUBMATE_TEST_KEYCHAIN=1 to run against a temporary keychain")
	}
	dir := t.TempDir()
	keychain := filepath.Join(dir, "test.keychain-db")
	security(t, "create-keychain", "-p", "pw", keychain)
	t.Cleanup(func() { _ = exec.Command(securityBin, "delete-keychain", keychain).Run() })
	security(t, "unlock-keychain", "-p", "pw", keychain)
	argv := filepath.Join(dir, "argv.log")
	wrapper := filepath.Join(dir, "security")
	require.NoError(t, os.WriteFile(wrapper, []byte("#!/bin/sh\necho \"$@\" >> '"+argv+"'\nexec "+securityBin+" \"$@\"\n"), 0o700))
	ks := keychainStore{bin: wrapper, account: "test-install", keychain: keychain}

	_, missing := ks.load(t.Context())
	made, err := loadOrCreateKey(t.Context(), ks)
	require.NoError(t, err)
	again, err := loadOrCreateKey(t.Context(), ks)
	require.NoError(t, err)

	require.ErrorIs(t, missing, errNoKey)
	assert.Equal(t, made.Bytes(), again.Bytes())
	logged, err := os.ReadFile(argv)
	require.NoError(t, err)
	assert.Equal(t, 4, strings.Count(string(logged), "-i\n"))
	assert.NotContains(t, string(logged), base64.StdEncoding.EncodeToString(made.Bytes()))
}

func security(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command(securityBin, args...).CombinedOutput()
	require.NoError(t, err, string(out))
}

func newTunnelFixture(t *testing.T) (*fixture, *testRelay) {
	t.Helper()
	t.Setenv(testModeEnv, "1")
	f := newFixture(t)
	tr := newTestRelay(t)
	require.NoError(t, f.srv.relay.configure(t.Context(), tr.ts.URL, testMacToken))
	return f, tr
}

func newTestTunnel(t *testing.T, f *fixture, keys keyStore) *tunnelClient {
	t.Helper()
	tc := newTunnelClient(context.Background(), f.srv.relay, f.kv, keys, f.srv.handler(), func() {})
	tc.backoffFirst, tc.backoffMax = 10*time.Millisecond, 50*time.Millisecond
	tc.allowed = true
	tc.tickets = func(keep ...string) []tunnelrelay.Ticket {
		return append(f.srv.ticketSet(keep...), tunnelrelay.Ticket{Hash: hashToken(testTicket)})
	}
	tc.sess.classify = func(hash string) ticketKind {
		if hash == hashToken(testTicket) {
			return ticketDevice
		}
		return f.srv.ticketKind(hash)
	}
	tc.now = f.clock.Now
	f.srv.tun = tc
	t.Cleanup(tc.stop)
	return tc
}

func macKey(t *testing.T, tc *tunnelClient) []byte {
	t.Helper()
	s, err := tc.key(t.Context())
	require.NoError(t, err)
	return s.Public()
}

func newTestRelay(t *testing.T) *testRelay {
	t.Helper()
	tr := &testRelay{tickets: filepath.Join(t.TempDir(), "tickets.json"), push: &fakeRelay{reply: `{"status":200,"reason":""}`}}
	tr.writeTickets(t, testTicket)
	tr.cur.Store(tr.newRelay(t))
	tr.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/mac/") || strings.HasPrefix(r.URL.Path, "/v1/d/") {
			tr.cur.Load().Handler().ServeHTTP(w, r)
			return
		}
		tr.push.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		tr.cur.Load().Shutdown()
		tr.ts.Close()
	})
	return tr
}

func (tr *testRelay) newRelay(t *testing.T) *tunnelrelay.Relay {
	t.Helper()
	r, err := tunnelrelay.New(tunnelrelay.Config{
		Auth:        func(b string) (string, bool) { return "studio", b == testMacToken },
		TicketsPath: tr.tickets,
	})
	require.NoError(t, err)
	return r
}

func (tr *testRelay) restart(t *testing.T) {
	t.Helper()
	tr.cur.Swap(tr.newRelay(t)).Shutdown()
}

func (tr *testRelay) writeTickets(t *testing.T, tickets ...string) {
	t.Helper()
	var set []tunnelrelay.Ticket
	for _, tk := range tickets {
		sum := sha256.Sum256([]byte(tk))
		set = append(set, tunnelrelay.Ticket{Hash: hex.EncodeToString(sum[:])})
	}
	raw, err := json.Marshal(map[string][]tunnelrelay.Ticket{"studio": set})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tr.tickets, raw, 0o600))
}

func waitConnected(t *testing.T, tc *tunnelClient) {
	t.Helper()
	testutil.Eventually(t, func() bool { return tc.status().Connected }, 10*time.Second)
}

func (tr *testRelay) openRefused(t *testing.T, logs *logLines, pub []byte, ticket, reason string) {
	t.Helper()
	before := logs.count(reason)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		phone := &fakePhone{base: tr.ts.URL, ticket: ticket}
		_, err := phone.tryOpen(ctx, pub)
		done <- err
	}()
	testutil.Eventually(t, func() bool { return logs.count(reason) > before }, 10*time.Second)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func openPhoneWith(t *testing.T, tr *testRelay, macPub []byte, ticket string) *fakePhone {
	t.Helper()
	p := &fakePhone{base: tr.ts.URL, ticket: ticket}
	testutil.Eventually(t, func() bool { return p.open(t.Context(), t, macPub) == http.StatusOK }, 10*time.Second)
	return p
}

func ticketStatus(tr *testRelay, macPub []byte, ticket string) func() int {
	return func() int {
		status, err := (&fakePhone{base: tr.ts.URL, ticket: ticket}).tryOpen(context.Background(), macPub)
		if err != nil {
			return 0
		}
		return status
	}
}

func openPhone(t *testing.T, tr *testRelay, macPub []byte) *fakePhone {
	t.Helper()
	return openPhoneAt(t, tr.ts.URL, macPub)
}

func openPhoneAt(t *testing.T, base string, macPub []byte) *fakePhone {
	t.Helper()
	p := &fakePhone{base: base, ticket: testTicket}
	testutil.Eventually(t, func() bool { return p.open(t.Context(), t, macPub) == http.StatusOK }, 10*time.Second)
	return p
}

func (p *fakePhone) open(ctx context.Context, t *testing.T, macPub []byte) int {
	status, err := p.tryOpen(ctx, macPub)
	require.NoError(t, err)
	return status
}

func (p *fakePhone) tryOpen(ctx context.Context, macPub []byte) (int, error) {
	ini, msg1, err := tunnel.Initiate(macPub, nil, []int{tunnel.DictV1}, hashToken(p.ticket))
	if err != nil {
		return 0, err
	}
	res, err := p.do(ctx, "/v1/d/open", "", msg1)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return res.StatusCode, nil
	}
	msg2, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, err
	}
	p.ch, err = ini.Finish(res.Header.Get(tunnelrelay.HeaderChannel), msg2)
	return res.StatusCode, err
}

func (p *fakePhone) rpc(t *testing.T, token, method string, params any) (tunnel.ReplyHead, reply) {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	body, err := json.Marshal(rpcRequest{Method: method, Params: raw})
	require.NoError(t, err)
	head, rawBody := p.call(t, "/v1/rpc", token, body)
	var out reply
	require.NoError(t, json.Unmarshal(rawBody, &out))
	return head, out
}

func (p *fakePhone) call(t *testing.T, path, token string, body []byte) (tunnel.ReplyHead, []byte) {
	t.Helper()
	sealed, re := p.seal(t, path, token, body)
	res, err := p.do(t.Context(), "/v1/d/call", p.ch.ID, sealed)
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	m, err := p.ch.ReadMessage(res.Body)
	require.NoError(t, err)
	require.NoError(t, m.Answers(re, 0))
	rawHead, rawBody, err := tunnel.Unpack(m, p.ch.Dict, tunnel.MaxMessage)
	require.NoError(t, err)
	var rh tunnel.ReplyHead
	require.NoError(t, json.Unmarshal(rawHead, &rh))
	return rh, rawBody
}

func (p *fakePhone) raw(t *testing.T, path string, body []byte) []byte {
	t.Helper()
	sealed, _ := p.seal(t, path, "", body)
	res, err := p.do(t.Context(), "/v1/d/call", p.ch.ID, sealed)
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	got, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return got
}

func (p *fakePhone) callStatus(t *testing.T, token, method string) int {
	t.Helper()
	body, err := json.Marshal(rpcRequest{Method: method})
	require.NoError(t, err)
	sealed, _ := p.seal(t, "/v1/rpc", token, body)
	res, err := p.do(t.Context(), "/v1/d/call", p.ch.ID, sealed)
	require.NoError(t, err)
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

func (p *fakePhone) seal(t *testing.T, path, token string, body []byte) ([]byte, uint32) {
	t.Helper()
	header := map[string]string{"Content-Type": "application/json"}
	if token != "" {
		header["Authorization"] = "Bearer " + token
	}
	head := tunnel.RequestHead{Method: http.MethodPost, Path: path, Header: header}
	plain, flags, err := tunnel.PackRequest(head, body, p.ch.Dict)
	require.NoError(t, err)
	sealed, re, err := p.ch.SealRequest(flags, plain)
	require.NoError(t, err)
	return sealed, re
}

func (p *fakePhone) stream(t *testing.T, token, lastEventID, query string) *phoneStream {
	t.Helper()
	head := tunnel.RequestHead{Method: http.MethodGet, Path: "/v1/events" + query, Header: map[string]string{"Authorization": "Bearer " + token}}
	if lastEventID != "" {
		head.Header[lastEventHeader] = lastEventID
	}
	plain, flags, err := tunnel.PackRequest(head, nil, p.ch.Dict)
	require.NoError(t, err)
	sealed, re, err := p.ch.SealRequest(flags, plain)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	res, err := p.do(ctx, "/v1/d/stream", p.ch.ID, sealed)
	require.NoError(t, err)
	t.Cleanup(func() {
		cancel()
		res.Body.Close()
	})
	require.Equal(t, http.StatusOK, res.StatusCode)
	return &phoneStream{ch: p.ch, re: re, r: bufio.NewReader(res.Body), cancel: cancel}
}

func (s *phoneStream) frame(t *testing.T) string {
	t.Helper()
	for {
		if i := bytes.Index(s.buf, []byte("\n\n")); i >= 0 {
			f := string(s.buf[:i])
			s.buf = s.buf[i+2:]
			return f
		}
		require.False(t, s.ended, "stream ended")
		s.read(t)
	}
}

func (s *phoneStream) rest(t *testing.T) string {
	t.Helper()
	for !s.ended {
		s.read(t)
	}
	return string(s.buf)
}

func (s *phoneStream) read(t *testing.T) {
	t.Helper()
	line, err := s.r.ReadString('\n')
	require.NoError(t, err)
	if !strings.HasPrefix(line, "data: ") {
		return
	}
	raw, err := tunnel.ParseSSEData(line)
	require.NoError(t, err)
	r := bytes.NewReader(raw)
	for {
		m, err := s.ch.ReadMessage(r)
		if errors.Is(err, io.EOF) {
			return
		}
		require.NoError(t, err)
		require.NoError(t, m.Answers(s.re, s.seq))
		s.seq++
		head, body, err := tunnel.Unpack(m, s.ch.Dict, tunnel.MaxMessage)
		require.NoError(t, err)
		switch m.Type {
		case tunnel.TypeReply:
			var rh tunnel.ReplyHead
			require.NoError(t, json.Unmarshal(head, &rh))
			s.status = rh.Status
		case tunnel.TypeChunk:
			s.buf = append(s.buf, body...)
		case tunnel.TypeEnd:
			s.ended = true
		}
	}
}

func (s *phoneStream) streamID(t *testing.T) string {
	t.Helper()
	data, ok := strings.CutPrefix(s.frame(t), "event: stream\ndata: ")
	require.True(t, ok)
	var out streamFrame
	require.NoError(t, json.Unmarshal([]byte(data), &out))
	return out.ID
}

func openProbe(t *testing.T, base, token string) *bufio.Reader {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/v1/events?"+probeQuery+"=1", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)
	return bufio.NewReader(res.Body)
}

func (p *fakePhone) do(ctx context.Context, path, ch string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set(tunnelrelay.HeaderTicket, p.ticket)
	if ch != "" {
		req.Header.Set(tunnelrelay.HeaderChannel, ch)
	}
	return http.DefaultClient.Do(req)
}

func captureLogs(t *testing.T) *logLines {
	t.Helper()
	l := &logLines{}
	prev := setLogSink(l.add)
	t.Cleanup(func() { setLogSink(prev) })
	return l
}

func (l *logLines) add(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
}

func (l *logLines) count(part string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, line := range l.lines {
		if strings.Contains(line, part) {
			n++
		}
	}
	return n
}

func (l *logLines) has(part string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, part) {
			return true
		}
	}
	return false
}

func TestPairLinkIsV2OnlyWithATunnel(t *testing.T) {
	c := computer{ID: "cmp", Name: "Mac"}
	exp := time.Unix(1_800_000_300, 0)

	plain, err := url.Parse(pairLink("CODE", []string{"mac.ts.net"}, 47621, c, exp, nil))
	require.NoError(t, err)
	relayed, err := url.Parse(pairLink("CODE", nil, 47621, c, exp, &linkRelay{URL: "https://relay.example", Key: "KEY", KID: "0011223344556677", Ticket: "cmk_x"}))
	require.NoError(t, err)

	assert.Equal(t, "1", plain.Query().Get("v"))
	assert.False(t, plain.Query().Has("r") || plain.Query().Has("pt"))
	q := relayed.Query()
	assert.Equal(t, []string{"2", "https://relay.example", "KEY", "0011223344556677", "cmk_x"}, []string{q.Get("v"), q.Get("r"), q.Get("k"), q.Get("kid"), q.Get("pt")})
	assert.False(t, q.Has("h"))
}

func TestHelloAndPairingNameTheTunnelKeyOnlyWhileItIsOn(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	token := f.pair(t, "phone")
	_, off := f.call(t, token, "hello", nil)
	offInfo, offErr := tc.pairInfo(t.Context())

	require.NoError(t, tc.configure(t.Context(), true))
	_, on := f.call(t, token, "hello", nil)
	onInfo, onErr := tc.pairInfo(t.Context())

	require.NoError(t, offErr)
	require.NoError(t, onErr)
	var hello helloResult
	require.NoError(t, json.Unmarshal(on.Result, &hello))
	assert.NotContains(t, string(off.Result), `"tunnel"`)
	assert.Nil(t, offInfo)
	assert.Equal(t, &tunnelInfo{URL: tr.ts.URL, Key: tunnel.EncodeKey(macKey(t, tc)), KID: tunnel.KeyID(macKey(t, tc))}, hello.Tunnel)
	assert.Equal(t, hello.Tunnel, onInfo)
}

func TestPairingThroughTheTunnelGivesATicketAndRefusesTheRest(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	waitConnected(t, tc)
	pub := macKey(t, tc)
	known := f.pair(t, "known")
	code, pairingTicket, _ := f.srv.pairing.issueWithTicket()
	require.NoError(t, f.srv.syncTickets(t.Context()))
	body, err := json.Marshal(pairRequest{Code: code, Device: deviceInfo{ID: "new", Name: "iPhone", Model: "iPhone"}})
	require.NoError(t, err)

	refused, refusedBody := openPhoneWith(t, tr, pub, pairingTicket).rpc(t, known, "hello", nil)
	head, raw := openPhoneWith(t, tr, pub, pairingTicket).call(t, "/v1/pair", "", body)
	var paired pairResponse
	require.NoError(t, json.Unmarshal(raw, &paired))
	phone := openPhoneWith(t, tr, pub, paired.RelayTicket)
	hello, _ := phone.rpc(t, paired.Token, "hello", nil)

	assert.Equal(t, http.StatusUnauthorized, refused.Status)
	assert.Equal(t, codeUnauthorized, refusedBody.Error.Code)
	assert.Equal(t, http.StatusOK, head.Status)
	assert.Regexp(t, `^cmk_[A-Za-z0-9_-]{43}$`, paired.RelayTicket)
	assert.Equal(t, http.StatusOK, hello.Status)
	testutil.Eventually(t, func() bool { return ticketStatus(tr, pub, pairingTicket)() == http.StatusUnauthorized }, 10*time.Second)
}

func TestPairingChannelTakesOneRequestAndFailuresCountPerTicket(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	waitConnected(t, tc)
	pub := macKey(t, tc)
	code, pairingTicket, _ := f.srv.pairing.issueWithTicket()
	require.NoError(t, f.srv.syncTickets(t.Context()))
	attempt := func(phone *fakePhone, c string) (int, []byte) {
		body, err := json.Marshal(pairRequest{Code: c, Device: deviceInfo{ID: "new", Name: "iPhone", Model: "iPhone"}})
		require.NoError(t, err)
		head, raw := phone.call(t, "/v1/pair", "", body)
		return head.Status, raw
	}

	first := openPhoneWith(t, tr, pub, pairingTicket)
	wrong, _ := attempt(first, "WRONG")
	again, _ := attempt(first, code)
	for range maxFailures - 1 {
		attempt(openPhoneWith(t, tr, pub, pairingTicket), "WRONG")
	}
	limited, raw := attempt(openPhoneWith(t, tr, pub, pairingTicket), code)

	assert.Equal(t, http.StatusForbidden, wrong)
	assert.Equal(t, http.StatusUnauthorized, again)
	assert.Equal(t, http.StatusForbidden, limited)
	assert.Contains(t, string(raw), codePairingFailed)
	status, _ := f.postPair(t, code, "other")
	assert.Equal(t, http.StatusOK, status)
}

func TestRevokeDropsTheDeviceTicketAtTheRelay(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	waitConnected(t, tc)
	pub := macKey(t, tc)
	paired := f.pairDevice(t, deviceInfo{ID: "phone", Name: "iPhone", Model: "iPhone"})
	openPhoneWith(t, tr, pub, paired.RelayTicket)

	require.NoError(t, f.srv.revoke(t.Context(), "phone"))

	testutil.Eventually(t, func() bool { return ticketStatus(tr, pub, paired.RelayTicket)() == http.StatusUnauthorized }, 10*time.Second)
	testutil.Eventually(t, func() bool { return tc.status().Channels == 0 }, 10*time.Second)
}

func TestTicketsSurviveARestartAndReachTheRelayOnConnect(t *testing.T) {
	f, tr := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, tc.configure(t.Context(), true))
	waitConnected(t, tc)
	pub := macKey(t, tc)
	paired := f.pairDevice(t, deviceInfo{ID: "phone", Name: "iPhone", Model: "iPhone"})
	tc.stop()
	tr.writeTickets(t, testTicket)
	tr.restart(t)
	reloaded := newDeviceStore(f.kv, f.clock.Now)
	require.NoError(t, reloaded.load(t.Context()))
	f.srv.devices = reloaded

	tc.restart()

	assert.True(t, reloaded.hasTicket(hashToken(paired.RelayTicket)))
	openPhoneWith(t, tr, pub, paired.RelayTicket)
}

func TestRotationAnswersBothKeysForSevenDays(t *testing.T) {
	logs := captureLogs(t)
	f, tr := newTunnelFixture(t)
	keys := fileStore{path: filepath.Join(t.TempDir(), "key")}
	tc := newTestTunnel(t, f, keys)
	require.NoError(t, tc.configure(t.Context(), true))
	waitConnected(t, tc)
	oldPub := macKey(t, tc)

	require.NoError(t, tc.rotate(t.Context()))
	token := f.pair(t, "phone")
	newPub := macKey(t, tc)
	_, hello := f.call(t, token, "hello", nil)
	tc.stop()
	restarted := newTestTunnel(t, f, keys)
	require.NoError(t, restarted.load(t.Context()))
	waitConnected(t, restarted)

	var info helloResult
	require.NoError(t, json.Unmarshal(hello.Result, &info))
	assert.NotEqual(t, oldPub, newPub)
	assert.Equal(t, tunnel.EncodeKey(newPub), info.Tunnel.Key)
	openPhone(t, tr, oldPub)
	openPhone(t, tr, newPub)

	f.clock.advance(oldKeyTTL)
	tr.openRefused(t, logs, oldPub, testTicket, "err=handshake")

	openPhone(t, tr, newPub)
	assert.NoFileExists(t, keys.path+oldSuffix)
	assert.Empty(t, f.kv.get(oldUntilKey))
}

func TestRotateNeedsTheTunnelOn(t *testing.T) {
	f, _ := newTunnelFixture(t)
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})

	assert.ErrorIs(t, tc.rotate(t.Context()), errTunnelOff)
}

func TestTunnelStopsWhenAnotherClubmateTakesTheStream(t *testing.T) {
	f, _ := newTunnelFixture(t)
	first := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, first.configure(t.Context(), true))
	waitConnected(t, first)
	second := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})

	require.NoError(t, second.configure(t.Context(), true))

	testutil.Eventually(t, func() bool { return first.status().Error == errReplaced.Error() }, 10*time.Second)
	testutil.Never(t, func() bool { return first.status().Connected }, 500*time.Millisecond)
	waitConnected(t, second)
}

func TestAReplacedClubmateLeavesTheLiveTicketSetAlone(t *testing.T) {
	f, tr := newTunnelFixture(t)
	first := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, first.configure(t.Context(), true))
	waitConnected(t, first)
	f.pair(t, "kept")
	live := newFixture(t)
	require.NoError(t, live.srv.relay.configure(t.Context(), tr.ts.URL, testMacToken))
	second := newTestTunnel(t, live, fileStore{path: filepath.Join(t.TempDir(), "key")})
	require.NoError(t, second.configure(t.Context(), true))
	testutil.Eventually(t, func() bool { return first.status().Error == errReplaced.Error() }, 10*time.Second)
	waitConnected(t, second)
	paired := live.pairDevice(t, deviceInfo{ID: "phone", Name: "iPhone", Model: "iPhone"})
	phone := openPhoneWith(t, tr, macKey(t, second), paired.RelayTicket)

	f.pair(t, "late")
	require.NoError(t, f.srv.revoke(t.Context(), "kept"))
	info, err := first.pairInfo(t.Context())
	hello, _ := phone.rpc(t, paired.Token, "hello", nil)

	require.NoError(t, err)
	assert.Nil(t, info)
	assert.Equal(t, http.StatusOK, hello.Status)
}

type flakyKeys struct {
	keyStore
	mu    sync.Mutex
	fails int
}

func (k *flakyKeys) load(ctx context.Context) ([]byte, error) {
	k.mu.Lock()
	failing := k.fails > 0
	if failing {
		k.fails--
	}
	k.mu.Unlock()
	if failing {
		return nil, errors.New("relay key not readable from the Keychain: locked")
	}
	return k.keyStore.load(ctx)
}

func TestTunnelRetriesAKeyThatCannotBeReadAtStart(t *testing.T) {
	f, _ := newTunnelFixture(t)
	keys := fileStore{path: filepath.Join(t.TempDir(), "key")}
	first := newTestTunnel(t, f, keys)
	require.NoError(t, first.configure(t.Context(), true))
	first.stop()
	flaky := &flakyKeys{keyStore: keys, fails: 1}
	second := newTestTunnel(t, f, flaky)

	require.NoError(t, second.load(t.Context()))

	waitConnected(t, second)
}
