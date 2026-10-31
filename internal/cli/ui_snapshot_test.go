package cli_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUISnapshotParams(t *testing.T) {
	startCore(t)
	nc, err := net.Dial("unix", filepath.Join(os.Getenv("CLUBMATE_RUNTIME_DIR"), "core.sock"))
	require.NoError(t, err)
	seen := make(chan map[string]any, 1)
	handler := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		var p map[string]any
		require.NoError(t, json.Unmarshal(*req.Params, &p))
		seen <- p
		return map[string]any{"windows": []any{map[string]any{"id": 7, "kind": "main", "title": "demo", "png": "/tmp/out/w7.png", "layout": "/tmp/out/w7.json"}}, "summary": "0 problems"}, nil
	})
	conn := jsonrpc2.NewConn(context.Background(), jsonrpc2.NewPlainObjectStream(nc), jsonrpc2.AsyncHandler(handler))
	t.Cleanup(func() { conn.Close() })
	require.NoError(t, conn.Call(t.Context(), "client.register", map[string]any{"handles": []string{"ui.snapshot"}}, nil))
	t.Chdir(t.TempDir())
	out := string(run(t, "ui", "snapshot", "--out", "rel", "--size", "640x800"))
	p := <-seen
	wd, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(wd, "rel"), p["out"])
	assert.Equal(t, 640.0, p["width"])
	assert.Equal(t, 800.0, p["height"])
	assert.Equal(t, true, p["png"])
	assert.Contains(t, out, "/tmp/out/w7.png")
	assert.Contains(t, out, "0 problems")
}
