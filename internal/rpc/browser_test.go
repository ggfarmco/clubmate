package rpc_test

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

const browserOwner = "agt_0123456789abcdef"

type browserApp struct {
	mu    sync.Mutex
	calls []rpc.BrowserCall
	reply string
}

func (a *browserApp) got() []rpc.BrowserCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]rpc.BrowserCall(nil), a.calls...)
}

func newBrowserApp(t *testing.T, h *testutil.PluginHarness, reply string) *browserApp {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server := h.Srv.ServeConn(context.Background(), jsonrpc2.NewPlainObjectStream(serverSide), rpc.Caller{})
	a := &browserApp{reply: reply}
	handler := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		if req.Method != "browser.call" {
			return nil, nil
		}
		var c rpc.BrowserCall
		if err := json.Unmarshal(*req.Params, &c); err != nil {
			return nil, err
		}
		a.mu.Lock()
		a.calls = append(a.calls, c)
		a.mu.Unlock()
		return json.RawMessage(a.reply), nil
	})
	conn := jsonrpc2.NewConn(context.Background(), jsonrpc2.NewPlainObjectStream(clientSide), jsonrpc2.AsyncHandler(handler))
	t.Cleanup(func() {
		conn.Close()
		server.Close()
	})
	require.NoError(t, conn.Call(t.Context(), "client.register", map[string]any{"handles": []string{"browser.call"}}, nil))
	return a
}

func browserRun(t *testing.T, c *rpc.Client, workspaceID, id string, extra map[string]any, args map[string]any) (json.RawMessage, error) {
	t.Helper()
	params := map[string]any{"id": id, "workspaceId": workspaceID, "agent": true, "args": args}
	for k, v := range extra {
		params[k] = v
	}
	var out json.RawMessage
	err := c.Call(t.Context(), "command.run", params, &out)
	return out, err
}

func TestBrowserWithoutAppFailsFast(t *testing.T) {
	h := testutil.NewHarness(t)
	w := h.CreateWorkspace(t, "demo")
	h.Start(t)
	_, err := browserRun(t, h.Client(t), w.ID, "browser.open", nil, map[string]any{"url": "https://example.com"})
	assert.ErrorContains(t, err, "the Clubmate app is not running")
}

func TestBrowserForwardsOwnerTabActionArgs(t *testing.T) {
	h := testutil.NewHarness(t)
	w := h.CreateWorkspace(t, "demo")
	h.Start(t)
	app := newBrowserApp(t, h, `{"url":"https://example.com/","status":200,"title":"Ex","blocked":false}`)
	out, err := browserRun(t, h.Client(t), w.ID, "browser.open", map[string]any{"caller": browserOwner}, map[string]any{"url": "https://example.com", "tab": "b"})
	require.NoError(t, err)
	_, err = browserRun(t, h.Client(t), w.ID, "browser.open", nil, map[string]any{"url": "https://example.com", "tab": "a/b"})
	assert.ErrorContains(t, err, "tab: want 1-32 of a-z 0-9 _ -")
	assert.JSONEq(t, app.reply, string(out))
	calls := app.got()
	require.Len(t, calls, 1)
	assert.Equal(t, [3]string{browserOwner, "b", "open"}, [3]string{calls[0].Owner, calls[0].Tab, calls[0].Action})
	assert.JSONEq(t, `{"url":"https://example.com"}`, string(calls[0].Args))
	assert.Zero(t, rpc.BrowserURLCount(h.Srv))
}

func TestBrowserOwnerFallsBackToTerminalThenDefault(t *testing.T) {
	h := testutil.NewHarness(t)
	w := h.CreateWorkspace(t, "demo")
	h.Start(t)
	app := newBrowserApp(t, h, `{}`)
	c := h.Client(t)
	args := map[string]any{"ref": "e1"}
	_, err := browserRun(t, c, w.ID, "browser.click", map[string]any{"terminal": "ses_0123456789abcdef"}, args)
	require.NoError(t, err)
	_, err = browserRun(t, c, w.ID, "browser.click", nil, args)
	require.NoError(t, err)
	calls := app.got()
	require.Len(t, calls, 2)
	assert.Equal(t, [2]string{"ses_0123456789abcdef", "main"}, [2]string{calls[0].Owner, calls[0].Tab})
	assert.Equal(t, [2]string{"default", "main"}, [2]string{calls[1].Owner, calls[1].Tab})
}

func TestBrowserGuardSeesActionAndLastURL(t *testing.T) {
	h := testutil.NewHarness(t)
	w := h.CreateWorkspace(t, "demo")
	h.Start(t)
	app := newBrowserApp(t, h, `{"url":"https://a.test/after"}`)
	var seen []rpc.BrowserGuardInput
	var deny error
	rpc.SetBrowserGuard(h.Srv, func(_ context.Context, in rpc.BrowserGuardInput) error {
		seen = append(seen, in)
		return deny
	})
	c := h.Client(t)
	extra := map[string]any{"caller": browserOwner}
	_, err := browserRun(t, c, w.ID, "browser.open", extra, map[string]any{"url": "https://a.test/"})
	require.NoError(t, err)
	_, err = browserRun(t, c, w.ID, "browser.click", extra, map[string]any{"ref": "e1"})
	require.NoError(t, err)
	require.Len(t, seen, 2)
	assert.Equal(t, [3]string{browserOwner, "open", "https://a.test/"}, [3]string{seen[0].Session, seen[0].Action, seen[0].URL})
	assert.Equal(t, [3]string{browserOwner, "click", "https://a.test/after"}, [3]string{seen[1].Session, seen[1].Action, seen[1].URL})

	deny = assert.AnError
	_, err = browserRun(t, c, w.ID, "browser.click", extra, map[string]any{"ref": "e2"})
	assert.ErrorContains(t, err, assert.AnError.Error())
	assert.Len(t, app.got(), 2)
}

func notifyEvents(t *testing.T, h *testutil.PluginHarness) []map[string]any {
	t.Helper()
	events, err := h.Bus.Read(t.Context(), "notify.posted", 0, 10)
	require.NoError(t, err)
	out := make([]map[string]any, len(events))
	for i, e := range events {
		require.NoError(t, json.Unmarshal(e.Payload, &out[i]))
	}
	return out
}

func TestBrowserHandoffNotifiesThenWaits(t *testing.T) {
	h := testutil.NewHarness(t)
	w := h.CreateWorkspace(t, "demo")
	h.Start(t)
	app := newBrowserApp(t, h, `{"result":"done"}`)
	c := h.Client(t)
	out, err := browserRun(t, c, w.ID, "browser.handoff", map[string]any{"caller": browserOwner}, map[string]any{"reason": "captcha"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"result":"done"}`, string(out))
	got := notifyEvents(t, h)
	require.Len(t, got, 1)
	assert.Equal(t, [4]any{"needs-input", "captcha", "Browser needs you", browserOwner}, [4]any{got[0]["kind"], got[0]["message"], got[0]["title"], got[0]["session"]})
	calls := app.got()
	require.Len(t, calls, 1)
	assert.Equal(t, 300, calls[0].Timeout)

	_, err = browserRun(t, c, w.ID, "browser.handoff", map[string]any{"terminal": "ses_0123456789abcdef"}, map[string]any{"reason": "login"})
	require.NoError(t, err)
	got = notifyEvents(t, h)
	require.Len(t, got, 2)
	assert.Equal(t, [2]any{"login", nil}, [2]any{got[1]["message"], got[1]["session"]})
	assert.Len(t, app.got(), 2)
}

func TestBrowserHandoffNeverNotifiesWhenItCannotProceed(t *testing.T) {
	h := testutil.NewHarness(t)
	w := h.CreateWorkspace(t, "demo")
	h.Start(t)
	c := h.Client(t)
	args := map[string]any{"reason": "captcha"}
	_, err := browserRun(t, c, w.ID, "browser.handoff", nil, args)
	assert.ErrorContains(t, err, "the Clubmate app is not running")
	assert.Empty(t, notifyEvents(t, h))

	app := newBrowserApp(t, h, `{}`)
	rpc.SetBrowserGuard(h.Srv, func(context.Context, rpc.BrowserGuardInput) error { return assert.AnError })
	_, err = browserRun(t, c, w.ID, "browser.handoff", nil, args)
	assert.ErrorContains(t, err, assert.AnError.Error())
	assert.Empty(t, notifyEvents(t, h))
	assert.Empty(t, app.got())
}
