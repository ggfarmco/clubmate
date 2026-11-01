package main

import (
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/agents/api"
)

func nodesOf(n sdui.Node, typ string) []sdui.Node {
	var out []sdui.Node
	if n.Type == typ {
		out = append(out, n)
	}
	for _, c := range n.Children {
		out = append(out, nodesOf(c, typ)...)
	}
	return out
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

func TestSidebarGroupsByWorktree(t *testing.T) {
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Title: "Refunds", Status: statusWaiting}, Activity: "go test"},
		{Rec: record{ID: "agt_2", Worktree: "/r/b", Agent: agentClaude, Title: "Auth", Status: statusDone, UpdatedAt: now.Add(-12 * time.Minute)}, Notify: notifyMuted, Branch: "feat/auth"},
		{Rec: record{ID: "agt_3", Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, UpdatedAt: now}},
		{Rec: record{ID: "agt_4", Worktree: "/r/b", Agent: agentClaude, Title: "Docs", Status: statusDone, Asks: true, UpdatedAt: now}},
	}
	tree := sidebarView(rows, "agt_2", nil, now, false, "")
	require.NoError(t, internalsdui.Validate(tree))

	groups := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, groups, 2)
	assert.Equal(t, []string{"a", "feat/auth"}, []string{groups[0].Label, groups[1].Label})
	assert.Empty(t, groups[1].Detail)
	assert.Equal(t, []string{"middle", "middle"}, []string{groups[0].Truncate, groups[1].Truncate})
	a, b := groups[0].Children, groups[1].Children
	require.Len(t, a, 2)
	require.Len(t, b, 2)
	assert.Equal(t, []string{"agt_1", "agt_2", "agt_4"}, []string{a[0].ID, b[0].ID, b[1].ID})
	assert.Equal(t, "Closed · 1", a[1].Label)
	assert.False(t, a[1].Expanded)
	assert.Empty(t, a[1].Children)
	assert.Equal(t, "Waiting for permission · go test", a[0].Detail)
	assert.Equal(t, "Muted · 12 min ago", b[0].Detail)
	assert.Equal(t, []string{"warning", ""}, []string{a[0].Tone, b[0].Tone})
	assert.Equal(t, []string{actNotifyNeeds, actNotifyMuted, actClose, actDelete}, menuActions(a[0].Menu))
}

func menuActions(menu []sdui.MenuEntry) []string {
	out := make([]string, len(menu))
	for i, m := range menu {
		out[i] = m.Action
	}
	return out
}

func TestSidebarExpandedClosedGroup(t *testing.T) {
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusDone, UpdatedAt: now}},
		{Rec: record{ID: "agt_2", Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, UpdatedAt: now}},
	}
	tree := sidebarView(rows, "", map[string]bool{closedKey + "/r/a": false}, now, false, "")
	require.NoError(t, internalsdui.Validate(tree))

	groups := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, groups, 1)
	closed := groups[0].Children[1]
	assert.True(t, closed.Expanded)
	require.Len(t, closed.Children, 1)
	assert.Equal(t, "agt_2", closed.Children[0].ID)
	assert.Equal(t, []string{actHandoff, actNotifyNeeds, actNotifyMuted, actDelete}, menuActions(closed.Children[0].Menu))
}

func TestSidebarFilter(t *testing.T) {
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Title: "Refunds", Status: statusWaiting}},
		{Rec: record{ID: "agt_2", Worktree: "/r/b", Agent: agentClaude, Title: "Auth", Status: statusRunning}},
		{Rec: record{ID: "agt_3", Worktree: "/r/b", Agent: agentClaude, Status: statusClosed, UpdatedAt: now}},
		{Rec: record{ID: "agt_4", Worktree: "/r/b", Agent: agentClaude, Title: "Docs", Status: statusDone, Asks: true, UpdatedAt: now}},
		{Rec: record{ID: "agt_5", Worktree: "/r/c", Agent: agentClaude, Title: "Orchestrator", Status: statusDone}, Children: statusWaiting},
		{Rec: record{ID: "agt_6", Worktree: "/r/c", Agent: agentClaude, lineage: lineage{ParentSession: "agt_gone"}, Title: "Child", Status: statusDone, Asks: true}},
	}
	tabs := func(tree sdui.Node) []sdui.Item { return nodesOf(tree, "tabs")[0].Props["items"].([]sdui.Item) }
	ids := func(tree sdui.Node) []string {
		var out []string
		for _, group := range nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem) {
			for _, item := range group.Children {
				out = append(out, item.ID)
			}
		}
		return out
	}

	needs := sidebarView(rows, "", nil, now, false, filterNeeds)
	require.NoError(t, internalsdui.Validate(needs))
	items := tabs(needs)
	assert.Equal(t, []string{"5", "3", "1"}, []string{items[0].Badge, items[1].Badge, items[2].Badge})
	assert.Equal(t, []string{"", "warning", ""}, []string{items[0].Tone, items[1].Tone, items[2].Tone})
	assert.Equal(t, filterNeeds, nodesOf(needs, "tabs")[0].Props["selected"])
	assert.Equal(t, []string{"agt_1", "agt_4", "agt_5"}, ids(needs))
	assert.Equal(t, []string{"agt_2"}, ids(sidebarView(rows, "", nil, now, false, filterRunning)))

	calm := sidebarView(rows[1:3], "", nil, now, false, filterNeeds)
	assert.Empty(t, tabs(calm)[1].Tone)
	assert.Empty(t, nodesOf(calm, "tree"))
	require.NoError(t, internalsdui.Validate(calm))
	assert.Equal(t, noneNeedYou, nodesOf(calm, "empty")[0].Props["text"])
	assert.Equal(t, actFilterAll, nodesOf(calm, "button")[2].Props["action"])
}

func TestDoneAsksWhenLastAgentMessageIsQuestion(t *testing.T) {
	for _, tc := range []struct {
		texts []string
		want  string
	}{
		{[]string{"Делаем?"}, "Делаем?"},
		{[]string{"Делаем?**\n\n"}, "Делаем?"},
		{[]string{"Tests pass. Ship it? Or wait for review?"}, "Or wait for review?"},
		{[]string{"Plan:\n- **Add retries to the charge step?**"}, "Add retries to the charge step?"},
		{[]string{"Готово."}, ""},
		{[]string{"Делаем?", "Сделал."}, ""},
		{nil, ""},
	} {
		s := &session{}
		for _, text := range tc.texts {
			s.entries = append(s.entries, &entry{Kind: entryAgent, Text: text}, &entry{Kind: entryTool})
		}
		s.setStatusLocked(statusDone, "")
		assert.Equal(t, tc.want, s.rec.Question, tc.texts)
		assert.Equal(t, tc.want != "", s.rec.Asks, tc.texts)
		s.setStatusLocked(statusIdle, "")
		assert.False(t, s.rec.Asks)
		assert.Empty(t, s.rec.Question)
	}
}

func TestPermissionSummaryPrefersCommand(t *testing.T) {
	at := time.Date(2026, 10, 31, 9, 41, 0, 0, time.UTC)
	for _, tc := range []struct {
		call string
		want pendingSummary
	}{
		{`{"title":"Run tests","kind":"execute","rawInput":{"command":"go test ./...\necho done"}}`, pendingSummary{Kind: "permission", Text: "go test ./...", Tool: "execute", At: at}},
		{`{"title":"Edit cart.go","kind":"edit","rawInput":{"file_path":"cart.go"}}`, pendingSummary{Kind: "permission", Text: "Edit cart.go", Tool: "edit", At: at}},
	} {
		s := &session{perms: map[string]*pendingPermission{"permission-2": {at: at}}}
		s.entries = []*entry{{Kind: entryPermission, ID: "permission-1", ToolCall: json.RawMessage(`{"title":"Answered"}`)}, {Kind: entryPermission, ID: "permission-2", ToolCall: json.RawMessage(tc.call)}}
		assert.Equal(t, &tc.want, s.permissionSummary())
	}
}

func TestSidebarNestsChildren(t *testing.T) {
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_c", Worktree: "/r/b", lineage: lineage{ParentSession: "agt_p"}, Agent: agentClaude, Status: statusDone, Asks: true, UpdatedAt: now}},
		{Rec: record{ID: "agt_d", Worktree: "/r/a", lineage: lineage{ParentSession: "agt_p"}, Agent: agentClaude, Status: statusClosed, UpdatedAt: now}},
		{Rec: record{ID: "agt_p", Worktree: "/r/a", Agent: agentClaude, Status: statusDone, UpdatedAt: now}},
	}

	folded := nodesOf(sidebarView(rows, "", map[string]bool{}, now, false, ""), "tree")[0].Props["items"].([]sdui.TreeItem)[0].Children[0]
	assert.True(t, folded.HasChildren)
	assert.False(t, folded.Expanded)
	assert.Empty(t, folded.Children)

	tree := sidebarView(rows, "agt_c", map[string]bool{"s:agt_p": false}, now, false, "")
	require.NoError(t, internalsdui.Validate(tree))

	groups := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Children, 1)
	parent := groups[0].Children[0]
	assert.True(t, parent.Expanded)
	assert.Equal(t, "a", groups[0].Label)
	require.Len(t, parent.Children, 2)
	assert.Equal(t, []string{"agt_c", "agt_d"}, []string{parent.Children[0].ID, parent.Children[1].ID})
}

func TestSidebarNestsGraphRunSessions(t *testing.T) {
	now := time.Now()
	run := &api.Run{ID: "gr_1", Title: "Review loop"}
	rows := []sessionRow{
		{Rec: record{ID: "agt_r", Worktree: "/r/a", Run: run, Title: "Review loop · Reviewer", Status: statusIdle, CreatedAt: now, UpdatedAt: now}},
		{Rec: record{ID: "agt_x", Worktree: "/r/a", Title: "Solo", Status: statusDone, UpdatedAt: now}},
		{Rec: record{ID: "agt_c", Worktree: "/r/a", Run: run, Title: "Review loop · Coder", Status: statusWaiting, CreatedAt: now.Add(-time.Second)}},
		{Rec: record{ID: "agt_k", Worktree: "/r/a", lineage: lineage{ParentSession: "agt_p"}, Run: &api.Run{ID: "gr_2", Title: "Fix"}, Title: "Fix · Tester", Status: statusRunning}},
		{Rec: record{ID: "agt_p", Worktree: "/r/a", Title: "Boss", Status: statusDone, UpdatedAt: now}},
	}
	tree := sidebarView(rows, "", map[string]bool{"s:agt_p": false}, now, false, "")
	require.NoError(t, internalsdui.Validate(tree))

	groups := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	wt := groups[len(groups)-1].Children
	require.Len(t, wt, 3)
	loop := wt[0]
	assert.Equal(t, groupPrefix+runGroupKey("/r/a", "gr_1"), loop.ID)
	assert.Equal(t, []string{"Review loop", "Coder needs you", graphIcon}, []string{loop.Label, loop.Detail, loop.Icon})
	assert.True(t, loop.Expanded)
	require.Len(t, loop.Children, 2)
	assert.Equal(t, []string{"Coder", "Reviewer"}, []string{loop.Children[0].Label, loop.Children[1].Label})
	assert.Equal(t, []string{"agt_x", "agt_p"}, []string{wt[1].ID, wt[2].ID})

	fix := wt[2].Children
	require.Len(t, fix, 1)
	assert.Equal(t, groupPrefix+runGroupKey("agt_p", "gr_2"), fix[0].ID)
	assert.Equal(t, statusLabels[statusRunning], fix[0].Detail)
	assert.Equal(t, "Tester", fix[0].Children[0].Label)

	folded := nodesOf(sidebarView(rows, "", map[string]bool{runGroupKey("/r/a", "gr_1"): true}, now, false, ""), "tree")[0].Props["items"].([]sdui.TreeItem)
	assert.Empty(t, folded[len(folded)-1].Children[0].Children)
}

func TestRunningActivityElapsed(t *testing.T) {
	now := time.Now()
	row := sessionRow{Rec: record{ID: "agt_1", Status: statusRunning}, Activity: "go test", Started: now.Add(-4 * time.Minute)}

	assert.Equal(t, "4 min · go test", activity(row, now))
	assert.Equal(t, "<1 min · go test", activity(sessionRow{Rec: row.Rec, Activity: row.Activity, Started: now.Add(-10 * time.Second)}, now))
	assert.Equal(t, "2 h 5 min", activity(sessionRow{Rec: row.Rec, Started: now.Add(-125 * time.Minute)}, now))
}

func TestTickerRefreshesElapsedWhileRunning(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	row := sessionRow{Rec: record{ID: "agt_1", Status: statusRunning}, Started: clock.Now()}
	var details []string
	var tick *ticker
	tick = newTicker(clock, tickEvery, func() {
		details = append(details, activity(row, clock.Now()))
		tick.arm()
	})

	tick.arm()
	clock.advance(tickEvery)
	clock.advance(tickEvery)
	tick.stop()
	clock.advance(tickEvery)
	tick.fire()

	assert.Equal(t, []string{"<1 min", "1 min"}, details)
}

func TestChildHeaderAndAgentMessage(t *testing.T) {
	v := transcriptView{Session: record{ID: "agt_c", Worktree: "/r", Agent: agentClaude, Status: statusDone, lineage: lineage{ParentSession: "agt_p"}}}
	v.Entries = []*entry{{Kind: entryUser, ID: "prompt-1", Turn: 1, Text: "do it", From: "agt_p"}, {Kind: entryUser, ID: "prompt-2", Turn: 2, Text: "again", From: unknownCaller}}

	tree := mainView(screen{View: v, Parent: "Orchestrator", FromTitles: map[string]string{"agt_p": "Orchestrator"}, Composer: "agt_c/composer/0"})
	require.NoError(t, internalsdui.Validate(tree))

	var link []sdui.Node
	for _, b := range nodesOf(tree, "button") {
		if b.Props["action"] == actSelectParent {
			link = append(link, b)
		}
	}
	require.Len(t, link, 1)
	assert.Equal(t, "from Orchestrator", link[0].Props["label"])
	msgs := nodesOf(tree, "message")
	require.Len(t, msgs, 2)
	assert.Equal(t, "Orchestrator", msgs[0].Props["from"])
	assert.Equal(t, "another agent", msgs[1].Props["from"])
}

func TestTranscriptWindowAndShowEarlier(t *testing.T) {
	v := transcriptView{Session: record{ID: "agt_1", Worktree: "/r", Agent: agentClaude, Status: statusDone}}
	for turn := 1; turn <= 45; turn++ {
		n := strconv.Itoa(turn)
		v.Turns = append(v.Turns, turnInfo{Turn: turn, CheckpointSha: "sha" + n, StopReason: "end_turn"})
		v.Entries = append(v.Entries, &entry{Kind: entryUser, ID: "prompt-" + n, Turn: turn, Text: "p" + n}, &entry{Kind: entryAgent, ID: "m" + n, Turn: turn, Text: "a" + n})
	}
	firstPrompt := func(st *sessionUI) (sdui.Node, int) {
		tree := mainView(screen{View: v, Window: st.window, Composer: "agt_1/composer/0"})
		require.NoError(t, internalsdui.Validate(tree))
		var users []sdui.Node
		for _, m := range nodesOf(tree, "message") {
			if m.Props["role"] == "user" {
				users = append(users, m)
			}
		}
		earlier := false
		for _, b := range nodesOf(tree, "button") {
			earlier = earlier || b.Props["action"] == actShowEarlier
		}
		assert.True(t, earlier)
		return users[0], len(users)
	}

	st := &sessionUI{window: windowStep}
	first, shown := firstPrompt(st)
	assert.Equal(t, 20, shown)
	assert.Equal(t, "p26", first.Props["text"])
	assert.Equal(t, "agt_1/msg/user/prompt-26", first.ID)

	st.showEarlier()
	first, shown = firstPrompt(st)
	assert.Equal(t, 40, shown)
	assert.Equal(t, "p6", first.Props["text"])
	shifted := map[string]string{}
	for _, m := range nodesOf(mainView(screen{View: v, Window: st.window, Composer: "agt_1/composer/0"}), "message") {
		shifted[m.Props["text"].(string)] = m.ID
	}
	assert.Equal(t, "agt_1/msg/user/prompt-26", shifted["p26"])
	assert.Equal(t, "agt_1/msg/agent/m26", shifted["a26"])
}

func TestPlanOnly(t *testing.T) {
	for text, want := range map[string]bool{
		"## Plan\n\n1. Read\n2. Edit\n":             true,
		"**Plan:**\n- [ ] Read\n  - nested\n- Edit": true,
		"1) Read\n2) Edit":                          true,
		"Plan\n":                                    true,
		"I'll do this:\n\n1. Read\n2. Edit":         false,
		"## Plan for the fix\n1. Read":              false,
		"1. Read\n\nThen I'll run the tests.":       false,
		"- Read\n## Plan":                           false,
	} {
		assert.Equal(t, want, planOnly(text), text)
	}
}

func TestPlanOnlyMessageFoldsIntoFollowingPlan(t *testing.T) {
	plan := rawJSON(t, map[string]any{"sessionUpdate": "plan", "entries": []map[string]string{{"content": "Read", "status": "completed"}, {"content": "Edit", "status": "pending"}}})
	v := transcriptView{Session: record{ID: "agt_1", Worktree: "/r", Agent: agentClaude, Status: statusDone}}
	v.Entries = []*entry{
		{Kind: entryUser, ID: "prompt-1", Turn: 1, Text: "go"},
		{Kind: entryAgent, ID: "m1", Turn: 1, Text: "## Plan\n1. Read\n2. Edit"},
		{Kind: entryPlan, ID: "plan-1", Turn: 1, Plan: plan},
		{Kind: entryAgent, ID: "m2", Turn: 1, Text: "1. Read\n2. Edit"},
		{Kind: entryUser, ID: "prompt-2", Turn: 2, Text: "again"},
		{Kind: entryAgent, ID: "m3", Turn: 2, Text: "1. Read\n2. Edit"},
		{Kind: entryThought, ID: "t3", Turn: 2, Text: "hm"},
		{Kind: entryPlan, ID: "plan-2", Turn: 2, Plan: plan},
	}
	tree := mainView(screen{View: v, Composer: "agt_1/composer/0"})
	require.NoError(t, internalsdui.Validate(tree))

	var agent []string
	for _, m := range nodesOf(tree, "message") {
		if m.Props["role"] == entryAgent {
			agent = append(agent, m.ID)
		}
	}
	assert.Equal(t, []string{"agt_1/msg/agent/m2", "agt_1/msg/agent/m3"}, agent)
	assert.Len(t, nodesOf(tree, "plan"), 2)
}

func TestInstallingNotice(t *testing.T) {
	v := transcriptView{Session: record{ID: "agt_1", Worktree: "/r", Agent: agentClaude, Status: statusIdle}}
	tree := mainView(screen{View: v, Installing: true, Composer: "agt_1/composer/0"})
	require.NoError(t, internalsdui.Validate(tree))

	var texts []string
	for _, n := range nodesOf(tree, "text") {
		texts = append(texts, n.Props["text"].(string))
	}
	assert.Contains(t, texts, "Installing Claude adapter…")
}

func TestHugeToolOutputRendersTruncated(t *testing.T) {
	output := strings.Repeat(strings.Repeat("x", 99)+"\n", 50_000)
	tool := map[string]json.RawMessage{
		"title":    rawJSON(t, "go test ./..."),
		"kind":     rawJSON(t, "execute"),
		"status":   rawJSON(t, "completed"),
		"rawInput": rawJSON(t, map[string]string{"command": "go test ./..."}),
		"content":  rawJSON(t, []map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": output}}}),
	}
	v := transcriptView{
		Session: record{ID: "agt_1", Worktree: "/r", Agent: agentClaude, Status: statusDone},
		Entries: []*entry{{Kind: entryTool, ID: "call-1", Turn: 1, Tool: tool}},
	}
	card := func(expanded map[string]bool) sdui.Node {
		tree := mainView(screen{View: v, Expanded: expanded, Composer: "agt_1/composer/0"})
		require.NoError(t, internalsdui.Validate(tree))
		cards := nodesOf(tree, "toolCard")
		require.Len(t, cards, 1)
		return cards[0]
	}

	c := card(nil)
	body := c.Props["body"].(sdui.ToolBody).Text
	assert.Equal(t, "terminal", c.Props["body"].(sdui.ToolBody).Format)
	assert.True(t, strings.HasPrefix(body, "$ go test ./...\n"))
	assert.LessOrEqual(t, len(body), bodyBytes)
	assert.LessOrEqual(t, strings.Count(body, "\n")+1, bodyLines)
	assert.Equal(t, true, c.Props["truncated"])
	actions := c.Props["actions"].([]sdui.CardAction)
	assert.Equal(t, actShowAll, actions[len(actions)-1].Action)

	full := card(map[string]bool{"call-1": true}).Props["body"].(sdui.ToolBody).Text
	assert.Greater(t, len(full), bodyBytes)
	assert.LessOrEqual(t, len(full), expandedBytes)
}

func TestOpenOutputOnlyWhenBodyWouldScroll(t *testing.T) {
	execute := func(lines int) map[string]json.RawMessage {
		return map[string]json.RawMessage{
			"kind":    rawJSON(t, "execute"),
			"status":  rawJSON(t, "completed"),
			"content": rawJSON(t, []map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": strings.Repeat("line\n", lines)}}}),
		}
	}
	v := transcriptView{
		Session: record{ID: "agt_1", Worktree: "/r", Agent: agentClaude, Status: statusDone},
		Entries: []*entry{{Kind: entryTool, ID: "short", Turn: 1, Tool: execute(scrollLines)}, {Kind: entryTool, ID: "long", Turn: 1, Tool: execute(scrollLines + 1)}},
	}
	cards := nodesOf(mainView(screen{View: v, Composer: "agt_1/composer/0"}), "toolCard")
	require.Len(t, cards, 2)
	opens := func(c sdui.Node) bool {
		actions, _ := c.Props["actions"].([]sdui.CardAction)
		return slices.ContainsFunc(actions, func(a sdui.CardAction) bool { return a.Action == actOpenOutput })
	}
	assert.False(t, opens(cards[0]))
	assert.True(t, opens(cards[1]))
}

func TestToolPathsRelativeToWorktree(t *testing.T) {
	tool := map[string]json.RawMessage{
		"title":   rawJSON(t, "Edit `/r/a/pkg/x.go`"),
		"kind":    rawJSON(t, "edit"),
		"status":  rawJSON(t, "completed"),
		"content": rawJSON(t, []map[string]any{{"type": "diff", "path": "/r/a/pkg/x.go", "oldText": "one\n", "newText": "two\n"}}),
	}
	v := transcriptView{
		Session: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusWaiting},
		Entries: []*entry{
			{Kind: entryTool, ID: "call-1", Turn: 1, Tool: tool},
			{Kind: entryPermission, ID: "perm-1", Turn: 1, ToolCall: rawJSON(t, map[string]string{"title": "Edit `/r/a/pkg/x.go`", "kind": "edit"})},
		},
	}
	tree := mainView(screen{View: v, Composer: "agt_1/composer/0"})
	require.NoError(t, internalsdui.Validate(tree))

	card := nodesOf(tree, "toolCard")[0]
	assert.Equal(t, "Edit `pkg/x.go`", card.Props["title"])
	assert.True(t, strings.HasPrefix(card.Props["body"].(sdui.ToolBody).Text, "--- a/pkg/x.go\n+++ b/pkg/x.go\n"))
	assert.Equal(t, "Claude wants to edit `pkg/x.go`", nodesOf(tree, "permissionCard")[0].Props["text"])
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at   time.Time
	f    func()
	dead bool
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	tm := &fakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, tm)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		live := !tm.dead
		tm.dead = true
		return live
	}
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	rest := c.timers[:0]
	for _, tm := range c.timers {
		switch {
		case tm.dead:
		case !tm.at.After(c.now):
			tm.dead = true
			due = append(due, tm)
		default:
			rest = append(rest, tm)
		}
	}
	c.timers = rest
	c.mu.Unlock()
	for _, tm := range due {
		tm.f()
	}
}

func TestRenderThrottle(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	var renders, lastRender int
	th := newThrottle(clock, renderEvery, func() {
		renders++
		lastRender = renders
	})
	for range 200 {
		th.kick()
		clock.advance(5 * time.Millisecond)
	}
	assert.LessOrEqual(t, renders, 12)

	before := renders
	th.kick()
	clock.advance(renderEvery)
	assert.Equal(t, before+1, lastRender)
}

func TestConfigPickers(t *testing.T) {
	var configs []acp.SessionConfigOption
	require.NoError(t, json.Unmarshal([]byte(`[
		{"id":"mode","name":"Mode","category":"mode","type":"select","currentValue":"plan","options":[{"value":"plan","name":"Plan"}]},
		{"id":"model","name":"Model","category":"model","type":"select","currentValue":"opus","options":[{"group":"claude","name":"Claude","options":[{"value":"sonnet","name":"Sonnet 5"},{"value":"opus","name":"Opus 5.5"}]}]},
		{"id":"effort","name":"Effort","category":"thought_level","type":"select","currentValue":"high","options":[{"value":"high","name":"High"}]}
	]`), &configs))
	v := transcriptView{Session: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusIdle}, Configs: configs}
	tree := mainView(screen{View: v, Composer: "agt_1/composer/0", Advisor: "opus", Branch: "feat/a"})
	require.NoError(t, internalsdui.Validate(tree))

	got := nodesOf(tree, "composer")[0].Props["pickers"].([]sdui.Picker)
	require.Len(t, got, 5)
	assert.Equal(t, sdui.Picker{ID: "model", Help: "Model", Value: "opus", Options: []sdui.Option{{Value: "sonnet", Label: "Sonnet 5"}, {Value: "opus", Label: "Opus 5.5"}}}, got[0])
	assert.Equal(t, []string{"Effort", "Advisor", "Finish", "AFK"}, []string{got[1].Title, got[2].Title, got[3].Title, got[4].Title})
	assert.Equal(t, []string{"opus", "ask", "false"}, []string{got[2].Value, got[3].Value, got[4].Value})

	v.Session.ParentSession = "agt_0"
	child := nodesOf(mainView(screen{View: v, Composer: "agt_1/composer/0", Advisor: "opus"}), "composer")[0].Props["pickers"].([]sdui.Picker)
	assert.Equal(t, "Advisor", child[len(child)-1].Title)
}

func TestComposerClipsCommandDescriptions(t *testing.T) {
	long := strings.Repeat("é", commandRunes+50)
	v := transcriptView{
		Session:  record{ID: "agt_1", Worktree: "/r", Agent: agentClaude, Status: statusIdle},
		Commands: []acp.AvailableCommand{{Name: "short", Description: "Review the diff"}, {Name: "long", Description: long}},
	}
	tree := mainView(screen{View: v, Composer: "agt_1/composer/0"})
	require.NoError(t, internalsdui.Validate(tree))

	got := nodesOf(tree, "composer")[0].Props["commands"].([]sdui.Command)
	assert.Equal(t, "Review the diff", got[0].Description)
	assert.Equal(t, strings.Repeat("é", commandRunes)+"…", got[1].Description)
}

func TestMergeableGates(t *testing.T) {
	ok := record{Worktree: "/r/.worktrees/feat/x", Status: statusDone, Finish: finishAsk}
	assert.True(t, mergeable(ok, true))
	assert.False(t, mergeable(ok, false))
	for _, change := range []func(*record){
		func(r *record) { r.Worktree = "/r" },
		func(r *record) { r.ParentSession = "agt_p" },
		func(r *record) { r.Status = statusRunning },
		func(r *record) { r.Finish = finishMerge },
	} {
		rec := ok
		change(&rec)
		assert.False(t, mergeable(rec, true), "%+v", rec)
	}
}

func TestPromptBlocksImage(t *testing.T) {
	args := promptArgs{Text: "look", Attachments: []attachment{{Kind: "image", MimeType: "image/jpeg", Data: "AAAA"}}}
	assert.Equal(t, []acp.ContentBlock{{Type: "text", Text: "look"}, {Type: "image", Data: "AAAA", MimeType: "image/jpeg"}}, promptBlocks(args, "/r", acp.PromptCapabilities{Image: true}))
	assert.Equal(t, []acp.ContentBlock{{Type: "text", Text: "look"}, {Type: "text", Text: imageOmitted}}, promptBlocks(args, "/r", acp.PromptCapabilities{}))
	args.Text = ""
	assert.Equal(t, []acp.ContentBlock{{Type: "image", Data: "AAAA", MimeType: "image/jpeg"}}, sendable(promptBlocks(args, "/r", acp.PromptCapabilities{Image: true})))
}

func TestDropImageData(t *testing.T) {
	entries := []*entry{{Kind: entryUser, Content: []json.RawMessage{rawJSON(t, acp.ContentBlock{Type: "text", Text: "see"}), rawJSON(t, acp.ContentBlock{Type: "image", Data: "AAAA", MimeType: "image/jpeg"})}}}
	dropImageData(entries)
	assert.JSONEq(t, `{"type":"image","mimeType":"image/jpeg"}`, string(entries[0].Content[1]))
}

func TestChildStatusesAggregateActiveDescendants(t *testing.T) {
	recs := []record{
		{ID: "agt_p", Status: statusDone},
		{ID: "agt_c", Status: statusIdle, lineage: lineage{ParentSession: "agt_p"}},
		{ID: "agt_g", Status: statusRunning, lineage: lineage{ParentSession: "agt_c"}},
		{ID: "agt_w", Status: statusWaiting, lineage: lineage{ParentSession: "agt_p"}},
		{ID: "agt_q", Status: statusDone, Asks: true},
		{ID: "agt_r", Status: statusRunning, lineage: lineage{ParentSession: "agt_q"}},
		{ID: "agt_x", Status: statusError},
		{ID: "agt_y", Status: statusRunning, lineage: lineage{ParentSession: "agt_x"}},
	}

	assert.Equal(t, map[string]string{"agt_p": statusWaiting, "agt_c": statusRunning}, childStatuses(recs))
}

func TestSidebarShowsParentWaitingForChildren(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		children, icon, tone, detail string
		afk                          bool
	}{
		{statusRunning, "circle.dotted", "", "AFK · Waiting for child sessions", true},
		{statusWaiting, "exclamationmark.circle", "warning", "Child session waiting", false},
	} {
		rows := []sessionRow{{Rec: record{ID: "agt_p", Worktree: "/r/a", Agent: agentClaude, Status: statusDone, UpdatedAt: now, AFK: tc.afk}, Children: tc.children}}
		tree := sidebarView(rows, "", nil, now, false, "")
		require.NoError(t, internalsdui.Validate(tree))

		groups := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
		require.Len(t, groups, 1, tc.children)
		item := groups[0].Children[0]
		assert.Equal(t, []string{tc.icon, tc.tone, tc.detail}, []string{item.Icon, item.Tone, item.Detail}, tc.children)
	}
}

func TestHeaderShowsAFKAndWaitingForChildren(t *testing.T) {
	v := transcriptView{Session: record{ID: "agt_p", Worktree: "/r/a", Agent: agentClaude, Status: statusDone, AFK: true}}

	tree := mainView(screen{View: v, Children: statusRunning, Composer: "agt_p/composer/0"})
	require.NoError(t, internalsdui.Validate(tree))

	var texts []string
	for _, n := range nodesOf(tree, "text") {
		texts = append(texts, n.Props["text"].(string))
	}
	assert.Contains(t, texts, "AFK · Waiting for child sessions")
}

func TestPendingPermissionDocksAboveTheComposer(t *testing.T) {
	call := rawJSON(t, map[string]string{"title": "go test", "kind": "execute"})
	options := []acp.PermissionOption{{OptionID: "always", Name: "Always Allow", Kind: "allow_always"}, {OptionID: "allow", Name: "Allow", Kind: "allow_once"}, {OptionID: "deny", Name: "Reject", Kind: "reject_once"}}
	v := transcriptView{
		Session: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusWaiting, Mode: "default"},
		Modes:   []acp.SessionMode{{ID: "default", Name: "Default"}},
		Entries: []*entry{
			{Kind: entryPermission, ID: "perm-1", Turn: 1, ToolCall: call, Outcome: &acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "allow"}},
			{Kind: entryPermission, ID: "perm-stale", Turn: 1, ToolCall: call},
			{Kind: entryPermission, ID: "perm-2", Turn: 2, ToolCall: call, Options: options},
			{Kind: entryPermission, ID: "perm-3", Turn: 2, ToolCall: call, Options: options},
		},
	}
	tree := mainView(screen{View: v, Composer: "agt_1/composer/0", Pending: map[string]bool{"perm-2": true, "perm-3": true}})
	require.NoError(t, internalsdui.Validate(tree))

	inTranscript := nodesOf(nodesOf(tree, "transcript")[0], "permissionCard")
	assert.Len(t, inTranscript, 2)
	docked := tree.Children[len(tree.Children)-2]
	require.Equal(t, "permissionCard", docked.Type)
	assert.Equal(t, "perm-2", docked.Props["request"])
	assert.Len(t, nodesOf(tree, "permissionCard"), 3)
	got := docked.Props["options"].([]sdui.PermissionOption)
	assert.Equal(t, []string{"allow", "always", "deny"}, []string{got[0].ID, got[1].ID, got[2].ID})
	assert.Equal(t, "Ask before edits", nodesOf(tree, "composer")[0].Props["modes"].([]sdui.Option)[0].Label)
}

func TestToolCardFollowsToolUpdates(t *testing.T) {
	s := newSession(record{}, nil, true, nil)
	s.running = true
	status := func() any {
		v := s.view(0)
		require.Len(t, v.Entries, 1)
		return entryNode(screen{View: v}, v.Entries[0], "").node.Props["status"]
	}
	s.applyTool(true, 1, rawJSON(t, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "Read", "kind": "read", "status": "in_progress"}))
	assert.Equal(t, "in_progress", status())

	s.applyTool(false, 1, rawJSON(t, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed"}))
	assert.Equal(t, "completed", status())

	s.applyTool(false, 1, rawJSON(t, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "in_progress"}))
	s.running = false
	assert.Equal(t, "cancelled", status())
}

func TestCodePanelHeaderAndStarters(t *testing.T) {
	rec := record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Title: "Reviewer", Status: statusIdle}
	idle := screen{View: transcriptView{Session: rec}, Composer: "agt_1/composer/0"}
	tree := codeView([]record{rec}, "agt_1", &idle, "/r/a/internal/notify/notify.go")
	require.NoError(t, internalsdui.Validate(tree))

	for _, n := range nodesOf(tree, "text") {
		assert.NotEqual(t, "Reviewer", n.Props["text"])
	}
	row := tree.Children[0].Children[0].Children
	assert.Equal(t, []string{"icon", "select"}, []string{row[0].Type, row[1].Type})
	assert.Equal(t, "circle", row[0].Props["name"])
	assert.Equal(t, true, row[len(row)-1].Props["iconOnly"])
	assert.Len(t, nodesOf(tree, "select"), 1)
	assert.Equal(t, "notify.go", nodesOf(tree, "badge")[0].Props["text"])
	var starts []string
	for _, b := range nodesOf(tree, "button") {
		if b.Props["action"] == actStarter {
			starts = append(starts, b.Props["label"].(string)+" "+b.Props["shortcut"].(string))
		}
	}
	assert.Equal(t, []string{"Explain notify.go opt+1", "Review the last commit opt+2", "Write a test for notify.go opt+3"}, starts)
}

func TestEntryNodeCachedUntilEntryChanges(t *testing.T) {
	s := newSession(record{ID: "agt_1"}, nil, true, nil)
	chunk := func(text string) {
		s.applyUpdate(1, rawJSON(t, map[string]any{"sessionUpdate": acp.UpdateAgentMessageChunk, "content": map[string]string{"type": "text", "text": text}}))
	}
	message := func() sdui.Node {
		v := s.view(0)
		require.Len(t, v.Entries, 1)
		return entryNode(screen{View: v}, v.Entries[0], "").node
	}
	chunk("Hello")
	first := message()
	assert.Equal(t, reflect.ValueOf(first.Props).Pointer(), reflect.ValueOf(message().Props).Pointer())

	chunk(", world")
	assert.Equal(t, "Hello, world", message().Props["text"])
}
