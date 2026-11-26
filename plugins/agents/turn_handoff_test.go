package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/agents/api"
)

func TestRelayedTurnHandsOffOnlyItsLastReply(t *testing.T) {
	t.Parallel()
	chunk := func(turn, id, text string) string {
		return `{"turn":` + turn + `,"kind":"session/update","payload":{"sessionUpdate":"agent_message_chunk","messageId":"` + id + `","content":{"type":"text","text":"` + text + `"}}}`
	}
	lines := []string{
		`{"turn":1,"kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"coder finished"}],"relayed":true}}`,
		chunk("1", "m1", "Reading"),
		chunk("1", "m2", `Report\nmore`),
		`{"turn":2,"kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"what now?"}]}}`,
		chunk("2", "m3", "Answer"),
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	s := newSession(record{ID: "agt_1", Run: &api.Run{ID: "gr_1", Next: []string{"Reviewer", "Tester"}}}, newEventLog(path), false, func(string, bool) {})

	v := s.view(0)
	marks := map[string]*handoffMark{}
	for _, en := range v.Entries {
		if en.Kind == entryAgent {
			marks[en.Text] = en.Handoff
		}
	}
	assert.Equal(t, map[string]*handoffMark{"Reading": nil, "Report\nmore": {To: "Reviewer, Tester"}, "Answer": nil}, marks)

	var props []any
	for _, n := range nodesOf(transcript(screen{View: v}), "message") {
		if n.Props["role"] == entryAgent {
			props = append(props, n.Props["handoff"])
		}
	}
	assert.Equal(t, []any{nil, &handoffMark{To: "Reviewer, Tester"}, nil}, props)
}

func TestRelayedTurnThatFailedOrWasCancelledHandsNothingOff(t *testing.T) {
	t.Parallel()
	for name, end := range map[string]string{
		"error":     `{"turn":1,"kind":"turn_end","payload":{"error":"acp error -32603: exit status 1"}}`,
		"cancelled": `{"turn":1,"kind":"turn_end","payload":{"stopReason":"cancelled"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			lines := []string{
				`{"turn":1,"kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"coder finished"}],"relayed":true}}`,
				`{"turn":1,"kind":"session/update","payload":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Half done"}}}`,
				end,
			}
			path := filepath.Join(t.TempDir(), "s.jsonl")
			require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
			s := newSession(record{ID: "agt_1", Run: &api.Run{ID: "gr_1", Next: []string{"Reviewer"}}}, newEventLog(path), false, func(string, bool) {})

			for _, en := range s.view(0).Entries {
				assert.Nil(t, en.Handoff, en.Text)
			}
		})
	}
}

func TestHandoffKeepsItsFoldAfterAnIdleSessionError(t *testing.T) {
	t.Parallel()
	lines := []string{
		`{"turn":1,"kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"coder finished"}],"relayed":true}}`,
		`{"turn":1,"kind":"session/update","payload":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Report"}}}`,
		`{"turn":1,"kind":"turn_end","payload":{"stopReason":"end_turn"}}`,
		`{"turn":1,"kind":"error","payload":{"error":"attach failed"}}`,
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	s := newSession(record{ID: "agt_1", Run: &api.Run{ID: "gr_1", Next: []string{"Reviewer"}}}, newEventLog(path), false, func(string, bool) {})

	var marks []*handoffMark
	for _, en := range s.view(0).Entries {
		if en.Kind == entryAgent {
			marks = append(marks, en.Handoff)
		}
	}
	assert.Equal(t, []*handoffMark{{To: "Reviewer"}}, marks)
}

func TestQueuedUserPromptMergedWithARelayedOneMakesAUserTurn(t *testing.T) {
	t.Parallel()
	s := newSession(record{ID: "agt_1"}, nil, true, nil)
	s.mu.Lock()
	s.enqueueLocked(promptArgs{Text: "edge"})
	relayed, _ := s.dequeueLocked()
	s.sending = nil
	s.enqueueLocked(promptArgs{Text: "edge"})
	s.enqueueLocked(promptArgs{Text: "mine", User: true})
	merged, _ := s.dequeueLocked()
	s.mu.Unlock()

	assert.Equal(t, []bool{false, true}, []bool{relayed.User, merged.User})
	assert.Equal(t, "edge\n\nmine", merged.Text)
}
