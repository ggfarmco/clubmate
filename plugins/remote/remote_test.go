package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

type fixture struct {
	ts      *httptest.Server
	srv     *server
	backend *fakeBackend
	kv      *memKV
	clock   *clock
}

type fakeBackend struct {
	mu     sync.Mutex
	runs   []runCall
	events []pluginsdk.Event
}

type runCall struct {
	id, workspace string
	args          string
}

type memKV struct {
	mu      sync.Mutex
	data    map[string][]byte
	fail    error
	entered chan struct{}
	release chan struct{}
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

type reply struct {
	Result json.RawMessage `json:"result"`
	Error  *apiError       `json:"error"`
}

func TestPairIssuesTokenReplacingTheDevicesOldOne(t *testing.T) {
	f := newFixture(t)
	first := f.pair(t, "phone-1")
	_, stream := f.openEvents(t, first, "")
	require.Equal(t, ": connected", frame(t, stream))

	assert.Regexp(t, `^cmt_[A-Za-z0-9_-]{43}$`, first)
	stored := f.kv.get(devicesKey)
	assert.NotContains(t, stored, first)
	assert.Contains(t, stored, hashToken(first))
	second := f.pair(t, "phone-1")
	_, err := stream.ReadString('\n')
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, http.StatusUnauthorized, f.rpcStatus(t, first))
	assert.Equal(t, http.StatusOK, f.rpcStatus(t, second))
	assert.Len(t, f.srv.devices.list(), 1)
}

func TestPairRejectsBadCodes(t *testing.T) {
	tests := []struct {
		name string
		code func(t *testing.T, f *fixture) string
	}{
		{"wrong", func(t *testing.T, f *fixture) string {
			f.srv.pairing.issue()
			return "WRONG"
		}},
		{"reused", func(t *testing.T, f *fixture) string {
			code, _ := f.srv.pairing.issue()
			f.postPair(t, code, "first")
			return code
		}},
		{"expired", func(t *testing.T, f *fixture) string {
			code, _ := f.srv.pairing.issue()
			f.clock.advance(codeTTL)
			return code
		}},
		{"locked out", func(t *testing.T, f *fixture) string {
			code, _ := f.srv.pairing.issue()
			for range maxFailures {
				f.postPair(t, "WRONG", "x")
			}
			return code
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			status, body := f.postPair(t, tt.code(t, f), "phone")
			assert.Equal(t, http.StatusForbidden, status)
			assert.Equal(t, codePairingFailed, body.Error.Code)
		})
	}
}

func TestPairingLockoutIsPerSource(t *testing.T) {
	tests := []struct {
		name    string
		sources int
		want    bool
	}{
		{"one source locked out", 1, true},
		{"global cap reached", maxTotalFailures / maxFailures, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPairing(time.Now)
			code, _ := p.issue()
			for i := range tt.sources {
				for range maxFailures {
					p.redeem("WRONG", strconv.Itoa(i))
				}
			}

			_, blocked := p.redeem(code, "0")
			_, ok := p.redeem(code, "other")

			assert.False(t, blocked)
			assert.Equal(t, tt.want, ok)
		})
	}
}

func TestPairKeepsOldTokenAndCodeWhenSaveFails(t *testing.T) {
	f := newFixture(t)
	old := f.pair(t, "phone")
	code, _ := f.srv.pairing.issue()
	f.kv.failWith(errors.New("disk full"))

	status, _ := f.postPair(t, code, "phone")
	f.kv.failWith(nil)

	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, http.StatusOK, f.rpcStatus(t, old))
	assert.Len(t, f.srv.devices.list(), 1)
	status, _ = f.postPair(t, code, "phone")
	assert.Equal(t, http.StatusOK, status)
}

func TestRevokeDuringAFailedRepairStaysRevoked(t *testing.T) {
	f := newFixture(t)
	old := f.pair(t, "phone")
	f.kv.failWith(errors.New("disk full"))
	entered, release := f.kv.holdNextSet()
	paired := make(chan struct{})
	go func() {
		_, _ = f.srv.devices.add(t.Context(), "phone", "iPhone", "iPhone")
		close(paired)
	}()
	<-entered
	revoked := make(chan error)
	go func() { revoked <- f.srv.devices.revoke(t.Context(), "phone") }()
	time.Sleep(20 * time.Millisecond)

	close(release)
	<-paired
	f.kv.failWith(nil)
	<-revoked

	assert.Equal(t, http.StatusUnauthorized, f.rpcStatus(t, old))
	assert.Empty(t, f.srv.devices.list())
}

func TestPairHostsError(t *testing.T) {
	assert.NoError(t, pairHostsError(status{Hosts: []string{"100.64.0.1"}}))
	assert.EqualError(t, pairHostsError(status{}), "pairing needs a Tailscale address: no Tailscale address")
	assert.EqualError(t, pairHostsError(status{Error: "listen tcp: busy"}), "pairing needs a Tailscale address: listen tcp: busy")
}

func TestResumeAfter(t *testing.T) {
	assert.Equal(t, int64(math.MaxInt64), resumeAfter(0))
	assert.Equal(t, int64(7), resumeAfter(7))
}

func TestRPCNeedsAKnownToken(t *testing.T) {
	f := newFixture(t)
	for _, token := range []string{"", "cmt_unknown"} {
		status, body := f.call(t, token, "hello", nil)
		assert.Equal(t, http.StatusUnauthorized, status)
		assert.Equal(t, codeUnauthorized, body.Error.Code)
	}
}

func TestCommandRunsOnlyRemoteCommands(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")

	_, hello := f.call(t, token, "hello", nil)
	_, ok := f.call(t, token, "command", map[string]any{"id": "agents.sessions", "workspace": "prj_1", "args": map[string]any{"x": 1}})
	_, denied := f.call(t, token, "command", map[string]any{"id": "agents.delete", "workspace": "prj_1"})

	assert.JSONEq(t, `{"computer":{"id":"cmp","name":"Mac"},"version":"1.2.3","commands":["agents.sessions"],"topics":["agents.changed","notify.posted"]}`, string(hello.Result))
	assert.JSONEq(t, `{"ran":"agents.sessions"}`, string(ok.Result))
	assert.Equal(t, []runCall{{id: "agents.sessions", workspace: "prj_1", args: `{"x":1}`}}, f.backend.calls())
	assert.Equal(t, codeNotAllowed, denied.Error.Code)
}

func TestEventsStreamLiveAllowlistedTopics(t *testing.T) {
	f := newFixture(t)
	res, r := f.openEvents(t, f.pair(t, "phone"), "")

	assert.Equal(t, "nosniff", res.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, ": connected", frame(t, r))
	f.srv.hub.publish(pluginsdk.Event{Seq: 6, Topic: "ui.opened", Payload: json.RawMessage(`{}`)})
	f.srv.hub.publish(pluginsdk.Event{Seq: 7, Topic: "agents.changed", Payload: json.RawMessage(`{"workspace":"prj_1","session":"s"}`)})
	assert.Equal(t, "id: 7\nevent: bus\ndata: {\"seq\":7,\"topic\":\"agents.changed\",\"workspace\":\"prj_1\",\"payload\":{\"workspace\":\"prj_1\",\"session\":\"s\"}}", frame(t, r))
}

func TestEventsResumeFromLastEventID(t *testing.T) {
	f := newFixture(t)
	f.backend.events = []pluginsdk.Event{
		{Seq: 1, Topic: "agents.changed", Payload: json.RawMessage(`{}`)},
		{Seq: 2, Topic: "agents.changed", Payload: json.RawMessage(`{}`)},
		{Seq: 3, Topic: "ui.opened", Payload: json.RawMessage(`{}`)},
		{Seq: 4, Topic: "notify.posted", Payload: json.RawMessage(`{}`)},
	}
	_, r := f.openEvents(t, f.pair(t, "phone"), "1")

	assert.Equal(t, ": connected", frame(t, r))
	assert.True(t, strings.HasPrefix(frame(t, r), "id: 2\n"))
	assert.True(t, strings.HasPrefix(frame(t, r), "id: 4\n"))
	f.srv.hub.publish(pluginsdk.Event{Seq: 4, Topic: "notify.posted", Payload: json.RawMessage(`{}`)})
	f.srv.hub.publish(pluginsdk.Event{Seq: 5, Topic: "agents.changed", Payload: json.RawMessage(`{}`)})
	assert.True(t, strings.HasPrefix(frame(t, r), "id: 5\n"))
}

func TestRevokeClosesStreamsAndToken(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")
	_, r := f.openEvents(t, token, "")
	assert.Equal(t, ": connected", frame(t, r))
	assert.True(t, f.srv.hub.connected("phone"))

	require.NoError(t, f.srv.revoke(t.Context(), "phone"))

	_, err := r.ReadString('\n')
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, http.StatusUnauthorized, f.rpcStatus(t, token))
}

func TestRestartMovesListenersToTheNewPort(t *testing.T) {
	loopback := netip.MustParseAddr("127.0.0.1")
	l := newListeners(http.NotFoundHandler(), "", func() ([]netip.Addr, error) { return []netip.Addr{loopback}, nil })
	t.Cleanup(l.stop)
	before, after := freePort(t), freePort(t)
	require.True(t, l.start(before))

	require.True(t, l.restart(after))

	st := l.state()
	assert.Equal(t, []string{netip.AddrPortFrom(loopback, uint16(after)).String()}, st.listening)
	assert.Equal(t, after, st.port)
	_, err := net.Dial("tcp", netip.AddrPortFrom(loopback, uint16(before)).String())
	assert.Error(t, err)
}

func TestTailnetAddrs(t *testing.T) {
	addrs := []net.Addr{
		ipNet("192.168.1.5"),
		ipNet("fd7a:115c:a1e0::5"),
		ipNet("100.101.1.2"),
		ipNet("100.128.0.1"),
		ipNet("100.63.255.255"),
		ipNet("fd7a:115c:a1e1::5"),
		ipNet("::ffff:100.64.0.1"),
		ipNet("127.0.0.1"),
		&net.IPAddr{IP: net.ParseIP("100.101.1.2")},
	}
	want := []netip.Addr{netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("100.101.1.2"), netip.MustParseAddr("fd7a:115c:a1e0::5")}
	assert.Equal(t, want, tailnetAddrs(addrs))
}

func TestParseTailscaleKeepsTailnetIPs(t *testing.T) {
	got := parseTailscale([]byte(`{"Self":{"DNSName":"mac.tail1.ts.net.","TailscaleIPs":["fd7a:115c:a1e0::5","100.101.1.2","192.168.1.5"]}}`))

	assert.Equal(t, "mac.tail1.ts.net", got.dns)
	assert.Equal(t, []netip.Addr{netip.MustParseAddr("100.101.1.2"), netip.MustParseAddr("fd7a:115c:a1e0::5")}, got.ips)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{backend: &fakeBackend{}, kv: &memKV{data: map[string][]byte{}}, clock: &clock{now: time.Unix(1_800_000_000, 0)}}
	f.srv = &server{
		backend:  f.backend,
		devices:  newDeviceStore(f.kv, f.clock.Now),
		pairing:  newPairing(f.clock.Now),
		hub:      newHub(func() {}),
		computer: func() computer { return computer{ID: "cmp", Name: "Mac"} },
		changed:  func() {},
		ping:     time.Hour,
	}
	f.ts = httptest.NewServer(f.srv.handler())
	t.Cleanup(func() {
		f.srv.hub.closeAll()
		f.ts.Close()
	})
	return f
}

func (f *fixture) pair(t *testing.T, deviceID string) string {
	t.Helper()
	code, _ := f.srv.pairing.issue()
	var out pairResponse
	res := f.post(t, "/v1/pair", "", pairRequest{Code: code, Device: deviceInfo{ID: deviceID, Name: "iPhone", Model: "iPhone"}})
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	return out.Token
}

func (f *fixture) postPair(t *testing.T, code, deviceID string) (int, reply) {
	t.Helper()
	res := f.post(t, "/v1/pair", "", pairRequest{Code: code, Device: deviceInfo{ID: deviceID}})
	defer res.Body.Close()
	var out reply
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	return res.StatusCode, out
}

func (f *fixture) call(t *testing.T, token, method string, params any) (int, reply) {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	res := f.post(t, "/v1/rpc", token, rpcRequest{Method: method, Params: raw})
	defer res.Body.Close()
	var out reply
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	return res.StatusCode, out
}

func (f *fixture) rpcStatus(t *testing.T, token string) int {
	t.Helper()
	status, _ := f.call(t, token, "workspaces", nil)
	return status
}

func (f *fixture) post(t *testing.T, path, token string, body any) *http.Response {
	t.Helper()
	return post(t, f.ts.URL, path, token, body)
}

func (f *fixture) openEvents(t *testing.T, token, lastEventID string) (*http.Response, *bufio.Reader) {
	t.Helper()
	return openEvents(t, f.ts.URL, token, lastEventID)
}

func post(t *testing.T, base, path, token string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+path, bytes.NewReader(raw))
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return res
}

func openEvents(t *testing.T, base, token, lastEventID string) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/v1/events", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	if lastEventID != "" {
		req.Header.Set(lastEventHeader, lastEventID)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)
	return res, bufio.NewReader(res.Body)
}

func frame(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var lines []string
	for {
		line, err := r.ReadString('\n')
		require.NoError(t, err)
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			return strings.Join(lines, "\n")
		}
		lines = append(lines, line)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func ipNet(s string) *net.IPNet {
	return &net.IPNet{IP: net.ParseIP(s), Mask: net.CIDRMask(32, 32)}
}

func (b *fakeBackend) Commands(context.Context) ([]commandInfo, error) {
	return []commandInfo{{ID: "agents.sessions", Remote: true, Running: true}, {ID: "agents.delete", Running: true}, {ID: "agents.stopped", Remote: true}}, nil
}

func (b *fakeBackend) Run(_ context.Context, id, workspace string, args json.RawMessage) (json.RawMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.runs = append(b.runs, runCall{id: id, workspace: workspace, args: string(args)})
	return json.RawMessage(`{"ran":"` + id + `"}`), nil
}

func (b *fakeBackend) Workspaces(context.Context) ([]pluginsdk.Project, error) {
	return []pluginsdk.Project{{ID: "prj_1", Name: "demo", Roots: []string{"/tmp/demo"}}}, nil
}

func (b *fakeBackend) Events(_ context.Context, after int64) ([]pluginsdk.Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []pluginsdk.Event
	for _, ev := range b.events {
		if ev.Seq > after {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (b *fakeBackend) Version(context.Context) (string, error) {
	return "1.2.3", nil
}

func (m *memKV) StorageGet(_ context.Context, key string, out any) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.data[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (m *memKV) StorageSet(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m.mu.Lock()
	fail, entered, release := m.fail, m.entered, m.release
	m.entered, m.release = nil, nil
	m.mu.Unlock()
	if entered != nil {
		close(entered)
		<-release
	}
	if fail != nil {
		return fail
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = raw
	return nil
}

func (m *memKV) holdNextSet() (entered, release chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entered, m.release = make(chan struct{}), make(chan struct{})
	return m.entered, m.release
}

func (m *memKV) failWith(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *memKV) get(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return string(m.data[key])
}

func (b *fakeBackend) calls() []runCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.runs)
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
