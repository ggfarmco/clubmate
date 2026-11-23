package testutil

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/session"
	"clubmate/internal/workspace"
	"clubmate/pkg/sdui"
)

const (
	harnessStopTimeout = 15 * time.Second
	harnessWait        = 5 * time.Second
	pluginLogTail      = 16 << 10
)

type PluginHarness struct {
	Svc     *core.Service
	Bus     *bus.Bus
	Srv     *rpc.Server
	Sup     *plugin.Supervisor
	Store   *internalsdui.Store
	KV      *kv.Store
	KVPath  string
	Control *plugin.Control
	Dirs    paths.Dirs
	client  *rpc.Client
	logs    SyncBuffer
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
	h.KVPath = filepath.Join(t.TempDir(), "plugins.db")
	store, err := kv.Open(h.KVPath)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	h.KV = store
	h.Srv = rpc.NewServer(h.Svc, h.Bus, rpc.WithKV(store))
	opts = append([]plugin.SupervisorOption{plugin.WithOnStop(h.Store.ClearOwner), plugin.WithBus(h.Bus), plugin.WithEnabled(func(m plugin.Manifest) bool { return h.Control.StartEnabled(m) })}, opts...)
	h.Sup = plugin.NewSupervisor(h.Srv, h.Dirs, slog.New(slog.NewTextHandler(&h.logs, nil)), opts...)
	h.Srv.SetPluginRestart(h.Sup.Restart)
	return h
}

func (h *PluginHarness) Start(t *testing.T, searchPath ...string) {
	t.Helper()
	manifests, errs := plugin.Discover(searchPath)
	require.Empty(t, errs)
	control, err := plugin.NewControl(context.Background(), h.KV, manifests)
	require.NoError(t, err)
	h.Control = control
	router := plugin.NewRouter(h.Sup)
	require.NoError(t, control.Attach(h.Sup, router))
	h.Srv.SetPluginControl(control)
	h.Srv.SetUI(h.Store, internalsdui.NewSections(plugin.SectionDefs(manifests), plugin.ViewDefs(manifests, slog.New(slog.DiscardHandler))...), router)
	h.Sup.Start(context.Background(), manifests)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), harnessStopTimeout)
		defer cancel()
		h.Sup.Stop(ctx)
	})
	t.Cleanup(func() { h.reportFailure(t) })
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
	return h.PluginCallResult(t, pluginID, method, params, nil)
}

func (h *PluginHarness) PluginCallResult(t *testing.T, pluginID, method string, params, result any) error {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server := h.Srv.ServeConn(context.Background(), jsonrpc2.NewPlainObjectStream(serverSide), rpc.Caller{PluginID: pluginID})
	defer server.Close()
	client := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(clientSide), noopHandler{})
	defer client.Close()
	err := client.Call(t.Context(), method, params, result)
	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) {
		return rpc.NewRemoteError(rpcErr)
	}
	return err
}

func (h *PluginHarness) CreateWorkspace(t *testing.T, name string) workspace.Workspace {
	t.Helper()
	p, err := h.Svc.CreateWorkspace(t.Context(), name, nil)
	require.NoError(t, err)
	return p
}

func (h *PluginHarness) WaitSlot(t *testing.T, workspaceID, slot string, cond func(sdui.Node) bool) sdui.Node {
	t.Helper()
	return h.WaitSlotWithin(t, workspaceID, slot, harnessWait, cond)
}

func (h *PluginHarness) WaitSlotWithin(t *testing.T, workspaceID, slot string, timeout time.Duration, cond func(sdui.Node) bool) sdui.Node {
	t.Helper()
	c := h.Client(t)
	var found sdui.Node
	Eventually(t, func() bool {
		var views []internalsdui.SlotView
		if err := c.Call(t.Context(), "ui.get", map[string]string{"workspaceId": workspaceID}, &views); err != nil {
			return false
		}
		for _, v := range views {
			if v.Slot != slot {
				continue
			}
			if n, err := v.Node(); err == nil && cond(n) {
				found = n
				return true
			}
		}
		return false
	}, timeout)
	return found
}

func (h *PluginHarness) Event(t *testing.T, workspaceID, slot, nodeID, action string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	params := map[string]any{"workspaceId": workspaceID, "slot": slot, "nodeId": nodeID, "action": action, "value": json.RawMessage(raw)}
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
	Eventually(t, func() bool {
		_, err := h.Svc.GetSession(t.Context(), id)
		return errors.Is(err, session.ErrNotFound)
	}, harnessWait)
}

func (h *PluginHarness) reportFailure(t *testing.T) {
	if !t.Failed() {
		return
	}
	t.Logf("supervisor log:\n%s", h.logs.String())
	for _, st := range h.Sup.Statuses() {
		t.Logf("plugin %s: state %s, restarts %d, last error %q", st.ID, st.State, st.Restarts, st.LastError)
		if data, err := os.ReadFile(h.Dirs.PluginLog(st.ID)); err == nil {
			t.Logf("plugin %s log:\n%s", st.ID, tail(data, pluginLogTail))
		}
	}
}

func tail(data []byte, n int) []byte {
	if len(data) <= n {
		return data
	}
	return data[len(data)-n:]
}

type SyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *SyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *SyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (noopHandler) Handle(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) {}

func (h *PluginHarness) FailKVDeletes(t *testing.T, plugin, prefix string) (heal func()) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+h.KVPath+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
	name := fmt.Sprintf("fail_deletes_%d", time.Now().UnixNano())
	_, err = db.Exec(fmt.Sprintf(`CREATE TRIGGER %s BEFORE DELETE ON kv WHEN old.plugin = %s AND substr(old.key, 1, %d) = %s BEGIN SELECT RAISE(ABORT, 'delete fault'); END`, name, quote(plugin), len(prefix), quote(prefix)))
	require.NoError(t, err)
	return func() {
		_, err := db.Exec("DROP TRIGGER " + name)
		require.NoError(t, err)
	}
}

func (h *PluginHarness) FailKVReads(t *testing.T, plugin, prefix string) (heal func()) {
	t.Helper()
	heal = h.KV.FailReads(plugin, prefix)
	t.Cleanup(heal)
	return heal
}

func (h *PluginHarness) OnKVRead(t *testing.T, plugin, prefix string, fn func() error) {
	t.Helper()
	t.Cleanup(h.KV.OnRead(plugin, prefix, fn))
}
