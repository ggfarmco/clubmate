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

	"clubmate/internal/plugin"
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/internal/workspace"
)

type confirmRequest struct {
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	WorkspaceName string          `json:"workspaceName"`
	Command       string          `json:"command"`
	Args          json.RawMessage `json:"args"`
	Body          string          `json:"body"`
	Plugin        bool            `json:"plugin"`
	TimeoutMs     int64           `json:"timeoutMs"`
	Session       *confirmSession `json:"session"`
	From          *confirmSession `json:"from"`
}

type confirmSession struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	WorkspaceName string `json:"workspaceName"`
}

type callerRouter struct {
	rpc.Router
}

type approver struct {
	conn      *jsonrpc2.Conn
	asked     chan confirmRequest
	reply     chan bool
	withdrawn chan string
}

func TestAgentCannotReachHidden(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","hidden":true,"provides":["echo.cap"]}`)
	c := h.Client(t)
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": p.ID}, nil))
	for _, id := range []string{"alpha.say", "echo.cap"} {
		err := c.Call(t.Context(), "command.run", map[string]any{"id": id, "workspaceId": p.ID, "agent": true}, nil)
		assert.ErrorIs(t, err, rpc.ErrNotFound, id)
	}
}

func TestAgentCallerReachesPlugin(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","agent":true}`)
	var out map[string]any
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": p.ID, "agent": true, "caller": "agt_x"}, &out))
	assert.Equal(t, "agt_x", out["caller"])
}

func TestAgentArgsExactKeys(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","agent":true,"args":{"properties":{"path":{"type":"string"},"items":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"}}}}}}}`)
	c := h.Client(t)
	var out map[string]any
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": p.ID, "agent": true, "args": map[string]any{"path": "a", "items": []any{map[string]any{"path": "a"}}}}, &out))
	for want, args := range map[string]map[string]any{
		"args.PATH":          {"path": "a", "PATH": "b"},
		"args.items[1].PATH": {"items": []any{map[string]any{"path": "a"}, map[string]any{"path": "a", "PATH": "b"}}},
	} {
		err := c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": p.ID, "agent": true, "args": args}, nil)
		require.ErrorIs(t, err, rpc.ErrInvalidParams)
		assert.ErrorContains(t, err, want+": not allowed in agent calls")
	}
}

func TestAgentArgsRejectedWithoutSchema(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","agent":true}`)
	c := h.Client(t)
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": p.ID, "agent": true, "args": map[string]any{}}, nil))
	err := c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": p.ID, "agent": true, "args": map[string]any{"path": "a"}}, nil)
	require.ErrorIs(t, err, rpc.ErrInvalidParams)
	assert.ErrorContains(t, err, "args.path: not allowed in agent calls")
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
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t)
	c := h.Client(t)
	for _, field := range []string{"command", "env", "Command", "ENV"} {
		err := c.Call(t.Context(), "command.run", map[string]any{"id": "session.create", "workspaceId": p.ID, "agent": true, "args": map[string]any{field: []string{"/bin/sh"}}}, nil)
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
		done <- c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": p.ID, "agent": true}, nil)
	}()
	<-a.asked
	require.NoError(t, a.conn.Close())
	err := <-done
	require.ErrorIs(t, err, rpc.ErrDenied)
	assert.ErrorContains(t, err, "no app to approve")
	close(a.reply)
}

func TestCallerGoneWithdrawsApproval(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","agent":true,"destructive":true}`)
	a := newApprover(t, h)
	h.Client(t)
	c, err := rpc.Dial(t.Context(), h.Dirs.CoreSocket())
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	go func() {
		_ = c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": p.ID, "agent": true}, nil)
	}()
	asked := <-a.asked
	require.NotEmpty(t, asked.ID)
	require.NoError(t, c.Close())
	select {
	case id := <-a.withdrawn:
		assert.Equal(t, asked.ID, id)
	case <-time.After(5 * time.Second):
		t.Fatal("approval not withdrawn")
	}
	close(a.reply)
}

func TestDestructiveNeedsApproval(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Drop Everything","agent":true,"destructive":true,"args":{"properties":{"n":{"type":"integer"}}}}`)
	c := h.Client(t)
	run := func() (map[string]any, error) {
		var out map[string]any
		err := c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": p.ID, "agent": true, "args": map[string]any{"n": 1}}, &out)
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
		asked := <-a.asked
		assert.Equal(t, confirmRequest{ID: asked.ID, Title: "Drop Everything", WorkspaceName: "demo", Command: "alpha.say", Args: json.RawMessage(`{"n":1}`), TimeoutMs: 60000}, asked)
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
			err := c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": p.ID, "agent": true, "args": tt.args}, nil)
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
	require.NoError(t, second.conn.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, &struct{}{}))
	c := h.Client(t)
	done := make(chan error, 1)
	go func() {
		done <- c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": p.ID, "agent": true}, nil)
	}()
	<-second.asked
	second.reply <- true
	require.NoError(t, <-done)
	assert.Empty(t, first.asked)
}

func TestPrimitivesListed(t *testing.T) {
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
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
	var workspaces []workspace.Workspace
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "workspace.list", "agent": true}, &workspaces))
	require.Len(t, workspaces, 1)
	assert.Equal(t, p.ID, workspaces[0].ID)
}

func startAlpha(t *testing.T, command string) (*testutil.PluginHarness, workspace.Workspace) {
	t.Helper()
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDirAs(t, manifestJSON("alpha", `{"commands":[`+command+`]}`)))
	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].State == "running" }, 5*time.Second)
	return h, p
}

func newApprover(t *testing.T, h *testutil.PluginHarness) *approver {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server := h.Srv.ServeConn(context.Background(), jsonrpc2.NewPlainObjectStream(serverSide), rpc.Caller{})
	a := &approver{asked: make(chan confirmRequest, 4), reply: make(chan bool), withdrawn: make(chan string, 4)}
	handler := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		if req.Method == "ui.confirm.cancelled" {
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(*req.Params, &p); err != nil {
				return nil, err
			}
			a.withdrawn <- p.ID
			return nil, nil
		}
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
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t)
	s, err := h.Svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: p.ID, Title: "Build", Command: []string{"sh", "-c", "exec cat"}})
	require.NoError(t, err)
	c := h.Client(t)
	send := func() error {
		return c.Call(t.Context(), "command.run", map[string]any{"id": "session.send", "workspaceId": p.ID, "agent": true, "args": map[string]any{"id": s.ID, "text": "hi"}}, nil)
	}
	err = send()
	require.ErrorIs(t, err, rpc.ErrDenied)
	assert.ErrorContains(t, err, "no app to approve")
	a := newApprover(t, h)
	done := make(chan error, 1)
	go func() { done <- send() }()
	asked := <-a.asked
	require.NotNil(t, asked.Session)
	assert.Equal(t, [3]string{s.ID, "Build", "demo"}, [3]string{asked.Session.ID, asked.Session.Title, asked.Session.WorkspaceName})
	a.reply <- false
	require.ErrorIs(t, <-done, rpc.ErrDenied)
}

func TestAgentReadsOnlyItsWorkspaceSessions(t *testing.T) {
	h := testutil.NewHarness(t)
	own, err := h.Svc.CreateWorkspace(t.Context(), "own", nil)
	require.NoError(t, err)
	other, err := h.Svc.CreateWorkspace(t.Context(), "other", nil)
	require.NoError(t, err)
	h.Start(t)
	s, err := h.Svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: other.ID, Title: "Build", Command: []string{"sh", "-c", "exec cat"}})
	require.NoError(t, err)
	c := h.Client(t)
	read := func(workspaceID string) error {
		return c.Call(t.Context(), "command.run", map[string]any{"id": "session.read", "workspaceId": workspaceID, "agent": true, "args": map[string]any{"id": s.ID}}, nil)
	}
	assert.ErrorIs(t, read(own.ID), rpc.ErrDenied)
	assert.NoError(t, read(other.ID))
}

func TestAgentListsOnlyItsWorkspace(t *testing.T) {
	h := testutil.NewHarness(t)
	own, err := h.Svc.CreateWorkspace(t.Context(), "own", nil)
	require.NoError(t, err)
	other, err := h.Svc.CreateWorkspace(t.Context(), "other", nil)
	require.NoError(t, err)
	h.Start(t)
	var mine string
	for _, ws := range []string{own.ID, other.ID} {
		s, err := h.Svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: ws, Command: []string{"sh", "-c", "exec cat"}})
		require.NoError(t, err)
		if ws == own.ID {
			mine = s.ID
		}
	}
	c := h.Client(t)
	var sessions []session.Session
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "session.list", "workspaceId": own.ID, "agent": true}, &sessions))
	require.Len(t, sessions, 1)
	assert.Equal(t, mine, sessions[0].ID)
	var nodes []struct {
		SessionID string `json:"sessionId"`
	}
	testutil.Eventually(t, func() bool {
		return c.Call(t.Context(), "command.run", map[string]any{"id": "process.tree", "workspaceId": own.ID, "agent": true}, &nodes) == nil && len(nodes) > 0
	}, 5*time.Second)
	for _, n := range nodes {
		assert.Equal(t, mine, n.SessionID)
	}
	assert.ErrorIs(t, c.Call(t.Context(), "command.run", map[string]any{"id": "session.list", "workspaceId": "", "agent": true}, nil), rpc.ErrInvalidParams)
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "session.list", "workspaceId": own.ID}, &sessions))
	assert.Len(t, sessions, 2, "a user call still sees every workspace")
}

func (r callerRouter) Resolve(id string) (rpc.Command, bool) {
	if id == "agents.sessions" {
		return rpc.Command{CommandDef: rpc.CommandDef{ID: id}, Plugin: "agents"}, true
	}
	return r.Router.Resolve(id)
}

func (r callerRouter) Command(ctx context.Context, c rpc.Command, run json.RawMessage) (json.RawMessage, error) {
	if c.ID != "agents.sessions" {
		return r.Router.Command(ctx, c, run)
	}
	var p struct {
		Args struct {
			Session string `json:"session"`
		} `json:"args"`
	}
	if err := json.Unmarshal(run, &p); err != nil || p.Args.Session != "agt_child" {
		return json.RawMessage(`[]`), err
	}
	return json.RawMessage(`[{"id":"agt_child","title":"Land pause reasons\nand the rest"}]`), nil
}

func TestApprovalNamesTheCallingSession(t *testing.T) {
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t)
	h.Srv.SetUI(h.Store, internalsdui.NewSections(nil), callerRouter{Router: plugin.NewRouter(h.Sup)})
	s, err := h.Svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: p.ID, Title: "Build", Command: []string{"sh", "-c", "exec cat"}})
	require.NoError(t, err)
	a := newApprover(t, h)
	c := h.Client(t)
	tests := []struct {
		caller string
		want   *confirmSession
	}{
		{caller: "agt_child", want: &confirmSession{ID: "agt_child", Title: "Land pause reasons"}},
		{caller: ""},
	}
	for _, tt := range tests {
		done := make(chan error, 1)
		go func() {
			done <- c.Call(t.Context(), "command.run", map[string]any{"id": "session.send", "workspaceId": p.ID, "agent": true, "caller": tt.caller, "args": map[string]any{"id": s.ID, "text": "hi"}}, nil)
		}()
		asked := <-a.asked
		assert.Equal(t, tt.want, asked.From, tt.caller)
		assert.Equal(t, "Build", asked.Session.Title)
		a.reply <- false
		require.ErrorIs(t, <-done, rpc.ErrDenied)
	}
}

func TestAgentCallerMustBeInTheWorkspace(t *testing.T) {
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t)
	h.Srv.SetUI(h.Store, internalsdui.NewSections(nil), callerRouter{Router: plugin.NewRouter(h.Sup)})
	c := h.Client(t)
	list := func(caller string, agent bool) error {
		return c.Call(t.Context(), "command.run", map[string]any{"id": "session.list", "workspaceId": p.ID, "agent": agent, "caller": caller}, nil)
	}
	assert.NoError(t, list("agt_child", true))
	assert.ErrorIs(t, list("agt_elsewhere", true), rpc.ErrDenied)
	assert.NoError(t, list("agt_elsewhere", false), "a user-level call is not checked")
}

func TestAppScopeCommandRunsWithoutWorkspace(t *testing.T) {
	h, _ := startAlpha(t, `{"id":"alpha.say","title":"Say","remote":true,"scope":"app"},{"id":"alpha.other","title":"Other"}`)
	c := h.Client(t)
	cmds := pluginCommands(t, h)
	require.Len(t, cmds, 2)
	assert.True(t, cmds[0].Remote)
	assert.True(t, cmds[0].Running)
	assert.Equal(t, "app", cmds[0].Scope)
	assert.False(t, cmds[1].Remote)
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "workspaceId": ""}, nil))
	assert.ErrorIs(t, c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.other", "workspaceId": ""}, nil), rpc.ErrInvalidParams)
}

func TestPluginApprove(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.x","title":"X"}`)
	approve := func(command, body string) error {
		return h.PluginCall(t, "alpha", "ui.approve", map[string]any{"workspaceId": p.ID, "title": "Run setup", "command": command, "args": map[string]string{"sha256": "abc"}, "body": body})
	}
	assert.ErrorContains(t, approve("beta.run", ""), "want a alpha. command")
	assert.ErrorContains(t, approve("alpha.run", strings.Repeat("x", 64<<10+1)), "body: longer than 65536 characters")
	a := newApprover(t, h)
	done := make(chan error, 1)
	go func() { done <- approve("alpha.run", "echo hi\n") }()
	asked := <-a.asked
	assert.Equal(t, confirmRequest{ID: asked.ID, Title: "Run setup", WorkspaceName: "demo", Command: "alpha.run", Args: asked.Args, Body: "echo hi\n", Plugin: true, TimeoutMs: 60000}, asked)
	assert.JSONEq(t, `{"sha256":"abc"}`, string(asked.Args))
	a.reply <- true
	require.NoError(t, <-done)
}
