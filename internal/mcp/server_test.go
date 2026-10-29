package mcp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/mcp"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

type reply struct {
	ID     json.RawMessage `json:"id"`
	Result struct {
		ProtocolVersion string `json:"protocolVersion"`
		Tools           []struct {
			Name string `json:"name"`
		} `json:"tools"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
	Error *struct {
		Code int `json:"code"`
	} `json:"error"`
}

type peer struct {
	in  *io.PipeWriter
	out *bufio.Reader
}

func manifest(id, commands string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q,"version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[%s]}}`, id, id, commands)
}

func startCore(t *testing.T) func(ctx context.Context) (mcp.Core, error) {
	t.Helper()
	t.Setenv("CLUBMATE_PROJECT_ID", "")
	h := testutil.NewHarness(t)
	_, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDirAs(t,
		manifest("alpha", `{"id":"alpha.say","title":"Open","agent":true,"provides":["editor.open"],"args":{"properties":{"path":{"type":"string"}}}},{"id":"alpha.secret","title":"Secret","hidden":true}`),
		manifest("terminals", `{"id":"terminals.close","title":"Close Terminal","agent":true,"destructive":true,"args":{"properties":{"sessionId":{"type":"string"}}}}`),
	))
	testutil.Eventually(t, func() bool {
		st := h.Sup.Statuses()
		return len(st) == 2 && st[0].State == "running" && st[1].State == "running"
	}, 5*time.Second)
	h.Client(t)
	return func(ctx context.Context) (mcp.Core, error) { return rpc.Dial(ctx, h.Dirs.CoreSocket()) }
}

func noCore(context.Context) (mcp.Core, error) {
	return nil, mcp.ErrNoCore
}

func start(t *testing.T, dial func(ctx context.Context) (mcp.Core, error)) *peer {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	srv := &mcp.Server{Dial: dial, Version: "test", Log: io.Discard}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ctx, inR, outW)
		outW.Close()
	}()
	t.Cleanup(func() {
		inW.Close()
		cancel()
		<-done
	})
	return &peer{in: inW, out: bufio.NewReader(outR)}
}

func (p *peer) send(t *testing.T, id any, method string, params any) {
	t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		msg["id"] = id
	}
	if params != nil {
		msg["params"] = params
	}
	raw, err := json.Marshal(msg)
	require.NoError(t, err)
	_, err = p.in.Write(append(raw, '\n'))
	require.NoError(t, err)
}

func (p *peer) recv(t *testing.T) reply {
	t.Helper()
	lines := make(chan []byte, 1)
	go func() {
		line, _ := p.out.ReadBytes('\n')
		lines <- line
	}()
	select {
	case line := <-lines:
		var r reply
		require.NoError(t, json.Unmarshal(line, &r), string(line))
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("no reply")
		return reply{}
	}
}

func (p *peer) call(t *testing.T, id int, method string, params any) reply {
	t.Helper()
	p.send(t, id, method, params)
	r := p.recv(t)
	require.JSONEq(t, fmt.Sprint(id), string(r.ID))
	return r
}

func names(r reply) []string {
	var out []string
	for _, tool := range r.Result.Tools {
		out = append(out, tool.Name)
	}
	return out
}

func TestMCPHandshakeAndTools(t *testing.T) {
	p := start(t, startCore(t))
	init := p.call(t, 1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}})
	assert.Equal(t, "2025-06-18", init.Result.ProtocolVersion)
	p.send(t, nil, "notifications/initialized", nil)
	tools := names(p.call(t, 2, "tools/list", nil))
	assert.Contains(t, tools, "editor_open")
	assert.Contains(t, tools, "workspace_list")
	assert.NotContains(t, tools, "alpha_secret")
	assert.NotContains(t, tools, "alpha_say")
	res := p.call(t, 3, "tools/call", map[string]any{"name": "workspace_list", "arguments": map[string]any{}})
	require.False(t, res.Result.IsError, res.Result.Content)
	require.Len(t, res.Result.Content, 1)
	assert.Equal(t, "text", res.Result.Content[0].Type)
	assert.Contains(t, res.Result.Content[0].Text, `"name":"demo"`)
}

func TestMCPDestructiveDenied(t *testing.T) {
	p := start(t, startCore(t))
	res := p.call(t, 1, "tools/call", map[string]any{"name": "terminals_close", "arguments": map[string]any{"sessionId": "ses_0123456789abcdef"}})
	assert.True(t, res.Result.IsError)
	assert.Contains(t, res.Result.Content[0].Text, "no app to approve")
}

func TestMCPFraming(t *testing.T) {
	p := start(t, noCore)
	p.send(t, nil, "notifications/whatever", nil)
	res := p.call(t, 7, "resources/list", nil)
	require.NotNil(t, res.Error)
	assert.Equal(t, -32601, res.Error.Code)
}

func TestMCPWithoutCore(t *testing.T) {
	p := start(t, noCore)
	tools := names(p.call(t, 1, "tools/list", nil))
	assert.Contains(t, tools, "workspace_list")
	assert.NotContains(t, tools, "editor_open")
	res := p.call(t, 2, "tools/call", map[string]any{"name": "workspace_list", "arguments": map[string]any{}})
	assert.True(t, res.Result.IsError)
	assert.Equal(t, "Clubmate isn't running", res.Result.Content[0].Text)
}
