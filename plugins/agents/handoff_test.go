package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/sdui"
)

func TestHandoffBrief(t *testing.T) {
	view := transcriptView{
		Session: record{ID: "agt_1", Title: "Refunds", Worktree: "/r/a"},
		Turns:   []turnInfo{{Turn: 1, StopReason: "end_turn"}, {Turn: 2, CheckpointSha: "abc", StopReason: "cancelled"}},
		Entries: []*entry{
			{Kind: entryUser, Turn: 1, Text: "fix refunds"},
			{Kind: entryThought, Turn: 1, Text: "secret"},
			{Kind: entryAgent, Turn: 1, Text: "fixed"},
			{Kind: entryUser, Turn: 2, Text: strings.Repeat("é", handoffPieceLimit+5)},
		},
	}

	tests := []struct {
		name string
		view transcriptView
		note string
		want []string
		not  []string
	}{
		{"no prompts gives the note", transcriptView{}, "go", []string{"go"}, nil},
		{
			"digest",
			view,
			"",
			[]string{
				`continue agent session "Refunds" (agt_1) in /r/a`,
				"agents.transcript session=agt_1",
				"Task (turn 1):\nfix refunds",
				"Last prompt (turn 2):\n" + strings.Repeat("é", handoffPieceLimit) + "…",
				"Last reply (turn 1):\nfixed",
				"Last turn 2: cancelled, checkpoint abc taken before it.",
				handoffDefault,
			},
			[]string{"secret", strings.Repeat("é", handoffPieceLimit+1)},
		},
		{"note replaces the default", view, "ship it", []string{"\n\nship it"}, []string{handoffDefault}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handoffBrief(tt.view, tt.note)
			for _, w := range tt.want {
				assert.Contains(t, got, w)
			}
			for _, n := range tt.not {
				assert.NotContains(t, got, n)
			}
		})
	}
}

func TestHandoffLinks(t *testing.T) {
	old := mainView(screen{View: transcriptView{Session: record{ID: "agt_1", Closed: true, Status: statusClosed, HandoffTo: "agt_2"}}})
	fresh := mainView(screen{View: transcriptView{Session: record{ID: "agt_2", Status: statusIdle, HandoffFrom: "agt_1"}}, Composer: "agt_2/composer/0"})

	assert.Equal(t, []string{"agt_1/" + handoffToNode}, buttonIDs(old, actOpenHandoff))
	assert.Empty(t, buttonIDs(old, actRetry))
	assert.Equal(t, []string{"agt_2/" + handoffFromNode}, buttonIDs(fresh, actOpenHandoff))
}

func buttonIDs(n sdui.Node, action string) []string {
	var out []string
	for _, b := range nodesOf(n, "button") {
		if b.Props["action"] == action {
			out = append(out, b.ID)
		}
	}
	return out
}
