package rpc_test

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
)

func BenchmarkWireNotify(b *testing.B) {
	tree := benchTree(b, 400)
	params := map[string]any{"subscription": "sub_1", "view": map[string]any{"workspaceId": "prj_1", "slot": "agents.main", "rev": 1, "tree": tree}}
	var got atomic.Int64
	done := make(chan struct{}, 1)
	sender, _ := benchConns(b, func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (any, error) {
		if got.Add(1) == int64(b.N) {
			done <- struct{}{}
		}
		return nil, nil
	})
	b.SetBytes(int64(len(tree)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := sender.Notify(b.Context(), "ui.updated", params); err != nil {
			b.Fatal(err)
		}
	}
	<-done
}

func BenchmarkWireCall(b *testing.B) {
	tree := benchTree(b, 400)
	caller, _ := benchConns(b, func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		return req.Params, nil
	})
	b.SetBytes(int64(len(tree)) * 2)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var out json.RawMessage
		if err := caller.Call(b.Context(), "ui.render", tree, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func benchConns(b *testing.B, h func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (any, error)) (*jsonrpc2.Conn, *jsonrpc2.Conn) {
	b.Helper()
	left, right := net.Pipe()
	a := jsonrpc2.NewConn(b.Context(), jsonrpc2.NewPlainObjectStream(left), jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (any, error) { return nil, nil }))
	z := jsonrpc2.NewConn(b.Context(), jsonrpc2.NewPlainObjectStream(right), jsonrpc2.HandlerWithError(h))
	b.Cleanup(func() {
		_ = a.Close()
		_ = z.Close()
	})
	return a, z
}

func benchTree(b *testing.B, rows int) json.RawMessage {
	b.Helper()
	children := make([]any, 0, rows)
	for i := range rows {
		children = append(children, map[string]any{
			"type":  "row",
			"id":    "msg-" + strings.Repeat("x", i%7),
			"props": map[string]any{"text": strings.Repeat("streamed agent text <b>&\"quoted\"</b> ", 3), "tone": "muted"},
		})
	}
	raw, err := json.Marshal(map[string]any{"type": "stack", "children": children})
	if err != nil {
		b.Fatal(err)
	}
	return raw
}
