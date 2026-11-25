package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

type asideView struct {
	State  string         `json:"state"`
	Label  string         `json:"label"`
	Detail string         `json:"detail"`
	Anchor map[string]any `json:"anchor"`
}

type phonePrefill struct{ Elicitation, Field, Open, Value, Note string }

type phoneMark struct{ Elicitation, Field, Open, Option, ID, State string }

type phonePanel struct {
	Question, Option                 int
	Header, Text, Label, Description string
	Answer, Multi                    bool
}

type asideTranscript struct {
	Prefills     []phonePrefill
	OptionAsides []phoneMark
	Pinned       *sdui.Node
	AsidePanel   *phonePanel
}

func (f *fixture) asideTranscript(t *testing.T, workspace, session string) asideTranscript {
	t.Helper()
	var out asideTranscript
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session}, &out))
	return out
}

type asideResult struct {
	Session  string `json:"session"`
	Reopened bool   `json:"reopened"`
}

func asideScript(steps ...map[string]any) map[string]any {
	modes := map[string]any{"currentModeId": "default", "availableModes": []map[string]any{{"id": "default", "name": "Default"}, {"id": "plan", "name": "Plan"}}}
	return map[string]any{"fork": true, "resume": true, "modes": modes, "prompt": append(steps, map[string]any{"echo": true})}
}

func pairs(kv ...string) []map[string]string {
	out := make([]map[string]string, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, map[string]string{"key": kv[i], "value": kv[i+1]})
	}
	return out
}

func (f *fixture) promptedParent(t *testing.T, workspace string) record {
	t.Helper()
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "hi")
	f.waitStatus(t, workspace, parent, "done")
	return f.waitRecord(t, workspace, parent, "canAside", func(r record) bool { return r.CanAside })
}

func (f *fixture) aside(t *testing.T, workspace, parent string, anchor ...string) asideResult {
	t.Helper()
	var out asideResult
	require.NoError(t, f.run(t, workspace, "agents.aside", map[string]any{"session": parent, "anchor": pairs(anchor...)}, &out))
	require.NotEmpty(t, out.Session)
	return out
}

func (f *fixture) forkOf(t *testing.T, workspace, aside string) string {
	t.Helper()
	return f.waitRecord(t, workspace, aside, "a fork", func(r record) bool { return r.AgentSessionID != "" }).AgentSessionID
}

func setModes(f *fixture, t *testing.T, agentID, mode string) int {
	n := 0
	for _, c := range f.calls(t, "session/set_mode") {
		var p struct {
			SessionID string `json:"sessionId"`
			ModeID    string `json:"modeId"`
		}
		require.NoError(t, json.Unmarshal(c.Params, &p))
		if p.SessionID == agentID && p.ModeID == mode {
			n++
		}
	}
	return n
}

func sessionOf(c fakeCall) string {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(c.Params, &p)
	return p.SessionID
}

func TestAsideForksParent(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	parent := f.promptedParent(t, workspace)

	aside := f.aside(t, workspace, parent.ID, "elicitation", "e1", "option", "Postgres", "question", "Which database?")
	fork := f.forkOf(t, workspace, aside.Session)
	call := f.waitCall(t, "session/fork", func(fakeCall) bool { return true })
	var req struct {
		SessionID string `json:"sessionId"`
		Cwd       string `json:"cwd"`
		Meta      struct {
			ClaudeCode struct {
				Options struct {
					Env             map[string]string `json:"env"`
					DisallowedTools []string          `json:"disallowedTools"`
					StrictMcpConfig bool              `json:"strictMcpConfig"`
				} `json:"options"`
			} `json:"claudeCode"`
		} `json:"_meta"`
	}
	require.NoError(t, json.Unmarshal(call.Params, &req))
	assert.Equal(t, parent.AgentSessionID, req.SessionID)
	assert.Equal(t, parent.Worktree, req.Cwd)
	assert.Equal(t, []string{"ExitPlanMode", "mcp__clubmate", "mcp__clubmate-dev"}, req.Meta.ClaudeCode.Options.DisallowedTools)
	assert.False(t, req.Meta.ClaudeCode.Options.StrictMcpConfig)
	assert.Equal(t, aside.Session, req.Meta.ClaudeCode.Options.Env["CLUBMATE_AGENT_SESSION"])
	f.waitCall(t, "session/set_mode", func(c fakeCall) bool { return setModes(f, t, fork, "plan") == 1 })
	assert.Len(t, f.calls(t, "session/new"), 1)

	got := f.session(t, workspace, aside.Session)
	assert.Equal(t, parent.ID, got.ParentSession)
	assert.False(t, got.Service)
	assert.False(t, got.CanAside)
	require.NotNil(t, got.Aside)
	assert.Equal(t, "open", got.Aside.State)
	assert.Equal(t, "Aside · Postgres", got.Aside.Label)
}

func TestAsideRestartResumesInPlan(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "message", "m1", "quote", "gives way to the title")
	fork := f.forkOf(t, workspace, aside.Session)
	f.waitCall(t, "session/set_mode", func(fakeCall) bool { return setModes(f, t, fork, "plan") == 1 })

	f.restartPlugin(t, workspace)
	f.prompt(t, workspace, aside.Session, "why?")
	f.waitCall(t, "session/resume", func(c fakeCall) bool { return sessionOf(c) == fork })
	f.waitCall(t, "session/set_mode", func(fakeCall) bool { return setModes(f, t, fork, "plan") == 2 })
	f.waitStatus(t, workspace, aside.Session, "done")
	assert.Len(t, f.calls(t, "session/fork"), 1)
	resume := f.waitCall(t, "session/resume", func(c fakeCall) bool { return sessionOf(c) == fork })
	assert.Contains(t, string(resume.Params), `"disallowedTools":["ExitPlanMode","mcp__clubmate","mcp__clubmate-dev"]`)
}

func TestCanAside(t *testing.T) {
	t.Parallel()
	t.Run("no fork capability", func(t *testing.T) {
		t.Parallel()
		f := start(t, map[string]any{"resume": true, "prompt": []map[string]any{{"echo": true}}})
		workspace := f.workspace(t, newRepo(t))
		parent := f.newSession(t, workspace)
		f.prompt(t, workspace, parent, "hi")
		f.waitStatus(t, workspace, parent, "done")
		assert.False(t, f.session(t, workspace, parent).CanAside)
		err := f.run(t, workspace, "agents.aside", map[string]any{"session": parent, "anchor": pairs("quote", "x")}, nil)
		require.ErrorContains(t, err, "fork_unsupported")
	})
	t.Run("never prompted", func(t *testing.T) {
		t.Parallel()
		f := start(t, asideScript())
		workspace := f.workspace(t, newRepo(t))
		parent := f.newSession(t, workspace)
		got := f.waitRecord(t, workspace, parent, "an agent session", func(r record) bool { return r.AgentSessionID != "" })
		assert.False(t, got.CanAside)
	})
}

func TestAsideNeedsYou(t *testing.T) {
	t.Parallel()
	gate := filepath.Join(t.TempDir(), "go")
	f := start(t, asideScript(map[string]any{"match": "aside forked", "wait": gate}, map[string]any{"match": "aside forked", "elicitation": askScript(false)}))
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")
	f.forkOf(t, workspace, aside.Session)

	f.prompt(t, workspace, aside.Session, "ask me")
	thinking := f.waitRecord(t, workspace, aside.Session, "thinking", func(r record) bool { return r.Aside != nil && r.Aside.State == "thinking" })
	assert.False(t, thinking.NeedsYou)
	write(t, gate, "")
	got := f.waitRecord(t, workspace, aside.Session, "asks", func(r record) bool { return r.Aside != nil && r.Aside.State == "asks" })
	assert.True(t, got.NeedsYou)
	assert.False(t, got.AsksParent)
	untouched := f.session(t, workspace, parent.ID)
	assert.False(t, untouched.NeedsYou)
	assert.Equal(t, "done", untouched.Status)
	assert.Empty(t, untouched.ChildStatus)
}

func TestAsideLeavesParentAlone(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_WAKE_DELAY=10ms"}, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")
	fork := f.forkOf(t, workspace, aside.Session)
	f.prompt(t, workspace, aside.Session, "why?")
	f.waitStatus(t, workspace, aside.Session, "done")

	parentPrompts := func() int {
		n := 0
		for _, c := range f.calls(t, "session/prompt") {
			if sessionOf(c) == parent.AgentSessionID {
				n++
			}
		}
		return n
	}
	testutil.Never(t, func() bool { return parentPrompts() != 1 }, time.Second)
	for _, c := range f.calls(t, "session/prompt") {
		if sessionOf(c) == fork {
			assert.NotContains(t, string(c.Params), "Clubmate finish mode")
		}
	}
	var listed []record
	require.NoError(t, f.runAs(t, workspace, parent.ID, "agents.sessions", map[string]any{}, &listed))
	assert.False(t, slices.ContainsFunc(listed, func(r record) bool { return r.ID == aside.Session }))
	require.Error(t, f.runAs(t, workspace, parent.ID, "agents.transcript", map[string]any{"session": aside.Session}, nil))
}

func TestAsideNeedsPlanMode(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"fork": true, "resume": true, "prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")

	got := f.waitStatus(t, workspace, aside.Session, "error")
	assert.Contains(t, got.Error, "plan mode")
	f.waitCall(t, "session/fork", func(fakeCall) bool { return true })
	f.waitCall(t, "session/close", func(c fakeCall) bool { return strings.HasPrefix(sessionOf(c), "fork-") })
}

func TestAsideOnePerAnchor(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	client := f.h.Client(t)
	anchor := pairs("elicitation", "e1", "option", "Postgres")
	results := make([]asideResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() {
			errs[i] = client.Call(t.Context(), "command.run", map[string]any{"id": "agents.aside", "workspaceId": workspace, "args": map[string]any{"session": parent.ID, "anchor": anchor}}, &results[i])
		})
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	assert.Equal(t, results[0].Session, results[1].Session)
	assert.True(t, results[0].Reopened != results[1].Reopened)
	reworded := f.aside(t, workspace, parent.ID, "elicitation", "e1", "option", "Postgres", "question", "Which store?")
	assert.Equal(t, results[0].Session, reworded.Session)
	assert.True(t, reworded.Reopened)
	other := f.aside(t, workspace, parent.ID, "elicitation", "e1", "option", "SQLite")
	assert.NotEqual(t, results[0].Session, other.Session)
	assert.False(t, other.Reopened)
}

func TestAsideSeedFrame(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "elicitation", "e1", "option", "Postgres", "question", "Which database?")
	fork := f.forkOf(t, workspace, aside.Session)

	f.prompt(t, workspace, aside.Session, "why?")
	f.waitStatus(t, workspace, aside.Session, "done")
	f.prompt(t, workspace, aside.Session, "and then?")
	f.waitTurns(t, aside.Session, func(turns []api.Turn) bool { return len(turns) == 2 })
	var texts [][]string
	for _, c := range f.calls(t, "session/prompt") {
		if sessionOf(c) != fork {
			continue
		}
		var req struct {
			Prompt []struct {
				Text string `json:"text"`
			} `json:"prompt"`
		}
		require.NoError(t, json.Unmarshal(c.Params, &req))
		var got []string
		for _, b := range req.Prompt {
			got = append(got, b.Text)
		}
		texts = append(texts, got)
	}
	require.Len(t, texts, 2)
	require.Len(t, texts[0], 2)
	assert.Equal(t, `You are an aside forked from this session to discuss option "Postgres" of the question "Which database?". Don't continue the session's task, don't edit files, don't land or start sessions; answer the user.`, texts[0][0])
	assert.Equal(t, "why?", texts[0][1])
	assert.Equal(t, "and then?", texts[1][0])
	users := ofKind(f.transcript(t, workspace, aside.Session).Entries, "user")
	require.Len(t, users, 2)
	assert.Equal(t, "why?", users[0].Text)
}

func TestAsideCloseDeletes(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")
	fork := f.forkOf(t, workspace, aside.Session)

	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": aside.Session}, nil))
	assert.Empty(t, f.session(t, workspace, aside.Session).ID)
	f.waitCall(t, "session/close", func(c fakeCall) bool { return sessionOf(c) == fork })
	f.waitCall(t, "session/delete", func(c fakeCall) bool { return sessionOf(c) == fork })
	assert.Equal(t, "done", f.session(t, workspace, parent.ID).Status)
}

func TestParentCloseDeletesAsides(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")
	f.forkOf(t, workspace, aside.Session)

	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": parent.ID}, nil))
	assert.Equal(t, "closed", f.session(t, workspace, parent.ID).Status)
	assert.Empty(t, f.session(t, workspace, aside.Session).ID)
}

func TestAsideBusyUnsent(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")
	f.forkOf(t, workspace, aside.Session)
	var busy struct {
		Busy    bool   `json:"busy"`
		Message string `json:"message"`
	}
	require.NoError(t, f.run(t, workspace, "agents.busy", map[string]any{"session": aside.Session}, &busy))
	assert.False(t, busy.Busy)

	f.prompt(t, workspace, aside.Session, "why?")
	f.waitStatus(t, workspace, aside.Session, "done")
	require.NoError(t, f.run(t, workspace, "agents.busy", map[string]any{"session": aside.Session}, &busy))
	assert.True(t, busy.Busy)
	assert.Equal(t, "Close without sending?", busy.Message)
}

func (f *fixture) askingParent(t *testing.T, workspace string) (record, string) {
	t.Helper()
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "pick")
	f.waitStatus(t, workspace, parent, "waiting")
	asks := ofKind(f.transcript(t, workspace, parent).Entries, "elicitation")
	require.Len(t, asks, 1)
	return f.waitRecord(t, workspace, parent, "canAside", func(r record) bool { return r.CanAside }), asks[0].ID
}

func (f *fixture) answerAsk(t *testing.T, workspace, session, ask, value string) {
	t.Helper()
	choice := session + "/elicitation/" + ask + "/f/0"
	f.h.Event(t, workspace, "agents.main", choice, "elicitationPick", map[string]any{choice: value})
}

func optionOn(n sdui.Node, value string) bool {
	options, _ := n.Props["options"].([]any)
	for _, o := range options {
		if m, _ := o.(map[string]any); m["value"] == value {
			return m["on"] == true
		}
	}
	return false
}

func otherText(n sdui.Node) string {
	other, _ := n.Props["other"].(map[string]any)
	if other["on"] != true {
		return ""
	}
	text, _ := other["value"].(string)
	return text
}

func elicitChoice(n sdui.Node) (sdui.Node, bool) {
	for _, form := range cardsOf(n, "form") {
		if form.Props["action"] == "elicitation" {
			if choices := cardsOf(form, "choice"); len(choices) > 0 {
				return choices[0], true
			}
		}
	}
	return sdui.Node{}, false
}

var askingScript = asideScript(map[string]any{"match": "pick", "elicitation": askScript(false)})

func TestAsideSendAnswerPrefills(t *testing.T) {
	t.Parallel()
	f := start(t, askingScript)
	workspace := f.workspace(t, newRepo(t))
	parent, ask := f.askingParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "elicitation", ask, "field", "0", "option", "Postgres", "question", "Which store?")
	f.forkOf(t, workspace, aside.Session)

	require.NoError(t, f.run(t, workspace, "agents.aside-send", map[string]any{"session": aside.Session, "to": "answer", "text": "too heavy"}, nil))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		c, ok := elicitChoice(n)
		return ok && optionOn(c, "Postgres") && otherText(c) == "too heavy"
	})
	assert.Empty(t, f.calls(t, "elicitation_response"))
	assert.Equal(t, "waiting", f.session(t, workspace, parent.ID).Status)
	assert.Empty(t, f.session(t, workspace, aside.Session).ID)
}

func TestAsideSendAnswerStale(t *testing.T) {
	t.Parallel()
	f := start(t, askingScript)
	workspace := f.workspace(t, newRepo(t))
	parent, ask := f.askingParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "elicitation", ask, "field", "0", "option", "Postgres")
	f.forkOf(t, workspace, aside.Session)
	f.answerAsk(t, workspace, parent.ID, ask, "Redis")
	f.waitCall(t, "elicitation_response", func(fakeCall) bool { return true })

	err := f.run(t, workspace, "agents.aside-send", map[string]any{"session": aside.Session, "to": "answer", "text": "too heavy"}, nil)
	require.ErrorContains(t, err, "the question is no longer pending")
	assert.Equal(t, aside.Session, f.session(t, workspace, aside.Session).ID)
	assert.Nil(t, f.session(t, workspace, parent.ID).ComposerDraft)
}

func TestAsideSendDraft(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "message", "m1", "quote", "the title")
	f.forkOf(t, workspace, aside.Session)
	f.prompt(t, workspace, aside.Session, "why?")
	f.waitStatus(t, workspace, aside.Session, "done")
	keep := map[string]any{"session": aside.Session, "to": "draft", "text": "too vague", "close": false}

	require.NoError(t, f.run(t, workspace, "agents.aside-send", keep, nil))
	draft := f.session(t, workspace, parent.ID).ComposerDraft
	require.NotNil(t, draft)
	assert.Equal(t, "> the title\n\ntoo vague", draft.Text)
	assert.Equal(t, int64(1), draft.Rev)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		c := cardsOf(n, "composer")
		if len(c) != 1 {
			return false
		}
		d, _ := c[0].Props["draft"].(map[string]any)
		return d["text"] == "> the title\n\ntoo vague"
	})
	got := f.session(t, workspace, aside.Session)
	assert.Equal(t, "sent", got.Aside.State)
	var busy struct {
		Busy bool `json:"busy"`
	}
	require.NoError(t, f.run(t, workspace, "agents.busy", map[string]any{"session": aside.Session}, &busy))
	assert.False(t, busy.Busy)

	require.NoError(t, f.run(t, workspace, "agents.aside-send", keep, nil))
	assert.Equal(t, int64(2), f.session(t, workspace, parent.ID).ComposerDraft.Rev)
	f.prompt(t, workspace, parent.ID, "next")
	assert.Nil(t, f.session(t, workspace, parent.ID).ComposerDraft)
	f.waitStatus(t, workspace, parent.ID, "done")

	require.NoError(t, f.run(t, workspace, "agents.aside-send", keep, nil))
	assert.Equal(t, int64(3), f.session(t, workspace, parent.ID).ComposerDraft.Rev)
}

func TestAsideSendOpenItems(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	f.postItems(t, workspace, parent.ID, decision("Lists"))
	aside := f.aside(t, workspace, parent.ID, "open", "oi-1", "option", "Two", "question", "Which Lists?")
	f.forkOf(t, workspace, aside.Session)

	require.NoError(t, f.run(t, workspace, "agents.aside-send", map[string]any{"session": aside.Session, "to": "answer", "text": "but simpler"}, nil))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		items := nodesWithID(n, parent.ID+"/open/oi-1")
		return len(items) == 1 && optionOn(items[0], "Two") && otherText(items[0]) == "but simpler"
	})
}

func TestAsidePinnedStrip(t *testing.T) {
	t.Parallel()
	f := start(t, askingScript)
	workspace := f.workspace(t, newRepo(t))
	parent, ask := f.askingParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "elicitation", ask, "field", "0", "option", "Postgres")
	f.forkOf(t, workspace, aside.Session)
	pinned := aside.Session + "/pinned"

	require.NoError(t, f.run(t, workspace, "agents.reveal", map[string]any{"session": aside.Session}, nil))
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(nodesWithID(n, pinned)) == 1 })
	strip := nodesWithID(main, pinned)[0]
	assert.Equal(t, true, strip.Props["pinned"])
	assert.Equal(t, "asidePick", strip.Props["action"])
	f.h.Event(t, workspace, "agents.main", pinned, "asidePick", "Redis")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		s := nodesWithID(n, pinned)
		return len(s) == 1 && optionOn(s[0], "Redis")
	})

	require.NoError(t, f.run(t, workspace, "agents.reveal", map[string]any{"session": parent.ID}, nil))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		c, ok := elicitChoice(n)
		return ok && optionOn(c, "Redis")
	})
	f.answerAsk(t, workspace, parent.ID, ask, "Redis")
	f.waitStatus(t, workspace, parent.ID, "done")
	require.NoError(t, f.run(t, workspace, "agents.reveal", map[string]any{"session": aside.Session}, nil))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return len(nodesWithID(n, aside.Session+"/transcript")) > 0 && len(nodesWithID(n, pinned)) == 0
	})
}

func TestAsideSendAnswerRules(t *testing.T) {
	t.Parallel()
	onlyChoice := askScript(false)
	delete(onlyChoice["requestedSchema"].(map[string]any)["properties"].(map[string]any), "question_0_custom")
	t.Run("a card that answers on pick", func(t *testing.T) {
		t.Parallel()
		f := start(t, asideScript(map[string]any{"match": "pick", "elicitation": onlyChoice}))
		workspace := f.workspace(t, newRepo(t))
		parent, ask := f.askingParent(t, workspace)
		aside := f.aside(t, workspace, parent.ID, "elicitation", ask, "field", "0", "option", "Postgres")
		f.forkOf(t, workspace, aside.Session)
		err := f.run(t, workspace, "agents.aside-send", map[string]any{"session": aside.Session, "to": "answer", "text": "x"}, nil)
		require.ErrorContains(t, err, "this question can't be prefilled; send it as a draft")
		require.NoError(t, f.run(t, workspace, "agents.reveal", map[string]any{"session": aside.Session}, nil))
		f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
			return len(nodesWithID(n, aside.Session+"/transcript")) > 0 && len(nodesWithID(n, aside.Session+"/pinned")) == 0
		})
	})
	t.Run("multi-select", func(t *testing.T) {
		t.Parallel()
		f := start(t, asideScript(map[string]any{"match": "pick", "elicitation": askScript(true)}))
		workspace := f.workspace(t, newRepo(t))
		parent, ask := f.askingParent(t, workspace)
		aside := f.aside(t, workspace, parent.ID, "elicitation", ask, "field", "0", "option", "Redis")
		f.forkOf(t, workspace, aside.Session)
		require.NoError(t, f.run(t, workspace, "agents.aside-send", map[string]any{"session": aside.Session, "to": "answer", "text": "as a cache"}, nil))
		f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
			c, ok := elicitChoice(n)
			return ok && optionOn(c, "Redis") && otherText(c) == ""
		})
		assert.Equal(t, "as a cache", f.session(t, workspace, parent.ID).ComposerDraft.Text)
	})
}

func TestOptionAsideMark(t *testing.T) {
	t.Parallel()
	t.Run("question card", func(t *testing.T) {
		t.Parallel()
		f := start(t, askingScript)
		workspace := f.workspace(t, newRepo(t))
		parent, ask := f.askingParent(t, workspace)
		aside := f.aside(t, workspace, parent.ID, "elicitation", ask, "field", "0", "option", "Postgres")
		main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
			c, ok := elicitChoice(n)
			return ok && optionAside(c, "Postgres") != nil
		})
		c, _ := elicitChoice(main)
		assert.Equal(t, map[string]any{"id": aside.Session, "state": "open"}, optionAside(c, "Postgres"))
		assert.Nil(t, optionAside(c, "Redis"))
		assert.Equal(t, "asideAsk", c.Props["asideAction"])

		f.h.Event(t, workspace, "agents.main", c.ID, "asideAsk", "Redis")
		f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
			c, ok := elicitChoice(n)
			return ok && optionAside(c, "Redis") != nil
		})
		reopened := f.aside(t, workspace, parent.ID, "elicitation", ask, "field", "0", "option", "Redis", "question", "Store")
		assert.True(t, reopened.Reopened)
	})
	t.Run("open items card", func(t *testing.T) {
		t.Parallel()
		f := start(t, asideScript())
		workspace := f.workspace(t, newRepo(t))
		parent := f.promptedParent(t, workspace)
		f.postItems(t, workspace, parent.ID, decision("Lists"))
		aside := f.aside(t, workspace, parent.ID, "open", "oi-1", "option", "Two")
		f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
			items := nodesWithID(n, parent.ID+"/open/oi-1")
			if len(items) != 1 {
				return false
			}
			mark := optionAside(items[0], "Two")
			return mark != nil && mark["id"] == aside.Session && items[0].Props["asideAction"] == "asideAsk"
		})
	})
}

func optionAside(n sdui.Node, value string) map[string]any {
	options, _ := n.Props["options"].([]any)
	for _, o := range options {
		if m, _ := o.(map[string]any); m["value"] == value {
			mark, _ := m["aside"].(map[string]any)
			return mark
		}
	}
	return nil
}

func (f *fixture) allCalls(t *testing.T) []fakeCall {
	t.Helper()
	data, err := os.ReadFile(f.fakeLog)
	require.NoError(t, err)
	var out []fakeCall
	for line := range strings.Lines(string(data)) {
		var c fakeCall
		if json.Unmarshal([]byte(line), &c) == nil {
			out = append(out, c)
		}
	}
	return out
}

func TestAsideKeep(t *testing.T) {
	t.Parallel()
	script := asideScript()
	script["modes"].(map[string]any)["availableModes"] = append(script["modes"].(map[string]any)["availableModes"].([]map[string]any), map[string]any{"id": "acceptEdits", "name": "Accept edits"})
	f := start(t, script)
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.mode", map[string]any{"session": parent.ID, "mode": "acceptEdits"}, nil))
	f.waitRecord(t, workspace, parent.ID, "acceptEdits", func(r record) bool { return r.Mode == "acceptEdits" })
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")
	fork := f.forkOf(t, workspace, aside.Session)
	f.prompt(t, workspace, aside.Session, "why?")
	f.waitStatus(t, workspace, aside.Session, "done")
	before := len(f.allCalls(t))

	require.NoError(t, f.run(t, workspace, "agents.aside-keep", map[string]any{"session": aside.Session}, nil))
	f.waitCall(t, "session/set_mode", func(fakeCall) bool { return setModes(f, t, fork, "acceptEdits") == 1 })
	var order []string
	for _, c := range f.allCalls(t)[before:] {
		if sessionOf(c) == fork && (c.Method == "session/close" || c.Method == "session/resume" || c.Method == "session/set_mode") {
			order = append(order, c.Method)
			if c.Method == "session/resume" {
				assert.NotContains(t, string(c.Params), "disallowedTools")
			}
		}
	}
	assert.Equal(t, []string{"session/close", "session/resume", "session/set_mode"}, order)
	kept := f.session(t, workspace, aside.Session)
	assert.Empty(t, kept.ParentSession)
	assert.Equal(t, parent.ID, kept.ForkedFrom)
	assert.Nil(t, kept.Aside)
	assert.Equal(t, "“the title”", kept.Title)

	f.prompt(t, workspace, aside.Session, "go on")
	f.waitTurns(t, aside.Session, func(turns []api.Turn) bool { return len(turns) == 2 })
	var last string
	for _, c := range f.calls(t, "session/prompt") {
		if sessionOf(c) == fork {
			last = string(c.Params)
		}
	}
	assert.Contains(t, last, `You are now a standalone session in `+parent.Worktree+`; you don't own the task of \"hi\"; don't land it.`)

	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": aside.Session}, nil))
	assert.Equal(t, "closed", f.session(t, workspace, aside.Session).Status)
}

func TestAsideKeepBusy(t *testing.T) {
	t.Parallel()
	gate := filepath.Join(t.TempDir(), "go")
	f := start(t, asideScript(map[string]any{"match": "aside forked", "wait": gate}))
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")
	f.forkOf(t, workspace, aside.Session)
	f.prompt(t, workspace, aside.Session, "why?")
	f.waitStatus(t, workspace, aside.Session, "running")

	err := f.run(t, workspace, "agents.aside-keep", map[string]any{"session": aside.Session}, nil)
	require.ErrorContains(t, err, "the aside is busy")
	assert.Equal(t, parent.ID, f.session(t, workspace, aside.Session).ParentSession)
	write(t, gate, "")
}

func TestAsideStaysInPlanMode(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")
	fork := f.forkOf(t, workspace, aside.Session)
	f.waitCall(t, "session/set_mode", func(fakeCall) bool { return setModes(f, t, fork, "plan") == 1 })

	err := f.run(t, workspace, "agents.mode", map[string]any{"session": aside.Session, "mode": "default"}, nil)
	require.ErrorContains(t, err, "an aside stays in plan mode")
	assert.Zero(t, setModes(f, t, fork, "default"))
	require.ErrorContains(t, f.run(t, workspace, "agents.handoff", map[string]any{"session": aside.Session}, nil), "can't be handed off")
	require.NoError(t, f.run(t, workspace, "agents.reveal", map[string]any{"session": aside.Session}, nil))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		c := cardsOf(n, "composer")
		return len(nodesWithID(n, aside.Session+"/transcript")) > 0 && len(c) == 1 && c[0].Props["modes"] == nil
	})
}

func TestParentHandoffDeletesAsides(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")
	f.forkOf(t, workspace, aside.Session)

	require.NoError(t, f.run(t, workspace, "agents.handoff", map[string]any{"session": parent.ID}, nil))
	f.waitStatus(t, workspace, parent.ID, "closed")
	testutil.Eventually(t, func() bool { return f.session(t, workspace, aside.Session).ID == "" }, wait)
	err := f.run(t, workspace, "agents.aside-send", map[string]any{"session": aside.Session, "to": "draft", "text": "x"}, nil)
	require.Error(t, err)
}

func TestAutoCloseWaitsForAsides(t *testing.T) {
	t.Parallel()
	gate := filepath.Join(t.TempDir(), "go")
	hold := filepath.Join(t.TempDir(), "close")
	f := startEnv(t, []string{"CLUBMATE_AGENTS_CLOSE_AFTER=300ms", "CLUBMATE_AGENTS_CLOSE_GATE=" + hold}, asideScript(map[string]any{"match": "aside forked", "wait": gate}),
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")
	f.forkOf(t, workspace, aside.Session)
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(hold + ".held")
		return err == nil
	}, wait)
	f.prompt(t, workspace, aside.Session, "why?")
	f.waitStatus(t, workspace, aside.Session, "running")
	write(t, hold, "")

	testutil.Never(t, func() bool { return f.session(t, workspace, parent.ID).Status == "closed" }, 2*time.Second)
	write(t, gate, "")
	f.waitStatus(t, workspace, parent.ID, "closed")
	testutil.Eventually(t, func() bool { return f.session(t, workspace, aside.Session).ID == "" }, wait)
}

func TestRevealUnfoldsAsides(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	var last asideResult
	for _, quote := range []string{"one", "two", "three"} {
		last = f.aside(t, workspace, parent.ID, "quote", quote)
	}
	require.NoError(t, f.run(t, workspace, "agents.reveal", map[string]any{"session": last.Session}, nil))
	testutil.Eventually(t, func() bool {
		item := f.sidebarItem(t, workspace, parent.ID)
		i := slices.IndexFunc(item.Children, func(c sdui.TreeItem) bool { return c.ID == "wt:asides:"+parent.ID })
		return i >= 0 && item.Children[i].Expanded && slices.ContainsFunc(item.Children[i].Children, func(c sdui.TreeItem) bool { return c.ID == last.Session })
	}, wait)
}

func TestRefusedPromptKeepsTheDraft(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "quote", "the title")
	require.NoError(t, f.run(t, workspace, "agents.aside-send", map[string]any{"session": aside.Session, "to": "draft", "text": "why"}, nil))
	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": parent.ID}, nil))

	require.Error(t, f.run(t, workspace, "agents.prompt", map[string]any{"session": parent.ID, "text": "next"}, nil))
	draft := f.session(t, workspace, parent.ID).ComposerDraft
	require.NotNil(t, draft)
	assert.Equal(t, "> the title\n\nwhy", draft.Text)
}

func TestAsidePhoneFields(t *testing.T) {
	t.Parallel()
	f := start(t, askingScript)
	workspace := f.workspace(t, newRepo(t))
	parent, ask := f.askingParent(t, workspace)
	aside := f.aside(t, workspace, parent.ID, "elicitation", ask, "field", "0", "option", "Postgres")
	f.forkOf(t, workspace, aside.Session)
	rec := f.session(t, workspace, aside.Session)
	assert.Equal(t, "Store", rec.Aside.Anchor["question"])
	assert.Equal(t, "open", rec.Aside.Detail)

	own := f.asideTranscript(t, workspace, aside.Session)
	assert.Equal(t, &phonePanel{Question: 1, Option: 1, Text: "Store", Label: "Postgres", Description: "Relational", Answer: true}, own.AsidePanel)
	require.NotNil(t, own.Pinned)
	assert.Equal(t, true, own.Pinned.Props["pinned"])

	require.NoError(t, f.run(t, workspace, "agents.aside-send", map[string]any{"session": aside.Session, "to": "answer", "text": "too heavy", "close": false}, nil))
	got := f.asideTranscript(t, workspace, parent.ID)
	assert.Equal(t, []phonePrefill{{Elicitation: ask, Field: "question_0", Value: "Postgres", Note: "too heavy"}}, got.Prefills)
	assert.Equal(t, []phoneMark{{Elicitation: ask, Field: "question_0", Option: "Postgres", ID: aside.Session, State: "sent"}}, got.OptionAsides)
	assert.Equal(t, "sent", f.session(t, workspace, aside.Session).Aside.Detail)

	var agent asideTranscript
	require.NoError(t, f.runAs(t, workspace, parent.ID, "agents.transcript", map[string]any{"session": parent.ID}, &agent))
	assert.Equal(t, asideTranscript{}, agent)
}

func TestAsidePhoneOpenItems(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	f.postItems(t, workspace, parent.ID, decision("Lists"))
	aside := f.aside(t, workspace, parent.ID, "open", "oi-1", "option", "Two")
	f.forkOf(t, workspace, aside.Session)
	assert.Equal(t, "Which Lists?", f.session(t, workspace, aside.Session).Aside.Anchor["question"])
	assert.Equal(t, &phonePanel{Question: 1, Option: 2, Header: "Lists", Text: "Which Lists?", Label: "Two", Description: "Second", Answer: true}, f.asideTranscript(t, workspace, aside.Session).AsidePanel)

	require.NoError(t, f.run(t, workspace, "agents.aside-send", map[string]any{"session": aside.Session, "to": "answer", "text": "but simpler", "close": false}, nil))
	got := f.asideTranscript(t, workspace, parent.ID)
	assert.Equal(t, []phonePrefill{{Open: "oi-1", Value: "Two", Note: "but simpler"}}, got.Prefills)
	assert.Equal(t, []phoneMark{{Open: "oi-1", Option: "Two", ID: aside.Session, State: "sent"}}, got.OptionAsides)
}
