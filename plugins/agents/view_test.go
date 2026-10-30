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
		{Rec: record{ID: "agt_2", Worktree: "/r/b", Agent: agentClaude, Title: "Auth", Status: statusDone, UpdatedAt: now.Add(-12 * time.Minute)}},
		{Rec: record{ID: "agt_3", Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, Closed: true, UpdatedAt: now}},
		{Rec: record{ID: "agt_4", Worktree: "/r/b", Agent: agentClaude, Title: "Docs", Status: statusDone, Asks: true, UpdatedAt: now}},
	}
	tree := sidebarView(rows, "agt_2", nil, now)
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
	assert.Equal(t, "Claude · Waiting for permission · go test", a[0].Detail)
	assert.Equal(t, "Claude · Done · 12 min ago", b[0].Detail)
	assert.Equal(t, []string{actClose, actDelete}, []string{a[0].Menu[0].Action, a[0].Menu[1].Action})
}

func TestSidebarExpandedClosedGroup(t *testing.T) {
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusDone, UpdatedAt: now}},
		{Rec: record{ID: "agt_2", Worktree: "/r/a", Agent: agentClaude, Status: statusClosed, Closed: true, UpdatedAt: now}},
	}
	tree := sidebarView(rows, "", map[string]bool{closedKey + "/r/a": false}, now)
	require.NoError(t, internalsdui.Validate(tree))

	groups := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, groups, 1)
	closed := groups[0].Children[1]
	assert.True(t, closed.Expanded)
	require.Len(t, closed.Children, 1)
	assert.Equal(t, "agt_2", closed.Children[0].ID)
	assert.Len(t, closed.Children[0].Menu, 1)
}

func TestSidebarNeedsSelectionFallsBackWhenAnswered(t *testing.T) {
	now := time.Now()
	selectedIn := func(status string) any {
		rows := []sessionRow{{Rec: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: status, UpdatedAt: now}}}
		return nodesOf(sidebarView(rows, needsPrefix+"agt_1", nil, now), "tree")[0].Props["selected"]
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

func TestConfigPickersAndModelInHeader(t *testing.T) {
	var configs []acp.SessionConfigOption
	require.NoError(t, json.Unmarshal([]byte(`[
		{"id":"mode","name":"Mode","category":"mode","type":"select","currentValue":"plan","options":[{"value":"plan","name":"Plan"}]},
		{"id":"model","name":"Model","category":"model","type":"select","currentValue":"opus","options":[{"group":"claude","name":"Claude","options":[{"value":"sonnet","name":"Sonnet 5"},{"value":"opus","name":"Opus 5.5"}]}]},
		{"id":"effort","name":"Effort","category":"thought_level","type":"select","currentValue":"high","options":[{"value":"high","name":"High"}]}
	]`), &configs))
	v := transcriptView{Session: record{ID: "agt_1", Worktree: "/r/a", Agent: agentClaude, Status: statusIdle}, Configs: configs}
	tree := mainView(screen{View: v, Composer: "agt_1/composer/0", Advisor: "opus"})
	require.NoError(t, internalsdui.Validate(tree))

	assert.Equal(t, "Claude · ⑂ a · Opus 5.5", nodesOf(tree, "text")[1].Props["text"])
	got := nodesOf(tree, "composer")[0].Props["pickers"].([]sdui.Picker)
	require.Len(t, got, 4)
	assert.Equal(t, sdui.Picker{ID: "model", Help: "Model", Value: "opus", Options: []sdui.Option{{Value: "sonnet", Label: "Sonnet 5"}, {Value: "opus", Label: "Opus 5.5"}}}, got[0])
	assert.Equal(t, []string{"Effort", "Advisor", "Finish"}, []string{got[1].Title, got[2].Title, got[3].Title})
	assert.Equal(t, []string{"opus", "ask"}, []string{got[2].Value, got[3].Value})
}
