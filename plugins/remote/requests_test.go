package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
)

func TestRepeatAfterDoneDoesNotRerun(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")

	_, first := f.call(t, token, "command", commandWithID("r1"))
	_, second := f.call(t, token, "command", commandWithID("r1"))

	assert.JSONEq(t, `{"ran":"agents.sessions"}`, string(first.Result))
	assert.JSONEq(t, string(first.Result), string(second.Result))
	assert.Len(t, f.backend.calls(), 1)
}

func TestConcurrentRepeatWaits(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")
	entered, release := f.backend.holdNextRun()

	firstDone := make(chan reply, 1)
	go func() {
		_, out := f.call(t, token, "command", commandWithID("r1"))
		firstDone <- out
	}()
	<-entered
	secondDone := make(chan reply, 1)
	go func() {
		_, out := f.call(t, token, "command", commandWithID("r1"))
		secondDone <- out
	}()
	select {
	case <-secondDone:
		close(release)
		t.Fatal("the repeat answered before the first run finished")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	first, second := <-firstDone, <-secondDone
	assert.Nil(t, second.Error)
	assert.JSONEq(t, string(first.Result), string(second.Result))
	assert.Len(t, f.backend.calls(), 1)
}

func TestDroppedRequestStillCompletes(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")
	entered, release := f.backend.holdNextRun()
	seen := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		go func() {
			<-r.Context().Done()
			close(seen)
		}()
		f.srv.handler().ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(t.Context())
	dropped := make(chan error, 1)
	go func() {
		dropped <- postWithContext(ctx, ts.URL, token, commandWithID("r1"))
	}()
	<-entered
	cancel()
	require.Error(t, <-dropped)
	<-seen
	close(release)

	require.Eventually(t, func() bool { return strings.Contains(f.kv.get(requestKey("r1")), `"done"`) }, 2*time.Second, 5*time.Millisecond)
	_, again := f.call(t, token, "command", commandWithID("r1"))

	assert.Nil(t, again.Error)
	assert.JSONEq(t, `{"ran":"agents.sessions"}`, string(again.Result))
	assert.Len(t, f.backend.calls(), 1)
}

func TestOnlyFailuresBeforeDispatchAreRetried(t *testing.T) {
	dispatched := &jsonrpc2.Error{Code: contract.CodePluginUnavailable, Message: "plugin unavailable: plugin agents command.run: context deadline exceeded"}
	dispatched.SetError(contract.ErrorReason{Reason: contract.ReasonUnknown})
	tests := []struct {
		name string
		err  error
		code string
		runs int
	}{
		{"plugin not started", &jsonrpc2.Error{Code: contract.CodePluginUnavailable, Message: "plugin unavailable: agents"}, codeFailed, 2},
		{"timed out after dispatch", dispatched, contract.ReasonUnknown, 1},
		{"timed out inside the plugin", pluginError("context deadline exceeded", ""), contract.ReasonUnknown, 1},
		{"remote gave up waiting", context.DeadlineExceeded, contract.ReasonUnknown, 1},
		{"refusal", pluginError("no such session", ""), codeRefused, 1},
		{"stale", pluginError("task changed", contract.ReasonStale), contract.ReasonStale, 1},
		{"invalid params", &jsonrpc2.Error{Code: jsonrpcInvalid, Message: "bad args"}, codeInvalidParams, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			token := f.pair(t, "phone")
			f.backend.failNextRun(tt.err)

			_, first := f.call(t, token, "command", commandWithID("r1"))
			_, second := f.call(t, token, "command", commandWithID("r1"))

			require.NotNil(t, first.Error)
			assert.Equal(t, tt.code, first.Error.Code)
			assert.Len(t, f.backend.calls(), tt.runs)
			if tt.runs == 1 {
				assert.Equal(t, first.Error, second.Error)
			} else {
				assert.Nil(t, second.Error)
			}
		})
	}
}

func pluginError(message, reason string) error {
	err := &jsonrpc2.Error{Code: contract.CodePluginError, Message: "plugin agents command.run: " + message}
	err.SetError(contract.PluginErrorData{Plugin: "agents", Method: "command.run", Message: message, Reason: reason})
	return err
}

func TestRunningFromOldProcessIsUnknown(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")
	require.NoError(t, f.kv.StorageSet(t.Context(), requestKey("r1"), requestRecord{State: requestRunning, At: f.clock.Now()}))

	_, out := f.call(t, token, "command", commandWithID("r1"))

	require.NotNil(t, out.Error)
	assert.Equal(t, contract.ReasonUnknown, out.Error.Code)
	assert.Empty(t, f.backend.calls())
}

func TestOversizedResultIsKeptAsDone(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")
	f.backend.reply = json.RawMessage(`{"big":"` + strings.Repeat("x", 300<<10) + `"}`)
	f.kv.limit = 256 << 10

	_, first := f.call(t, token, "command", commandWithID("r1"))
	_, second := f.call(t, token, "command", commandWithID("r1"))

	assert.Nil(t, first.Error)
	assert.Len(t, first.Result, len(f.backend.reply))
	assert.Nil(t, second.Error)
	assert.JSONEq(t, `null`, string(second.Result))
	assert.Len(t, f.backend.calls(), 1)
}

func TestRequestRecordsArePrunedAfterAWeek(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")
	f.call(t, token, "command", commandWithID("old"))
	f.clock.advance(requestKeep - time.Hour)
	f.call(t, token, "command", commandWithID("new"))
	f.clock.advance(2 * time.Hour)

	require.NoError(t, f.srv.requests.prune(t.Context()))

	assert.Empty(t, f.kv.get(requestKey("old")))
	assert.NotEmpty(t, f.kv.get(requestKey("new")))
}

func TestBadRequestIDIsRefused(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")

	for _, id := range []string{"a/b", strings.Repeat("a", maxRequestID+1)} {
		_, out := f.call(t, token, "command", commandWithID(id))
		require.NotNil(t, out.Error, id)
		assert.Equal(t, codeInvalidParams, out.Error.Code)
	}
	assert.Empty(t, f.backend.calls())
}

func commandWithID(id string) map[string]any {
	return map[string]any{"id": "agents.sessions", "workspace": "prj_1", "requestId": id}
}

func postWithContext(ctx context.Context, base, token string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	body, err := json.Marshal(rpcRequest{Method: "command", Params: raw})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/rpc", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	return res.Body.Close()
}
