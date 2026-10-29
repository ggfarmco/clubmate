package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
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
	}
	tree := sidebarView(rows, "agt_2", nil, now)
	require.NoError(t, internalsdui.Validate(tree))

	groups := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, groups, 2)
	assert.Equal(t, []string{"a", "b"}, []string{groups[0].Label, groups[1].Label})
	a, b := groups[0].Children, groups[1].Children
	require.Len(t, a, 2)
	require.Len(t, b, 1)
	assert.Equal(t, []string{"agt_1", "agt_3", "agt_2"}, []string{a[0].ID, a[1].ID, b[0].ID})
	assert.Equal(t, "Claude · Waiting for permission · go test", a[0].Detail)
	assert.Equal(t, "Claude · Done · 12 min ago", b[0].Detail)
	assert.Equal(t, []string{actClose, actDelete}, []string{a[0].Menu[0].Action, a[0].Menu[1].Action})
	assert.Len(t, a[1].Menu, 1)
}

func TestTranscriptWindowAndShowEarlier(t *testing.T) {
	v := transcriptView{Session: record{ID: "agt_1", Worktree: "/r", Agent: agentClaude, Status: statusDone}}
	for turn := 1; turn <= 45; turn++ {
		n := strconv.Itoa(turn)
		v.Turns = append(v.Turns, turnInfo{Turn: turn, CheckpointSha: "sha" + n, StopReason: "end_turn"})
		v.Entries = append(v.Entries, &entry{Kind: entryUser, ID: "prompt-" + n, Turn: turn, Text: "p" + n}, &entry{Kind: entryAgent, ID: "m" + n, Turn: turn, Text: "a" + n})
	}
	firstPrompt := func(st *sessionUI) (string, int) {
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
		return users[0].Props["text"].(string), len(users)
	}

	st := &sessionUI{window: windowStep}
	first, shown := firstPrompt(st)
	assert.Equal(t, 20, shown)
	assert.Equal(t, "p26", first)

	st.showEarlier()
	first, shown = firstPrompt(st)
	assert.Equal(t, 40, shown)
	assert.Equal(t, "p6", first)
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

func TestRenderThrottle(t *testing.T) {
	var renders, after atomic.Int64
	var done atomic.Bool
	th := newThrottle(renderEvery, func() {
		renders.Add(1)
		if done.Load() {
			after.Add(1)
		}
	})
	start := time.Now()
	for i := range 200 {
		th.kick()
		time.Sleep(time.Until(start.Add(time.Duration(i+1) * 5 * time.Millisecond)))
	}
	done.Store(true)
	th.kick()
	time.Sleep(2 * renderEvery)
	assert.LessOrEqual(t, renders.Load(), int64(12))
	assert.GreaterOrEqual(t, after.Load(), int64(1))
}
