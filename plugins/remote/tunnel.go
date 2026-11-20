package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"clubmate/internal/tunnelrelay"
	"clubmate/pkg/tunnel"
)

const (
	tunnelOnKey    = "tunnelOn"
	oldUntilKey    = "tunnelOldUntil"
	oldKeyTTL      = 7 * 24 * time.Hour
	macStreamPath  = "/v1/mac/stream"
	macTicketsPath = "/v1/mac/tickets"
	maxSSELine     = 1 << 20
	sweepEvery     = time.Minute
	eventPrefix    = "event: "
	dataPrefix     = "data: "
	frameOpen      = "open"
	frameReq       = "req"
	frameClose     = "close"
	frameReplaced  = "replaced"
	tunnelTokenErr = "the push relay refused this Mac's token"

	tunnelOff        = "off"
	tunnelConnecting = "connecting"
	tunnelConnected  = "connected"
	tunnelError      = "error"
)

var (
	errTunnelToken      = errors.New(tunnelTokenErr)
	errTunnelNoRelay    = errors.New("set the push relay first: remote.set-push-relay")
	errRelayUnreachable = errors.New("relay unreachable")
	errReplaced         = errors.New("another Clubmate with this Mac's token took over the relay stream")
	errNoLiveness       = errors.New("no byte from the relay for 55 s")
	errAccessOff        = errors.New("companion access is off: remote.enable")
	errTunnelOff        = errors.New("the relay path is off: remote.set-tunnel on=true")
)

type tunnelClient struct {
	ctx     context.Context
	relay   *relayClient
	kv      kvStore
	keys    keyStore
	client  *http.Client
	changed func()
	sess    *sessions
	now     func() time.Time
	tickets func(keep ...string) []tunnelrelay.Ticket

	backoffFirst time.Duration
	backoffMax   time.Duration
	liveness     time.Duration

	runMu  sync.Mutex
	keyMu  sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	conn   macConn

	mu        sync.Mutex
	on        bool
	allowed   bool
	static    *tunnel.Static
	old       *tunnel.Static
	oldUntil  time.Time
	dirty     bool
	url       string
	connected bool
	err       string
	replaced  bool
	stream    string
}

type macConn struct {
	base   string
	token  string
	stream string
}

type tunnelStatus struct {
	On        bool   `json:"on"`
	URL       string `json:"url"`
	Connected bool   `json:"connected"`
	Error     string `json:"error"`
	Channels  int    `json:"channels"`
	State     string `json:"state"`
	Detail    string `json:"detail"`
}

type tunnelArgs struct {
	On bool `json:"on"`
}

type tunnelInfo struct {
	URL string `json:"url"`
	Key string `json:"key"`
	KID string `json:"kid"`
}

type macFrame struct {
	event string
	data  []byte
}

func newTunnelClient(ctx context.Context, relay *relayClient, kv kvStore, keys keyStore, handler http.Handler, changed func()) *tunnelClient {
	client := noRedirects(&http.Client{})
	tc := &tunnelClient{
		ctx:          ctx,
		relay:        relay,
		kv:           kv,
		keys:         keys,
		client:       client,
		changed:      changed,
		sess:         newSessions(handler, client, time.Now),
		backoffFirst: tunnelrelay.BackoffFirst,
		backoffMax:   tunnelrelay.BackoffMax,
		liveness:     tunnelrelay.Liveness,
		now:          time.Now,
	}
	return tc
}

func (c *tunnelClient) load(ctx context.Context) error {
	var on bool
	if _, err := c.kv.StorageGet(ctx, tunnelOnKey, &on); err != nil {
		return err
	}
	c.mu.Lock()
	c.on = on
	c.mu.Unlock()
	c.restart()
	return nil
}

func (c *tunnelClient) defaultOn(ctx context.Context) {
	var on bool
	found, err := c.kv.StorageGet(ctx, tunnelOnKey, &on)
	if err != nil {
		logEvent("tunnel", "err", "read the relay path setting: "+err.Error())
		return
	}
	if found {
		return
	}
	if err := c.kv.StorageSet(ctx, tunnelOnKey, true); err != nil {
		logEvent("tunnel", "err", "turn the relay path on: "+err.Error())
		return
	}
	c.mu.Lock()
	c.on = true
	c.mu.Unlock()
}

func (c *tunnelClient) configure(ctx context.Context, on bool) error {
	if on {
		if !c.relay.configured() {
			if err := c.relay.signUpNow(ctx); err != nil {
				return err
			}
		}
		if _, err := c.key(ctx); err != nil {
			return err
		}
	}
	if err := c.kv.StorageSet(ctx, tunnelOnKey, on); err != nil {
		return err
	}
	c.mu.Lock()
	c.on = on
	c.mu.Unlock()
	c.restart()
	return nil
}

func (c *tunnelClient) allow(allowed bool) {
	c.mu.Lock()
	same := c.allowed == allowed
	c.allowed = allowed
	c.mu.Unlock()
	if !same {
		c.restart()
	}
}

func (c *tunnelClient) key(ctx context.Context) (*tunnel.Static, error) {
	c.keyMu.Lock()
	defer c.keyMu.Unlock()
	c.mu.Lock()
	s := c.static
	c.mu.Unlock()
	if s != nil {
		return s, nil
	}
	s, err := loadOrCreateKey(ctx, c.keys)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.static == nil {
		c.static = s
	}
	cur := c.static
	c.mu.Unlock()
	c.loadOld(ctx)
	return cur, nil
}

func (c *tunnelClient) loadOld(ctx context.Context) {
	var until time.Time
	found, err := c.kv.StorageGet(ctx, oldUntilKey, &until)
	if err != nil || !found {
		return
	}
	if !c.now().Before(until) {
		c.dropOld(ctx)
		return
	}
	b, err := c.keys.old().load(ctx)
	if err != nil {
		logEvent("tunnel", "err", "old key: "+err.Error())
		return
	}
	s, err := tunnel.LoadStatic(b)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.old, c.oldUntil = s, until
	c.mu.Unlock()
}

func (c *tunnelClient) expireOld(ctx context.Context) {
	c.mu.Lock()
	expired := c.old != nil && !c.now().Before(c.oldUntil)
	if expired {
		c.old = nil
	}
	c.mu.Unlock()
	if expired {
		c.dropOld(ctx)
	}
}

func (c *tunnelClient) answerKeys(ctx context.Context) []*tunnel.Static {
	c.expireOld(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []*tunnel.Static{c.static}
	if c.old != nil {
		out = append(out, c.old)
	}
	return out
}

func (c *tunnelClient) dropOld(ctx context.Context) {
	if err := c.keys.old().remove(ctx); err != nil {
		logEvent("tunnel", "err", "remove old key: "+err.Error())
		return
	}
	if err := c.kv.StorageDelete(ctx, oldUntilKey); err != nil {
		logEvent("tunnel", "err", "forget old key: "+err.Error())
	}
}

func (c *tunnelClient) rotate(ctx context.Context) error {
	c.mu.Lock()
	on := c.on
	c.mu.Unlock()
	if !on {
		return errTunnelOff
	}
	cur, err := c.key(ctx)
	if err != nil {
		return err
	}
	fresh, err := tunnel.NewStatic()
	if err != nil {
		return err
	}
	until := c.now().Add(oldKeyTTL)
	oldStore := c.keys.old()
	if err := oldStore.save(ctx, cur.Bytes()); err != nil {
		return err
	}
	if err := c.kv.StorageSet(ctx, oldUntilKey, until); err != nil {
		_ = oldStore.remove(ctx)
		return err
	}
	if err := c.keys.save(ctx, fresh.Bytes()); err != nil {
		_ = c.kv.StorageDelete(ctx, oldUntilKey)
		_ = oldStore.remove(ctx)
		return err
	}
	c.mu.Lock()
	c.static, c.old, c.oldUntil = fresh, cur, until
	c.mu.Unlock()
	logEvent("tunnel rotate", "kid", fresh.KeyID(), "old", cur.KeyID())
	c.changed()
	return nil
}

func (c *tunnelClient) info() *tunnelInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.on || c.url == "" || c.static == nil {
		return nil
	}
	return &tunnelInfo{URL: c.url, Key: base64.RawURLEncoding.EncodeToString(c.static.Public()), KID: c.static.KeyID()}
}

func (c *tunnelClient) pairInfo(ctx context.Context) (*tunnelInfo, error) {
	if c == nil {
		return nil, nil
	}
	c.mu.Lock()
	on, base, replaced := c.on, c.url, c.replaced
	c.mu.Unlock()
	if !on || base == "" || replaced {
		return nil, nil
	}
	s, err := c.key(ctx)
	if err != nil {
		return nil, err
	}
	return &tunnelInfo{URL: base, Key: base64.RawURLEncoding.EncodeToString(s.Public()), KID: s.KeyID()}, nil
}

func (c *tunnelClient) syncTickets(ctx context.Context, keep ...string) error {
	c.mu.Lock()
	run := c.on && c.allowed && !c.replaced && c.stream != ""
	stream := c.stream
	c.mu.Unlock()
	base, token := c.relay.pushTarget()
	if !run || base == "" || c.tickets == nil {
		return nil
	}
	err := c.putTickets(ctx, macConn{base: base, token: token, stream: stream}, c.tickets(keep...))
	c.mu.Lock()
	c.dirty = err != nil && !errors.Is(err, errReplaced)
	c.mu.Unlock()
	return err
}

func (c *tunnelClient) putTickets(ctx context.Context, mc macConn, set []tunnelrelay.Ticket) error {
	body, err := json.Marshal(map[string][]tunnelrelay.Ticket{"tickets": set})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, mc.base+macTicketsPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	mc.auth(req)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", errRelayUnreachable, err)
	}
	defer res.Body.Close()
	if res.Header.Get(tunnelrelay.HeaderRelay) != "1" {
		return fmt.Errorf("%w: %d without %s", errRelayUnreachable, res.StatusCode, tunnelrelay.HeaderRelay)
	}
	if res.StatusCode == http.StatusNoContent {
		return nil
	}
	msg := relayError(res.Body)
	if res.StatusCode == http.StatusConflict && msg == tunnelrelay.NotCurrentStream {
		return fmt.Errorf("%w: %s", errReplaced, msg)
	}
	return fmt.Errorf("relay: %d %s", res.StatusCode, msg)
}

func (mc macConn) auth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+mc.token)
	if mc.stream != "" {
		req.Header.Set(tunnelrelay.HeaderStream, mc.stream)
	}
}

func (c *tunnelClient) restart() {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.stopLocked()
	base, token := c.relay.pushTarget()
	mc := macConn{base: base, token: token}
	c.mu.Lock()
	on, allowed := c.on, c.allowed
	c.url, c.connected, c.err, c.replaced = "", false, "", false
	if on {
		c.url = base
		switch {
		case base == "" && c.relay.relayMode() != relayAuto:
			c.err = errTunnelNoRelay.Error()
		case base != "" && !allowed:
			c.err = errAccessOff.Error()
		}
	}
	c.mu.Unlock()
	defer c.changed()
	run := on && allowed && base != ""
	if !run || mc != c.conn {
		c.sess.clear()
	}
	c.conn = mc
	if !run {
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	done := make(chan struct{})
	c.cancel, c.done = cancel, done
	go func() {
		defer close(done)
		c.run(ctx, mc)
	}()
}

func (c *tunnelClient) stop() {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.stopLocked()
	c.sess.clear()
}

func (c *tunnelClient) stopLocked() {
	if c.cancel == nil {
		return
	}
	c.cancel()
	<-c.done
	c.cancel, c.done = nil, nil
}

func (c *tunnelClient) status() tunnelStatus {
	c.mu.Lock()
	st := tunnelStatus{On: c.on, URL: c.url, Connected: c.connected, Error: c.err, Channels: c.sess.count()}
	allowed := c.allowed
	c.mu.Unlock()
	st.State, st.Detail = c.stateDetail(st, allowed)
	return st
}

func (c *tunnelClient) stateDetail(st tunnelStatus, allowed bool) (string, string) {
	rv := c.relay.view(allowed)
	switch {
	case !st.On:
		return tunnelOff, "Off"
	case !allowed:
		return tunnelOff, "Companion access is off"
	case st.Connected:
		return tunnelConnected, "Connected through " + relayHost(st.URL)
	case st.Error != "":
		return tunnelError, oneLine(st.Error)
	case st.URL == "" && rv.State == relayStateSigningUp:
		return tunnelConnecting, rv.Detail
	case st.URL == "" || rv.State == relayStateError:
		return tunnelError, rv.Detail
	}
	return tunnelConnecting, "Connecting to " + relayHost(st.URL) + "…"
}

func oneLine(msg string) string {
	switch {
	case strings.HasPrefix(msg, errRelayUnreachable.Error()):
		return "Relay unreachable, trying again"
	case strings.HasPrefix(msg, "relay closed the stream"):
		return "Relay closed the stream, trying again"
	}
	line, _, _ := strings.Cut(msg, "\n")
	return line
}

func (c *tunnelClient) setState(connected bool, err string) {
	c.mu.Lock()
	same := c.connected == connected && c.err == err
	c.connected, c.err = connected, err
	c.mu.Unlock()
	if !same {
		c.changed()
	}
}

func (c *tunnelClient) run(ctx context.Context, mc macConn) {
	go c.sweep(ctx)
	delay := c.backoffFirst
	for {
		started := time.Now()
		var err error
		if _, kerr := c.key(ctx); kerr != nil {
			err = kerr
			logEvent("tunnel", "err", kerr.Error())
		} else {
			err = c.connect(ctx, mc)
		}
		c.sess.cancelStreams()
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errReplaced) {
			c.mu.Lock()
			c.replaced = true
			c.mu.Unlock()
			c.setState(false, errReplaced.Error())
			logEvent("tunnel stream close", "err", err.Error())
			return
		}
		if time.Since(started) > tunnelrelay.BackoffReset {
			delay = c.backoffFirst
		}
		c.setState(false, err.Error())
		logEvent("tunnel stream close", "err", err.Error())
		t := time.NewTimer(jitter(delay))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		delay = min(delay*2, c.backoffMax)
	}
}

func (c *tunnelClient) sweep(ctx context.Context) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if c.sess.forgetIdle() > 0 {
				c.changed()
			}
			c.expireOld(ctx)
			c.retryTickets(ctx)
		}
	}
}

func (c *tunnelClient) retryTickets(ctx context.Context) {
	c.mu.Lock()
	dirty := c.dirty
	c.mu.Unlock()
	if !dirty {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, relaySyncTimeout)
	defer cancel()
	if err := c.syncTickets(ctx); err != nil {
		logEvent("tunnel tickets", "err", err.Error())
	}
}

func (c *tunnelClient) connect(run context.Context, mc macConn) error {
	ctx, cancel := context.WithCancelCause(run)
	defer cancel(nil)
	live := time.AfterFunc(c.liveness, func() { cancel(errNoLiveness) })
	defer live.Stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mc.base+macStreamPath, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+mc.token)
	req.Header.Set("Accept", "text/event-stream")
	res, err := c.client.Do(req)
	if err != nil {
		return streamErr(ctx, err)
	}
	defer res.Body.Close()
	if err := relayStatus(res); err != nil {
		if errors.Is(err, errTunnelToken) {
			c.relay.tokenRefused(mc.token)
		}
		return err
	}
	mc.stream = res.Header.Get(tunnelrelay.HeaderStream)
	c.mu.Lock()
	c.stream = mc.stream
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.stream == mc.stream {
			c.stream = ""
		}
		c.mu.Unlock()
	}()
	if c.tickets != nil {
		sctx, scancel := context.WithTimeout(ctx, relaySyncTimeout)
		serr := c.putTickets(sctx, mc, c.tickets())
		scancel()
		if serr != nil {
			return fmt.Errorf("tickets: %w", serr)
		}
		c.mu.Lock()
		c.dirty = false
		c.mu.Unlock()
	}
	logEvent("tunnel stream open")
	c.setState(true, "")
	r := &liveReader{r: res.Body, live: live, d: c.liveness}
	err = readSSE(r, func(f macFrame) error { return c.dispatch(run, mc, f) })
	return streamErr(ctx, err)
}

func (c *tunnelClient) dispatch(run context.Context, mc macConn, f macFrame) error {
	switch f.event {
	case frameOpen:
		var of openFrame
		if err := json.Unmarshal(f.data, &of); err != nil {
			return nil
		}
		go func() {
			if c.sess.respond(run, mc, c.answerKeys(run), of) {
				c.changed()
			}
		}()
	case frameReq:
		var rf reqFrame
		if err := json.Unmarshal(f.data, &rf); err != nil {
			return nil
		}
		go c.sess.serve(run, mc, rf)
	case frameClose:
		var cf closeFrame
		if err := json.Unmarshal(f.data, &cf); err != nil {
			return nil
		}
		if c.sess.closed(cf) {
			c.changed()
		}
	case frameReplaced:
		return errReplaced
	}
	return nil
}

func relayStatus(res *http.Response) error {
	if res.Header.Get(tunnelrelay.HeaderRelay) != "1" {
		return fmt.Errorf("%w: %d without %s", errRelayUnreachable, res.StatusCode, tunnelrelay.HeaderRelay)
	}
	switch res.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return errTunnelToken
	}
	return fmt.Errorf("relay: %d %s", res.StatusCode, relayError(res.Body))
}

func relayError(body io.Reader) string {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(body, relayMaxReply)).Decode(&e)
	return e.Error
}

func streamErr(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	if errors.Is(err, errReplaced) {
		return errReplaced
	}
	if err == nil || errors.Is(err, io.EOF) {
		return errors.New("relay closed the stream")
	}
	return fmt.Errorf("%w: %w", errRelayUnreachable, err)
}

func readSSE(r io.Reader, fn func(macFrame) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxSSELine)
	var f macFrame
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if f.event != "" {
				if err := fn(f); err != nil {
					return err
				}
			}
			f = macFrame{}
		case strings.HasPrefix(line, eventPrefix):
			f.event = strings.TrimPrefix(line, eventPrefix)
		case strings.HasPrefix(line, dataPrefix):
			f.data = append(f.data, strings.TrimPrefix(line, dataPrefix)...)
		}
	}
	return sc.Err()
}

type liveReader struct {
	r    io.Reader
	live *time.Timer
	d    time.Duration
}

func (l *liveReader) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	if n > 0 {
		l.live.Reset(l.d)
	}
	return n, err
}

func jitter(d time.Duration) time.Duration {
	f := 1 + tunnelrelay.BackoffJitter*(2*rand.Float64()-1)
	return time.Duration(float64(d) * f)
}
