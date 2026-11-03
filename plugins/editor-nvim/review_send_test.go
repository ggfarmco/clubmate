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
	a.exportComments = func(dir string) string { return "# open comments in " + dir }
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

func uiEvent(t *testing.T, a *app, h *fakeHost, node, action string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, a.reviewUIEvent(t.Context(), h, pluginsdk.UIEvent{WorkspaceID: "ws", Slot: reviewSlot, NodeID: node, Action: action, Value: raw}))
}

func sheetNode(t *testing.T, h *fakeHost) sdui.Node {
	t.Helper()
	var found sdui.Node
	var walk func(sdui.Node)
	walk = func(n sdui.Node) {
		if n.Type == "form" && n.Props["sheet"] == true {
			found = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(h.view("ws"))
	require.Equal(t, "form", found.Type, "no send sheet")
	return found
}

func TestSendSheetDefaultsToOpener(t *testing.T) {
	a, h, r := sendTestReview(t)
	uiEvent(t, a, h, "review/send", "send", nil)
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
	send, ok := findNode(h.view("ws"), "review/send")
	require.True(t, ok)
	assert.Equal(t, true, send.Props["disabled"])

	r.Opener = "agt_gone"
	uiEvent(t, a, h, sheet.ID+"/cancel", "cancel", nil)
	_, ok = findNode(h.view("ws"), sendToNode)
	assert.False(t, ok)
	uiEvent(t, a, h, "review/send", "send", nil)
	to, _ = findNode(sheetNode(t, h), sendToNode)
	assert.Equal(t, "agt_new", to.Props["value"])

	h.sessions = nil
	uiEvent(t, a, h, "review/send", "send", nil)
	to, _ = findNode(sheetNode(t, h), sendToNode)
	assert.Equal(t, newRecipient, to.Props["value"])
}

func TestSendSkipsResolved(t *testing.T) {
	a, h, _ := sendTestReview(t)
	uiEvent(t, a, h, "review/send", "send", nil)
	sheet := sheetNode(t, h)
	assert.Contains(t, texts(sheet), "Say why.")
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
	uiEvent(t, a, h, "review/send", "send", nil)
	uiEvent(t, a, h, sheetNode(t, h).ID, "sendReview", map[string]string{sendToNode: newRecipient})
	assert.Empty(t, h.sent[0].Session)
	assert.Equal(t, []any{"agt_made", 1}, []any{r.Recipient, r.Sent})
	assert.Equal(t, 1, h.last().Sent)
	assert.Equal(t, "agt_made", h.last().Recipient)
	_, ok := findNode(h.view("ws"), sendToNode)
	assert.False(t, ok)
	var st reviewState
	ok, err := h.StorageGet(t.Context(), reviewStorePrefix+r.ID, &st)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []any{"agt_made", 1}, []any{st.Recipient, st.Sent})
	assert.Contains(t, texts(h.view("ws")), "1/1 · 1 comment sent")
}

func TestCopyIsTheNrevExport(t *testing.T) {
	_, h, _ := sendTestReview(t)
	copyButton, ok := findNode(h.view("ws"), "review/copy")
	require.True(t, ok)
	assert.Equal(t, "# open comments in /repo", copyButton.Props["copy"])
	assert.Nil(t, copyButton.Props["disabled"])
}
