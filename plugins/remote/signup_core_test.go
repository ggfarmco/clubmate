package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

const (
	remoteWait     = 15 * time.Second
	unusedDevice   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	unusedPushBody = `{"aps":{"alert":"x"}}`
)

type remoteEnv struct {
	t *testing.T
	h *testutil.PluginHarness
	c *rpc.Client
}

func newRemoteEnv(t *testing.T, relayBase string, seed map[string]any) *remoteEnv {
	t.Helper()
	t.Setenv(listenEnv, "127.0.0.1:0")
	t.Setenv(testModeEnv, "1")
	t.Setenv(relayURLEnv, relayBase)
	h := testutil.NewHarness(t)
	for k, v := range seed {
		raw, err := json.Marshal(v)
		require.NoError(t, err)
		require.NoError(t, h.KV.Set(t.Context(), "remote", k, raw))
	}
	h.Start(t, testutil.BuildPluginDir(t, "clubmate/plugins/remote", "plugins/remote/plugin.json"))
	e := &remoteEnv{t: t, h: h, c: h.Client(t)}
	testutil.Eventually(t, func() bool { return e.run("remote.status", nil, nil) == nil }, remoteWait)
	return e
}

func (e *remoteEnv) run(id string, args map[string]any, out any) error {
	return e.c.Call(e.t.Context(), "command.run", map[string]any{"id": id, "workspaceId": "", "args": args}, out)
}

func (e *remoteEnv) status() status {
	e.t.Helper()
	var st status
	require.NoError(e.t, e.run("remote.status", nil, &st))
	return st
}

func (e *remoteEnv) waitStatus(cond func(status) bool) status {
	e.t.Helper()
	var st status
	testutil.Eventually(e.t, func() bool {
		if e.run("remote.status", nil, &st) != nil {
			return false
		}
		return cond(st)
	}, remoteWait)
	return st
}

func (e *remoteEnv) enable() {
	e.t.Helper()
	require.NoError(e.t, e.run("remote.enable", map[string]any{"enabled": true}, nil))
}

func (e *remoteEnv) restart() {
	e.t.Helper()
	pids := e.h.Sup.PIDs()
	require.NoError(e.t, e.h.Sup.Restart("remote"))
	testutil.Eventually(e.t, func() bool {
		now := e.h.Sup.PIDs()
		return len(now) == 1 && now[0] != pids[0] && e.run("remote.status", nil, nil) == nil
	}, remoteWait)
}

func (e *remoteEnv) stored(key string) string {
	e.t.Helper()
	raw, found, err := e.h.KV.Get(e.t.Context(), "remote", key)
	require.NoError(e.t, err)
	if !found {
		return ""
	}
	return string(raw)
}

func (e *remoteEnv) forget(key string) {
	e.t.Helper()
	require.NoError(e.t, e.h.KV.Delete(e.t.Context(), "remote", key))
}

func TestAccessOnSignsUpToTheRealRelayOpensTheRelayPathAndSurvivesARestart(t *testing.T) {
	bin := testutil.BuildBinary(t, "clubmate/cmd/clubmate-push-relay")
	dir := t.TempDir()
	addr := freeAddr(t)
	tokens := filepath.Join(dir, "tokens")
	require.NoError(t, os.WriteFile(tokens, nil, 0o600))
	relay := exec.CommandContext(t.Context(), bin, "-listen", addr, "-key", apnsKey(t, dir), "-team-id", "TEAM000001", "-tokens", tokens, "-devices", filepath.Join(dir, "devices.json"), "-signup", "on")
	require.NoError(t, relay.Start())
	t.Cleanup(func() { _ = relay.Wait() })
	base := "http://" + addr
	testutil.Eventually(t, func() bool {
		res, err := http.Get(base + "/healthz")
		if err == nil {
			res.Body.Close()
		}
		return err == nil
	}, 10*time.Second)
	e := newRemoteEnv(t, base, nil)

	e.enable()
	st := e.waitStatus(func(st status) bool { return st.Push && st.Tunnel.State == tunnelConnected })
	var link pairResult
	require.NoError(t, e.run("remote.pair", nil, &link))
	token := e.stored(pushRelayTokenKey)
	pushed := pushUnregistered(t, base, strings.Trim(token, `"`))
	require.NoError(t, e.run("remote.set-tunnel", map[string]any{"on": false}, nil))
	var plain pairResult
	require.NoError(t, e.run("remote.pair", nil, &plain))
	require.NoError(t, e.run("remote.set-tunnel", map[string]any{"on": true}, nil))
	e.restart()
	again := e.waitStatus(func(st status) bool { return st.Push && st.Tunnel.State == tunnelConnected })

	assert.Equal(t, relayView{Mode: relayAuto, URL: base, State: relayStateReady, Detail: "Signed up to " + addr}, st.Relay)
	assert.True(t, st.PairReady)
	assert.Empty(t, st.PairHint)
	assert.True(t, link.Relay)
	assert.Contains(t, link.Link, "clubmate://pair?v=2&")
	assert.False(t, plain.Relay)
	assert.Contains(t, plain.Link, "clubmate://pair?v=1&")
	assert.Equal(t, http.StatusForbidden, pushed)
	assert.Equal(t, "true", e.stored(tunnelOnKey))
	assert.Equal(t, `"auto"`, e.stored(pushRelayModeKey))
	assert.Equal(t, token, e.stored(pushRelayTokenKey))
	assert.Equal(t, relayAuto, again.Relay.Mode)
}

func pushUnregistered(t *testing.T, base, token string) int {
	t.Helper()
	body, err := json.Marshal(relayRequest{Token: unusedDevice, Environment: "production", Type: "alert", Payload: json.RawMessage(unusedPushBody)})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+pushRelayPath, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	return res.StatusCode
}

func TestARestartKeepsTheNextSignUpTimeAndResumesWhenItIsDue(t *testing.T) {
	fs := newFakeSignup(t, signupStep{status: http.StatusServiceUnavailable})
	e := newRemoteEnv(t, fs.srv.URL, nil)

	e.enable()
	failed := e.waitStatus(func(st status) bool { return st.Relay.State == relayStateError })
	testutil.Eventually(t, func() bool { return e.stored(pushRelaySignupNextKey) != "" }, remoteWait)
	next := e.stored(pushRelaySignupNextKey)
	e.restart()
	testutil.Never(t, func() bool { return fs.callCount() > 1 }, time.Second)
	e.forget(pushRelaySignupNextKey)
	e.restart()
	ready := e.waitStatus(func(st status) bool { return st.Relay.State == relayStateReady })

	assert.Equal(t, "The relay takes no new Macs now, trying again later", failed.Relay.Detail)
	assert.Equal(t, relayAuto, failed.Relay.Mode)
	assert.NotEmpty(t, next)
	assert.Equal(t, 2, fs.callCount())
	assert.Equal(t, strconv.Quote("cmr_2"), e.stored(pushRelayTokenKey))
	assert.True(t, ready.Push)
}

func TestAStoredFalseKeepsTheRelayPathOffAfterTheFirstSignUp(t *testing.T) {
	fs := newFakeSignup(t)
	e := newRemoteEnv(t, fs.srv.URL, map[string]any{tunnelOnKey: false})

	e.enable()
	st := e.waitStatus(func(st status) bool { return st.Relay.State == relayStateReady })

	assert.False(t, st.Tunnel.On)
	assert.Equal(t, tunnelOff, st.Tunnel.State)
	assert.Equal(t, "false", e.stored(tunnelOnKey))
}

func TestAMacWithAHandMadeTokenNeverSignsUpUntilAutoIsAskedFor(t *testing.T) {
	fs := newFakeSignup(t)
	e := newRemoteEnv(t, fs.srv.URL, map[string]any{pushRelayURLKey: fs.srv.URL, pushRelayTokenKey: "cmr_mine"})

	e.enable()
	manual := e.waitStatus(func(st status) bool { return st.Relay.Mode == relayManual })
	migrated := e.stored(pushRelayModeKey)
	e.restart()
	testutil.Never(t, func() bool { return fs.callCount() > 0 }, 500*time.Millisecond)
	require.NoError(t, e.run("remote.set-push-relay", map[string]any{"url": ""}, nil))
	off := e.status()
	require.NoError(t, e.run("remote.enable", map[string]any{"enabled": false}, nil))
	e.enable()
	testutil.Never(t, func() bool { return fs.callCount() > 0 }, 500*time.Millisecond)
	require.NoError(t, e.run("remote.set-push-relay", map[string]any{"auto": true}, nil))
	auto := e.waitStatus(func(st status) bool { return st.Relay.State == relayStateReady })

	assert.True(t, manual.Push)
	assert.Equal(t, `"manual"`, migrated)
	assert.Equal(t, relayOff, off.Relay.Mode)
	assert.False(t, off.Push)
	assert.Equal(t, relayAuto, auto.Relay.Mode)
	assert.Equal(t, 1, fs.callCount())
	assert.Error(t, e.run("remote.set-push-relay", map[string]any{"auto": true, "url": "https://relay.example", "token": "cmr_x"}, nil))
}

func TestABlockedMacStaysStoppedAcrossARestartUntilAccessIsTurnedOffAndOn(t *testing.T) {
	fs := newFakeSignup(t, signupStep{status: http.StatusForbidden})
	e := newRemoteEnv(t, fs.srv.URL, nil)

	e.enable()
	blocked := e.waitStatus(func(st status) bool { return st.Relay.Detail == "This Mac is blocked by the relay" })
	e.restart()
	testutil.Never(t, func() bool { return fs.callCount() > 1 }, time.Second)
	afterRestart := e.status()
	require.NoError(t, e.run("remote.enable", map[string]any{"enabled": false}, nil))
	e.enable()
	ready := e.waitStatus(func(st status) bool { return st.Relay.State == relayStateReady })

	assert.Equal(t, relayStateError, blocked.Relay.State)
	assert.Equal(t, "This Mac is blocked by the relay", afterRestart.Relay.Detail)
	assert.Equal(t, 2, fs.callCount())
	assert.True(t, ready.Push)
	assert.Empty(t, e.stored(pushRelayBlockedKey))
}

func TestAutoWithAccessOffWaitsForAccess(t *testing.T) {
	fs := newFakeSignup(t)
	e := newRemoteEnv(t, fs.srv.URL, map[string]any{pushRelayModeKey: relayOff})

	require.NoError(t, e.run("remote.set-push-relay", map[string]any{"auto": true}, nil))
	testutil.Never(t, func() bool { return fs.callCount() > 0 }, 500*time.Millisecond)
	waiting := e.status()
	e.enable()
	ready := e.waitStatus(func(st status) bool { return st.Relay.State == relayStateReady })

	assert.Equal(t, detailAccessOff, waiting.Relay.Detail)
	assert.True(t, ready.Push)
}
