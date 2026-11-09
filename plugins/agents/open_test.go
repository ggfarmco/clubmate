package main_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

type openItem struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	State    string `json:"state"`
	Answer   string `json:"answer"`
	Task     int    `json:"task"`
	Number   int    `json:"number"`
	Question string `json:"question"`
	Label    string `json:"label"`
}

type openPosted struct {
	Items    []string `json:"items"`
	Overflow []int    `json:"overflow"`
	Handed   bool     `json:"handed"`
}

var echoScript = map[string]any{"prompt": []map[string]any{{"echo": true}}}

func decision(header string) map[string]any {
	return map[string]any{"kind": "decision", "header": header, "question": "Which " + header + "?", "quote": "About " + header + ".", "options": []string{"One", "Two"}, "descriptions": []string{"First", "Second"}}
}

func multiDecision(header string) map[string]any {
	d := decision(header)
	d["multiSelect"] = true
	return d
}

func offer(label string) map[string]any {
	return map[string]any{"kind": "offer", "label": label, "quote": "I could " + label + "."}
}

func backlogDir(t *testing.T) string {
	t.Helper()
	return testutil.BuildPluginDir(t, "clubmate/plugins/backlog", "plugins/backlog/plugin.json")
}

func (f *fixture) doneSession(t *testing.T, workspace string) string {
	t.Helper()
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "done")
	f.waitStatus(t, workspace, session, "done")
	testutil.Eventually(t, func() bool { return f.session(t, workspace, session).AgentSessionID != "" }, wait)
	return session
}

func (f *fixture) postItems(t *testing.T, workspace, session string, items ...map[string]any) openPosted {
	t.Helper()
	var out openPosted
	require.NoError(t, f.run(t, workspace, "agents.open-items", map[string]any{"session": session, "items": items}, &out))
	return out
}

func (f *fixture) openEntry(t *testing.T, workspace, session string) entry {
	t.Helper()
	for _, en := range f.transcript(t, workspace, session).Entries {
		if en.Kind == "openItems" {
			return en
		}
	}
	return entry{}
}

func (f *fixture) taskText(t *testing.T, workspace string, id int) string {
	t.Helper()
	var task struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	require.NoError(t, f.run(t, workspace, "backlog.get", map[string]any{"id": id}, &task))
	return task.Status + "\n" + task.Title + "\n" + task.Description
}

func TestOpenItemsLeaveTheSessionDoneAndSurviveARestart(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"resume": true, "prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)

	posted := f.postItems(t, workspace, session, decision("Lists"), offer("add a screen"))

	assert.Equal(t, []string{"oi-1", "oi-2"}, posted.Items)
	rec := f.session(t, workspace, session)
	assert.Equal(t, "done", rec.Status)
	assert.False(t, rec.NeedsYou)
	assert.Equal(t, 2, rec.OpenCount)
	assert.Equal(t, "open", rec.Group)
	assert.NotEmpty(t, rec.OpenSince)

	f.restartPlugin(t, workspace)

	assert.Equal(t, 2, f.session(t, workspace, session).OpenCount)
	items := f.openEntry(t, workspace, session).Items
	require.Len(t, items, 2)
	assert.Equal(t, openItem{ID: "oi-1", Kind: "decision", State: "open", Number: 1, Question: "Which Lists?"}, items[0])
	assert.Equal(t, []string{"oi-3"}, f.postItems(t, workspace, session, offer("more")).Items)
}

func TestAgentPostsItemsForItsOwnSessionOnly(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	other := f.doneSession(t, workspace)
	args := map[string]any{"items": []map[string]any{offer("a")}}

	require.NoError(t, f.runAs(t, workspace, session, "agents.open-items", args, nil))

	args["session"] = other
	assert.ErrorContains(t, f.runAs(t, workspace, session, "agents.open-items", args, nil), "its own session")
	assert.Error(t, f.runAs(t, workspace, session, "agents.open-no", map[string]any{"session": session}, nil))
	assert.Equal(t, 1, f.session(t, workspace, session).OpenCount)
}

func TestOnlyTheUserAnswersOpenItems(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, decision("Lists"), offer("a"))

	for command, args := range map[string]map[string]any{
		"agents.open-answer":  {"session": session, "offers": []string{"oi-2"}},
		"agents.open-no":      {"session": session, "items": []string{"oi-1"}},
		"agents.open-later":   {"session": session},
		"agents.open-backlog": {"session": session, "items": []string{"oi-1"}},
		"agents.open-convert": {"session": session},
	} {
		assert.ErrorContains(t, f.runFrom(t, workspace, session, command, args, nil), "only the user answers open items", command)
	}
	assert.Equal(t, 2, f.session(t, workspace, session).OpenCount)
}

func TestItemsOverTheLimitGoToTheBacklog(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript, backlogDir(t))
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)

	posted := f.postItems(t, workspace, session, decision("a"), decision("b"), decision("c"), decision("d"), offer("e"), offer("f"), offer("g"), offer("h"), offer("i"))

	assert.Len(t, posted.Items, 7)
	require.Len(t, posted.Overflow, 2)
	assert.Equal(t, 7, f.session(t, workspace, session).OpenCount)
	assert.Equal(t, posted.Overflow, f.openEntry(t, workspace, session).Overflow)
	text := f.taskText(t, workspace, posted.Overflow[0])
	assert.Contains(t, text, "todo\nWhich d?")
	assert.Contains(t, text, "About d.")
	assert.Contains(t, text, session)

	later := f.postItems(t, workspace, session, offer("j"))
	assert.Empty(t, later.Items)
	assert.Len(t, later.Overflow, 1)
}

func TestAnswersCloseItemsAndStartOneTurn(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, decision("Lists"), multiDecision("Grouping"), offer("add a screen"), offer("skip it"))

	require.NoError(t, f.run(t, workspace, "agents.open-answer", map[string]any{
		"session": session,
		"answers": []map[string]any{{"item": "oi-1", "option": "Two"}, {"item": "oi-2", "options": []string{"One", "Two"}, "other": "both"}},
		"offers":  []string{"oi-3"},
		"note":    "and hurry",
	}, nil))

	f.waitCall(t, "session/prompt", func(c fakeCall) bool {
		return promptText(t, c) == "Answers: Lists → Two; Grouping → One, Two, both; do: add a screen\n\nand hurry"
	})
	f.waitStatus(t, workspace, session, "done")
	assert.Len(t, f.calls(t, "session/prompt"), 2)
	rec := f.session(t, workspace, session)
	assert.Equal(t, 1, rec.OpenCount)
	assert.Equal(t, "open", rec.Group)
	items := f.openEntry(t, workspace, session).Items
	assert.Equal(t, "answered", items[1].State)
	assert.Equal(t, "One, Two, both", items[1].Answer)
	assert.Equal(t, "open", items[3].State)

	assert.ErrorContains(t, f.run(t, workspace, "agents.open-answer", map[string]any{"session": session, "offers": []string{"oi-3"}}, nil), "not an open item")
	assert.ErrorContains(t, f.run(t, workspace, "agents.open-answer", map[string]any{"session": session, "answers": []map[string]any{{"item": "oi-4", "option": "One"}}}, nil), "not a decision")
}

func TestAnswerToARunningSessionIsQueuedNotSteered(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"steering": true, "prompt": []map[string]any{{"match": "long", "sleep": 1000}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, offer("add a screen"))
	f.prompt(t, workspace, session, "long")
	f.waitStatus(t, workspace, session, "running")

	require.NoError(t, f.run(t, workspace, "agents.open-answer", map[string]any{"session": session, "offers": []string{"oi-1"}}, nil))

	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "Answers: do: add a screen" })
	assert.Empty(t, f.calls(t, "_session/steering"))
}

func TestItemTheQueuedAnswerCoversIsNotPostedAgain(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "long", "sleep": 1000}, {"echo": true}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, offer("drop the branch"))
	f.prompt(t, workspace, session, "long")
	f.waitStatus(t, workspace, session, "running")
	require.NoError(t, f.run(t, workspace, "agents.open-answer", map[string]any{"session": session, "offers": []string{"oi-1"}}, nil))

	posted := f.postItems(t, workspace, session, offer("Drop the branch"), offer("write the docs"))

	assert.ElementsMatch(t, []string{"oi-1", "oi-2"}, posted.Items)
	assert.Equal(t, 1, f.session(t, workspace, session).OpenCount)

	f.waitPrompt(t, "Answers: do: drop the branch")
	f.waitStatus(t, workspace, session, "done")

	again := f.postItems(t, workspace, session, offer("drop the branch"))

	assert.Equal(t, []string{"oi-3"}, again.Items)
	assert.Equal(t, 2, f.session(t, workspace, session).OpenCount)
}

func TestItemDeclinedInARunningTurnIsNotPostedAgain(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "long", "sleep": 1000}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, decision("Lists"))
	f.prompt(t, workspace, session, "long")
	f.waitStatus(t, workspace, session, "running")
	require.NoError(t, f.run(t, workspace, "agents.open-no", map[string]any{"session": session}, nil))

	posted := f.postItems(t, workspace, session, decision("Lists"))

	assert.Equal(t, []string{"oi-1"}, posted.Items)
	assert.Equal(t, 0, f.session(t, workspace, session).OpenCount)
}

func TestNoClosesItemsAndTheNextTurnSaysSo(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, decision("Lists"), offer("add a screen"))

	require.NoError(t, f.run(t, workspace, "agents.open-no", map[string]any{"session": session}, nil))

	assert.Equal(t, "done", f.session(t, workspace, session).Status)
	assert.Equal(t, 0, f.session(t, workspace, session).OpenCount)
	assert.Equal(t, "done", f.session(t, workspace, session).Group)
	assert.Len(t, f.calls(t, "session/prompt"), 1)

	f.prompt(t, workspace, session, "go on")
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "None of these: Lists; add a screen" })
	f.waitStatus(t, workspace, session, "done")
	f.prompt(t, workspace, session, "again")
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "again" })
}

func TestLaterKeepsItemsOpenAndFoldsTheCard(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, offer("add a screen"))

	require.NoError(t, f.run(t, workspace, "agents.open-later", map[string]any{"session": session}, nil))

	assert.True(t, f.openEntry(t, workspace, session).Later)
	assert.Equal(t, 1, f.session(t, workspace, session).OpenCount)
}

func TestBacklogFilesTheItemsNamed(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript, backlogDir(t))
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, decision("Lists"), offer("add a screen"))
	var out struct {
		Tasks []int `json:"tasks"`
	}

	require.NoError(t, f.run(t, workspace, "agents.open-backlog", map[string]any{"session": session, "items": []string{"oi-2"}}, &out))

	require.Len(t, out.Tasks, 1)
	items := f.openEntry(t, workspace, session).Items
	assert.Equal(t, "open", items[0].State)
	assert.Equal(t, "backlog", items[1].State)
	assert.Equal(t, out.Tasks[0], items[1].Task)
	assert.Equal(t, 1, f.session(t, workspace, session).OpenCount)
}

func TestQueueListsOpenItemsOldestFirst(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	first := f.doneSession(t, workspace)
	second := f.doneSession(t, workspace)
	f.doneSession(t, workspace)
	f.postItems(t, workspace, second, offer("older"))
	f.postItems(t, workspace, first, offer("newer"), offer("answered"))
	require.NoError(t, f.run(t, workspace, "agents.open-no", map[string]any{"session": first, "items": []string{"oi-2"}}, nil))
	var queue []struct {
		Session  string     `json:"session"`
		Title    string     `json:"title"`
		Worktree string     `json:"worktree"`
		Items    []openItem `json:"items"`
	}

	require.NoError(t, f.run(t, workspace, "agents.open-queue", map[string]any{}, &queue))

	require.Len(t, queue, 2)
	assert.Equal(t, second, queue[0].Session)
	assert.Equal(t, first, queue[1].Session)
	assert.Equal(t, f.session(t, workspace, first).Worktree, queue[1].Worktree)
	require.Len(t, queue[1].Items, 1)
	assert.Equal(t, "newer", queue[1].Items[0].Label)
}

func TestNumbersFollowTheOpenSetAcrossTurns(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, decision("First"))
	f.prompt(t, workspace, session, "again")
	testutil.Eventually(t, func() bool { return len(f.transcript(t, workspace, session).Turns) == 2 }, wait)
	f.waitStatus(t, workspace, session, "done")
	f.postItems(t, workspace, session, decision("Second"), offer("one"), offer("two"))
	numbers := func(entries []entry) [][]int {
		var out [][]int
		for _, en := range entries {
			if en.Kind == "openItems" {
				var list []int
				for _, it := range en.Items {
					list = append(list, it.Number)
				}
				out = append(out, list)
			}
		}
		return out
	}
	var view struct {
		Cursor  string  `json:"cursor"`
		Entries []entry `json:"entries"`
	}
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session}, &view))
	assert.Equal(t, [][]int{{1}, {2, 3, 3}}, numbers(view.Entries))
	var queue []struct {
		Items []openItem `json:"items"`
	}
	require.NoError(t, f.run(t, workspace, "agents.open-queue", map[string]any{}, &queue))
	require.Len(t, queue, 1)
	require.Len(t, queue[0].Items, 4)
	assert.Equal(t, 3, queue[0].Items[3].Number)

	require.NoError(t, f.run(t, workspace, "agents.open-no", map[string]any{"session": session, "items": []string{"oi-1"}}, nil))

	var delta struct {
		Entries []entry `json:"entries"`
	}
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session, "since": view.Cursor}, &delta))
	assert.Equal(t, [][]int{{0}, {1, 2, 2}}, numbers(delta.Entries))
}

func TestConvertPromptsTheSession(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)

	require.NoError(t, f.run(t, workspace, "agents.open-convert", map[string]any{"session": session}, nil))

	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(promptText(t, c), "agents_open_items") })
}

func TestItemsAgeIntoTheBacklogWithALine(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_OPEN_AFTER=200ms"}, echoScript, backlogDir(t))
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)

	f.postItems(t, workspace, session, decision("Lists"), offer("add a screen"))

	testutil.Eventually(t, func() bool { return f.session(t, workspace, session).OpenCount == 0 }, wait)
	assert.Equal(t, "done", f.session(t, workspace, session).Group)
	items := f.openEntry(t, workspace, session).Items
	assert.Equal(t, "backlog", items[0].State)
	assert.NotZero(t, items[0].Task)
	testutil.Eventually(t, func() bool { return strings.Contains(f.logText(t, session), "2 open items moved to the backlog") }, wait)
}

func TestClosingASessionMovesItsItemsToTheBacklog(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript, backlogDir(t))
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, offer("add a screen"))

	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": session}, nil))

	testutil.Eventually(t, func() bool { return f.session(t, workspace, session).OpenCount == 0 }, wait)
	task := f.openEntry(t, workspace, session).Items[0].Task
	assert.Contains(t, f.taskText(t, workspace, task), "add a screen")
}

func TestOpenItemsHoldOffAutoClose(t *testing.T) {
	t.Parallel()
	const after = 300 * time.Millisecond
	f := startEnv(t, []string{"CLUBMATE_AGENTS_CLOSE_AFTER=" + after.String()}, echoScript,
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.postItems(t, workspace, session, offer("add a screen"))
	f.prompt(t, workspace, session, "done")
	f.waitStatus(t, workspace, session, "done")

	time.Sleep(3 * after)
	assert.Equal(t, "done", f.session(t, workspace, session).Status)

	require.NoError(t, f.run(t, workspace, "agents.open-no", map[string]any{"session": session}, nil))
	f.waitStatus(t, workspace, session, "closed")
}

func TestOfferOfAClosedSessionStartsANewSessionInItsWorktree(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, offer("add a screen"))
	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": session}, nil))
	var out struct {
		Session string `json:"session"`
	}

	require.NoError(t, f.run(t, workspace, "agents.open-answer", map[string]any{"session": session, "offers": []string{"oi-1"}}, &out))

	require.NotEmpty(t, out.Session)
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "do: add a screen" })
	assert.Equal(t, f.session(t, workspace, session).Worktree, f.session(t, workspace, out.Session).Worktree)
	assert.Equal(t, "answered", f.openEntry(t, workspace, session).Items[0].State)
}

func TestChildItemsGoToItsParent(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	parent := f.doneSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "one"}).Session
	f.waitStatus(t, workspace, kid, "done")
	var out openPosted

	require.NoError(t, f.runAs(t, workspace, kid, "agents.open-items", map[string]any{"items": []map[string]any{decision("Lists"), offer("add a screen")}}, &out))

	assert.True(t, out.Handed)
	f.waitCall(t, "session/prompt", func(c fakeCall) bool {
		text := promptText(t, c)
		return strings.Contains(text, "left open items:") && strings.Contains(text, kid) && strings.Contains(text, "Which Lists?") && strings.Contains(text, "add a screen")
	})
	assert.Equal(t, 0, f.session(t, workspace, kid).OpenCount)
	assert.Equal(t, "handed", f.openEntry(t, workspace, kid).Items[0].State)
}

func TestMarkersShowInTheAgentMessage(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "Shipped. About Lists: your pick. Then rest.")
	f.waitStatus(t, workspace, session, "done")
	var before struct {
		Cursor string `json:"cursor"`
	}
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session}, &before))

	f.postItems(t, workspace, session, map[string]any{"kind": "offer", "label": "x", "quote": "About Lists"})

	var delta struct {
		Entries []entry `json:"entries"`
	}
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session, "since": before.Cursor}, &delta))
	raw, err := json.Marshal(delta.Entries)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "About Lists: your pick. ① Then rest.")
}

func TestLastParagraphThatMayAskIsFlagged(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "Shipped.\n\nIf you want, I can also add a screen.")
	f.waitStatus(t, workspace, session, "done")
	assert.True(t, f.session(t, workspace, session).MayAsk)

	f.prompt(t, workspace, session, "Shipped.")
	testutil.Eventually(t, func() bool {
		rec := f.session(t, workspace, session)
		return rec.Status == "done" && !rec.MayAsk
	}, wait)
}

func (f *fixture) openCardSlot(t *testing.T, workspace, session string) {
	t.Helper()
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(nodesWithID(n, session+"/open/keys")) == 1 })
}

func nodesWithID(n sdui.Node, id string) []sdui.Node {
	var out []sdui.Node
	if n.ID == id {
		out = append(out, n)
	}
	for _, c := range n.Children {
		out = append(out, nodesWithID(c, id)...)
	}
	return out
}

func (f *fixture) waitPrompt(t *testing.T, text string) {
	t.Helper()
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == text })
}

func TestCardKeysPickAndSendOneTurn(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, decision("Lists"), offer("add a screen"))
	f.openCardSlot(t, workspace, session)

	keys := session + "/open/keys"
	f.h.Event(t, workspace, "agents.main", keys, "openKey", "2")
	f.h.Event(t, workspace, "agents.main", keys, "openKey", "j")
	f.h.Event(t, workspace, "agents.main", keys, "openKey", "x")
	f.h.Event(t, workspace, "agents.main", keys, "openKey", "cmd+return")

	f.waitPrompt(t, "Answers: Lists → Two; do: add a screen")
	assert.Zero(t, f.session(t, workspace, session).OpenCount)
}

func TestCardOffersNoToAllWithoutAMenu(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, offer("add a screen"))

	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return len(nodesWithID(n, session+"/open/no")) == 1 && len(nodesWithID(n, session+"/open/more")) == 0
	})
	f.h.Event(t, workspace, "agents.main", session+"/open/no", "openNoAll", nil)

	testutil.Eventually(t, func() bool { return f.session(t, workspace, session).OpenCount == 0 }, wait)
	assert.Len(t, f.calls(t, "session/prompt"), 1)
}

func TestComposerSendCarriesTheCardsPicks(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, decision("Lists"), decision("Grouping"))
	f.openCardSlot(t, workspace, session)

	f.h.Event(t, workspace, "agents.main", session+"/open", "openDraft", map[string]any{session + "/open/oi-1": "One"})
	f.h.Event(t, workspace, "agents.main", session+"/composer/0", "send", map[string]any{"text": "and hurry"})

	f.waitPrompt(t, "Answers: Lists → One\n\nand hurry")
	assert.Equal(t, 1, f.session(t, workspace, session).OpenCount)
}

func TestQueueSendsAnswersPerSession(t *testing.T) {
	t.Parallel()
	f := start(t, echoScript)
	workspace := f.workspace(t, newRepo(t))
	first := f.doneSession(t, workspace)
	second := f.doneSession(t, workspace)
	f.postItems(t, workspace, first, decision("Lists"))
	f.postItems(t, workspace, second, decision("Grouping"), offer("add a screen"))

	f.h.Event(t, workspace, "agents.sidebar", "filter", "filter", "open")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(nodesWithID(n, "openQueue")) == 1 })
	f.h.Event(t, workspace, "agents.main", "openQueue", "openKey", "1")
	f.h.Event(t, workspace, "agents.main", "openQueue", "openKey", "j")
	f.h.Event(t, workspace, "agents.main", "openQueue", "openKey", "2")
	f.h.Event(t, workspace, "agents.main", "openQueue", "openKey", "cmd+return")

	f.waitPrompt(t, "Answers: Lists → One")
	f.waitPrompt(t, "Answers: Grouping → Two")
	assert.Equal(t, 1, f.session(t, workspace, second).OpenCount)
}
