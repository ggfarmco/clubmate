package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

var blockNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func gatedTool(t *testing.T, id string, turn int, mark *gateMark) *entry {
	t.Helper()
	tool := map[string]json.RawMessage{"toolCallId": rawJSON(t, id), "kind": rawJSON(t, "execute"), "title": rawJSON(t, "git push origin fix/cart"), "status": rawJSON(t, "pending"), "rawInput": rawJSON(t, map[string]any{"command": "git push origin fix/cart"})}
	if mark != nil {
		tool["gate"] = rawJSON(t, mark)
	}
	return &entry{Kind: entryTool, ID: id, Turn: turn, Tool: tool, toolName: "Bash"}
}

func plainTool(t *testing.T, id string, turn int) *entry {
	t.Helper()
	en := gatedTool(t, id, turn, nil)
	en.Tool["status"] = rawJSON(t, "completed")
	return en
}

func pushHold(caption string) callMark {
	return callMark{Gate: "gt_push", Name: "git push", Repo: "shop", Round: 1, Of: 2, Caption: caption}
}

func gateScreenOf(entries []*entry, turns []turnInfo, calls map[string]map[string]callLive) screen {
	return screen{View: transcriptView{Session: record{ID: "agt_1", Agent: agentClaude, Status: statusRunning}, Entries: entries, Turns: turns}, Calls: calls, Now: blockNow}
}

func childIDs(n sdui.Node) []string {
	var out []string
	for _, c := range n.Children {
		out = append(out, c.Type+":"+c.ID)
	}
	return out
}

func TestHeldCallIsNotFolded(t *testing.T) {
	t.Parallel()
	held := &gateMark{State: callStateHeld, Holds: []callMark{pushHold("")}}
	agent := &entry{Kind: entryTool, ID: "sub-1", Turn: 1, Tool: map[string]json.RawMessage{"kind": rawJSON(t, "agent"), "title": rawJSON(t, "ship it"), "status": rawJSON(t, "in_progress")}}
	agent.HeldCalls = heldInside(&subagent{entries: []*entry{plainTool(t, "s0", 1), gatedTool(t, "s1", 1, held)}})
	entries := []*entry{plainTool(t, "t1", 1), gatedTool(t, "t2", 1, held), plainTool(t, "t3", 1), agent, plainTool(t, "t4", 1)}
	sc := gateScreenOf(entries, nil, map[string]map[string]callLive{"t2": {"gt_push": {Graph: "Fable review", Since: blockNow.Add(-2 * time.Minute), Reviewer: "agt_rev"}}})

	tree := transcript(sc)

	require.NoError(t, internalsdui.Validate(tree))
	assert.Equal(t, []string{"toolCard:agt_1/tool/t1", "card:agt_1/tool/t2", "toolCard:agt_1/tool/t3", "toolCard:agt_1/tool/sub-1", "card:agt_1/tool/s1", "toolCard:agt_1/tool/t4"}, childIDs(tree))
	assert.Len(t, windowRows(sc, entries, nil), 5)
	block := tree.Children[1]
	assert.Equal(t, []string{"Held by a gate", "shop · git push · round 1 of 2", "Bash", "git push origin fix/cart", "Fable review → approved / changes", "reviewing · 2 min", blockWaitsOneFor("Claude")}, textsOf(block))
	assert.Equal(t, "agt_1/heldReview/1/agt_rev", nodesOf(block, "button")[0].ID)
}

func blockWaitsOneFor(agent string) string {
	return fmt.Sprintf(blockWaitsOne, agent)
}

func TestHeldToBeSafeCaptionShows(t *testing.T) {
	t.Parallel()
	caption := "git push · inside sh -c, held to be safe"
	en := gatedTool(t, "t1", 1, &gateMark{State: callStateHeld, Holds: []callMark{pushHold(caption)}})
	g := gateConfig{ID: "gt_push", Repo: "/src/shop", Point: gatePointTool, Tool: "Bash", How: "command", Text: "git push", Rounds: 2}
	row := gateRow{gateConfig: g, Name: toolGateName(g), Words: toolMatchWords(g), Landings: []gateLanding{{Lander: "agt_1", Title: "Ship it", Call: `sh -c "git push"`, Caption: caption, State: gateStateReview, Round: 1, Since: blockNow}}}

	block := transcript(gateScreenOf([]*entry{en}, nil, nil)).Children[0]
	page := gateMain(gateScreen{Row: row, Sessions: map[string]bool{"agt_1": true}, Now: blockNow, Loc: time.UTC})

	assert.Contains(t, textsOf(block), "shop · git push · inside sh -c, held to be safe · round 1 of 2")
	assert.Equal(t, `Ship it · sh -c "git push" · `+caption, nodesOf(page, "table")[0].Props["rows"].([]sdui.TableRow)[0].Cells[2].Text)
}

func TestRefusedBlockHasTheMessageUnderItWithoutATurnBreak(t *testing.T) {
	t.Parallel()
	held := gatedTool(t, "t1", 1, &gateMark{State: callStateHeld, Holds: []callMark{pushHold("")}})
	refusal := map[string]json.RawMessage{"gate": rawJSON(t, gateMark{State: callStateRefd, Round: 1})}
	keepHolds(held.Tool, refusal)
	held.Tool["gate"] = refusal["gate"]
	one := gateMessage{Call: "t1", Groups: []gateFindings{{Head: "[Clubmate] From the gate · shop · git push · round 1 of 2 · changes", Findings: []string{"total.go:5 — discount first"}}}, Tail: "The next git push goes through the gate again, as round 2."}
	message := func(turn int, m gateMessage) *entry {
		return &entry{Kind: entryUser, ID: gateEntryID(m.Call), Turn: turn, Text: m.text(), Gate: &m}
	}
	reply := &entry{Kind: entryAgent, ID: "a1", Turn: 1, Text: "The push was refused."}
	steered := transcript(gateScreenOf([]*entry{held, message(1, one), reply}, nil, nil))
	queued := transcript(gateScreenOf([]*entry{held, reply, message(2, one)}, []turnInfo{{Turn: 1, StopReason: "max_tokens"}}, nil))
	two := one
	two.Groups = append(two.Groups, gateFindings{Head: "[Clubmate] From the gate · shop · gh pr create · round 1 of 1 · changes"})
	both := gatedTool(t, "t1", 1, &gateMark{State: callStateRefd, Round: 1})
	pair := transcript(gateScreenOf([]*entry{both, message(1, two)}, nil, nil))

	for _, tree := range []sdui.Node{steered, queued, pair} {
		require.NoError(t, internalsdui.Validate(tree))
	}
	assert.Equal(t, []string{"Refused by the gate · round 1 · changes", "shop · git push · round 1 of 2", "Bash", "git push origin fix/cart"}, textsOf(steered.Children[0]))
	assert.Equal(t, []string{"card:agt_1/tool/t1", "message:agt_1/msg/user/gate-t1", "message:agt_1/msg/agent/a1"}, childIDs(steered))
	assert.Equal(t, []string{"card:agt_1/tool/t1", "message:agt_1/msg/user/gate-t1", "message:agt_1/msg/agent/a1", "text:"}, childIDs(queued))
	assert.Equal(t, "Stopped: max tokens", queued.Children[3].Props["text"])
	assert.Equal(t, []string{"card:agt_1/tool/t1", "message:agt_1/msg/user/gate-t1"}, childIDs(pair))
	assert.Equal(t, []string{"Refused by 2 gates · changes", "shop · git push · gh pr create"}, textsOf(pair.Children[0])[:2])
	assert.Len(t, pair.Children[1].Props["gate"].(sdui.GateMessage).Groups, 2)
	noTurn := gatedTool(t, "s2", 1, &gateMark{State: callStateRefd, Round: 1, Reason: "no turn"})
	noTurn.Tool["status"] = rawJSON(t, "failed")
	assert.Equal(t, "• git push origin fix/cart", (&subagent{entries: []*entry{noTurn}}).body())
}

func TestGateMessageBlock(t *testing.T) {
	t.Parallel()
	done := doneMessage(promptArgs{Gate: true, Text: fmt.Sprintf(heldChangesFormat, "shop", "plans and specs", 1, 2, bullets([]string{"plan.md:3 — no rollback\n  step 4 drops the table"})) + fmt.Sprintf(heldNotBlocking, bullets([]string{"typo in the title"})) + fixRule(true) + heldChangesTail}, true)
	tool := gateMessage{Call: "t1", Groups: []gateFindings{{Head: "[Clubmate] From the gate · shop · git push · round 1 of 2 · changes", Findings: []string{"a"}, Deferred: []string{"b"}}}, Tail: "The next git push goes through the gate again, as round 2."}
	sc := gateScreenOf(nil, nil, nil)

	require.NotNil(t, done)
	doneNode, _ := buildEntryNode(sc, &entry{Kind: entryUser, ID: "prompt-2", Turn: 2, Text: "x", Gate: done}, "")
	toolNode, _ := buildEntryNode(sc, &entry{Kind: entryUser, ID: gateEntryID("t1"), Turn: 1, Text: tool.text(), Gate: &tool}, "")

	require.NoError(t, internalsdui.Validate(sdui.VStack(doneNode, toolNode)))
	assert.Equal(t, sdui.GateMessage{Groups: []sdui.GateGroup{{Title: "From the gate · shop · plans and specs · round 1 of 2 · changes", Findings: []string{"plan.md:3 — no rollback\n  step 4 drops the table"}, Deferred: []string{"typo in the title"}}}, Tail: "Fix these; your next finished turn goes back to the gate."}, doneNode.Props["gate"])
	assert.Equal(t, sdui.GateMessage{Groups: []sdui.GateGroup{{Title: "From the gate · shop · git push · round 1 of 2 · changes", Findings: []string{"a"}, Deferred: []string{"b"}}}, Tail: tool.Tail}, toolNode.Props["gate"])
	assert.Nil(t, doneMessage(promptArgs{Text: "Merge it"}, true))
}

func TestComposerSaysHeld(t *testing.T) {
	t.Parallel()
	sc := gateScreenOf(nil, nil, nil)
	sc.Row.Landing = &landingView{Hint: callHint}

	assert.Equal(t, "Held by a gate — messages queue", composerPlaceholder(sc))
	assert.Equal(t, "Queue a message", composerPlaceholder(gateScreenOf(nil, nil, nil)))
}

func TestHeldSubagentCallCopiesItsTool(t *testing.T) {
	t.Parallel()
	en := gatedTool(t, "s1", 1, &gateMark{State: callStateHeld, Holds: []callMark{pushHold("")}})
	sub := &subagent{entry: &entry{Turn: 1}, entries: []*entry{en}, index: map[string]int{key(entryTool, "s1"): 0}}
	copies := heldInside(sub)
	require.Len(t, copies, 1)
	update := rawJSON(t, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "s1", "status": "failed", "gate": gateMark{State: callStateRefd, Round: 1}})

	var wg sync.WaitGroup
	wg.Go(func() {
		for range 2000 {
			sub.tool(false, update, blockNow)
		}
	})
	wg.Go(func() {
		for range 2000 {
			_ = markOf(copies[0])
			_, _ = callShown(copies[0])
		}
	})
	wg.Wait()
}

func TestPhoneGetsTheCallStateAgain(t *testing.T) {
	t.Parallel()
	reviewing := map[string]map[string]callLive{"t1": {"gt_push": {Graph: "Fable review"}}}
	asking := map[string]map[string]callLive{"t1": {"gt_push": {Graph: "Fable review", Request: "req_1"}}}
	agent := &entry{Kind: entryTool, ID: "sub-1", HeldCalls: []*entry{gatedTool(t, "s1", 1, &gateMark{State: callStateHeld})}}

	stamp := func(lives map[string]map[string]callLive, now time.Time) string {
		return decorationKey(record{}, pluginsdk.Workspace{}, callStamp(lives, now)...)
	}
	since := map[string]map[string]callLive{"t1": {"gt_push": {Graph: "Fable review", Since: blockNow}}}

	assert.NotEqual(t, stamp(reviewing, blockNow), stamp(asking, blockNow))
	assert.NotEqual(t, stamp(since, blockNow), stamp(since, blockNow.Add(2*time.Minute)))
	assert.Equal(t, []bool{true, true, false}, []bool{gateDecorated(gatedTool(t, "t1", 1, &gateMark{State: callStateRefd})), gateDecorated(agent), gateDecorated(plainTool(t, "t2", 1))})
}

func TestQueuedGateMessageTouchesItsCallAndCarriesTitles(t *testing.T) {
	t.Parallel()
	s := &session{applying: true, stamp: 1, index: map[string]int{}}
	s.applyTool(true, 1, rawJSON(t, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "git push"}))
	s.stamp = 2
	m := gateMessage{Call: "t1", Groups: []gateFindings{{Head: "[Clubmate] From the gate · shop · git push · round 1 of 2 · changes"}}}
	s.applyPrompt(envelope{Turn: 2, Payload: rawJSON(t, promptPayload{Gate: &m})})

	assert.Equal(t, int64(2), s.entries[s.index[key(entryTool, "t1")]].seq)
	assert.Equal(t, "From the gate · shop · git push · round 1 of 2 · changes", titled(m).Groups[0].Title)
	assert.Empty(t, m.Groups[0].Title)
}

func TestGateUnderCallsMovesAGateMessageBelowItsCallOnly(t *testing.T) {
	t.Parallel()
	call := &entry{Kind: entryTool, ID: "t1", Turn: 2}
	other := &entry{Kind: entryAgent, Turn: 3}
	gate := &entry{Kind: entryUser, Turn: 5, Gate: &gateMessage{Call: "t1"}}
	loose := &entry{Kind: entryUser, Turn: 5, Gate: &gateMessage{Call: "gone"}}

	got := gateUnderCalls([]*entry{call, other, gate, loose})
	require.Len(t, got, 4)
	assert.Same(t, call, got[0])
	assert.Equal(t, 2, got[1].Turn)
	assert.Equal(t, "t1", got[1].Gate.Call)
	assert.Same(t, other, got[2])
	assert.Same(t, loose, got[3])

	plain := []*entry{call, other}
	assert.Equal(t, plain, gateUnderCalls(plain))
}
