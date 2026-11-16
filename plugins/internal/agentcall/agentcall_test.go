package agentcall_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
)

func TestValidateTargetAlias(t *testing.T) {
	coreSide, pluginSide := net.Pipe()
	commands := []map[string]any{{"id": "a.graph-run", "aliases": []string{"graphs.run"}, "agent": true, "plugin": "a"}}
	core := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		if req.Method == "command.list" {
			return commands, nil
		}
		return nil, nil
	}))
	t.Cleanup(func() { core.Close() })
	ready := make(chan *pluginsdk.Host, 1)
	go func() {
		_ = pluginsdk.RunConn(t.Context(), pluginSide, pluginsdk.Plugin{Init: func(_ context.Context, h *pluginsdk.Host) error {
			ready <- h
			return nil
		}})
	}()
	require.NoError(t, core.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": 1, "pluginId": "cron"}, nil))
	var h *pluginsdk.Host
	select {
	case h = <-ready:
	case <-time.After(testutil.Timeout(3 * time.Second)):
		t.Fatal("Init not called")
	}
	canonical, args, err := agentcall.ValidateTarget(t.Context(), h, "graphs.run", "{}")
	require.NoError(t, err)
	assert.Equal(t, "a.graph-run", canonical)
	assert.JSONEq(t, `{}`, string(args))
}
