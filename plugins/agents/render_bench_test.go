package main

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/sourcegraph/jsonrpc2"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

func BenchmarkRenderMainAndCode(b *testing.B) {
	s := benchSession(b, 60)
	rec := s.snapshot()
	b.Run("build", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			mainView(benchScreen(s))
			sc := benchScreen(s)
			codeView([]record{rec}, rec.ID, &sc, "")
		}
	})
	b.Run("marshal", func(b *testing.B) {
		b.ReportAllocs()
		var mainBytes, codeBytes int
		for b.Loop() {
			main, err := json.Marshal(mainView(benchScreen(s)))
			if err != nil {
				b.Fatal(err)
			}
			sc := benchScreen(s)
			code, err := json.Marshal(codeView([]record{rec}, rec.ID, &sc, ""))
			if err != nil {
				b.Fatal(err)
			}
			mainBytes, codeBytes = len(main), len(code)
		}
		b.ReportMetric(float64(mainBytes), "main-bytes")
		b.ReportMetric(float64(codeBytes), "code-bytes")
	})
}

func BenchmarkStreamingFrame(b *testing.B) {
	chunk, err := json.Marshal(map[string]any{"sessionUpdate": acp.UpdateAgentMessageChunk, "content": map[string]string{"type": "text", "text": "and the next few streamed words. "}})
	if err != nil {
		b.Fatal(err)
	}
	for _, mode := range []string{"full", "delta"} {
		b.Run(mode, func(b *testing.B) {
			s := benchSession(b, 60)
			s.applyPrompt(envelope{Turn: 61, Payload: json.RawMessage(`{"prompt":[{"type":"text","text":"Explain the change"}]}`)})
			store := internalsdui.NewStore()
			sub, err := store.Subscribe("prj_bench")
			if err != nil {
				b.Fatal(err)
			}
			tracker := internalsdui.NewDeltaTracker(internalsdui.DeltaOptions{Appends: true, Keeps: true})
			conn := benchCore(b, store)
			var prev *sdui.TreeIndex
			var rev int64
			var pluginBytes, appBytes int
			b.ReportAllocs()
			for b.Loop() {
				s.applyUpdate(61, chunk)
				raw, err := json.Marshal(mainView(benchScreen(s)))
				if err != nil {
					b.Fatal(err)
				}
				params := benchRender{WorkspaceID: "prj_bench", Slot: mainSlot, Tree: raw}
				if mode == "delta" {
					next, err := sdui.IndexTree(raw)
					if err != nil {
						b.Fatal(err)
					}
					if prev != nil {
						if delta, ok := next.DeltaFrom(prev, sdui.DeltaOptions{Appends: true, Keeps: true}); ok {
							params = benchRender{WorkspaceID: "prj_bench", Slot: mainSlot, Base: rev, Delta: delta}
						}
					}
					prev = next
				}
				pluginBytes += len(params.Tree) + len(params.Delta)
				var out struct {
					Rev int64 `json:"rev"`
				}
				if err := conn.Call(b.Context(), "ui.render", params, &out); err != nil {
					b.Fatal(err)
				}
				rev = out.Rev
				views, _ := sub.Take()
				for _, v := range views {
					f := tracker.Frame(v)
					appBytes += len(f.Tree) + len(f.Delta)
				}
			}
			b.ReportMetric(float64(pluginBytes)/float64(b.N), "plugin-B/frame")
			b.ReportMetric(float64(appBytes)/float64(b.N), "app-B/frame")
		})
	}
}

type benchRender struct {
	WorkspaceID string          `json:"workspaceId"`
	Slot        string          `json:"slot"`
	Tree        json.RawMessage `json:"tree,omitempty"`
	Base        int64           `json:"base,omitempty"`
	Delta       json.RawMessage `json:"delta,omitempty"`
}

func benchCore(b *testing.B, store *internalsdui.Store) *jsonrpc2.Conn {
	b.Helper()
	pluginSide, coreSide := net.Pipe()
	core := jsonrpc2.NewConn(b.Context(), jsonrpc2.NewPlainObjectStream(coreSide), jsonrpc2.HandlerWithError(func(ctx context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		var p benchRender
		if err := json.Unmarshal(*req.Params, &p); err != nil {
			return nil, err
		}
		if p.Delta != nil {
			rev, err := store.RenderDelta(ctx, p.WorkspaceID, p.Slot, "agents", p.Base, p.Delta)
			return map[string]int64{"rev": rev}, err
		}
		rev, err := store.RenderJSON(ctx, p.WorkspaceID, p.Slot, "agents", p.Tree)
		return map[string]int64{"rev": rev}, err
	}))
	plugin := jsonrpc2.NewConn(b.Context(), jsonrpc2.NewPlainObjectStream(pluginSide), jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (any, error) { return nil, nil }))
	b.Cleanup(func() {
		_ = plugin.Close()
		_ = core.Close()
	})
	return plugin
}

func benchScreen(s *session) screen {
	v := s.view(0)
	return screen{View: v, Window: windowStep, Composer: v.Session.ID + "/composer/0"}
}

func benchSession(b *testing.B, turns int) *session {
	b.Helper()
	s := newSession(record{ID: "agt_bench", Worktree: "/r", Agent: agentClaude, Status: statusDone}, nil, true, nil)
	raw := func(v any) json.RawMessage {
		out, err := json.Marshal(v)
		if err != nil {
			b.Fatal(err)
		}
		return out
	}
	source := func(n, salt int) string {
		var sb strings.Builder
		for i := range n {
			sb.WriteString("\tvalue" + strconv.Itoa(i) + " := compute(ctx, \"" + strconv.Itoa(salt) + "\", item.Field)\n")
		}
		return sb.String()
	}
	for turn := 1; turn <= turns; turn++ {
		n := strconv.Itoa(turn)
		s.applyPrompt(envelope{Turn: turn, Payload: raw(promptPayload{PromptRequest: acp.PromptRequest{Prompt: []acp.ContentBlock{{Type: "text", Text: "Fix the failing test in pkg/x number " + n}}}})})
		s.applyUpdate(turn, raw(map[string]any{"sessionUpdate": acp.UpdateAgentThoughtChunk, "content": map[string]string{"type": "text", "text": strings.Repeat("Thinking about the change. ", 20)}}))
		s.applyUpdate(turn, raw(map[string]any{"sessionUpdate": acp.UpdatePlan, "entries": []map[string]string{{"content": "Read the code", "status": "completed"}, {"content": "Edit", "status": "in_progress"}}}))
		for c := range 6 {
			id := "call-" + n + "-" + strconv.Itoa(c)
			old := source(200+c*100, turn)
			updated := strings.Replace(old, "value"+strconv.Itoa(c*7)+" ", "renamed ", 1) + "\treturn nil\n"
			tool := map[string]any{"sessionUpdate": acp.UpdateToolCall, "toolCallId": id, "status": "completed"}
			switch c % 3 {
			case 0:
				tool["title"], tool["kind"] = "Read /r/pkg/x.go", "read"
				tool["content"] = []map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": old}}}
			case 1:
				tool["title"], tool["kind"] = "Edit `/r/pkg/x"+n+".go`", "edit"
				tool["content"] = []map[string]any{{"type": "diff", "path": "/r/pkg/x" + n + ".go", "oldText": old, "newText": updated}}
				tool["locations"] = []map[string]any{{"path": "/r/pkg/x" + n + ".go", "line": 3}}
			default:
				tool["title"], tool["kind"] = "go test ./...", "execute"
				tool["rawInput"] = map[string]string{"command": "go test ./..."}
				tool["content"] = []map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": source(150, turn)}}}
			}
			s.applyTool(true, turn, raw(tool))
		}
		s.applyUpdate(turn, raw(map[string]any{"sessionUpdate": acp.UpdateAgentMessageChunk, "content": map[string]string{"type": "text", "text": strings.Repeat("Done: renamed the value and **tests pass**. ", 40)}}))
		s.turnInfo(turn).StopReason = acp.StopEndTurn
		s.turnInfo(turn).CheckpointSha = "sha" + n
	}
	return s
}
