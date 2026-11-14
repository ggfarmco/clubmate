package main

import (
	"cmp"
	"encoding/json"
	"fmt"
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
	"clubmate/plugins/internal/shortcuttest"
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

func texts(n sdui.Node) []string {
	var out []string
	for _, t := range nodesOf(n, "text") {
		out = append(out, t.Props["text"].(string))
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
	t.Parallel()
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Title: "Refunds", Status: statusWaiting}, Activity: "go test", Branch: branchLabel("(detached 03a9ff5f)"), Detached: true},
		{Rec: record{ID: "agt_2", Worktree: "/r/b", Agent: agentClaude, Title: "Auth", Status: statusDone, UpdatedAt: now.Add(-12 * time.Minute)}, Notify: notifyMuted, Branch: "feat/auth"},
		{Rec: record{ID: "agt_3", Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, UpdatedAt: now}},
		{Rec: record{ID: "agt_4", Worktree: "/r/b", Agent: agentClaude, Title: "Docs", Status: statusDone, Asks: true, UpdatedAt: now}},
		{Rec: record{ID: "agt_5", Worktree: "/r/c", lineage: lineage{ParentSession: "agt_gone"}, Agent: agentClaude, Status: statusClosed, UpdatedAt: now}},
	}
	tree := sidebarView(rows, "agt_2", nil, now, "/r/a", false, "", sidebarSearch{}, 0)
	require.NoError(t, internalsdui.Validate(tree))

	groups := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, groups, 3)
	assert.Equal(t, []string{"detached · 03a9ff5f", "feat/auth", "Closed"}, []string{groups[0].Label, groups[1].Label, groups[2].Label})
	assert.Equal(t, "2", groups[2].Detail)
	assert.Equal(t, []bool{true, false}, []bool{groups[0].Faint, groups[1].Faint})
	assert.Empty(t, groups[1].Detail)
	assert.Equal(t, []string{"middle", "middle"}, []string{groups[0].Truncate, groups[1].Truncate})
	a, b := groups[0].Children, groups[1].Children
	require.Len(t, a, 1)
	require.Len(t, b, 2)
	assert.Equal(t, []string{"agt_1", "agt_2", "agt_4"}, []string{a[0].ID, b[0].ID, b[1].ID})
	assert.Equal(t, groupPrefix+closedKey, groups[2].ID)
	assert.False(t, groups[2].Expanded)
	assert.Empty(t, groups[2].Children)
	assert.Equal(t, "waiting", a[0].Detail)
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
	t.Parallel()
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusDone, UpdatedAt: now}},
		{Rec: record{ID: "agt_2", Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, UpdatedAt: now}},
		{Rec: record{ID: "agt_3", Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, ClosedStatus: statusDone, UpdatedAt: now.Add(-time.Minute)}},
		{Rec: record{ID: "agt_4", Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, ClosedStatus: statusError, UpdatedAt: now.Add(-2 * time.Minute)}},
	}
	tree := sidebarView(rows, "", map[string]bool{closedKey: false}, now, "/r/a", false, "", sidebarSearch{}, 0)
	require.NoError(t, internalsdui.Validate(tree))

	items := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, items, 2)
	assert.Equal(t, "agt_1", items[0].ID)
	closed := items[1]
	assert.True(t, closed.Expanded)
	require.Len(t, closed.Children, 3)
	assert.Equal(t, "agt_2", closed.Children[0].ID)
	assert.Equal(t, "just now", closed.Children[0].Detail)
	assert.Equal(t, []string{actHandoff, actNotifyNeeds, actNotifyMuted, actDelete}, menuActions(closed.Children[0].Menu))
	var glyphs [][2]string
	for _, kid := range closed.Children {
		glyphs = append(glyphs, [2]string{kid.Icon, kid.Tone})
	}
	assert.Equal(t, [][2]string{{"archivebox", ""}, {"checkmark.circle", ""}, {"xmark.circle", ""}}, glyphs)
}

func TestSidebarBranchRowWhenTheOnlyWorktreeIsNotTheWindows(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rows := []sessionRow{{Rec: record{ID: "agt_1", Worktree: "/r/b", Agent: agentClaude, Status: statusDone, UpdatedAt: now}, Branch: "feat/b"}}

	items := nodesOf(sidebarView(rows, "", nil, now, "/r/a", false, "", sidebarSearch{}, 0), "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, items, 1)
	assert.Equal(t, "feat/b", items[0].Label)
	assert.Equal(t, "agt_1", items[0].Children[0].ID)
}

func TestSidebarClosedShowsNewestWithWorktreeAndMore(t *testing.T) {
	t.Parallel()
	now := time.Now()
	var rows []sessionRow
	for i, wt := range []string{"/r/a", "/r/b", "/r/a", "/r/c", "/r/a"} {
		rows = append(rows, sessionRow{Rec: record{ID: fmt.Sprintf("agt_%d", i), Worktree: wt, Agent: agentClaude, Status: statusClosed, UpdatedAt: now.Add(time.Duration(i) * time.Minute)}, Branch: map[string]string{"/r/a": "main", "/r/b": "docs/design-brief"}[wt]})
	}
	for i := range 26 {
		rows = append(rows, sessionRow{Rec: record{ID: fmt.Sprintf("agt_old%d", i), Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, UpdatedAt: now.Add(-time.Duration(i+1) * time.Hour)}, Branch: "main"})
	}

	closed := nodesOf(sidebarView(rows, "agt_0", map[string]bool{closedKey: false}, now, "/r/a", false, "", sidebarSearch{}, 0), "tree")[0].Props["items"].([]sdui.TreeItem)[0]
	assert.Equal(t, []string{"Closed", "31"}, []string{closed.Label, closed.Detail})
	var got [][2]string
	var actions []bool
	for _, item := range closed.Children {
		got = append(got, [2]string{item.ID, cmp.Or(item.Detail, item.Label)})
		actions = append(actions, item.Action)
	}
	assert.Equal(t, [][2]string{{"agt_4", "main"}, {"agt_3", "c"}, {"agt_2", "main"}, {"agt_0", "main"}, {moreID, "Show 25 more · 27 left"}}, got)
	assert.True(t, closed.Action)
	assert.Equal(t, []bool{false, false, false, false, true}, actions)
}

func TestSidebarFilter(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Title: "Refunds", Status: statusWaiting}, Since: now.Add(-time.Minute)},
		{Rec: record{ID: "agt_2", Worktree: "/r/b", Agent: agentClaude, Title: "Auth", Status: statusRunning}},
		{Rec: record{ID: "agt_3", Worktree: "/r/b", Agent: agentClaude, Status: statusClosed, UpdatedAt: now}},
		{Rec: record{ID: "agt_4", Worktree: "/r/b", Agent: agentClaude, Title: "Docs", Status: statusDone, Asks: true, UpdatedAt: now.Add(-time.Hour)}},
		{Rec: record{ID: "agt_5", Worktree: "/r/c", Agent: agentClaude, Title: "Orchestrator", Status: statusDone}, Children: statusWaiting},
		{Rec: record{ID: "agt_6", Worktree: "/r/c", Agent: agentClaude, lineage: lineage{ParentSession: "agt_gone"}, Title: "Child", Status: statusDone, Asks: true}},
	}
	tabs := func(tree sdui.Node) []sdui.Item { return nodesOf(tree, "tabs")[0].Props["items"].([]sdui.Item) }
	ids := func(tree sdui.Node) []string {
		var out []string
		for _, item := range nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem) {
			if !strings.HasPrefix(item.ID, groupPrefix) {
				out = append(out, item.ID)
			}
			for _, kid := range item.Children {
				out = append(out, kid.ID)
			}
		}
		return out
	}

	needs := sidebarView(rows, "", nil, now, "/r/a", false, filterNeeds, sidebarSearch{}, 0)
	require.NoError(t, internalsdui.Validate(needs))
	items := tabs(needs)
	assert.Equal(t, []string{"5", "3", "0", "1"}, []string{items[0].Badge, items[1].Badge, items[2].Badge, items[3].Badge})
	assert.Equal(t, []string{"", "warning", "", ""}, []string{items[0].Tone, items[1].Tone, items[2].Tone, items[3].Tone})
	assert.Equal(t, filterNeeds, nodesOf(needs, "tabs")[0].Props["selected"])
	assert.Equal(t, []string{"agt_4", "agt_1", "agt_5"}, ids(needs))
	assert.Equal(t, []string{"agt_2"}, ids(sidebarView(rows, "", nil, now, "/r/a", false, filterRunning, sidebarSearch{}, 0)))

	calm := sidebarView(rows[1:3], "", nil, now, "/r/a", false, filterNeeds, sidebarSearch{}, 0)
	assert.Empty(t, tabs(calm)[1].Tone)
	assert.Empty(t, nodesOf(calm, "tree"))
	require.NoError(t, internalsdui.Validate(calm))
	assert.Equal(t, noneNeedYou, nodesOf(calm, "empty")[0].Props["text"])
	assert.Equal(t, actFilterAll, nodesOf(calm, "button")[2].Props["action"])
}

func TestDoneAsksWhenLastAgentMessageIsQuestion(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_c", Worktree: "/r/b", lineage: lineage{ParentSession: "agt_p"}, Agent: agentClaude, Status: statusDone, Asks: true, UpdatedAt: now}},
		{Rec: record{ID: "agt_d", Worktree: "/r/a", lineage: lineage{ParentSession: "agt_p"}, Agent: agentClaude, Status: statusClosed, UpdatedAt: now}},
		{Rec: record{ID: "agt_p", Worktree: "/r/a", Agent: agentClaude, Status: statusDone, UpdatedAt: now}},
	}

	folded := nodesOf(sidebarView(rows, "", map[string]bool{}, now, "/r/a", false, "", sidebarSearch{}, 0), "tree")[0].Props["items"].([]sdui.TreeItem)[0]
	assert.True(t, folded.HasChildren)
	assert.False(t, folded.Expanded)
	assert.Empty(t, folded.Children)

	tree := sidebarView(rows, "agt_c", map[string]bool{"s:agt_p": false}, now, "/r/a", false, "", sidebarSearch{}, 0)
	require.NoError(t, internalsdui.Validate(tree))

	items := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, items, 1)
	parent := items[0]
	assert.True(t, parent.Expanded)
	require.Len(t, parent.Children, 2)
	assert.Equal(t, []string{"agt_c", "agt_d"}, []string{parent.Children[0].ID, parent.Children[1].ID})
}

func TestSidebarNestsGraphRunSessions(t *testing.T) {
	t.Parallel()
	now := time.Now()
	run := &api.Run{ID: "gr_1", Title: "Review loop"}
	rows := []sessionRow{
		{Rec: record{ID: "agt_r", Worktree: "/r/a", Run: run, Title: "Review loop · Reviewer", Status: statusIdle, CreatedAt: now, UpdatedAt: now}},
		{Rec: record{ID: "agt_x", Worktree: "/r/a", Title: "Solo", Status: statusDone, UpdatedAt: now}},
		{Rec: record{ID: "agt_c", Worktree: "/r/a", Run: run, Title: "Review loop · Coder", Status: statusWaiting, CreatedAt: now.Add(-time.Second)}},
		{Rec: record{ID: "agt_k", Worktree: "/r/a", lineage: lineage{ParentSession: "agt_p"}, Run: &api.Run{ID: "gr_2", Title: "Fix"}, Title: "Fix · Tester", Status: statusRunning}},
		{Rec: record{ID: "agt_p", Worktree: "/r/a", Title: "Boss", Status: statusDone, UpdatedAt: now}},
	}
	tree := sidebarView(rows, "", map[string]bool{"s:agt_p": false}, now, "/r/a", false, "", sidebarSearch{}, 0)
	require.NoError(t, internalsdui.Validate(tree))

	wt := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
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

	folded := nodesOf(sidebarView(rows, "", map[string]bool{runGroupKey("/r/a", "gr_1"): true}, now, "/r/a", false, "", sidebarSearch{}, 0), "tree")[0].Props["items"].([]sdui.TreeItem)
	assert.Empty(t, folded[0].Children)
}

func TestRunningActivityElapsed(t *testing.T) {
	t.Parallel()
	now := time.Now()
	row := sessionRow{Rec: record{ID: "agt_1", Status: statusRunning}, Activity: "go test", Started: now.Add(-4 * time.Minute)}

	assert.Equal(t, "4 min · go test", activity(row, now))
	assert.Equal(t, "<1 min · go test", activity(sessionRow{Rec: row.Rec, Activity: row.Activity, Started: now.Add(-10 * time.Second)}, now))
	assert.Equal(t, "2 h 5 min", activity(sessionRow{Rec: row.Rec, Started: now.Add(-125 * time.Minute)}, now))
}

func TestCommandActivityHidesTheCommand(t *testing.T) {
	t.Parallel()
	tool := func(fields map[string]any) map[string]json.RawMessage {
		out := map[string]json.RawMessage{}
		for k, v := range fields {
			out[k], _ = json.Marshal(v)
		}
		return out
	}

	assert.Equal(t, "Editing reserve.go", toolActivityTitle(tool(map[string]any{"kind": "edit", "title": "Editing reserve.go"})))
	assert.Equal(t, "Run the tests", toolActivityTitle(tool(map[string]any{"kind": "execute", "title": "`go test ./...`", "rawInput": map[string]any{"command": "go test ./...", "description": "Run the tests"}})))
	assert.Equal(t, commandActivity, toolActivityTitle(tool(map[string]any{"kind": "execute", "title": "`go test ./...`", "rawInput": map[string]any{"command": "go test ./..."}})))
}

func TestAutoClosedActivity(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rec := record{ID: "agt_1", Status: statusClosed, UpdatedAt: now.Add(-2 * time.Hour)}

	assert.Equal(t, "2 h ago", activity(sessionRow{Rec: rec}, now))
	rec.AutoClosed = true
	assert.Equal(t, "auto", activity(sessionRow{Rec: rec}, now))
}

func TestTickerRefreshesElapsedWhileRunning(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestVoicePromptRendersHeard(t *testing.T) {
	t.Parallel()
	text := `{"utterance":"vu_1","heard":"открой бэклог","context":{"section":"agents"}}`
	v := transcriptView{Session: record{ID: "agt_1", Worktree: "/r", Agent: agentClaude, Status: statusDone}, Entries: []*entry{{Kind: entryUser, ID: "prompt-1", Turn: 1, Text: text, Voice: &voicePrompt{Heard: "открой бэклог", Utterance: "vu_1"}}}}

	tree := mainView(screen{View: v, Window: windowStep, Composer: "agt_1/composer/0"})

	require.NoError(t, internalsdui.Validate(tree))
	msgs := nodesOf(tree, "message")
	require.Len(t, msgs, 1)
	assert.Equal(t, "открой бэклог", msgs[0].Props["heard"])
	assert.Equal(t, text, msgs[0].Props["text"])
}

func TestTranscriptWindowAndShowEarlier(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestPromptBlocksImage(t *testing.T) {
	t.Parallel()
	args := promptArgs{Text: "look", Attachments: []attachment{{Kind: "image", MimeType: "image/jpeg", Data: "AAAA"}}}
	assert.Equal(t, []acp.ContentBlock{{Type: "text", Text: "look"}, {Type: "image", Data: "AAAA", MimeType: "image/jpeg"}}, promptBlocks(args, "/r", acp.PromptCapabilities{Image: true}))
	assert.Equal(t, []acp.ContentBlock{{Type: "text", Text: "look"}, {Type: "text", Text: imageOmitted}}, promptBlocks(args, "/r", acp.PromptCapabilities{}))
	args.Text = ""
	assert.Equal(t, []acp.ContentBlock{{Type: "image", Data: "AAAA", MimeType: "image/jpeg"}}, sendable(promptBlocks(args, "/r", acp.PromptCapabilities{Image: true})))
}

func TestDropImageData(t *testing.T) {
	t.Parallel()
	entries := []*entry{{Kind: entryUser, Content: []json.RawMessage{rawJSON(t, acp.ContentBlock{Type: "text", Text: "see"}), rawJSON(t, acp.ContentBlock{Type: "image", Data: "AAAA", MimeType: "image/jpeg"})}}}
	dropImageData(entries)
	assert.JSONEq(t, `{"type":"image","mimeType":"image/jpeg"}`, string(entries[0].Content[1]))
}

func TestChildStatusesAggregateActiveDescendants(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	now := time.Now()
	for _, tc := range []struct {
		children, icon, tone, detail string
		afk                          bool
	}{
		{statusRunning, "play.circle", "accent", "AFK · Waiting for child sessions", true},
		{statusWaiting, "exclamationmark.circle", "warning", "Child session waiting", false},
	} {
		rows := []sessionRow{{Rec: record{ID: "agt_p", Worktree: "/r/a", Agent: agentClaude, Status: statusDone, UpdatedAt: now, AFK: tc.afk}, Children: tc.children}}
		tree := sidebarView(rows, "", nil, now, "/r/a", false, "", sidebarSearch{}, 0)
		require.NoError(t, internalsdui.Validate(tree))

		items := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
		require.Len(t, items, 1, tc.children)
		item := items[0]
		assert.Equal(t, []string{tc.icon, tc.tone, tc.detail}, []string{item.Icon, item.Tone, item.Detail}, tc.children)
	}
}

func TestHeaderMetaRepeatsTheSidebarDetail(t *testing.T) {
	t.Parallel()
	now := time.Now()
	for _, tc := range []struct {
		name     string
		rec      record
		row      sessionRow
		children string
		meta     any
	}{
		{"children", record{Status: statusDone, AFK: true}, sessionRow{}, statusRunning, "AFK · Waiting for child sessions"},
		{"running", record{Status: statusRunning}, sessionRow{Notify: notifyMuted, Started: now.Add(-15 * time.Minute), Activity: "go test ./..."}, "", "Muted · 15 min · go test ./..."},
		{"badged", record{Status: statusWaiting}, sessionRow{}, "", nil},
	} {
		tc.rec.ID, tc.rec.Worktree, tc.rec.Agent = "agt_p", "/r/a", agentClaude
		tree := mainView(screen{View: transcriptView{Session: tc.rec}, Row: tc.row, Now: now, Children: tc.children, Composer: "agt_p/composer/0"})
		require.NoError(t, internalsdui.Validate(tree), tc.name)

		assert.Equal(t, tc.meta, nodesOf(tree, "header")[0].Props["meta"], tc.name)
	}
}

func TestPendingPermissionDocksAboveTheComposer(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	rec := record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusIdle}
	idle := screen{View: transcriptView{Session: rec}, Composer: "agt_1/composer/0", Branch: "docs/design-brief"}
	tree := codeView([]record{rec}, "agt_1", &idle, "/r/a/internal/notify/notify.go")
	require.NoError(t, internalsdui.Validate(tree))

	assert.Subset(t, texts(tree), []string{"New chat in docs/design-brief", starterNote})
	row := tree.Children[0].Children[0].Children
	assert.Equal(t, []string{"icon", "select"}, []string{row[0].Type, row[1].Type})
	assert.Equal(t, []sdui.Option{{Value: "agt_1", Label: "New chat"}}, row[1].Props["options"])
	assert.Equal(t, "circle", row[0].Props["name"])
	assert.Equal(t, true, row[len(row)-1].Props["iconOnly"])
	assert.Len(t, nodesOf(tree, "select"), 1)
	assert.Equal(t, "notify.go", nodesOf(tree, "badge")[0].Props["text"])
	var starts []string
	for _, b := range nodesOf(tree, "button") {
		if b.Props["action"] == actStarter {
			assert.NotContains(t, b.Props, "shortcut")
			starts = append(starts, b.Props["label"].(string))
		}
	}
	assert.Equal(t, []string{"Explain notify.go", "Review the last commit", "Write a test for notify.go"}, starts)
}

func TestCodePanelOnUnstartedGraphStep(t *testing.T) {
	t.Parallel()
	coderRun := &api.Run{ID: "gr_1", Title: "Review loop", After: []api.RunAfter{{Role: "Reviewer", On: "outcome", Outcome: "changes"}}}
	reviewerRun := &api.Run{ID: "gr_1", Title: "Review loop", After: []api.RunAfter{{Role: "Coder", On: "done"}, {Role: "Coder", On: "error"}}}
	coder := record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Title: "Review loop · Coder", Status: statusWaiting, Run: coderRun, CreatedAt: time.Unix(1, 0)}
	reviewer := record{ID: "agt_2", Worktree: "/r/a", Agent: agentClaude, Title: "Review loop · Reviewer", Status: statusIdle, Run: reviewerRun, CreatedAt: time.Unix(2, 0)}
	sc := screen{View: transcriptView{Session: reviewer}, Composer: "agt_2/composer/0"}
	tree := codeView([]record{coder, reviewer}, "agt_2", &sc, "")
	require.NoError(t, internalsdui.Validate(tree))

	composer := nodesOf(tree, "composer")
	require.Len(t, composer, 1)
	assert.Equal(t, []any{true, "Reviewer runs as part of Review loop"}, []any{composer[0].Props["disabled"], composer[0].Props["placeholder"]})
	assert.Equal(t, graphIcon, tree.Children[0].Children[0].Children[0].Props["name"])
	assert.Equal(t, true, nodesOf(tree, "transcript")[0].Props["center"])
	assert.Contains(t, texts(tree), "It starts on its own when Coder finishes its turn or stops with an error, and gets Coder's result. You can write here once it has run.")
	tones := map[string]any{}
	for _, n := range append(nodesOf(tree, "text"), nodesOf(tree, "icon")...) {
		tones[cmp.Or(n.Props["text"], n.Props["name"]).(string)] = n.Props["tone"]
	}
	assert.Contains(t, tones, "Reviewer is a step of Review loop")
	assert.Equal(t, "warning", tones["waiting for you"])
	assert.Equal(t, "warning", tones[statusIcons[statusWaiting]])
	assert.Contains(t, tones, "not started · runs after Coder")
	assert.Empty(t, nodesOf(tree, "badge"))
	var actions []string
	for _, b := range nodesOf(tree, "button") {
		actions = append(actions, b.Props["action"].(string))
	}
	assert.Subset(t, actions, []string{actOpenGraphs, actCodeNew})
}

func TestStepNoteNamesEveryIncomingEdge(t *testing.T) {
	t.Parallel()
	after := []api.RunAfter{{Role: "Reviewer", On: "outcome", Outcome: "changes"}, {Role: "Tester", On: "asks"}}
	assert.Equal(t, "It starts on its own when Reviewer says changes or Tester asks a question, and gets their result. You can write here once it has run.", stepNoteFor(after))
}

func TestDefaultCodeSessionSkipsGraphSteps(t *testing.T) {
	t.Parallel()
	step := record{ID: "agt_1", Status: statusWaiting, Run: &api.Run{ID: "gr_1"}, UpdatedAt: time.Unix(9, 0)}
	older := record{ID: "agt_2", Status: statusDone, UpdatedAt: time.Unix(1, 0)}
	newer := record{ID: "agt_3", Status: statusIdle, UpdatedAt: time.Unix(5, 0)}
	closed := record{ID: "agt_4", Status: statusClosed, UpdatedAt: time.Unix(8, 0)}
	assert.Equal(t, "agt_3", defaultCodeSession([]record{step, older, newer, closed}))
	assert.Empty(t, defaultCodeSession([]record{step}))
}

func TestEntryNodeCachedUntilEntryChanges(t *testing.T) {
	t.Parallel()
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

func TestFailedSessionReadsInWords(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err, detail, badge, line string
	}{
		{"acp error -32603: exit status 1", "exit 1", "Stopped · exit 1", "Claude exited with status 1."},
		{"acp error -32603: Internal error: socket hang up", "Stopped", "Stopped · error", "Claude stopped: socket hang up"},
		{"Claude is temporarily overloaded.\nmore", "Stopped", "Stopped · error", "Claude is temporarily overloaded."},
		{"", "Stopped", "Stopped · error", "Claude stopped with an error."},
	}
	for _, tc := range cases {
		rec := record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusError, Error: tc.err}
		tree := sidebarView([]sessionRow{{Rec: rec}}, "", nil, time.Now(), "/r/a", false, "", sidebarSearch{}, 0)
		require.NoError(t, internalsdui.Validate(tree))
		item := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)[0]
		assert.Equal(t, []string{"xmark.circle", "danger", tc.detail}, []string{item.Icon, item.Tone, item.Detail}, tc.err)

		main := mainView(screen{View: transcriptView{Session: rec}, Composer: "agt_1/composer/0"})
		require.NoError(t, internalsdui.Validate(main))
		assert.Equal(t, []sdui.HeaderBadge{{Text: tc.badge, Tone: "danger"}}, nodesOf(main, "header")[0].Props["badges"], tc.err)
		assert.Contains(t, texts(main), tc.line, tc.err)
		assert.Equal(t, sessionFailure{Word: tc.detail, Badge: tc.badge, Line: tc.line}, failureOf(rec), tc.err)
	}
}

func TestFailedSessionOffersTheMacButtonsAsActions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		needsAuth, cannotResume bool
		id, label, command      string
	}{
		{false, false, "retry", "Retry", "agents.resume"},
		{true, false, "signin", "Sign in", ""},
		{false, true, "continue", "Hand off to a new session", "agents.handoff"},
	}
	for _, tc := range cases {
		rec := record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusError, Error: "exit status 1"}
		main := mainView(screen{View: transcriptView{Session: rec}, Composer: "agt_1/composer/0", NeedsAuth: tc.needsAuth, Continue: tc.cannotResume})
		require.NoError(t, internalsdui.Validate(main))
		require.True(t, slices.ContainsFunc(nodesOf(main, "button"), func(b sdui.Node) bool { return b.ID == "agt_1/"+tc.id }), tc.id)

		acts := failureActions(rec.ID, tc.needsAuth, tc.cannotResume)
		require.Len(t, acts, 1, tc.id)
		assert.Equal(t, []string{tc.id, tc.label, tc.command}, []string{acts[0].ID, acts[0].Label, acts[0].Command}, tc.id)
		if tc.command != "" {
			assert.Equal(t, map[string]string{"session": "agt_1"}, acts[0].Args, tc.id)
		}
	}
}

func TestSidebarNeverFoldsSessionThatNeedsYou(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_p", Worktree: "/r/a", Agent: agentClaude, Status: statusRunning}},
		{Rec: record{ID: "agt_c", Worktree: "/r/a", Agent: agentClaude, Status: statusWaiting, lineage: lineage{ParentSession: "agt_p"}}, Since: now},
	}
	tree := sidebarView(rows, "", map[string]bool{"/r/a": true, expandPrefix + "agt_p": true}, now, "/r/a", false, "", sidebarSearch{}, 0)
	require.NoError(t, internalsdui.Validate(tree))

	parent := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)[0]
	assert.True(t, parent.Expanded)
	require.Len(t, parent.Children, 1)
	assert.Equal(t, "agt_c", parent.Children[0].ID)
}

func TestSidebarFoldsServiceSessions(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusRunning}},
		{Rec: record{ID: "agt_2", Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, UpdatedAt: now}},
		{Rec: record{ID: "agt_v", Worktree: "/r/a", Agent: agentClaude, Title: "Voice", Status: statusRunning, SystemPrompt: "route"}},
	}
	groups := func(rows []sessionRow) []sdui.TreeItem {
		tree := sidebarView(rows, "", map[string]bool{"/r/a": true}, now, "/r/a", false, "", sidebarSearch{}, 0)
		require.NoError(t, internalsdui.Validate(tree))
		return nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	}
	tabs := nodesOf(sidebarView(rows, "", nil, now, "/r/a", false, "", sidebarSearch{}, 0), "tabs")[0].Props["items"].([]sdui.Item)
	assert.Equal(t, []string{"1", "0", "1"}, []string{tabs[0].Badge, tabs[1].Badge, tabs[3].Badge})

	quiet := groups(rows)
	assert.False(t, quiet[2].Expanded)

	rows[2].Rec.Status, rows[2].Since = statusWaiting, now
	waiting := groups(rows)
	assert.Equal(t, []string{"agt_1", groupPrefix + closedKey, groupPrefix + serviceKey}, []string{waiting[0].ID, waiting[1].ID, waiting[2].ID})
	service := waiting[2]
	assert.Equal(t, []string{"Service", "1", "", "warning"}, []string{service.Label, service.Detail, service.Tone, service.DetailTone})
	assert.True(t, service.Expanded)
	assert.Equal(t, "agt_v", service.Children[0].ID)
}

func TestChildAskingParentShowsAsAskingParent(t *testing.T) {
	t.Parallel()
	parent := record{ID: "agt_p", Status: statusDone}
	kid := record{ID: "agt_c", Status: statusWaiting, AsksParent: true, lineage: lineage{ParentSession: "agt_p"}}

	item := sessionItem(sessionRow{Rec: kid}, 0, time.Now())
	assert.Equal(t, "arrow.up.circle", item.Icon)
	assert.Equal(t, "asking parent", item.Detail)
	assert.False(t, matchesFilter(sessionRow{Rec: kid}, filterNeeds))
	assert.Equal(t, statusRunning, childStatuses([]record{parent, kid})["agt_p"])
}

func TestThoughtCarriesItsDuration(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	v := transcriptView{
		Session: record{ID: "agt_1", Agent: agentClaude, Status: statusRunning},
		Entries: []*entry{
			{Kind: entryThought, ID: "t1", Turn: 1, Text: "weighing", At: at},
			{Kind: entryAgent, ID: "a1", Turn: 1, Text: "done", At: at.Add(4200 * time.Millisecond)},
			{Kind: entryThought, ID: "t2", Turn: 1, Text: "still thinking", At: at.Add(5 * time.Second)},
		},
	}
	tree := transcript(screen{View: v})
	require.NoError(t, internalsdui.Validate(tree))

	var seconds []any
	for _, n := range nodesOf(tree, "message") {
		if n.Props["role"] == "thought" {
			seconds = append(seconds, n.Props["seconds"])
		}
	}
	assert.Equal(t, []any{4, nil}, seconds)
}

func TestSessionHeaderNamesStatusAgentBranchAndMode(t *testing.T) {
	t.Parallel()
	v := transcriptView{
		Session: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Title: "Rate-limit", Status: statusWaiting, Mode: "default"},
		Modes:   []acp.SessionMode{{ID: "default", Name: "Default"}},
	}
	tree := mainView(screen{View: v, Branch: branchLabel("(detached 03a9ff5f)"), Composer: "agt_1/composer/0"})
	require.NoError(t, internalsdui.Validate(tree))

	h := nodesOf(tree, "header")[0]
	assert.Equal(t, []any{"Rate-limit", "waiting", "Claude", "detached · 03a9ff5f", "Ask before edits"}, []any{h.Props["title"], h.Props["status"], h.Props["agent"], h.Props["branch"], h.Props["mode"]})
	assert.Equal(t, []sdui.HeaderBadge{{Text: "waiting for you", Tone: "warning"}}, h.Props["badges"])
	assert.Equal(t, []string{"agt_1/stop"}, []string{h.Children[0].ID})
}

func TestPermissionCardForACommand(t *testing.T) {
	t.Parallel()
	call := rawJSON(t, map[string]any{"title": "go test ./internal/notify/...", "kind": "execute", "rawInput": map[string]string{"command": "go test ./internal/notify/...", "description": "Run the notify tests"}})
	options := []acp.PermissionOption{
		{OptionID: "reject", Name: "No", Kind: "reject_once"},
		{OptionID: "allow-with-updates", Name: "Yes, and don't ask again for go test and go vet commands", Kind: "allow_always"},
		{OptionID: "allow-once", Name: "Yes", Kind: "allow_once"},
		{OptionID: "never", Name: "No, never", Kind: "reject_always"},
	}
	v := transcriptView{Session: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusWaiting}, Entries: []*entry{{Kind: entryPermission, ID: "perm-1", Turn: 1, ToolCall: call, Options: options}}}
	card := permissionCard(screen{View: v, Workspace: "shop", Pending: map[string]bool{"perm-1": true}}, v.Entries[0])
	require.NoError(t, internalsdui.Validate(card))

	assert.Equal(t, []any{"Run a command?", "go test ./internal/notify/...", "Claude says: Run the notify tests"}, []any{card.Props["title"], card.Props["command"], card.Props["caption"]})
	assert.Equal(t, []sdui.PermissionOption{
		{ID: "allow-once", Label: "Allow once", Kind: "allow_once", Shortcut: "opt+1"},
		{ID: "allow-with-updates", Label: "Always allow…", Kind: "allow_always", Shortcut: "opt+2", Rule: "Bash(go test:*) Bash(go vet:*)", Scope: "in shop on this Mac", Workspace: "shop"},
		{ID: "reject", Label: "Reject", Kind: "reject_once", Shortcut: "opt+3"},
		{ID: "never", Label: "Always reject", Kind: "reject_always", Menu: true},
	}, card.Props["options"])
}

func TestComposerPlaceholderFollowsState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		rec     record
		pending map[string]bool
		want    string
	}{
		{record{Status: statusIdle}, nil, "Message Claude…"},
		{record{Status: statusRunning}, nil, "Queue a message"},
		{record{Status: statusDone, Asks: true}, nil, "Answer Claude…"},
		{record{Status: statusWaiting}, map[string]bool{"elicitation-1": true}, "Answer Claude…"},
	} {
		tc.rec.ID, tc.rec.Agent = "agt_1", agentClaude
		v := transcriptView{Session: tc.rec, Entries: []*entry{{Kind: entryElicitation, ID: "elicitation-1", Turn: 1}}}
		n := composer(screen{View: v, Composer: "agt_1/composer/0", Pending: tc.pending})
		assert.Equal(t, tc.want, n.Props["placeholder"], tc.rec.Status)
		assert.Equal(t, composerHint, n.Props["hint"])
	}
}

func TestShownPromptDropsIDs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `[Clubmate] Child "Fix tests" is done.`, shownPrompt(`[Clubmate] Child "Fix tests" (agt_a928b82b8bcb94ab) is done.`))
	head := fmt.Sprintf(handoffHead, "Old", "agt_0123abcd", "/r/a", "agt_0123abcd")
	assert.Equal(t, `[Clubmate handoff] You continue agent session "Old" in /r/a; it is closed now.`, shownPrompt(head))
}

func TestReviewStatusYieldsToAsks(t *testing.T) {
	t.Parallel()
	rec := record{ID: "agt_1", Status: statusDone, Review: true}

	assert.Equal(t, statusReview, displayStatus(rec, ""))
	assert.Equal(t, "review", activity(sessionRow{Rec: rec}, time.Now()))
	rec.Asks = true
	assert.Equal(t, statusAsks, displayStatus(rec, ""))
}

func TestViewShortcutsDeclared(t *testing.T) {
	t.Parallel()
	options := make([]elicitOption, maxOptionShortcut+1)
	for i := range options {
		options[i] = elicitOption{Value: strconv.Itoa(i), Label: strconv.Itoa(i)}
	}
	toggle, ok := contextButton(screen{View: transcriptView{Session: record{ID: "agt_1", Agent: agentClaude, AgentSessionID: "s"}}})
	require.True(t, ok)
	en := &entry{Options: []acp.PermissionOption{{OptionID: "once", Kind: "allow_once"}, {OptionID: "always", Kind: "allow_always"}, {OptionID: "reject", Kind: "reject_once"}, {OptionID: "never", Kind: "reject_always"}}}
	card := sdui.PermissionCard("p", "permission-1", sdui.PermissionCardProps{Text: "Run?", Options: permissionOptions("ws", en, toolFields{})})
	fields := []elicitField{{Key: "q", Type: fieldChoice, Options: options}}
	shortcuttest.RequireDeclared(t, append(fieldNodes("q", fields), toggle, card)...)
}

func TestNeedsAnswer(t *testing.T) {
	t.Parallel()
	choice := elicitField{Key: "question_0", Type: fieldChoice}
	multi := elicitField{Key: "question_1", Type: fieldMulti}
	other := elicitField{Key: "question_0_custom", Type: fieldText}
	cases := []struct {
		name   string
		fields []elicitField
		want   bool
	}{
		{"questions with Other", []elicitField{choice, other, multi}, true},
		{"no fields", nil, false},
		{"a free text field", []elicitField{choice, {Key: "note", Type: fieldText}}, false},
		{"a number", []elicitField{{Key: "n", Type: fieldNumber}}, false},
		{"a boolean", []elicitField{multi, {Key: "b", Type: fieldBoolean}}, false},
		{"its own required", []elicitField{{Key: "question_0", Type: fieldChoice, Required: true}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, needsAnswer(tc.fields))
		})
	}
}

func TestHeaderValidatesForEveryDisplayStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rec      record
		children string
	}{
		{rec: record{Status: statusRunning}},
		{rec: record{Status: statusWaiting}},
		{rec: record{Status: statusDone, Asks: true}},
		{rec: record{Status: statusWaiting, AsksParent: true, lineage: lineage{ParentSession: "agt_p"}}},
		{rec: record{Status: statusDone, Review: true}},
		{rec: record{Status: statusDone}},
		{rec: record{Status: statusIdle}},
		{rec: record{Status: statusError}},
		{rec: record{Status: statusClosed}},
		{rec: record{Status: statusDone}, children: statusRunning},
		{rec: record{Status: statusDone}, children: statusWaiting},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		c.rec.ID = "agt_1"
		display := displayStatus(c.rec, c.children)
		seen[display] = true
		tree := header(screen{View: transcriptView{Session: c.rec}, Children: c.children, Now: time.Now()})
		require.NoError(t, internalsdui.Validate(tree), display)
	}
	assert.Len(t, seen, len(statusIcons))
}
