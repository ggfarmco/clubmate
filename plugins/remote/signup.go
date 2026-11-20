package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"clubmate/internal/buildinfo"
	"clubmate/pkg/report"
)

const (
	relayURLEnv            = "CLUBMATE_RELAY_URL"
	pushRelayModeKey       = "pushRelayMode"
	pushRelaySignupNextKey = "pushRelaySignupNext"
	pushRelayBlockedKey    = "pushRelayBlocked"
	relayMacsPath          = "/v1/macs"

	relayAuto   = "auto"
	relayManual = "manual"
	relayOff    = "off"

	relayStateNone      = "none"
	relayStateSigningUp = "signing-up"
	relayStateReady     = "ready"
	relayStateError     = "error"

	signupFirst     = time.Minute
	signupMax       = time.Hour
	signupJitter    = 0.2
	signupTimeout   = 30 * time.Second
	resignEvery     = 10 * time.Minute
	maxRetryAfter   = 24 * time.Hour
	detailTestMode  = "sign-up off in test mode"
	detailAccessOff = "Signs up when companion access is on"
	detailSigningUp = "Signing up to the Clubmate relay…"
)

var (
	errSignupTestMode = errors.New(detailTestMode)
	errSignupFailed   = errors.New("sign up to the Clubmate relay failed")
	errSignupAutoArgs = errors.New("auto cannot be combined with url or token")
)

type relayView struct {
	Mode   string `json:"mode"`
	URL    string `json:"url"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

type signupReply struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

type signupError struct {
	status int
	retry  time.Duration
	msg    string
}

func (e *signupError) Error() string {
	return fmt.Sprintf("relay sign-up: %d %s", e.status, e.msg)
}

func relayDefault() string {
	return cmp.Or(os.Getenv(relayURLEnv), buildinfo.RelayURL)
}

func signupBase() (string, error) {
	if os.Getenv(relayURLEnv) == "" && os.Getenv(testModeEnv) == "1" {
		return "", errSignupTestMode
	}
	base, ok := httpsBase(relayDefault())
	if !ok {
		return "", errRelayNotHTTPS
	}
	return base, nil
}

func relayHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func (c *relayClient) bind(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx = ctx
}

func (c *relayClient) baseCtx() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

func (c *relayClient) notify() {
	c.mu.Lock()
	changed := c.changed
	c.mu.Unlock()
	if changed != nil {
		changed()
	}
}

func (c *relayClient) relayMode() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mode
}

func (c *relayClient) needsLocked() bool {
	return c.mode == relayAuto && (c.token == "" || c.resign)
}

func (c *relayClient) view(access bool) relayView {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := relayView{Mode: c.mode, URL: c.url, State: relayStateNone}
	switch c.mode {
	case relayOff:
		v.URL, v.Detail = "", "Off"
		return v
	case relayManual:
		v.State, v.Detail = relayStateReady, "Using the relay token set by hand for "+relayHost(c.url)
		return v
	}
	if c.token != "" && !c.resign {
		v.State, v.Detail = relayStateReady, "Signed up to "+relayHost(c.url)
		return v
	}
	base, err := signupBase()
	v.URL = cmp.Or(c.url, base)
	switch {
	case errors.Is(err, errSignupTestMode):
		v.Detail = detailTestMode
	case err != nil:
		v.URL = cmp.Or(c.url, relayDefault())
		v.State, v.Detail = relayStateError, "The relay address is not valid"
	case c.blocked:
		v.State, v.Detail = relayStateError, "This Mac is blocked by the relay"
	case c.state != "":
		v.State, v.Detail = c.state, c.detail
	case !access:
		v.Detail = detailAccessOff
	default:
		v.State, v.Detail = relayStateSigningUp, detailSigningUp
	}
	return v
}

func (c *relayClient) setAccess(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.access = on
	if !on {
		c.stopLoopLocked()
		c.state, c.detail = "", ""
	}
}

func (c *relayClient) resume(ctx context.Context) {
	if err := c.kv.StorageDelete(ctx, pushRelayBlockedKey); err != nil {
		logf("forget the relay block: %v", err)
	}
	c.mu.Lock()
	c.blocked = false
	c.next = time.Time{}
	c.stopLoopLocked()
	c.mu.Unlock()
}

func (c *relayClient) setAuto(ctx context.Context) error {
	c.cfgMu.Lock()
	defer c.cfgMu.Unlock()
	c.mu.Lock()
	same := c.mode == relayAuto
	c.mu.Unlock()
	if !same {
		if err := c.writeConfig(ctx, relayAuto, "", ""); err != nil {
			return err
		}
	}
	if err := c.kv.StorageDelete(ctx, pushRelaySignupNextKey); err != nil {
		return err
	}
	if err := c.kv.StorageDelete(ctx, pushRelayBlockedKey); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !same {
		c.mode, c.url, c.token, c.registered = relayAuto, "", "", ""
		c.state, c.detail, c.resign = "", "", false
		c.gen++
		c.stopLoopLocked()
	}
	c.blocked = false
	c.next = time.Time{}
	return nil
}

func (c *relayClient) ensure() {
	c.mu.Lock()
	if !c.access || c.running || c.blocked || !c.needsLocked() {
		c.mu.Unlock()
		return
	}
	if _, err := signupBase(); err != nil {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(c.baseCtxLocked())
	c.loopID++
	id := c.loopID
	c.running, c.cancelLoop = true, cancel
	c.mu.Unlock()
	go c.loop(ctx, id)
}

func (c *relayClient) baseCtxLocked() context.Context {
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

func (c *relayClient) stopLoopLocked() {
	if c.cancelLoop != nil {
		c.cancelLoop()
	}
	c.running, c.cancelLoop = false, nil
	c.loopID++
}

func (c *relayClient) loop(ctx context.Context, id int) {
	defer func() {
		c.mu.Lock()
		if c.loopID == id {
			c.running, c.cancelLoop = false, nil
		}
		c.mu.Unlock()
	}()
	delay := c.first
	for {
		c.mu.Lock()
		next := c.next
		c.mu.Unlock()
		if d := next.Sub(c.now()); d > 0 && !sleepCtx(ctx, d) {
			return
		}
		err := c.attempt(ctx)
		if ctx.Err() != nil {
			return
		}
		c.mu.Lock()
		stop := !c.needsLocked() || c.blocked || c.loopID != id
		c.mu.Unlock()
		if stop {
			return
		}
		wait := signupWait(delay)
		delay = min(delay*2, c.max)
		if se, ok := errors.AsType[*signupError](err); ok && se.retry > 0 {
			wait = se.retry
		}
		c.scheduleNext(ctx, c.now().Add(wait))
	}
}

func (c *relayClient) scheduleNext(ctx context.Context, at time.Time) {
	c.mu.Lock()
	c.next = at
	c.mu.Unlock()
	if err := c.kv.StorageSet(ctx, pushRelaySignupNextKey, at); err != nil {
		logf("store the next relay sign-up: %v", err)
	}
	logEvent("relay signup retry", "at", at.UTC().Format(time.RFC3339))
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func signupWait(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (1 + signupJitter*(2*rand.Float64()-1)))
}

func (c *relayClient) attempt(ctx context.Context) error {
	c.attemptMu.Lock()
	signed, err := c.attemptLocked(ctx)
	c.attemptMu.Unlock()
	if signed {
		c.mu.Lock()
		hook := c.onSigned
		c.mu.Unlock()
		if hook != nil {
			hook(ctx)
		}
		c.notify()
	}
	return err
}

func (c *relayClient) attemptLocked(ctx context.Context) (bool, error) {
	base, err := signupBase()
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	if !c.needsLocked() {
		c.mu.Unlock()
		return false, nil
	}
	gen := c.gen
	c.state, c.detail = relayStateSigningUp, detailSigningUp
	c.mu.Unlock()
	c.notify()
	reply, err := c.post(ctx, base)
	if err != nil {
		c.fail(err)
		return false, err
	}
	if err := c.storeSignup(ctx, gen, base, reply.Token); err != nil {
		c.fail(err)
		return false, err
	}
	return true, nil
}

func (c *relayClient) post(ctx context.Context, base string) (signupReply, error) {
	key, err := loadReportKey(c.dir)
	if err != nil {
		return signupReply{}, err
	}
	body := []byte("{}")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+relayMacsPath, bytes.NewReader(body))
	if err != nil {
		return signupReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	report.Sign(req, key, body, c.now())
	res, err := c.client.Do(req)
	if err != nil {
		return signupReply{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, relayMaxReply))
	if err != nil {
		return signupReply{}, err
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return signupReply{}, &signupError{status: res.StatusCode, retry: retryAfter(res.Header), msg: strings.TrimSpace(string(raw))}
	}
	var out signupReply
	if err := json.Unmarshal(raw, &out); err != nil || out.Token == "" {
		return signupReply{}, fmt.Errorf("relay sign-up reply: %w", cmp.Or(err, errors.New("no token")))
	}
	return out, nil
}

func retryAfter(h http.Header) time.Duration {
	sec, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil || sec <= 0 {
		return 0
	}
	return min(time.Duration(sec)*time.Second, maxRetryAfter)
}

func (c *relayClient) storeSignup(ctx context.Context, gen int, base, token string) error {
	c.cfgMu.Lock()
	defer c.cfgMu.Unlock()
	c.mu.Lock()
	stale := c.gen != gen
	c.mu.Unlock()
	if stale {
		return context.Canceled
	}
	if err := c.writeConfig(ctx, relayAuto, base, token); err != nil {
		return err
	}
	if err := c.kv.StorageDelete(ctx, pushRelaySignupNextKey); err != nil {
		logf("forget the next relay sign-up: %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode, c.url, c.token, c.registered = relayAuto, base, token, ""
	c.state, c.detail, c.resign, c.blocked, c.next = relayStateReady, "", false, false, time.Time{}
	logEvent("relay signup", "host", relayHost(base))
	return nil
}

func (c *relayClient) fail(err error) {
	state, detail := relayStateError, signupDetail(err)
	if errors.Is(err, context.Canceled) {
		c.mu.Lock()
		c.state, c.detail = "", ""
		c.mu.Unlock()
		return
	}
	blocked := false
	if se, ok := errors.AsType[*signupError](err); ok && se.status == http.StatusForbidden {
		blocked = true
	}
	if blocked {
		if err := c.kv.StorageSet(c.baseCtx(), pushRelayBlockedKey, true); err != nil {
			logf("store the relay block: %v", err)
		}
	}
	c.mu.Lock()
	c.blocked = c.blocked || blocked
	c.state, c.detail = state, detail
	c.mu.Unlock()
	logEvent("relay signup failed", "err", err.Error())
	c.notify()
}

func signupDetail(err error) string {
	if se, ok := errors.AsType[*signupError](err); ok {
		switch se.status {
		case http.StatusForbidden:
			return "This Mac is blocked by the relay"
		case http.StatusTooManyRequests:
			return "Too many sign-ups, trying again later"
		case http.StatusServiceUnavailable:
			return "The relay takes no new Macs now, trying again later"
		case http.StatusUnauthorized:
			return "The relay refused this Mac's signature, check the clock"
		case http.StatusNotFound:
			return "This relay does not take sign-ups"
		}
		return fmt.Sprintf("The relay refused the sign-up (%d), trying again", se.status)
	}
	if errors.Is(err, errSignupTestMode) {
		return detailTestMode
	}
	if errors.Is(err, errRelayNotHTTPS) {
		return "The relay address is not valid"
	}
	if errors.Is(err, errNoStateDir) {
		return "This install has no key to sign up with"
	}
	if _, ok := errors.AsType[*url.Error](err); ok {
		return "Relay unreachable, trying again"
	}
	return "Sign-up failed, trying again"
}

func (c *relayClient) signUpNow(ctx context.Context) error {
	if err := c.setAuto(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, signupTimeout)
	defer cancel()
	if err := c.attempt(ctx); err != nil {
		return fmt.Errorf("%w: %s", errSignupFailed, signupDetail(err))
	}
	return nil
}

func (c *relayClient) tokenRefused(token string) {
	c.mu.Lock()
	now := c.now()
	ok := c.mode == relayAuto && token != "" && c.token == token && !c.resign && !c.blocked && (c.lastResign.IsZero() || now.Sub(c.lastResign) >= resignEvery)
	if ok {
		c.resign, c.lastResign, c.next = true, now, time.Time{}
	}
	c.mu.Unlock()
	if ok {
		logEvent("relay signup again", "why", "token refused")
		c.ensure()
		c.notify()
	}
}
