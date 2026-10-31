package main

import (
	"encoding/json"
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
		{Rec: record{ID: "agt_2", Worktree: "/r/b", Agent: agentClaude, Title: "Auth", Status: statusDone, UpdatedAt: now.Add(-12 * time.Minute)}, Notify: notifyMuted},
		{Rec: record{ID: "agt_3", Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, Closed: true, UpdatedAt: now}},
		{Rec: record{ID: "agt_4", Worktree: "/r/b", Agent: agentClaude, Title: "Docs", Status: statusDone, Asks: true, UpdatedAt: now}},
	}
	tree := sidebarView(rows, "agt_2", nil, now, false)
	require.NoError(t, internalsdui.Validate(tree))

	groups := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, groups, 3)
	assert.Equal(t, []string{"Needs you · 2", "a", "b"}, []string{groups[0].Label, groups[1].Label, groups[2].Label})
	needs, a, b := groups[0].Children, groups[1].Children, groups[2].Children
	require.Len(t, needs, 2)
	require.Len(t, a, 2)
	require.Len(t, b, 2)
	assert.Equal(t, []string{needsPrefix + "agt_1", needsPrefix + "agt_4"}, []string{needs[0].ID, needs[1].ID})
	assert.Equal(t, []string{"agt_1", "agt_2", "agt_4"}, []string{a[0].ID, b[0].ID, b[1].ID})
	assert.Equal(t, "Closed · 1", a[1].Label)
	assert.False(t, a[1].Expanded)
	assert.Empty(t, a[1].Children)
	assert.Equal(t, "Waiting for permission · go test", a[0].Detail)
	assert.Equal(t, "Muted · 12 min ago", b[0].Detail)
	assert.Equal(t, []string{"warning", "success"}, []string{a[0].Tone, b[0].Tone})
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
		{Rec: record{ID: "agt_2", Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, Closed: true, UpdatedAt: now}},
	}
	tree := sidebarView(rows, "", map[string]bool{closedKey + "/r/a": false}, now, false)
	require.NoError(t, internalsdui.Validate(tree))

	groups := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, groups, 1)
	closed := groups[0].Children[1]
	assert.True(t, closed.Expanded)
	require.Len(t, closed.Children, 1)
	assert.Equal(t, "agt_2", closed.Children[0].ID)
	assert.Equal(t, []string{actHandoff, actNotifyNeeds, actNotifyMuted, actDelete}, menuActions(closed.Children[0].Menu))
}

func TestSidebarNeedsSelectionFallsBackWhenAnswered(t *testing.T) {
	now := time.Now()
	selectedIn := func(status string) any {
		rows := []sessionRow{{Rec: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: status, UpdatedAt: now}}}
		return nodesOf(sidebarView(rows, needsPrefix+"agt_1", nil, now, false), "tree")[0].Props["selected"]
	}
	assert.Equal(t, needsPrefix+"agt_1", selectedIn(statusWaiting))
	assert.Equal(t, "agt_1", selectedIn(statusRunning))
}

func TestDoneAsksWhenLastAgentMessageIsQuestion(t *testing.T) {
	for _, tc := range []struct {
		texts []string
		want  bool
	}{
		{[]string{"Делаем?"}, true},
		{[]string{"Делаем?**\n\n"}, true},
		{[]string{"Готово."}, false},
		{[]string{"Делаем?", "Сделал."}, false},
		{nil, false},
	} {
		s := &session{}
		for _, text := range tc.texts {
			s.entries = append(s.entries, &entry{Kind: entryAgent, Text: text}, &entry{Kind: entryTool})
		}
		s.setStatusLocked(statusDone, "")
		assert.Equal(t, tc.want, s.rec.Asks, tc.texts)
		s.setStatusLocked(statusIdle, "")
		assert.False(t, s.rec.Asks)
	}
}

func TestSidebarNestsChildren(t *testing.T) {
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_c", Worktree: "/r/b", lineage: lineage{ParentSession: "agt_p"}, Agent: agentClaude, Status: statusDone, Asks: true, UpdatedAt: now}},
		{Rec: record{ID: "agt_d", Worktree: "/r/a", lineage: lineage{ParentSession: "agt_p"}, Agent: agentClaude, Status: statusClosed, Closed: true, UpdatedAt: now}},
		{Rec: record{ID: "agt_p", Worktree: "/r/a", Agent: agentClaude, Status: statusDone, UpdatedAt: now}},
	}

	folded := nodesOf(sidebarView(rows, "", map[string]bool{}, now, false), "tree")[0].Props["items"].([]sdui.TreeItem)[0].Children[0]
	assert.True(t, folded.HasChildren)
	assert.False(t, folded.Expanded)
	assert.Empty(t, folded.Children)

	tree := sidebarView(rows, "agt_c", map[string]bool{"s:agt_p": false}, now, false)
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

func TestConfigPickersAndModelEffortInHeader(t *testing.T) {
	var configs []acp.SessionConfigOption
	require.NoError(t, json.Unmarshal([]byte(`[
		{"id":"mode","name":"Mode","category":"mode","type":"select","currentValue":"plan","options":[{"value":"plan","name":"Plan"}]},
		{"id":"model","name":"Model","category":"model","type":"select","currentValue":"opus","options":[{"group":"claude","name":"Claude","options":[{"value":"sonnet","name":"Sonnet 5"},{"value":"opus","name":"Opus 5.5"}]}]},
		{"id":"effort","name":"Effort","category":"thought_level","type":"select","currentValue":"high","options":[{"value":"high","name":"High"}]}
	]`), &configs))
	v := transcriptView{Session: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusIdle}, Configs: configs}
	tree := mainView(screen{View: v, Composer: "agt_1/composer/0", Advisor: "opus"})
	require.NoError(t, internalsdui.Validate(tree))

	assert.Equal(t, "Claude · ⑂ a · Opus 5.5 · High effort", nodesOf(tree, "text")[1].Props["text"])
	got := nodesOf(tree, "composer")[0].Props["pickers"].([]sdui.Picker)
	require.Len(t, got, 5)
	assert.Equal(t, sdui.Picker{ID: "model", Help: "Model", Value: "opus", Options: []sdui.Option{{Value: "sonnet", Label: "Sonnet 5"}, {Value: "opus", Label: "Opus 5.5"}}}, got[0])
	assert.Equal(t, []string{"Effort", "Advisor", "Finish", "AFK"}, []string{got[1].Title, got[2].Title, got[3].Title, got[4].Title})
	assert.Equal(t, []string{"opus", "ask", "off"}, []string{got[2].Value, got[3].Value, got[4].Value})

	v.Session.ParentSession = "agt_0"
	child := nodesOf(mainView(screen{View: v, Composer: "agt_1/composer/0", Advisor: "opus"}), "composer")[0].Props["pickers"].([]sdui.Picker)
	assert.Equal(t, "Advisor", child[len(child)-1].Title)
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
