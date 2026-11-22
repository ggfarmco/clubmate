package main_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	reviewCodeManifest = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"sections":[{"id":"editor-nvim","title":"Code","icon":"code","order":10}],"commands":[{"id":"editor-nvim.open","provides":["editor.open"],"title":"Open File","hidden":true},{"id":"editor-nvim.review","title":"Open review","hidden":true}]},"echoResults":{"editor-nvim.open":{},"editor-nvim.review":{}}}`

	codeSlot           = "editor-nvim.chat"
	wikiChatSlot       = "wiki.chat"
	editorSection      = "editor-nvim"
	chatSessionsNode   = "chat/sessions"
	chatNewNode        = "chat/new"
	chatBackNode       = "chat/back"
	panelNode          = "panel"
	actChatPick        = "chatPick"
	actChatNew         = "chatNew"
	actChatBack        = "chatBack"
	actChatOpen        = "open"
	actSend            = "send"
	reviewTopic        = "editor-nvim.review"
	currentReviewTopic = "editor-nvim.review.current"
	wikiPageTopic      = "wiki.page"
	editorFileTopic    = "editor-nvim.file"
	reasonLast         = "last used here"
	reasonNewest       = "newest"
	newSessionLabel    = "New session"
	newReviewLabel     = "New session for this review"
)

type chatEntry struct {
	Session string `json:"session,omitempty"`
	Picking bool   `json:"picking,omitempty"`
}

func chatItems(t *testing.T, n sdui.Node) []sdui.Item {
	t.Helper()
	lists := slices.DeleteFunc(cardsOf(n, "list"), func(l sdui.Node) bool { return l.ID != chatSessionsNode })
	if len(lists) == 0 {
		return nil
	}
	var items []sdui.Item
	raw, err := json.Marshal(lists[0].Props["items"])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &items))
	return items
}

func chatChips(t *testing.T, n sdui.Node) []sdui.Attachment {
	t.Helper()
	composers := cardsOf(n, "composer")
	if len(composers) == 0 {
		return nil
	}
	var chips []sdui.Attachment
	raw, err := json.Marshal(composers[0].Props["attachments"])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &chips))
	return slices.DeleteFunc(chips, func(a sdui.Attachment) bool { return !a.Context })
}

func picking(t *testing.T, suggested, caption string) func(sdui.Node) bool {
	return func(n sdui.Node) bool {
		items := chatItems(t, n)
		return len(items) > 1 && items[1].ID == suggested && strings.HasPrefix(items[1].Caption, caption)
	}
}

func chatting(sid string) func(sdui.Node) bool {
	return func(n sdui.Node) bool {
		composers := cardsOf(n, "composer")
		return len(composers) == 1 && strings.HasPrefix(composers[0].ID, sid+"/") && len(buttonsWith(n, actChatBack)) == 1
	}
}

func newLabel(label string) func(sdui.Node) bool {
	return func(n sdui.Node) bool {
		b := buttonsWith(n, actChatNew)
		return len(b) == 1 && b[0].Props["label"] == label
	}
}

func (f *fixture) publish(t *testing.T, topic string, payload any) {
	t.Helper()
	require.NoError(t, f.h.Client(t).Call(t.Context(), "bus.publish", map[string]any{"topic": topic, "payload": payload}, nil))
}

func TestChatPanelOpenShowsPicker(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.EchoPluginDirAs(t, codeManifest))
	workspace := f.workspace(t, newRepo(t))
	first := f.newSession(t, workspace)
	second := f.newSession(t, workspace)
	f.h.WaitSlot(t, workspace, codeSlot, picking(t, second, reasonNewest))

	f.h.Event(t, workspace, codeSlot, chatSessionsNode, actChatPick, first)
	f.h.WaitSlot(t, workspace, codeSlot, chatting(first))

	f.h.Event(t, workspace, codeSlot, panelNode, actChatOpen, nil)
	f.h.WaitSlot(t, workspace, codeSlot, picking(t, first, reasonLast))

	f.h.Event(t, workspace, codeSlot, chatSessionsNode, actChatPick, second)
	f.h.WaitSlot(t, workspace, codeSlot, chatting(second))
	f.h.Event(t, workspace, codeSlot, chatBackNode, actChatBack, nil)
	f.h.WaitSlot(t, workspace, codeSlot, picking(t, second, reasonLast))
}

func TestChatPanelPickDoesNotMoveEditor(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"), testutil.EchoPluginDirAs(t, codeManifest))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	var kid struct {
		Session  string `json:"session"`
		Worktree string `json:"worktree"`
	}
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"repo": repo, "branch": "side", "prompt": "x"}, &kid))
	f.waitBranchChildDone(t, workspace, kid.Session)
	roots := func() []string {
		p, err := f.h.Svc.GetWorkspace(t.Context(), workspace)
		require.NoError(t, err)
		return p.Roots()
	}
	require.Equal(t, []string{repo}, roots())

	f.h.Event(t, workspace, codeSlot, chatSessionsNode, actChatPick, kid.Session)

	f.h.WaitSlot(t, workspace, codeSlot, chatting(kid.Session))
	testutil.Never(t, func() bool { return !slices.Equal(roots(), []string{repo}) }, time.Second)
}

func (f *fixture) currentReview(t *testing.T, workspace, repo, id string) {
	t.Helper()
	if id != "" {
		f.publish(t, reviewTopic, map[string]any{"workspaceId": workspace, "id": id, "worktree": repo, "base": "main"})
	}
	f.publish(t, currentReviewTopic, map[string]any{"workspaceId": workspace, "worktree": repo, "id": id})
}

func TestChatPanelNewForReviewBecomesOpener(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.EchoPluginDirAs(t, reviewCodeManifest))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.currentReview(t, workspace, repo, "rv_00000001")
	f.h.WaitSlot(t, workspace, codeSlot, newLabel(newReviewLabel))

	f.h.Event(t, workspace, codeSlot, chatNewNode, actChatNew, nil)

	events := testutil.WaitEvents(t, f.h.Bus, "echo.command", func(events []bus.Event) bool {
		return slices.ContainsFunc(events, func(e bus.Event) bool { return strings.Contains(string(e.Payload), `"editor-nvim.review"`) })
	})
	var call struct {
		Args   map[string]string `json:"args"`
		Caller string            `json:"caller"`
	}
	i := slices.IndexFunc(events, func(e bus.Event) bool { return strings.Contains(string(e.Payload), `"editor-nvim.review"`) })
	require.NoError(t, json.Unmarshal(events[i].Payload, &call))
	assert.Equal(t, map[string]string{"base": "main", "head": "", "cwd": repo}, call.Args)
	f.h.WaitSlot(t, workspace, codeSlot, chatting(call.Caller))
}

func TestChatPanelReviewClosedDropsReviewNew(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.EchoPluginDirAs(t, codeManifest))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.newSession(t, workspace)
	f.currentReview(t, workspace, repo, "rv_00000001")
	f.h.WaitSlot(t, workspace, codeSlot, newLabel(newReviewLabel))

	f.currentReview(t, workspace, repo, "")

	f.h.WaitSlot(t, workspace, codeSlot, newLabel(newSessionLabel))
}

func TestChatPanelPickerAfterSessionClosed(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.EchoPluginDirAs(t, codeManifest))
	workspace := f.workspace(t, newRepo(t))
	first := f.newSession(t, workspace)
	second := f.newSession(t, workspace)
	f.h.Event(t, workspace, codeSlot, chatSessionsNode, actChatPick, first)
	f.h.WaitSlot(t, workspace, codeSlot, chatting(first))

	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": first}, nil))

	f.h.WaitSlot(t, workspace, codeSlot, picking(t, second, reasonNewest))
}

func TestChatPanelStateSurvivesRestart(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.EchoPluginDirAs(t, codeManifest))
	workspace := f.workspace(t, newRepo(t))
	first := f.newSession(t, workspace)
	f.newSession(t, workspace)
	f.h.Event(t, workspace, codeSlot, chatSessionsNode, actChatPick, first)
	f.h.WaitSlot(t, workspace, codeSlot, chatting(first))
	f.h.Event(t, workspace, codeSlot, chatBackNode, actChatBack, nil)
	f.h.WaitSlot(t, workspace, codeSlot, picking(t, first, reasonLast))

	f.restartAgents(t, workspace)

	f.h.WaitSlot(t, workspace, codeSlot, picking(t, first, reasonLast))
}

func TestChatPanelReadsLegacyCodeKey(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.EchoPluginDirAs(t, codeManifest))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	first := f.newSession(t, workspace)
	f.newSession(t, workspace)
	raw, err := json.Marshal(map[string]string{repo: first})
	require.NoError(t, err)
	require.NoError(t, f.h.KV.Set(t.Context(), "agents", "code/"+workspace, raw))

	f.restartAgents(t, workspace)

	f.h.WaitSlot(t, workspace, codeSlot, chatting(first))
	testutil.Eventually(t, func() bool {
		stored, found, err := f.h.KV.Get(t.Context(), "agents", "chat/"+workspace)
		var chat map[string]chatEntry
		return err == nil && found && json.Unmarshal(stored, &chat) == nil && chat["code:"+repo] == chatEntry{Session: first}
	}, wait)
}

func TestChatPanelWikiShowsPageChip(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.EchoPluginDirAs(t, wikiManifest))
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.publish(t, wikiPageTopic, map[string]string{"workspace": workspace, "page": "shop/roadmap", "title": "Roadmap"})
	f.h.Event(t, workspace, wikiChatSlot, chatSessionsNode, actChatPick, session)

	f.h.WaitSlot(t, workspace, wikiChatSlot, func(n sdui.Node) bool {
		chips := chatChips(t, n)
		return chatting(session)(n) && len(chips) == 1 && chips[0].Kind == "page" && chips[0].Path == "Roadmap page" && chips[0].Removable
	})

	f.h.Event(t, workspace, wikiChatSlot, session+"/composer/0", actSend, map[string]string{"text": "what is next?"})
	call := f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), "what is next?") })
	assert.Contains(t, string(call.Params), "shop/roadmap")
}

func TestChatPanelOtherWorktreeHasNoFileChip(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.EchoPluginDirAs(t, codeManifest))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	here := f.newSession(t, workspace)
	var other struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"branch": "side"}, &other))
	f.publish(t, editorFileTopic, map[string]string{"workspace": workspace, "root": repo, "path": filepath.Join(repo, "notify.go")})
	f.h.Event(t, workspace, codeSlot, chatSessionsNode, actChatPick, here)
	f.h.WaitSlot(t, workspace, codeSlot, func(n sdui.Node) bool {
		chips := chatChips(t, n)
		return chatting(here)(n) && len(chips) == 1 && chips[0].Path == "notify.go"
	})

	f.h.Event(t, workspace, codeSlot, chatSessionsNode, actChatPick, other.Session)

	panel := f.h.WaitSlot(t, workspace, codeSlot, chatting(other.Session))
	assert.Empty(t, chatChips(t, panel))
}
