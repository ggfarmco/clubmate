package rpc_test

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
)

type confirmRequest struct {
	Title     string          `json:"title"`
	Workspace string          `json:"workspace"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args"`
	TimeoutMs int64           `json:"timeoutMs"`
	Session   *struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Workspace string `json:"workspace"`
	} `json:"session"`
}

type approver struct {
	conn  *jsonrpc2.Conn
	asked chan confirmRequest
	reply chan bool
}

func TestAgentCannotReachHidden(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","hidden":true,"provides":["echo.cap"]}`)
	c := h.Client(t)
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "projectId": p.ID}, nil))
	for _, id := range []string{"alpha.say", "echo.cap"} {
		err := c.Call(t.Context(), "command.run", map[string]any{"id": id, "projectId": p.ID, "agent": true}, nil)
		assert.ErrorIs(t, err, rpc.ErrNotFound, id)
	}
}

func TestAgentCallerReachesPlugin(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","agent":true}`)
	var out map[string]any
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "projectId": p.ID, "agent": true, "caller": "agt_x"}, &out))
	assert.Equal(t, "agt_x", out["caller"])
}

func TestAgentArgsExactKeys(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","agent":true,"args":{"properties":{"path":{"type":"string"},"items":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"}}}}}}}`)
	c := h.Client(t)
	var out map[string]any
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "projectId": p.ID, "agent": true, "args": map[string]any{"path": "a", "items": []any{map[string]any{"path": "a"}}}}, &out))
	for want, args := range map[string]map[string]any{
		"args.PATH":          {"path": "a", "PATH": "b"},
		"args.items[1].PATH": {"items": []any{map[string]any{"path": "a"}, map[string]any{"path": "a", "PATH": "b"}}},
	} {
		err := c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "projectId": p.ID, "agent": true, "args": args}, nil)
		require.ErrorIs(t, err, rpc.ErrInvalidParams)
		assert.ErrorContains(t, err, want+": not allowed in agent calls")
	}
}

func TestCapabilityNamedLikePrimitive(t *testing.T) {
	h, _ := startAlpha(t, `{"id":"alpha.say","title":"Say","provides":["workspace.list"]}`)
	var caps struct {
		Capabilities map[string]string `json:"capabilities"`
	}
	require.NoError(t, h.Client(t).Call(t.Context(), "command.capabilities", struct{}{}, &caps))
	assert.Equal(t, map[string]string{"workspace.list": "workspace.list"}, caps.Capabilities)
}

func TestAgentSessionCreateTakesNoCommand(t *testing.T) {
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t)
	c := h.Client(t)
	for _, field := range []string{"command", "env", "Command", "ENV"} {
		err := c.Call(t.Context(), "command.run", map[string]any{"id": "session.create", "projectId": p.ID, "agent": true, "args": map[string]any{field: []string{"/bin/sh"}}}, nil)
		require.ErrorIs(t, err, rpc.ErrInvalidParams)
		assert.ErrorContains(t, err, "args."+field+": not allowed in agent calls")
	}
}

func TestApproverDisconnectDenies(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","agent":true,"destructive":true}`)
	a := newApprover(t, h)
	c := h.Client(t)
	done := make(chan error, 1)
	go func() {
		done <- c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "projectId": p.ID, "agent": true}, nil)
	}()
	<-a.asked
	require.NoError(t, a.conn.Close())
	err := <-done
	require.ErrorIs(t, err, rpc.ErrDenied)
	assert.ErrorContains(t, err, "no app to approve")
	close(a.reply)
}

func TestDestructiveNeedsApproval(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Drop Everything","agent":true,"destructive":true,"args":{"properties":{"n":{"type":"integer"}}}}`)
	c := h.Client(t)
	run := func() (map[string]any, error) {
		var out map[string]any
		err := c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "projectId": p.ID, "agent": true, "args": map[string]any{"n": 1}}, &out)
		return out, err
	}
	_, err := run()
	require.ErrorIs(t, err, rpc.ErrDenied)
	assert.ErrorContains(t, err, "no app to approve")

	a := newApprover(t, h)
	for _, approved := range []bool{true, false} {
		done := make(chan struct{})
		var out map[string]any
		go func() {
			defer close(done)
			out, err = run()
		}()
		assert.Equal(t, confirmRequest{Title: "Drop Everything", Workspace: "demo", Command: "alpha.say", Args: json.RawMessage(`{"n":1}`), TimeoutMs: 60000}, <-a.asked)
		a.reply <- approved
		<-done
		if approved {
			require.NoError(t, err)
			assert.Equal(t, "alpha", out["plugin"])
			continue
		}
		require.ErrorIs(t, err, rpc.ErrDenied)
		assert.ErrorContains(t, err, "denied by user")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = run()
	}()
	<-a.asked
	_, err = run()
	require.ErrorIs(t, err, rpc.ErrDenied)
	assert.ErrorContains(t, err, "approval busy")
	a.reply <- false
	<-done
}

func TestDestructiveAgentArgsStrict(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","agent":true,"destructive":true,"args":{"properties":{"s":{"type":"string"},"n":{"type":"integer"}}}}`)
	a := newApprover(t, h)
	c := h.Client(t)
	tests := []struct {
		name string
		args any
		err  string
	}{
		{name: "undeclared key", args: map[string]any{"s": "x", "extra": "y"}, err: "args.extra: not allowed in agent calls"},
		{name: "overlong value", args: map[string]any{"s": strings.Repeat("é", 201)}, err: "args.s: longer than 200 characters"},
		{name: "non-object args", args: []int{1}, err: "args: want object"},
		{name: "integer beyond 2^53", args: map[string]any{"n": int64(1)<<53 + 1}, err: "args.n: number outside ±2^53"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "projectId": p.ID, "agent": true, "args": tt.args}, nil)
			require.ErrorIs(t, err, rpc.ErrInvalidParams)
			assert.ErrorContains(t, err, tt.err)
			assert.Empty(t, a.asked)
		})
	}
}

func TestApprovalPrefersWorkspaceClient(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","agent":true,"destructive":true}`)
	first := newApprover(t, h)
	second := newApprover(t, h)
	require.NoError(t, second.conn.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &struct{}{}))
	c := h.Client(t)
	done := make(chan error, 1)
	go func() {
		done <- c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "projectId": p.ID, "agent": true}, nil)
	}()
	<-second.asked
	second.reply <- true
	require.NoError(t, <-done)
	assert.Empty(t, first.asked)
}

func TestPrimitivesListed(t *testing.T) {
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t)
	c := h.Client(t)
	var cmds []rpc.Command
	require.NoError(t, c.Call(t.Context(), "command.list", struct{}{}, &cmds))
	var listed *rpc.Command
	for i := range cmds {
		if cmds[i].ID == "workspace.list" {
			listed = &cmds[i]
		}
	}
	require.NotNil(t, listed)
	assert.Equal(t, "core", listed.Plugin)
	assert.True(t, listed.Agent)
	var projects []project.Project
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "workspace.list", "agent": true}, &projects))
	require.Len(t, projects, 1)
	assert.Equal(t, p.ID, projects[0].ID)
}

func startAlpha(t *testing.T, command string) (*testutil.PluginHarness, project.Project) {
	t.Helper()
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDirAs(t, manifestJSON("alpha", `{"commands":[`+command+`]}`)))
	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].State == "running" }, 5*time.Second)
	return h, p
}

func newApprover(t *testing.T, h *testutil.PluginHarness) *approver {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server := h.Srv.ServeConn(context.Background(), jsonrpc2.NewPlainObjectStream(serverSide), rpc.Caller{})
	a := &approver{asked: make(chan confirmRequest, 4), reply: make(chan bool)}
	handler := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		if req.Method != "ui.confirm" {
			return nil, nil
		}
		var p confirmRequest
		if err := json.Unmarshal(*req.Params, &p); err != nil {
			return nil, err
		}
		a.asked <- p
		return map[string]bool{"approved": <-a.reply}, nil
	})
	a.conn = jsonrpc2.NewConn(context.Background(), jsonrpc2.NewPlainObjectStream(clientSide), jsonrpc2.AsyncHandler(handler))
	t.Cleanup(func() {
		a.conn.Close()
		server.Close()
	})
	require.NoError(t, a.conn.Call(t.Context(), "client.register", map[string]any{"handles": []string{"ui.confirm"}}, nil))
	return a
}

func TestAgentSendNeedsApproval(t *testing.T) {
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t)
	s, err := h.Svc.CreateSession(t.Context(), session.CreateRequest{ProjectID: p.ID, Title: "Build", Command: []string{"sh", "-c", "exec cat"}})
	require.NoError(t, err)
	c := h.Client(t)
	send := func() error {
		return c.Call(t.Context(), "command.run", map[string]any{"id": "session.send", "agent": true, "args": map[string]any{"id": s.ID, "text": "hi"}}, nil)
	}
	err = send()
	require.ErrorIs(t, err, rpc.ErrDenied)
	assert.ErrorContains(t, err, "no app to approve")
	a := newApprover(t, h)
	done := make(chan error, 1)
	go func() { done <- send() }()
	asked := <-a.asked
	require.NotNil(t, asked.Session)
	assert.Equal(t, [3]string{s.ID, "Build", "demo"}, [3]string{asked.Session.ID, asked.Session.Title, asked.Session.Workspace})
	a.reply <- false
	require.ErrorIs(t, <-done, rpc.ErrDenied)
}

func TestAppScopeCommandRunsWithoutProject(t *testing.T) {
	h, _ := startAlpha(t, `{"id":"alpha.say","title":"Say","remote":true,"scope":"app"},{"id":"alpha.other","title":"Other"}`)
	c := h.Client(t)
	cmds := pluginCommands(t, h)
	require.Len(t, cmds, 2)
	assert.True(t, cmds[0].Remote)
	assert.True(t, cmds[0].Running)
	assert.Equal(t, "app", cmds[0].Scope)
	assert.False(t, cmds[1].Remote)
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "projectId": ""}, nil))
	assert.ErrorIs(t, c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.other", "projectId": ""}, nil), rpc.ErrInvalidParams)
}

func TestPluginApprove(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.x","title":"X"}`)
	approve := func(command string) error {
		return h.PluginCall(t, "alpha", "ui.approve", map[string]any{"projectId": p.ID, "title": "Run setup", "command": command, "args": map[string]string{"sha256": "abc"}})
	}
	assert.ErrorContains(t, approve("beta.run"), "want a alpha. command")
	a := newApprover(t, h)
	done := make(chan error, 1)
	go func() { done <- approve("alpha.run") }()
	asked := <-a.asked
	assert.Equal(t, [3]string{"Run setup", "demo", "alpha.run"}, [3]string{asked.Title, asked.Workspace, asked.Command})
	assert.JSONEq(t, `{"sha256":"abc"}`, string(asked.Args))
	a.reply <- true
	require.NoError(t, <-done)
}
