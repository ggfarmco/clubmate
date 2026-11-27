package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

type guideTranscript struct {
	Entries []struct {
		Kind string               `json:"kind"`
		Step *sdui.GuideStepProps `json:"step"`
	} `json:"entries"`
	Steps *sdui.StepControlsProps `json:"steps"`
	Tour  *sdui.TourMapProps      `json:"tour"`
}

func writeLedger(t *testing.T) string {
	t.Helper()
	concept := func(id, title, status, parent string, links ...string) map[string]any {
		return map[string]any{"id": id, "title": title, "status": status, "parent": parent, "links": links}
	}
	l := map[string]any{"current": "c5", "concepts": []map[string]any{
		concept("c1", "Limiter", "known", ""),
		concept("c2", "Bucket per workspace", "known", "c1"),
		concept("c3", "Lazy create", "shaky", "c2"),
		concept("c4", "Refill", "todo", "c2"),
		concept("c5", "Cap at burst", "todo", "c4"),
		concept("c6", "Spend a token", "todo", "c2"),
		concept("c7", "ErrRateLimited", "todo", "c6"),
		concept("c8", "Global cap", "todo", "", "c7"),
	}}
	raw, err := json.Marshal(l)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "tour.json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	return path
}

func sampleNode(t *testing.T, name string) sdui.Node {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	var n sdui.Node
	require.NoError(t, json.Unmarshal(raw, &n))
	return n
}

func nodeIn(t *testing.T, f *fixture, workspace, slot, typ string, match func(sdui.Node) bool) sdui.Node {
	t.Helper()
	var found sdui.Node
	f.h.WaitSlot(t, workspace, slot, func(n sdui.Node) bool {
		cards := cardsOf(n, typ)
		i := slices.IndexFunc(cards, match)
		if i >= 0 {
			found = cards[i]
		}
		return i >= 0
	})
	return found
}

func anyNode(sdui.Node) bool { return true }

func (f *fixture) guideStep(t *testing.T, workspace, session string, args map[string]any) {
	t.Helper()
	require.NoError(t, f.runAs(t, workspace, session, "agents.guide-step", args, nil))
}

func (f *fixture) phone(t *testing.T, workspace, command string, args map[string]any, out any) error {
	t.Helper()
	return f.h.PluginCallResult(t, "remote", "command.run", map[string]any{"id": command, "workspaceId": workspace, "args": args}, out)
}

func TestGuideStepControlsAndMapFollowTheLedgerAndTheQuiz(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, quizRepo(t))
	parent := f.promptedParent(t, workspace)
	done := f.askQuiz(t, workspace, parent.ID, lineQuiz(nil))
	asking := quizNode(t, f, workspace, inState("asking"))
	quizEvent(t, f, workspace, asking, "quizPick", map[string]any{"from": 32})
	quizEvent(t, f, workspace, asking, "quizRate", map[string]any{"confidence": "think"})
	waitQuiz(t, done)

	f.guideStep(t, workspace, parent.ID, map[string]any{"title": "Cap at burst", "check": false, "concept": "c5", "ledger": writeLedger(t)})
	step := nodeIn(t, f, workspace, "agents.main", "guide.step", anyNode)
	assert.Equal(t, "Step 5 · Cap at burst", step.Props["label"])
	steps := nodeIn(t, f, workspace, "agents.main", "guide.steps", anyNode)
	f.h.Event(t, workspace, "agents.main", steps.ID, "stepAct", map[string]any{"action": "go_deeper"})
	opened := nodeIn(t, f, workspace, "agents.main", "guide.steps", func(n sdui.Node) bool { return n.Props["aside"] != nil })

	want := sampleNode(t, "guide-steps.json")
	want.Props["session"], want.Props["step"] = parent.ID, opened.Props["step"]
	aside := want.Props["aside"].(map[string]any)
	aside["step"], aside["session"] = opened.Props["step"], opened.Props["aside"].(map[string]any)["session"]
	want.Props["action"] = "stepAct"
	assert.JSONEq(t, jsonOf(t, want.Props), jsonOf(t, opened.Props))

	strip := nodeIn(t, f, workspace, "agents.main", "guide.strip", anyNode)
	tour := sampleNode(t, "guide-tour.json")
	tour.Props["session"], tour.Props["action"] = parent.ID, "tourOffer"
	assert.JSONEq(t, jsonOf(t, tour.Props), jsonOf(t, strip.Props))

	f.guideStep(t, workspace, parent.ID, map[string]any{"title": "Spend a token", "check": true, "concept": "c6"})
	nodeIn(t, f, workspace, "agents.main", "guide.step", func(n sdui.Node) bool { return n.Props["label"] == "Step 6 · Spend a token" })
}

func TestGuideStepControlsSendFixedRequestsAndSurviveRestart(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	f.guideStep(t, workspace, parent.ID, map[string]any{"title": "Lazy create", "check": false, "concept": "c3", "ledger": writeLedger(t)})

	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "guide.steps")) == 1 })
	composer := cardsOf(main, "composer")[0]
	assert.Equal(t, "Continue", composer.Props["continue"])
	f.h.Event(t, workspace, "agents.main", composer.ID, "send", map[string]any{"text": ""})
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parent.AgentSessionID, "Continue: go to the next step.")) == 1 }, wait)
	f.waitStatus(t, workspace, parent.ID, "done")

	args := map[string]any{"session": parent.ID, "action": "show_me"}
	asSession := map[string]any{"id": "agents.step-act", "workspaceId": workspace, "args": args, "caller": parent.ID}
	require.ErrorContains(t, f.h.Client(t).Call(t.Context(), "command.run", asSession, nil), "only the user")
	require.NoError(t, f.phone(t, workspace, "agents.step-act", args, nil))
	testutil.Eventually(t, func() bool {
		return len(f.promptsTo(t, parent.AgentSessionID, "Show me: reveal the answer of this step.")) == 1
	}, wait)
	f.waitStatus(t, workspace, parent.ID, "done")
	require.ErrorContains(t, f.phone(t, workspace, "agents.tour-offer", map[string]any{"session": parent.ID, "concept": "c6", "offer": "ask_again"}, nil), "no such offer")
	require.NoError(t, f.phone(t, workspace, "agents.tour-offer", map[string]any{"session": parent.ID, "concept": "c3", "offer": "go_back"}, nil))
	testutil.Eventually(t, func() bool {
		return len(f.promptsTo(t, parent.AgentSessionID, `Go back to step 3: "Lazy create" (c3), and teach it again.`)) == 1
	}, wait)
	f.waitStatus(t, workspace, parent.ID, "done")
	for _, l := range f.logLines(t, parent.ID) {
		if text := string(l["payload"]); string(l["kind"]) == `"session/prompt"` && (strings.Contains(text, "Show me") || strings.Contains(text, "Go back")) {
			assert.NotContains(t, text, `"relayed":true`)
		}
	}

	f.restartPlugin(t, workspace)
	var tr guideTranscript
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": parent.ID}, &tr))
	i := slices.IndexFunc(tr.Entries, func(e struct {
		Kind string               `json:"kind"`
		Step *sdui.GuideStepProps `json:"step"`
	}) bool {
		return e.Kind == "step"
	})
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, "Step 3 · Lazy create", tr.Entries[i].Step.Label)
	require.NotNil(t, tr.Steps)
	assert.Equal(t, "show_me", tr.Steps.Pressed)
	require.NotNil(t, tr.Tour)
	assert.Equal(t, "step 5 of 8", tr.Tour.StepLabel)
}
