package rpc_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
)

type homeRouter struct {
	rpc.Router
	members map[string]string
	asked   *atomic.Int32
}

func (r homeRouter) Resolve(id string) (rpc.Command, bool) {
	if id == "agents.sessions" {
		return rpc.Command{CommandDef: rpc.CommandDef{ID: id}, Plugin: "agents"}, true
	}
	return r.Router.Resolve(id)
}

func (r homeRouter) Command(ctx context.Context, c rpc.Command, run json.RawMessage) (json.RawMessage, error) {
	if c.ID != "agents.sessions" {
		return r.Router.Command(ctx, c, run)
	}
	var p struct {
		WorkspaceID string `json:"workspaceId"`
		Args        struct {
			Session string `json:"session"`
		} `json:"args"`
	}
	if err := json.Unmarshal(run, &p); err != nil {
		return nil, err
	}
	r.asked.Add(1)
	if r.members[p.Args.Session] != p.WorkspaceID {
		return json.RawMessage(`[]`), nil
	}
	return json.Marshal([]map[string]string{{"id": p.Args.Session}})
}

func TestHomeCallerReadsOtherWorkspaces(t *testing.T) {
	h := testutil.NewHarness(t)
	home, err := h.Svc.HomeWorkspace(t.Context())
	require.NoError(t, err)
	project, err := h.Svc.CreateWorkspace(t.Context(), "project", nil)
	require.NoError(t, err)
	third, err := h.Svc.CreateWorkspace(t.Context(), "third", nil)
	require.NoError(t, err)
	gone, err := h.Svc.CreateWorkspace(t.Context(), "gone", nil)
	require.NoError(t, err)
	h.Start(t)
	members := map[string]string{"agt_home": home.ID, "agt_project": project.ID, "agt_gone": gone.ID}
	asked := &atomic.Int32{}
	h.Srv.SetUI(h.Store, internalsdui.NewSections(nil), homeRouter{Router: plugin.NewRouter(h.Sup), members: members, asked: asked})
	s, err := h.Svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: project.ID, Command: []string{"sh", "-c", "exec cat"}})
	require.NoError(t, err)
	c := h.Client(t)
	tests := []struct {
		name      string
		caller    string
		id        string
		workspace string
		args      map[string]any
		denied    string
	}{
		{name: "home lists another workspace", caller: "agt_home", id: "session.list", workspace: project.ID},
		{name: "home reads a session of another workspace", caller: "agt_home", id: "session.read", workspace: home.ID, args: map[string]any{"id": s.ID}},
		{name: "home writes to another workspace", caller: "agt_home", id: "session.create", workspace: project.ID, denied: "from Home only read tools reach other workspaces"},
		{name: "home types into a session of another workspace", caller: "agt_home", id: "session.send", workspace: home.ID, args: map[string]any{"id": s.ID, "text": "hi"}, denied: "from Home only read tools reach other workspaces"},
		{name: "project reads home", caller: "agt_project", id: "session.list", workspace: home.ID, denied: "agents act only in their own workspace"},
		{name: "project reads a third workspace", caller: "agt_project", id: "session.list", workspace: third.ID, denied: "agents act only in their own workspace"},
		{name: "no caller and no workspace", id: "session.read", args: map[string]any{"id": s.ID}, denied: "agents act only in their own workspace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Call(t.Context(), "command.run", map[string]any{"id": tt.id, "workspaceId": tt.workspace, "agent": true, "caller": tt.caller, "args": tt.args}, nil)
			if tt.denied == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, rpc.ErrDenied)
			assert.ErrorContains(t, err, tt.denied)
		})
	}
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "session.list", "workspaceId": gone.ID, "agent": true, "caller": "agt_gone"}, nil))
	require.NoError(t, h.Svc.DeleteWorkspace(t.Context(), gone.ID))
	err = c.Call(t.Context(), "command.run", map[string]any{"id": "session.list", "workspaceId": project.ID, "agent": true, "caller": "agt_gone"}, nil)
	assert.ErrorIs(t, err, rpc.ErrDenied, "a caller whose workspace is gone is denied, not failed")
	before := asked.Load()
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "session.list", "workspaceId": project.ID, "agent": true, "caller": "agt_home"}, nil))
	assert.Equal(t, before, asked.Load(), "a caller known to be in Home is not looked up again")
}
