package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

func sentComments() []reviewComment {
	return []reviewComment{
		{ID: 4, File: "pkg/sdui/node.go", Side: "right", LineStart: 23, LineEnd: 23, Type: "issue", Text: "Workspace is set only by the Always allow path.\nSay so in a comment.", Status: "open", LineText: "\tWorkspace string"},
		{ID: 7, File: "internal/sdui/catalog.go", Side: "left", LineStart: 41, LineEnd: 44, Type: "question", Text: "Why sort by title now?", Status: "open"},
	}
}

func TestSendReviewPromptFormat(t *testing.T) {
	t.Parallel()
	args := sendReviewArgs{Base: "main", Head: "review/missed-cron", Comments: sentComments(), Note: "Keep the old label for Allow once.", Nrev: "/Applications/Clubmate.app/Contents/Resources/tools/nrev"}
	want := "Review of review/missed-cron against main: 2 comments from the Clubmate review panel.\n" +
		"\n" +
		"For each one: fix it if you agree and resolve it with `/Applications/Clubmate.app/Contents/Resources/tools/nrev resolve <id> --note \"<what you did>\"`; if it is for someone else, don't fix it and say who should handle it. End with one line per comment: <id> fixed | forwarded | disagree — why.\n" +
		"\n" +
		"4. [issue] pkg/sdui/node.go:23 (right)\n" +
		"   > \tWorkspace string\n" +
		"   Workspace is set only by the Always allow path.\n" +
		"   Say so in a comment.\n" +
		"\n" +
		"7. [question] internal/sdui/catalog.go:41-44 (left)\n" +
		"   Why sort by title now?\n" +
		"\n" +
		"Note: Keep the old label for Allow once."
	assert.Equal(t, want, reviewPrompt(args))
	args.Comments, args.Note = args.Comments[:1], ""
	assert.Contains(t, reviewPrompt(args), ": 1 comment from the Clubmate review panel.")
	assert.NotContains(t, reviewPrompt(args), "Note:")
}

func TestReviewCardRendersStates(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 4, 17, 52, 0, 0, time.Local)
	en := &entry{Kind: entryUser, ID: "prompt-1", Turn: 1, Text: "Review of …", At: at, Review: &reviewSent{ID: "rv_1", Base: "main", Head: "feat", Reviewed: 15, Total: 15, Comments: sentComments(), Note: "Keep it short."}}
	sc := screen{View: transcriptView{Session: record{ID: "agt_1", Status: statusIdle}}, ReviewStates: map[string]map[int64]string{"rv_1": {4: "resolved"}}, ReviewsOpen: map[string]bool{"rv_1": true}}
	n, ok := buildEntryNode(sc, en, "")
	require.True(t, ok)
	require.NoError(t, internalsdui.Validate(n))
	assert.Equal(t, "card", n.Type)
	got := texts(n)
	assert.Contains(t, got, "Review")
	assert.Contains(t, got, "you · 15/15 files · 2 comments · 17:52")
	assert.Contains(t, got, "pkg/sdui/node.go:23")
	assert.Contains(t, got, "internal/sdui/catalog.go:41")
	assert.Contains(t, got, "Workspace is set only by the Always allow path.")
	assert.Contains(t, got, "Also: Keep it short.")
	assert.Equal(t, []string{"resolved", "open"}, cardStates(n))
	assert.Equal(t, 1, n.Children[1].Children[2].Props["lines"])
	head := n.Children[0]
	assert.Equal(t, []string{"spacer", "button"}, []string{head.Children[len(head.Children)-2].Type, head.Children[len(head.Children)-1].Type})
	open := buttonByID(t, n, "agt_1/"+openReviewNode+"/prompt-1")
	assert.Equal(t, actOpenReview, open.Props["action"])
	assert.Nil(t, open.Props["shortcut"])

	sc.View.Session.Status = statusRunning
	sc.Review = &reviewProgress{ID: "rv_1"}
	n, _ = buildEntryNode(sc, en, "")
	assert.Equal(t, []string{"resolved", "working"}, cardStates(n))
	assert.Equal(t, "cmd+r", buttonByID(t, n, "agt_1/"+openReviewNode+"/prompt-1").Props["shortcut"])
	assert.NotEqual(t, reviewCardKey(screen{View: transcriptView{Session: record{Status: statusIdle}}}, en), reviewCardKey(screen{View: transcriptView{Session: record{Status: statusRunning}}}, en))

	sc.ReviewStates, sc.ReviewsOpen, en.At = nil, nil, time.Time{}
	n, _ = buildEntryNode(sc, en, "")
	assert.Equal(t, []string{"working", "working"}, cardStates(n))
	assert.Contains(t, texts(n), "you · 15/15 files · 2 comments")
	assert.Empty(t, nodesOf(n, "button"))
}

func cardStates(n sdui.Node) []string {
	var out []string
	for _, row := range n.Children[1:] {
		if row.ID == "" {
			continue
		}
		out = append(out, row.Children[len(row.Children)-1].Props["text"].(string))
	}
	return out
}
