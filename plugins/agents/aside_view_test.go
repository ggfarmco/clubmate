package main_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const summaryText = "Postgres is too heavy for one table."

var summaryScript = asideScript(
	map[string]any{"match": "pick", "elicitation": askScript(false)},
	map[string]any{"match": "Summarize", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": summaryText}}},
)

func (f *fixture) askBeside(t *testing.T, workspace string) (record, string, string) {
	t.Helper()
	parent, ask := f.askingParent(t, workspace)
	choice := parent.ID + "/elicitation/" + ask + "/f/0"
	f.h.Event(t, workspace, "agents.main", choice, "asideAsk", "Postgres")
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		c, ok := elicitChoice(n)
		return ok && optionAside(c, "Postgres") != nil
	})
	c, _ := elicitChoice(main)
	aside, _ := optionAside(c, "Postgres")["id"].(string)
	require.NotEmpty(t, aside)
	f.forkOf(t, workspace, aside)
	return parent, ask, aside
}

func shows(id string) func(sdui.Node) bool {
	return func(n sdui.Node) bool { return len(nodesWithID(n, id)) > 0 }
}

func TestAsideOpensBesideAndExpands(t *testing.T) {
	t.Parallel()
	f := start(t, askingScript)
	workspace := f.workspace(t, newRepo(t))
	parent, _, aside := f.askBeside(t, workspace)

	beside := f.h.WaitSlot(t, workspace, "agents.aside", shows(aside+"/asideFull"))
	assert.Equal(t, "asideFull", nodesWithID(beside, aside+"/asideFull")[0].Props["action"])
	assert.Empty(t, nodesWithID(beside, aside+"/pinned"))
	assert.NotEmpty(t, nodesWithID(beside, aside+"/forked"))
	f.h.WaitSlot(t, workspace, "agents.main", shows(parent.ID+"/asideNext"))

	f.h.Event(t, workspace, "agents.aside", aside+"/asideFull", "asideFull", nil)
	f.h.WaitSlot(t, workspace, "agents.main", shows(aside+"/pinned"))
	f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool { return n.Type == "empty" })

	f.h.Event(t, workspace, "agents.main", aside+"/asideFull", "asideBeside", nil)
	f.h.WaitSlot(t, workspace, "agents.aside", shows(aside+"/asideFull"))
	f.h.WaitSlot(t, workspace, "agents.main", shows(parent.ID+"/asideNext"))

	f.h.Event(t, workspace, "agents.aside", aside+"/asideClose", "asideClose", nil)
	f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool { return n.Type == "empty" })
	assert.Empty(t, f.session(t, workspace, aside).ID)
}

func TestAsideSendBackPanel(t *testing.T) {
	t.Parallel()
	f := start(t, summaryScript)
	workspace := f.workspace(t, newRepo(t))
	parent, _, aside := f.askBeside(t, workspace)
	f.prompt(t, workspace, aside, "why not?")
	f.waitStatus(t, workspace, aside, "done")

	f.h.Event(t, workspace, "agents.aside", aside+"/sendBackButton", "sendBackOpen", nil)
	form := aside + "/sendBack/form"
	panel := f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool {
		text := nodesWithID(n, form+"/text")
		if len(text) != 1 {
			return false
		}
		value, _ := text[0].Props["value"].(string)
		return strings.HasPrefix(value, summaryText)
	})
	to := nodesWithID(panel, form+"/to")[0]
	assert.True(t, optionOn(to, "answer"))

	f.h.Event(t, workspace, "agents.aside", form+"/send", "sendBackSend", map[string]any{form + "/to": "answer", form + "/text": "too heavy", form + "/close": true})
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		c, ok := elicitChoice(n)
		return ok && optionOn(c, "Postgres") && otherText(c) == "too heavy"
	})
	f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool { return n.Type == "empty" })
	assert.Equal(t, "waiting", f.session(t, workspace, parent.ID).Status)
}

func TestAsideSendBackAfterTheQuestionEnded(t *testing.T) {
	t.Parallel()
	f := start(t, askingScript)
	workspace := f.workspace(t, newRepo(t))
	parent, ask, aside := f.askBeside(t, workspace)
	f.answerAsk(t, workspace, parent.ID, ask, "Redis")
	f.waitStatus(t, workspace, parent.ID, "done")
	f.prompt(t, workspace, aside, "why not?")
	f.waitStatus(t, workspace, aside, "done")

	require.NoError(t, f.run(t, workspace, "agents.reveal", map[string]any{"session": aside}, nil))
	f.h.Event(t, workspace, "agents.main", aside+"/sendBackButton", "sendBackOpen", nil)
	form := aside + "/sendBack/form"
	panel := f.h.WaitSlot(t, workspace, "agents.main", shows(form+"/to"))
	to := nodesWithID(panel, form+"/to")[0]
	options, _ := to.Props["options"].([]any)
	require.Len(t, options, 1)
	assert.Equal(t, "draft", options[0].(map[string]any)["value"])

	f.h.Event(t, workspace, "agents.main", aside+"/sendBack", "sendBackKey", "esc")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(nodesWithID(n, form+"/to")) == 0 })
}

func TestAskAsideAboutAPassage(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	agent := ofKind(f.transcript(t, workspace, parent.ID).Entries, "agent")
	require.NotEmpty(t, agent)
	main := f.h.WaitSlot(t, workspace, "agents.main", shows(parent.ID+"/transcript"))
	assert.Equal(t, "asideQuote", nodesWithID(main, parent.ID+"/transcript")[0].Props["asideAction"])

	f.h.Event(t, workspace, "agents.main", parent.ID+"/msg/agent/"+agent[0].ID, "asideQuote", map[string]any{"quote": "hi"})
	beside := f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool { return n.Type != "empty" })
	id, _ := strings.CutSuffix(forkedID(beside), "/forked")
	require.NotEmpty(t, id)
	got := f.session(t, workspace, id)
	require.NotNil(t, got.Aside)
	assert.Equal(t, agent[0].ID, got.Aside.Anchor["message"])
	assert.Equal(t, "hi", got.Aside.Anchor["quote"])
	assert.Contains(t, nodesWithID(beside, id+"/forked")[0].Children, sdui.Markdown("hi"))
}

func forkedID(n sdui.Node) string {
	if strings.HasSuffix(n.ID, "/forked") {
		return n.ID
	}
	for _, c := range n.Children {
		if id := forkedID(c); id != "" {
			return id
		}
	}
	return ""
}

func TestAThenANumberAsksAsideWithoutPicking(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	f.postItems(t, workspace, parent.ID, decision("Lists"))
	keys := parent.ID + "/open/keys"
	armed := func(n sdui.Node) bool {
		k := nodesWithID(n, keys)
		return len(k) == 1 && k[0].Props["hints"] == "1–4 ask aside about · ⎋ cancel"
	}
	f.h.WaitSlot(t, workspace, "agents.main", shows(keys))

	f.h.Event(t, workspace, "agents.main", keys, "openKey", "a")
	f.h.WaitSlot(t, workspace, "agents.main", armed)
	f.h.Event(t, workspace, "agents.main", keys, "openKey", "esc")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(nodesWithID(n, keys)) == 1 && !armed(n) })
	f.h.Event(t, workspace, "agents.main", keys, "openKey", "a")
	f.h.Event(t, workspace, "agents.main", keys, "openKey", "j")
	f.h.Event(t, workspace, "agents.main", keys, "openKey", "a")
	f.h.Event(t, workspace, "agents.main", keys, "openKey", "2")
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		items := nodesWithID(n, parent.ID+"/open/oi-1")
		return len(items) == 1 && optionAside(items[0], "Two") != nil
	})
	item := nodesWithID(main, parent.ID+"/open/oi-1")[0]
	assert.False(t, optionOn(item, "Two"))
	assert.Nil(t, optionAside(item, "One"))
	id, _ := optionAside(item, "Two")["id"].(string)
	f.h.WaitSlot(t, workspace, "agents.aside", shows(id+"/asideFull"))
}

func TestAOnTheOpenQueueShowsTheAsideBeside(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	f.postItems(t, workspace, parent.ID, decision("Lists"))
	f.h.Event(t, workspace, "agents.sidebar", "filter", "filter", "open")
	f.h.WaitSlot(t, workspace, "agents.main", shows("openQueue"))

	f.h.Event(t, workspace, "agents.main", "openQueue", "openKey", "a")
	f.h.Event(t, workspace, "agents.main", "openQueue", "openKey", "2")
	beside := f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool { return n.Type != "empty" })
	id, _ := strings.CutSuffix(forkedID(beside), "/forked")
	f.h.WaitSlot(t, workspace, "agents.main", shows(parent.ID+"/asideNext"))
	assert.Equal(t, "Two", f.session(t, workspace, id).Aside.Anchor["option"])
}

func TestKeepFromTheAsideHeader(t *testing.T) {
	t.Parallel()
	f := start(t, askingScript)
	workspace := f.workspace(t, newRepo(t))
	parent, _, aside := f.askBeside(t, workspace)

	f.h.Event(t, workspace, "agents.aside", aside+"/asideKeep", "asideKeep", nil)
	main := f.h.WaitSlot(t, workspace, "agents.main", shows(aside+"/forked"))
	assert.Equal(t, "asideForked", nodesWithID(main, aside+"/forked")[0].Props["action"])
	f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool { return n.Type == "empty" })
	kept := f.session(t, workspace, aside)
	assert.Nil(t, kept.Aside)
	assert.Equal(t, parent.ID, kept.ForkedFrom)
}

func TestSendBackFromAFreshAsideTakesTheSummary(t *testing.T) {
	t.Parallel()
	f := start(t, askingScript)
	workspace := f.workspace(t, newRepo(t))
	_, _, aside := f.askBeside(t, workspace)

	f.h.Event(t, workspace, "agents.aside", aside+"/sendBackButton", "sendBackOpen", nil)
	form := aside + "/sendBack/form"
	f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool {
		text := nodesWithID(n, form+"/text")
		if len(text) != 1 {
			return false
		}
		value, _ := text[0].Props["value"].(string)
		return value != ""
	})
}

func panelText(n sdui.Node, form string) string {
	text := nodesWithID(n, form+"/text")
	if len(text) != 1 {
		return ""
	}
	value, _ := text[0].Props["value"].(string)
	return value
}

func (f *fixture) asideLabelled(t *testing.T, workspace, label string) string {
	t.Helper()
	var id string
	require.True(t, testutil.Poll(func() bool {
		var list []record
		if f.run(t, workspace, "agents.sessions", map[string]any{}, &list) != nil {
			return false
		}
		for _, r := range list {
			if r.Aside != nil && r.Aside.Label == label {
				id = r.ID
				return true
			}
		}
		return false
	}, testutil.Timeout(10*time.Second)), "an aside labelled %q", label)
	return id
}

func TestAThenOSendsBackAsTheOtherText(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	f.postItems(t, workspace, parent.ID, decision("Lists"))
	keys := parent.ID + "/open/keys"
	f.h.WaitSlot(t, workspace, "agents.main", shows(keys))
	f.h.Event(t, workspace, "agents.main", keys, "openKey", "a")
	f.h.Event(t, workspace, "agents.main", keys, "openKey", "o")
	aside := f.asideLabelled(t, workspace, "Aside · Other")
	f.forkOf(t, workspace, aside)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		items := nodesWithID(n, parent.ID+"/open/oi-1")
		if len(items) != 1 {
			return false
		}
		other, _ := items[0].Props["other"].(map[string]any)
		mark, _ := other["aside"].(map[string]any)
		return mark["id"] == aside
	})

	f.h.Event(t, workspace, "agents.aside", aside+"/sendBackButton", "sendBackOpen", nil)
	form := aside + "/sendBack/form"
	f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool { return panelText(n, form) != "" })
	f.h.Event(t, workspace, "agents.aside", form+"/send", "sendBackSend", map[string]any{form + "/to": "answer", form + "/text": "only for the sheet", form + "/close": true})
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		items := nodesWithID(n, parent.ID+"/open/oi-1")
		return len(items) == 1 && otherText(items[0]) == "only for the sheet"
	})
}

func TestSendBackFromSwitchRefillsInEitherOrder(t *testing.T) {
	t.Parallel()
	f := start(t, summaryScript)
	workspace := f.workspace(t, newRepo(t))
	_, _, aside := f.askBeside(t, workspace)
	f.prompt(t, workspace, aside, "why not?")
	f.waitStatus(t, workspace, aside, "done")
	f.h.Event(t, workspace, "agents.aside", aside+"/sendBackButton", "sendBackOpen", nil)
	form := aside + "/sendBack/form"
	summary := panelText(f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool {
		return strings.HasPrefix(panelText(n, form), summaryText)
	}), form)
	values := func(from, text string) map[string]any {
		return map[string]any{form + "/to": "answer", form + "/from": from, form + "/text": text, form + "/close": true}
	}

	f.h.Event(t, workspace, "agents.aside", form, "sendBackDraft", values("last", summary))
	f.h.Event(t, workspace, "agents.aside", form+"/from", "sendBackFrom", values("last", summary))
	last := panelText(f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool {
		return strings.HasPrefix(panelText(n, form), "You are an aside")
	}), form)

	f.h.Event(t, workspace, "agents.aside", form+"/from", "sendBackFrom", values("summary", last))
	f.h.Event(t, workspace, "agents.aside", form, "sendBackDraft", values("summary", last))
	f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool { return panelText(n, form) == summary })
}

func TestSendBackOpenedWhileRunningThenSummary(t *testing.T) {
	t.Parallel()
	gate := filepath.Join(t.TempDir(), "go")
	f := start(t, asideScript(
		map[string]any{"match": "pick", "elicitation": askScript(false)},
		map[string]any{"match": "slow", "wait": gate},
		map[string]any{"match": "Summarize", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": summaryText}}},
	))
	workspace := f.workspace(t, newRepo(t))
	_, _, aside := f.askBeside(t, workspace)
	f.prompt(t, workspace, aside, "first")
	f.waitStatus(t, workspace, aside, "done")
	f.prompt(t, workspace, aside, "slow one")
	f.waitStatus(t, workspace, aside, "running")

	f.h.Event(t, workspace, "agents.aside", aside+"/sendBackButton", "sendBackOpen", nil)
	form := aside + "/sendBack/form"
	open := f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool { return panelText(n, form) != "" })
	assert.True(t, optionOn(nodesWithID(open, form+"/from")[0], "last"))
	write(t, gate, "")
	f.waitStatus(t, workspace, aside, "done")

	current := panelText(open, form)
	f.h.Event(t, workspace, "agents.aside", form+"/from", "sendBackFrom", map[string]any{form + "/to": "answer", form + "/from": "summary", form + "/text": current, form + "/close": true})
	f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool { return strings.HasPrefix(panelText(n, form), summaryText) })
}

func TestSendBackFallsBackWhenNoSummaryComes(t *testing.T) {
	t.Parallel()
	script := asideScript(map[string]any{"match": "pick", "elicitation": askScript(false)})
	script["prompt"] = []map[string]any{{"match": "pick", "elicitation": askScript(false)}, {"match": "aside forked", "echo": true}}
	f := start(t, script)
	workspace := f.workspace(t, newRepo(t))
	_, _, aside := f.askBeside(t, workspace)
	f.prompt(t, workspace, aside, "why not?")
	f.waitStatus(t, workspace, aside, "done")

	f.h.Event(t, workspace, "agents.aside", aside+"/sendBackButton", "sendBackOpen", nil)
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), "Summarize what we found") })
	form := aside + "/sendBack/form"
	panel := f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool {
		return strings.HasPrefix(panelText(n, form), "You are an aside")
	})
	assert.True(t, optionOn(nodesWithID(panel, form+"/from")[0], "last"))
}

func TestAClickInTheQueueDisarms(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	workspace := f.workspace(t, newRepo(t))
	parent := f.promptedParent(t, workspace)
	f.postItems(t, workspace, parent.ID, decision("Lists"), decision("Grouping"))
	f.h.Event(t, workspace, "agents.sidebar", "filter", "filter", "open")
	armed := func(n sdui.Node) bool {
		k := nodesWithID(n, "openQueue")
		return len(k) == 1 && k[0].Props["hints"] == "1–4 ask aside about · ⎋ cancel"
	}
	f.h.WaitSlot(t, workspace, "agents.main", shows("openQueue"))
	f.h.Event(t, workspace, "agents.main", "openQueue", "openKey", "a")
	f.h.WaitSlot(t, workspace, "agents.main", armed)
	f.h.Event(t, workspace, "agents.main", parent.ID+"/open", "openMove", parent.ID+"/open/oi-2")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(nodesWithID(n, "openQueue")) == 1 && !armed(n) })
}

func TestSendBackTextEditsAreNeverSwallowed(t *testing.T) {
	t.Parallel()
	f := start(t, summaryScript)
	workspace := f.workspace(t, newRepo(t))
	_, _, aside := f.askBeside(t, workspace)
	f.prompt(t, workspace, aside, "why not?")
	f.waitStatus(t, workspace, aside, "done")
	f.h.Event(t, workspace, "agents.aside", aside+"/sendBackButton", "sendBackOpen", nil)
	form := aside + "/sendBack/form"
	summary := panelText(f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool {
		return strings.HasPrefix(panelText(n, form), summaryText)
	}), form)
	values := func(from, text string) map[string]any {
		return map[string]any{form + "/to": "answer", form + "/from": from, form + "/text": text, form + "/close": true}
	}

	f.h.Event(t, workspace, "agents.aside", form, "sendBackDraft", values("summary", ""))
	cleared := f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool {
		send := nodesWithID(n, form+"/send")
		return len(send) == 1 && send[0].Props["disabled"] == true
	})
	assert.Empty(t, panelText(cleared, form))

	f.h.Event(t, workspace, "agents.aside", form, "sendBackDraft", values("summary", summary))
	f.h.Event(t, workspace, "agents.aside", form, "sendBackDraft", values("last", summary))
	f.h.Event(t, workspace, "agents.aside", form+"/from", "sendBackFrom", values("last", summary))
	f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool {
		return strings.HasPrefix(panelText(n, form), "You are an aside")
	})
	f.h.Event(t, workspace, "agents.aside", form, "sendBackDraft", values("last", summary))
	f.h.WaitSlot(t, workspace, "agents.aside", func(n sdui.Node) bool { return panelText(n, form) == summary })
}
