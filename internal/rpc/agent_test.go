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
	"clubmate/internal/testutil"
)

type confirmRequest struct {
	Title     string          `json:"title"`
	Workspace string          `json:"workspace"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args"`
}

type approver struct {
	conn  *jsonrpc2.Conn
	asked chan confirmRequest
	reply chan bool
}

func TestAgentCannotReachHidden(t *testing.T) {
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","hidden":true}`)
	c := h.Client(t)
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "projectId": p.ID}, nil))
	err := c.Call(t.Context(), "command.run", map[string]any{"id": "alpha.say", "projectId": p.ID, "agent": true}, nil)
	assert.ErrorIs(t, err, rpc.ErrNotFound)
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
		assert.Equal(t, confirmRequest{Title: "Drop Everything", Workspace: "demo", Command: "alpha.say", Args: json.RawMessage(`{"n":1}`)}, <-a.asked)
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
	h, p := startAlpha(t, `{"id":"alpha.say","title":"Say","agent":true,"destructive":true,"args":{"properties":{"s":{"type":"string"}}}}`)
	a := newApprover(t, h)
	c := h.Client(t)
	tests := []struct {
		name string
		args any
		err  string
	}{
		{name: "undeclared key", args: map[string]any{"s": "x", "extra": "y"}, err: "args.extra: not declared"},
		{name: "overlong value", args: map[string]any{"s": strings.Repeat("é", 201)}, err: "args.s: longer than 200 characters"},
		{name: "non-object args", args: []int{1}, err: "args: want object"},
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
