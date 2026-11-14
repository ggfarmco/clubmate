package main

import (
	"encoding/json"
	"testing"
	"time"
	"unicode/utf8"

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
	assert.Equal(t, "5/15 · 1", sessionItem(done, 0, now).Detail)
	assert.Equal(t, statusIcons[statusReview], sessionItem(done, 0, now).Icon)
	assert.Equal(t, &reviewProgress{ID: "rv_2", Reviewed: 2, Total: 6, Text: "review 2/6 · 0", Short: "2/6"}, b.progress("agt_2"))
	changed := sessionRow{Rec: record{ID: "agt_1", Status: statusDone, Review: true}, Review: b.progress("agt_1")}
	assert.Equal(t, "5/15 · 1", sessionItem(changed, 0, now).Detail)
	running := sessionRow{Rec: record{ID: "agt_1", Status: statusRunning}, Activity: "fixing review", Review: b.progress("agt_1")}
	assert.Equal(t, "fixing review", sessionItem(running, 0, now).Detail)
	assert.Equal(t, statusIcons[statusRunning], sessionItem(running, 0, now).Icon)
	closed := sessionRow{Rec: record{ID: "agt_1", Status: statusClosed, UpdatedAt: now}, Review: b.progress("agt_1")}
	assert.Equal(t, "just now", sessionItem(closed, 0, now).Detail)
	assert.Nil(t, b.progress("agt_3"))
}

func TestReviewShortFitsTheRowDetail(t *testing.T) {
	t.Parallel()
	assert.LessOrEqual(t, utf8.RuneCountInString(shortProgress(99, 99, 9)), 9)
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

func TestReviewBoardSurvivesRestart(t *testing.T) {
	t.Parallel()
	b := newReviewBoard()
	reviewEvent(t, b, reviewPayload{WorkspaceID: "ws", ID: "rv_1", Opener: "agt_1", Reviewed: 1, Total: 2})
	reviewEvent(t, b, reviewPayload{WorkspaceID: "ws", ID: "rv_2", Opener: "agt_1", Reviewed: 3, Total: 4})
	reviewEvent(t, b, reviewPayload{WorkspaceID: "ws", ID: "rv_3", Opener: "agt_2", Closed: true, Comments: []reviewPayloadComment{{ID: 4, Status: "resolved"}}})
	kv := memStore{}
	require.NoError(t, b.save(t.Context(), kv, 42))

	restarted := newReviewBoard()
	seq, err := restarted.load(t.Context(), kv)
	require.NoError(t, err)
	assert.Equal(t, int64(42), seq)
	assert.Equal(t, b.progress("agt_1"), restarted.progress("agt_1"))
	assert.Nil(t, restarted.progress("agt_2"))
	states, open := restarted.cards(nil)
	assert.Equal(t, map[string]map[int64]string{"rv_3": {4: "resolved"}}, states)
	assert.Equal(t, map[string]bool{"rv_1": true, "rv_2": true}, open)

	seq, err = newReviewBoard().load(t.Context(), memStore{})
	require.NoError(t, err)
	assert.Zero(t, seq)
}

func TestEditorRestartClosesReviews(t *testing.T) {
	t.Parallel()
	b := newReviewBoard()
	reviewEvent(t, b, reviewPayload{WorkspaceID: "ws", ID: "rv_1", Opener: "agt_1", Recipient: "agt_2", Total: 2, Comments: []reviewPayloadComment{{ID: 4, Status: "resolved"}}})
	reviewEvent(t, b, reviewPayload{WorkspaceID: "ws2", ID: "rv_2", Opener: "agt_3", Total: 1})
	assert.Equal(t, map[string][]string{"ws": {"agt_1", "agt_2"}, "ws2": {"agt_3"}}, b.closeAll())
	assert.Nil(t, b.progress("agt_1"))
	states, open := b.cards(nil)
	assert.Equal(t, map[string]map[int64]string{"rv_1": {4: "resolved"}}, states)
	assert.Empty(t, open)
}

func reviewApp(t *testing.T) *app {
	t.Helper()
	a := searchApp(t, record{ID: "agt_1", Workspace: "ws"}, record{ID: "agt_2", Workspace: "ws", Worktree: "/repo"}, record{ID: "agt_3", Workspace: "ws", Worktree: "/repo"})
	a.reviews = newReviewBoard()
	for _, id := range []string{"agt_1", "agt_2"} {
		appendLog(t, a, id, logLine(t, kindPrompt, 1, promptPayload{Review: &reviewSent{ID: "rv_1", Comments: sentComments()}}))
	}
	return a
}

func TestClosedReviewCardKeepsItsStates(t *testing.T) {
	t.Parallel()
	a := reviewApp(t)
	p := reviewPayload{WorkspaceID: "ws", ID: "rv_1", Worktree: "/repo", Recipient: "agt_1", Comments: []reviewPayloadComment{{ID: 4, Status: "resolved"}, {ID: 7, Status: "open"}}}
	reviewEvent(t, a.reviews, p)
	assert.False(t, a.freezeClosedReviews(nil, true))
	assert.Empty(t, a.session("agt_1").view(0).ReviewStates)

	p.Closed = true
	reviewEvent(t, a.reviews, p)
	require.True(t, a.freezeClosedReviews([]*session{a.session("agt_1")}, false))
	want := map[string]map[int64]string{"rv_1": {4: "resolved", 7: "open"}}
	board, _ := a.reviews.cards(nil)
	assert.Empty(t, board)
	assert.Equal(t, want, a.session("agt_1").view(0).ReviewStates)
	assert.Equal(t, want, a.session("agt_2").view(0).ReviewStates)
	assert.Empty(t, a.session("agt_3").view(0).ReviewStates)

	restarted := newSession(record{ID: "agt_1"}, newEventLog(logPath(a.stateDir, "agt_1")), false, func(string, bool) {})
	states, _ := a.reviews.cards(restarted.view(0).ReviewStates)
	assert.Equal(t, want, states)
}

func TestLegacyReviewStatesMoveIntoTheLog(t *testing.T) {
	t.Parallel()
	a := reviewApp(t)
	kv := memStore{}
	require.NoError(t, kv.StorageSet(t.Context(), reviewBoardKey, savedBoard{Seq: 9, States: map[string]map[int64]string{"rv_1": {4: "resolved"}, "rv_9": {1: "open"}}}))
	seq, err := a.loadReviews(t.Context(), kv)
	require.NoError(t, err)
	assert.Equal(t, int64(9), seq)
	assert.Equal(t, map[string]map[int64]string{"rv_1": {4: "resolved"}}, a.session("agt_1").view(0).ReviewStates)
	var saved savedBoard
	_, err = kv.StorageGet(t.Context(), reviewBoardKey, &saved)
	require.NoError(t, err)
	assert.Equal(t, savedBoard{Seq: 9}, saved)
}

func TestClosedReviewLineContinuesTheLogSeq(t *testing.T) {
	t.Parallel()
	a := reviewApp(t)
	for _, id := range []string{"agt_1", "agt_3"} {
		appendLog(t, a, id, `{"seq":50,"kind":"update","payload":{}}`+"\n")
	}
	reviewEvent(t, a.reviews, reviewPayload{WorkspaceID: "ws", ID: "rv_1", Recipient: "agt_1", Closed: true, Comments: []reviewPayloadComment{{ID: 4, Status: "resolved"}}})

	require.True(t, a.freezeClosedReviews(nil, true))
	assert.Zero(t, a.session("agt_3").log.seq)

	var last envelope
	require.NoError(t, newEventLog(logPath(a.stateDir, "agt_1")).replay(func(e envelope) { last = e }))
	assert.Equal(t, kindReviewClosed, last.Kind)
	assert.Equal(t, int64(51), last.Seq)
}
