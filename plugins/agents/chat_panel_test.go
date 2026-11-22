package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/agents/api"
)

func TestSuggestChatOrder(t *testing.T) {
	t.Parallel()
	now := time.Unix(100, 0)
	opener := record{ID: "agt_op", UpdatedAt: now}
	recipient := record{ID: "agt_rc", UpdatedAt: now.Add(-time.Minute)}
	stored := record{ID: "agt_st", UpdatedAt: now.Add(-2 * time.Minute)}
	newest := record{ID: "agt_nw", UpdatedAt: now.Add(time.Minute)}
	step := record{ID: "agt_gs", Run: &api.Run{ID: "gr_1"}, UpdatedAt: now.Add(time.Hour)}
	service := record{ID: "agt_sv", SystemPrompt: "review", UpdatedAt: now.Add(2 * time.Hour)}
	closed := func(r record) record { r.Status = statusClosed; return r }
	code := chatContext{Slot: codeSlot, Key: "code:/r", Root: "/r"}
	rev := &reviewRef{ID: "rv_1", Opener: opener.ID, Recipient: recipient.ID}
	cases := []struct {
		name   string
		recs   []record
		cc     chatContext
		rev    *reviewRef
		sid    string
		reason string
	}{
		{"review opener", []record{opener, recipient, stored, newest, step}, code, rev, opener.ID, reasonOpener},
		{"opener closed", []record{closed(opener), recipient, stored, newest}, code, rev, recipient.ID, reasonRecipient},
		{"no review", []record{opener, recipient, stored, newest}, code, nil, stored.ID, reasonLast},
		{"wiki ignores the review", []record{opener, stored}, chatContext{Slot: wikiChatSlot, Key: wikiChatContext}, rev, stored.ID, reasonLast},
		{"stored closed", []record{opener, closed(stored), newest, step, service}, code, nil, newest.ID, reasonNewest},
		{"only service sessions", []record{step, service}, code, nil, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sid, reason := suggestChat(c.recs, c.cc, chatEntry{Session: stored.ID}, c.rev)
			assert.Equal(t, []string{c.sid, c.reason}, []string{sid, reason})
		})
	}
}

func TestPickerViewRows(t *testing.T) {
	t.Parallel()
	now := time.Now()
	recs := []record{
		{ID: "agt_1", Title: "Fix login", Worktree: "/r", Status: statusDone, UpdatedAt: now.Add(-time.Hour)},
		{ID: "agt_2", Title: "Docs", Worktree: "/r/.worktrees/docs", Status: statusWaiting, UpdatedAt: now},
		{ID: "agt_4", Title: "Gate review", Worktree: "/r", Status: statusDone, SystemPrompt: "review", UpdatedAt: now},
		{ID: "agt_3", Worktree: "/r", Status: statusClosed, UpdatedAt: now},
	}
	pk := chatPicker{Workspace: "shop", Root: "/r", Recs: recs, Suggested: "agt_1", Reason: reasonLast, NewLabel: newSessionLabel, NewNote: "Starts on main", Now: now}

	tree := pickerView(pk)
	require.NoError(t, internalsdui.Validate(tree))

	list := nodesOf(tree, "list")[0]
	items := list.Props["items"].([]sdui.Item)
	assert.Equal(t, []sdui.Item{
		{ID: suggestedRow, Label: suggestedLabel, Section: true},
		{ID: "agt_1", Label: "Fix login", Icon: statusIcons[statusDone], Caption: "last used here · 1 h ago"},
		{ID: openInRow, Label: "Open in shop", Detail: "1", Section: true},
		{ID: "agt_2", Label: "Docs", Icon: statusIcons[statusWaiting], Tone: "warning", Badge: needsYouBadge, Caption: "docs · just now"},
	}, items)
	assert.Equal(t, []any{"agt_1", sdui.ListFilter{Placeholder: filterHint, MinRows: filterMinRows}}, []any{list.Props["focus"], list.Props["filter"]})
	assert.Equal(t, []any{"keys", []string{newKey}, actChatNew, pickerHints}, []any{tree.Type, tree.Props["keys"], tree.Props["action"], tree.Props["hints"]})
	add := nodesOf(tree, "button")[0]
	assert.Equal(t, []any{newSessionLabel, sdui.VariantSecondary, newKey}, []any{add.Props["label"], add.Props["variant"], add.Props["hint"]})

	pk.Recs = recs[2:]
	pk.Suggested = ""
	empty := pickerView(pk)
	require.NoError(t, internalsdui.Validate(empty))
	assert.Equal(t, []any{"No open sessions in shop", "A new session starts on main.", closedStay}, []any{empty.Children[0].Props["title"], empty.Children[0].Props["text"], empty.Children[0].Props["hint"]})
	assert.Equal(t, sdui.VariantPrimary, nodesOf(empty, "button")[0].Props["variant"])
	assert.Equal(t, emptyHints, empty.Props["hints"])
}

func TestReviewNamesShortenCommits(t *testing.T) {
	t.Parallel()
	rev := &reviewRef{Base: "1cd7f025ff2e47614a70d3a578a96552469fadeb", Head: "feat/send"}
	assert.Equal(t, "Starts on feat/send and opens the review 1cd7f02 → feat/send", newSessionNote("feat/send", rev, nil))
	assert.Equal(t, "Review main → feat/send", reviewChipLabel(reviewRef{Base: "main"}, "feat/send"))
}

func TestPanelReviewIsPerWorktree(t *testing.T) {
	t.Parallel()
	a := &app{uiState: newUIState(), reviews: newReviewBoard()}
	raw, err := json.Marshal(reviewPayload{WorkspaceID: "ws", ID: "rv_1", Worktree: "/r/a", Base: "main"})
	require.NoError(t, err)
	a.reviews.apply(raw)
	a.setCurrentReview(currentReview{WorkspaceID: "ws", Worktree: "/r/a", ID: "rv_1"})

	a.setCurrentReview(currentReview{WorkspaceID: "ws", Worktree: "/r/b", ID: ""})

	rev := a.panelReview("ws", chatContext{Slot: codeSlot, Root: "/r/a"})
	require.NotNil(t, rev)
	assert.Equal(t, "rv_1", rev.ID)
	assert.Nil(t, a.panelReview("ws", chatContext{Slot: codeSlot, Root: "/r/b"}))
}

func TestPanelContextButtonOnlyWithoutChips(t *testing.T) {
	t.Parallel()
	rec := record{ID: "agt_1", Worktree: "/r", Agent: agentClaude, AgentSessionID: "s1", Status: statusDone}
	sc := screen{View: transcriptView{Session: rec, Entries: []*entry{{Kind: entryUser, ID: "u1", Text: "hi"}}}, Composer: "agt_1/composer/0"}
	back := sdui.WithAction(sdui.Button(chatBackNode, backLabel, "chevron.left"), actChatBack)

	bare := buttonsOf(codeView([]record{rec}, sc, codePanel{Back: back}), actContext)
	chipped := buttonsOf(codeView([]record{rec}, sc, codePanel{Back: back, Chips: []sdui.Attachment{{ID: "ctx/page", Kind: "page", Path: "Roadmap page", Context: true}}}), actContext)

	require.Len(t, bare, 1)
	assert.Equal(t, []any{true, contextKey}, []any{bare[0].Props["iconOnly"], bare[0].Props["shortcut"]})
	assert.Empty(t, chipped)
}

func buttonsOf(n sdui.Node, action string) []sdui.Node {
	var out []sdui.Node
	for _, b := range nodesOf(n, "button") {
		if b.Props["action"] == action {
			out = append(out, b)
		}
	}
	return out
}

func TestContextChipsShowInTheSentMessage(t *testing.T) {
	t.Parallel()
	atts := []attachment{
		{Kind: "review", Path: "Review 0123456 → feat/x", Text: fmt.Sprintf(reviewNote, "0123456789abcdef0123456789abcdef01234567", "feat/x", "/r")},
		{Kind: "page", Path: "Roadmap page", Text: fmt.Sprintf(pageNote, "Roadmap", "shop/roadmap")},
	}
	var content []json.RawMessage
	for _, b := range promptBlocks(promptArgs{Text: "what next?", Attachments: atts}, "/r", acp.PromptCapabilities{}) {
		raw, err := json.Marshal(b)
		require.NoError(t, err)
		content = append(content, raw)
	}

	assert.Equal(t, []sdui.Attachment{{Kind: "review", Path: "Review 0123456 → feat/x"}, {Kind: "page", Path: "Roadmap page"}}, promptAttachments(content, "/r"))
}

func TestRemovedChipComesBackForNewContent(t *testing.T) {
	t.Parallel()
	a := &app{uiState: newUIState(), reviews: newReviewBoard()}
	for _, p := range []reviewPayload{{WorkspaceID: "ws", ID: "rv_1", Worktree: "/r", Base: "main"}, {WorkspaceID: "ws", ID: "rv_2", Worktree: "/r", Base: "dev"}} {
		raw, err := json.Marshal(p)
		require.NoError(t, err)
		a.reviews.apply(raw)
	}
	cc := chatContext{Slot: codeSlot, Root: "/r"}
	rec := record{ID: "agt_1", Worktree: "/r"}
	a.setCurrentReview(currentReview{WorkspaceID: "ws", Worktree: "/r", ID: "rv_1"})
	chips := a.contextChips("ws", cc, rec, "feat/x")
	require.Len(t, chips, 1)
	a.sessionState("ws", rec.ID).contextOff = map[string]string{chips[0].chip.ID: chips[0].content()}
	require.Empty(t, a.shownChips("ws", cc, rec, "feat/x"))

	a.setCurrentReview(currentReview{WorkspaceID: "ws", Worktree: "/r", ID: "rv_2"})

	shown := a.shownChips("ws", cc, rec, "feat/x")
	require.Len(t, shown, 1)
	assert.Equal(t, "Review dev → feat/x", shown[0].chip.Path)
}
