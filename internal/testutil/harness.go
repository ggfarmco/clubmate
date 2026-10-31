package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/core"
	"clubmate/internal/kv"
	"clubmate/internal/paths"
	"clubmate/internal/plugin"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/session"
	"clubmate/pkg/sdui"
)

const (
	harnessStopTimeout = 15 * time.Second
	harnessWait        = 5 * time.Second
)

type PluginHarness struct {
	Svc    *core.Service
	Bus    *bus.Bus
	Srv    *rpc.Server
	Sup    *plugin.Supervisor
	Store  *internalsdui.Store
	KV     *kv.Store
	Dirs   paths.Dirs
	client *rpc.Client
}

type noopHandler struct{}

func NewPluginHarness(t *testing.T, pluginDirs ...string) *PluginHarness {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	h := NewHarness(t)
	h.Start(t, pluginDirs...)
	return h
}

func NewHarness(t *testing.T, opts ...plugin.SupervisorOption) *PluginHarness {
	t.Helper()
	h := &PluginHarness{Store: internalsdui.NewStore(), Dirs: ShortDirs(t)}
	h.Svc, h.Bus = NewServiceWithBusIn(t, h.Dirs, core.WithExcludePIDs(func() []int { return h.Sup.PIDs() }))
	store, err := kv.Open(filepath.Join(t.TempDir(), "plugins.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	h.KV = store
	h.Srv = rpc.NewServer(h.Svc, h.Bus, rpc.WithKV(store))
	opts = append([]plugin.SupervisorOption{plugin.WithOnStop(h.Store.ClearOwner)}, opts...)
	h.Sup = plugin.NewSupervisor(h.Srv, h.Dirs, slog.New(slog.DiscardHandler), opts...)
	h.Srv.SetPluginRestart(h.Sup.Restart)
	return h
}

func (h *PluginHarness) Start(t *testing.T, searchPath ...string) {
	t.Helper()
	manifests, errs := plugin.Discover(searchPath)
	require.Empty(t, errs)
	h.Srv.SetUI(h.Store, internalsdui.NewSections(plugin.SectionDefs(manifests), plugin.ViewDefs(manifests, slog.New(slog.DiscardHandler))...), plugin.NewRouter(h.Sup))
	h.Sup.Start(context.Background(), manifests)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), harnessStopTimeout)
		defer cancel()
		h.Sup.Stop(ctx)
	})
}

func (h *PluginHarness) Client(t *testing.T) *rpc.Client {
	t.Helper()
	if h.client != nil {
		return h.client
	}
	l, err := net.Listen("unix", h.Dirs.CoreSocket())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Srv.Serve(ctx, l) }()
	c, err := rpc.Dial(t.Context(), h.Dirs.CoreSocket())
	require.NoError(t, err)
	t.Cleanup(func() {
		c.Close()
		cancel()
		assert.NoError(t, <-done)
	})
	h.client = c
	return c
}

func (h *PluginHarness) PluginCall(t *testing.T, pluginID, method string, params any) error {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server := h.Srv.ServeConn(context.Background(), jsonrpc2.NewPlainObjectStream(serverSide), rpc.Caller{PluginID: pluginID})
	defer server.Close()
	client := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(clientSide), noopHandler{})
	defer client.Close()
	err := client.Call(t.Context(), method, params, nil)
	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) {
		return &rpc.RemoteError{Code: int(rpcErr.Code), Message: rpcErr.Message}
	}
	return err
}

func (h *PluginHarness) CreateProject(t *testing.T, name string) project.Project {
	t.Helper()
	p, err := h.Svc.CreateProject(t.Context(), name, nil)
	require.NoError(t, err)
	return p
}

func (h *PluginHarness) WaitSlot(t *testing.T, projectID, slot string, cond func(sdui.Node) bool) sdui.Node {
	t.Helper()
	c := h.Client(t)
	var found sdui.Node
	require.Eventually(t, func() bool {
		var views []internalsdui.SlotView
		if err := c.Call(t.Context(), "ui.get", map[string]string{"projectId": projectID}, &views); err != nil {
			return false
		}
		for _, v := range views {
			if v.Slot == slot && cond(v.Tree) {
				found = v.Tree
				return true
			}
		}
		return false
	}, harnessWait, 20*time.Millisecond)
	return found
}

func (h *PluginHarness) Event(t *testing.T, projectID, slot, nodeID, action string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	params := map[string]any{"projectId": projectID, "slot": slot, "nodeId": nodeID, "action": action, "value": json.RawMessage(raw)}
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.event", params, nil))
}

func (h *PluginHarness) GetSession(t *testing.T, id string) session.Session {
	t.Helper()
	s, err := h.Svc.GetSession(t.Context(), id)
	require.NoError(t, err)
	return s
}

func (h *PluginHarness) WaitSessionGone(t *testing.T, id string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := h.Svc.GetSession(t.Context(), id)
		return errors.Is(err, session.ErrNotFound)
	}, harnessWait, 20*time.Millisecond)
}

func (noopHandler) Handle(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) {}
