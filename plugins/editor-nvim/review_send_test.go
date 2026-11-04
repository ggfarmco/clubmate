package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/shortcuttest"
)

func sendTestReview(t *testing.T) (*app, *fakeHost, *review) {
	t.Helper()
	a, h := newTestApp(), &fakeHost{}
	a.nrev = "/res/tools/nrev"
	now := time.Now()
	h.sessions = []agentSession{
		{ID: "agt_new", Worktree: "/repo", Title: "Newest", Status: "done", UpdatedAt: now},
		{ID: "agt_open", Worktree: "/repo", Title: "Opener", Status: "idle", UpdatedAt: now.Add(-time.Hour)},
		{ID: "agt_gone", Worktree: "/repo", Title: "Gone", Status: "closed", UpdatedAt: now},
		{ID: "agt_far", Worktree: "/elsewhere", Title: "Far", Status: "done", UpdatedAt: now},
	}
	nv := &fakeReviewer{
		entries: []reviewEntry{{Path: "a.go", Kind: "modified", Category: "code", Reviewed: true}},
		comments: []reviewComment{
			{ID: 4, File: "a.go", Side: "right", LineStart: 3, LineEnd: 3, Type: "issue", Text: "Say why.", Status: "open"},
			{ID: 5, File: "a.go", Side: "left", LineStart: 9, LineEnd: 9, Type: "suggestion", Text: "Done already.", Status: "resolved"},
		},
	}
	r := openTestReview(t, a, h, nv)
	r.Opener, r.OpenerTitle = "agt_open", "Opener"
	return a, h, r
}

func sendFromNvim(t *testing.T, a *app, h *fakeHost) {
	t.Helper()
	a.applyReviewEvent(t.Context(), h, "ws", "/repo", nvimEvent{Event: "review", Kind: "send", Tab: 1})
}

func uiEvent(t *testing.T, a *app, h *fakeHost, node, action string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, a.reviewUIEvent(t.Context(), h, pluginsdk.UIEvent{WorkspaceID: "ws", Slot: reviewSlot, NodeID: node, Action: action, Value: raw}))
}

func sheetNode(t *testing.T, h *fakeHost) sdui.Node {
	t.Helper()
	n := h.view("ws")
	require.Equal(t, []any{"form", true}, []any{n.Type, n.Props["sheet"]}, "no send sheet")
	return n
}

func TestSendSheetDefaultsToOpener(t *testing.T) {
	a, h, r := sendTestReview(t)
	sendFromNvim(t, a, h)
	require.NoError(t, internalsdui.Validate(h.view("ws")))
	shortcuttest.RequireDeclared(t, h.view("ws"))
	sheet := sheetNode(t, h)
	to, ok := findNode(sheet, sendToNode)
	require.True(t, ok)
	assert.Equal(t, "agt_open", to.Props["value"])
	var values []string
	for _, o := range to.Props["options"].([]sdui.Option) {
		values = append(values, o.Value)
	}
	assert.Equal(t, []string{"agt_open", "agt_new", newRecipient}, values)
	assert.Contains(t, texts(sheet), sendToOpener)
	assert.Equal(t, reviewMeta{Subtitle: "Claude · Opener", Recipient: "Opener"}, r.nv.(*fakeReviewer).lastMeta())

	r.Opener = "agt_gone"
	uiEvent(t, a, h, sheet.ID+"/cancel", "cancel", nil)
	assert.Equal(t, "empty", h.view("ws").Type)
	sendFromNvim(t, a, h)
	to, _ = findNode(sheetNode(t, h), sendToNode)
	assert.Equal(t, "agt_new", to.Props["value"])
	assert.NotContains(t, texts(sheetNode(t, h)), sendToOpener)
	assert.Contains(t, texts(sheetNode(t, h)), sendToOther)

	h.sessions = nil
	sendFromNvim(t, a, h)
	to, _ = findNode(sheetNode(t, h), sendToNode)
	assert.Equal(t, newRecipient, to.Props["value"])
}

func TestSendNeedsOpenComments(t *testing.T) {
	a, h, r := sendTestReview(t)
	r.nv.(*fakeReviewer).comments[0].Status = "resolved"
	a.syncReviews(t.Context(), h, "ws")
	sendFromNvim(t, a, h)
	assert.Equal(t, "empty", h.view("ws").Type)
}

func TestSendSkipsResolved(t *testing.T) {
	a, h, _ := sendTestReview(t)
	sendFromNvim(t, a, h)
	sheet := sheetNode(t, h)
	assert.Contains(t, texts(sheet), "Say why.")
	assert.Contains(t, texts(sheet), "issue · a.go:3")
	assert.NotContains(t, texts(sheet), "Done already.")
	assert.ErrorIs(t, a.reviewUIEvent(t.Context(), h, pluginsdk.UIEvent{WorkspaceID: "ws", Slot: reviewSlot, NodeID: sheet.ID, Action: "sendReview", Value: json.RawMessage(`{"review/sendSheet/to":"agt_far"}`)}), errNotRecipient)
	uiEvent(t, a, h, sheet.ID, "sendReview", map[string]string{sendToNode: "agt_open", sendNoteNode: "Keep it short."})
	require.Len(t, h.sent, 1)
	got := h.sent[0]
	assert.Equal(t, []int64{4}, []int64{got.Comments[0].ID})
	assert.Len(t, got.Comments, 1)
	assert.Equal(t, sendReviewArgs{Session: "agt_open", Worktree: "/repo", Review: got.Review, Base: "main", Head: "Working tree", Reviewed: 1, Total: 1, Comments: got.Comments, Note: "Keep it short.", Nrev: "/res/tools/nrev"}, got)
}

func TestSendMarksSent(t *testing.T) {
	a, h, r := sendTestReview(t)
	sendFromNvim(t, a, h)
	uiEvent(t, a, h, sheetNode(t, h).ID, "sendReview", map[string]string{sendToNode: newRecipient})
	assert.Empty(t, h.sent[0].Session)
	assert.Equal(t, []any{"agt_made", 1}, []any{r.Recipient, r.Sent})
	assert.Equal(t, 1, h.last().Sent)
	assert.Equal(t, "agt_made", h.last().Recipient)
	assert.Equal(t, "empty", h.view("ws").Type)
	assert.Equal(t, 1, r.nv.(*fakeReviewer).lastMeta().Sent)
	var st reviewState
	ok, err := h.StorageGet(t.Context(), reviewStorePrefix+r.ID, &st)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []any{"agt_made", 1}, []any{st.Recipient, st.Sent})
}

func texts(n sdui.Node) []string {
	var out []string
	if s, ok := n.Props["text"].(string); ok {
		out = append(out, s)
	}
	if s, ok := n.Props["label"].(string); ok && n.Type != "header" {
		out = append(out, s)
	}
	for _, c := range n.Children {
		out = append(out, texts(c)...)
	}
	return out
}

func TestSendSheetIDsDifferAcrossRestarts(t *testing.T) {
	a, h, _ := sendTestReview(t)
	sendFromNvim(t, a, h)
	first := sheetNode(t, h).ID
	restarted, h2, _ := sendTestReview(t)
	sendFromNvim(t, restarted, h2)
	assert.NotEqual(t, first, sheetNode(t, h2).ID)
}
