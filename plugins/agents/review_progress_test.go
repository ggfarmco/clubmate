package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/shortcuttest"
)

func reviewEvent(t *testing.T, b *reviewBoard, p reviewPayload) []string {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	_, people, ok := b.apply(raw)
	require.True(t, ok)
	return people
}

func TestReviewDetailOnOpenerRow(t *testing.T) {
	t.Parallel()
	b := newReviewBoard()
	reviewEvent(t, b, reviewPayload{WorkspaceID: "ws", ID: "rv_1", Opener: "agt_1", Reviewed: 5, Total: 15, Open: 1})
	reviewEvent(t, b, reviewPayload{WorkspaceID: "ws", ID: "rv_2", Opener: "agt_9", Recipient: "agt_2", Reviewed: 2, Total: 6, Open: 0})
	now := time.Now()
	done := sessionRow{Rec: record{ID: "agt_1", Status: statusDone, UpdatedAt: now.Add(-2 * time.Hour)}, Review: b.progress("agt_1")}
	assert.Equal(t, "review 5/15 · 1", sessionItem(done, 0, now).Detail)
	assert.Equal(t, &reviewProgress{ID: "rv_2", Reviewed: 2, Total: 6, Text: "review 2/6 · 0"}, b.progress("agt_2"))
	changed := sessionRow{Rec: record{ID: "agt_1", Status: statusDone, Review: true}, Review: b.progress("agt_1")}
	assert.Equal(t, "review 5/15 · 1", sessionItem(changed, 0, now).Detail)
	running := sessionRow{Rec: record{ID: "agt_1", Status: statusRunning}, Activity: "fixing review", Review: b.progress("agt_1")}
	assert.Equal(t, "fixing review", sessionItem(running, 0, now).Detail)
	closed := sessionRow{Rec: record{ID: "agt_1", Status: statusClosed, UpdatedAt: now}, Review: b.progress("agt_1")}
	assert.Equal(t, "just now", sessionItem(closed, 0, now).Detail)
	assert.Nil(t, b.progress("agt_3"))
}

func TestReviewDetailDropsOnClose(t *testing.T) {
	t.Parallel()
	b := newReviewBoard()
	open := reviewPayload{WorkspaceID: "ws", ID: "rv_1", Opener: "agt_1", Reviewed: 1, Total: 2}
	reviewEvent(t, b, open)
	require.NotNil(t, b.progress("agt_1"))
	open.Closed, open.Recipient = true, "agt_2"
	assert.Equal(t, []string{"agt_1", "agt_2"}, reviewEvent(t, b, open))
	assert.Nil(t, b.progress("agt_1"))
	now := time.Now()
	row := sessionRow{Rec: record{ID: "agt_1", Status: statusDone, UpdatedAt: now}, Review: b.progress("agt_1")}
	assert.Equal(t, "just now", sessionItem(row, 0, now).Detail)
}

func TestReviewHeaderButton(t *testing.T) {
	t.Parallel()
	sc := screen{View: transcriptView{Session: record{ID: "agt_1", Status: statusDone, Title: "Fix"}}, Review: &reviewProgress{ID: "rv_1", Reviewed: 5, Total: 15, Open: 1}}
	top := header(sc)
	require.NoError(t, internalsdui.Validate(top))
	h := nodesOf(top, "header")[0]
	assert.Contains(t, h.Props["badges"], sdui.HeaderBadge{Text: "Review 5/15 · 1 issue"})
	open := buttonByID(t, top, "agt_1/"+openReviewNode)
	assert.Equal(t, []any{"Open review", actOpenReview, "cmd+r"}, []any{open.Props["label"], open.Props["action"], open.Props["shortcut"]})
	shortcuttest.RequireDeclared(t, open)

	sc.Review = &reviewProgress{ID: "rv_1", Reviewed: 15, Total: 15, Sent: 3}
	h = nodesOf(header(sc), "header")[0]
	assert.Contains(t, h.Props["badges"], sdui.HeaderBadge{Text: "Review 15/15 · 3 comments sent", Tone: "success"})
}

func buttonByID(t *testing.T, n sdui.Node, id string) sdui.Node {
	t.Helper()
	for _, b := range nodesOf(n, "button") {
		if b.ID == id {
			return b
		}
	}
	require.Failf(t, "no button", "%s", id)
	return sdui.Node{}
}
