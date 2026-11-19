package main

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/graphs"
)

func scriptedHost(t *testing.T, handle func(method, key string) any) *pluginsdk.Host {
	t.Helper()
	coreSide, pluginSide := net.Pipe()
	core := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		var p struct{ Key string }
		if req.Params != nil {
			_ = json.Unmarshal(*req.Params, &p)
		}
		return handle(req.Method, p.Key), nil
	}))
	t.Cleanup(func() { core.Close() })
	ready := make(chan *pluginsdk.Host, 1)
	go func() {
		_ = pluginsdk.RunConn(t.Context(), pluginSide, pluginsdk.Plugin{
			Init: func(_ context.Context, h *pluginsdk.Host) error {
				ready <- h
				return nil
			},
		})
	}()
	require.NoError(t, core.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": 1, "pluginId": "test"}, nil))
	return <-ready
}

func TestGateHistoryIsReadFromStorageOnceUntilItChanges(t *testing.T) {
	var mu sync.Mutex
	stored := map[string]json.RawMessage{}
	gets := 0
	h := scriptedHost(t, func(method, key string) any {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "storage.get":
			gets++
			if v, ok := stored[key]; ok {
				return map[string]any{"found": true, "value": v}
			}
		}
		return nil
	})
	a := &app{host: h}
	read := func() []gateVerdict {
		got, err := a.readGateHistory(t.Context(), "gt_1")
		require.NoError(t, err)
		return got
	}

	assert.Empty(t, read())
	assert.Empty(t, read())
	mu.Lock()
	assert.Equal(t, 1, gets)
	mu.Unlock()

	a.cacheGateHistory("gt_1", []gateVerdict{{Gate: "gt_1", Lander: "agt_1"}})
	assert.Equal(t, "agt_1", read()[0].Lander)

	require.NoError(t, a.forgetGateHistory(t.Context(), "gt_1"))
	assert.Empty(t, read())
	mu.Lock()
	assert.Equal(t, 2, gets)
	mu.Unlock()
}

func TestStaleGatesReadDoesNotReplaceTheSavedGates(t *testing.T) {
	a := &app{stateDir: t.TempDir()}
	gen := a.gatesGen.Load()

	a.keepGates([]gateConfig{{ID: "gt_new"}})
	a.keepReadGates([]gateConfig{{ID: "gt_old"}}, gen)
	a.keepReadGates([]gateConfig{{ID: "gt_old"}}, a.gatesGen.Load())

	assert.Equal(t, []gateConfig{{ID: "gt_new"}}, *a.lastGatesRead())
	raw := (&app{stateDir: a.stateDir}).lastGatesRead()
	require.NotNil(t, raw)
	assert.Equal(t, []gateConfig{{ID: "gt_new"}}, *raw)
	assert.FileExists(t, filepath.Join(a.stateDir, lastGatesFile))
}

func TestTrustIsSyncedOnlyWhenASessionStartsOrStopsWaiting(t *testing.T) {
	s := newSession(record{ID: "agt_1", Workspace: "ws"}, nil, false, func(string, bool) {})
	hold := func(h *heldTurn) {
		s.mu.Lock()
		s.rec.Held = h
		s.mu.Unlock()
	}
	changed := &heldTurn{Gate: "gt_1", Wait: graphs.ErrGateGraphChanged.Error()}

	assert.False(t, s.trustFlipped())
	hold(&heldTurn{Gate: "gt_1", Wait: "waiting for a reviewer"})
	assert.False(t, s.trustFlipped())
	hold(changed)
	assert.True(t, s.trustFlipped())
	assert.False(t, s.trustFlipped())
	hold(nil)
	assert.True(t, s.trustFlipped())
}

func TestTrustWithdrawalComesAfterThePostItRacesWith(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	posting := make(chan struct{})
	release := make(chan struct{})
	h := scriptedHost(t, func(method, _ string) any {
		switch method {
		case "notify.post":
			close(posting)
			<-release
		case "bus.publish":
		default:
			return nil
		}
		mu.Lock()
		calls = append(calls, method)
		mu.Unlock()
		return nil
	})
	s := newSession(record{ID: "agt_1", Workspace: "ws", Held: &heldTurn{Gate: "gt_1", Wait: graphs.ErrGateGraphChanged.Error()}}, nil, false, func(string, bool) {})
	a := &app{host: h, notes: newNotifier(h, ""), sessions: map[string]*session{"agt_1": s}}

	a.trustSynced("ws")
	<-posting
	s.mu.Lock()
	s.rec.Held = nil
	s.mu.Unlock()
	a.trustSynced("ws")
	close(release)

	testutil.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) == 2
	}, 10*time.Second)
	assert.Equal(t, []string{"notify.post", "bus.publish"}, calls)
}
